#!/usr/bin/env python3
"""Install and exercise the integrated Elastic review plane; never route traffic."""
import argparse
import datetime
import hashlib
import json
import math
from pathlib import Path
import re
import sys

import yaml

from kibana_client import Client, ROOT

WORKFLOW_ID = 'sloproof-loop'
WORKFLOW = ROOT / 'elastic/workflows/sloproof-loop.yaml'


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def save(path, value, *, exclusive=False):
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open('x' if exclusive else 'w') as stream:
        json.dump(value, stream, indent=2)
        stream.write('\n')


def read_definitions():
    tools = [json.loads(p.read_text()) for p in sorted((ROOT / 'elastic/tools').glob('*.json'))]
    agents = [json.loads(p.read_text()) for p in sorted((ROOT / 'elastic/agents').glob('*.json'))]
    workflow = yaml.safe_load(WORKFLOW.read_text())
    return tools, agents, workflow


def require(condition, message):
    if not condition:
        raise ValueError(message)


def validate(tools, agents, workflow):
    """Fail before deployment if these assets acquire an execution/write path."""
    allowed_tools = {'sloproof_window_metrics', 'sloproof_evidence_completeness', 'sloproof_recorded_verdict'}
    require({t['id'] for t in tools} == allowed_tools, 'Unexpected tool set')
    for tool in tools:
        require(tool['type'] == 'esql', 'Only saved ES|QL tools are allowed')
        query = tool['configuration']['query']
        if tool['id'] == 'sloproof_recorded_verdict':
            require('event == "verdict"' in query and 'actor == "deterministic-verifier"' in query and 'run_id == ?run_id' in query, 'Recorded decision tool lost provenance filters')
        else:
            source = {'sloproof_window_metrics': 'window-metrics', 'sloproof_evidence_completeness': 'evidence-completeness'}[tool['id']]
            require(query == (ROOT / 'elastic/queries' / (source + '.esql')).read_text(), 'Tool diverges from canonical bounded query')
            require(set(tool['configuration']['params']) == {'run_id', 'window_start', 'window_end'}, 'Explicit time bounds are required')
    agent_ids = {a['id'] for a in agents}
    require(agent_ids == {'sloproof_investigator', 'sloproof_verifier', 'sloproof_briefer'}, 'Unexpected agent set')
    for agent in agents:
        cfg = agent['configuration']
        require(cfg.get('enable_elastic_capabilities') is False, 'Built-in capabilities must be disabled')
        require(set(cfg) == {'instructions', 'tools', 'enable_elastic_capabilities'}, 'Unexpected agent capability configuration')
        used = {t for group in cfg['tools'] for t in group['tool_ids']}
        require(bool(used) and used <= allowed_tools, 'Agent has an unreviewed tool')
    require([t['type'] for t in workflow['triggers']] == ['manual'], 'Review must remain manually triggered')
    require(workflow['triggers'][0].get('inputs') == [{'name': 'run_id', 'type': 'string', 'required': True}], 'Workflow must accept only a run ID')
    fetch = workflow['steps'][0]
    require(fetch['name'] == 'read_verdict' and fetch['type'] == 'elasticsearch.search', 'First step must retrieve the recorded decision')
    expected_filters = [{'term': {'run_id': '{{ inputs.run_id }}'}}, {'term': {'event': 'verdict'}}, {'term': {'actor': 'deterministic-verifier'}}]
    require(fetch['with']['index'] == 'sloproof-ledger' and fetch['with']['query'] == {'bool': {'filter': expected_filters}}, 'Decision lookup lost its provenance filters')
    gate = workflow['steps'][1]
    require(gate['name'] == 'require_recorded_verdict' and gate['type'] == 'if' and gate['condition'] == 'steps.read_verdict.output.hits.total.value: 1', 'Exactly one recorded decision is required')
    def inspect(steps, *, gated=False):
        for step in steps:
            require(step['type'] in {'elasticsearch.search', 'if', 'ai.agent', 'console'}, 'Workflow contains an execution or write step')
            require('connector-id' not in step and 'on-failure' not in step, 'Review must not call connectors or bypass failed reads')
            if step['type'] == 'ai.agent':
                require(gated and step['agent-id'] in agent_ids, 'Ungated or unknown agent')
            inspect(step.get('steps', []), gated=gated or step['name'] == 'require_recorded_verdict')
            inspect(step.get('else', []), gated=gated)
    inspect(workflow['steps'])


def definitions():
    result = read_definitions()
    validate(*result)
    return result


def install(client):
    tools, agents, _ = definitions()
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    backup = ROOT / '.local-scratch/elastic-integration' / stamp
    current = client.get_optional('/api/workflows/workflow/' + WORKFLOW_ID)
    if current:
        save(backup / 'workflow.json', current, exclusive=True)
        # The previous prototype could mutate routing; stop it before repurposing
        # the legacy agent IDs. On a partial install it stays disabled.
        client.request('PUT', '/api/workflows/workflow/' + WORKFLOW_ID, {'enabled': False})
    applied = []
    for kind, items in [('tools', tools), ('agents', agents)]:
        for item in items:
            path = '/api/agent_builder/' + kind + '/' + item['id']
            old = client.get_optional(path)
            if old:
                save(backup / kind / (item['id'] + '.json'), old, exclusive=True)
                body = {k: v for k, v in item.items() if k not in {'id', 'type'}}
                client.request('PUT', path, body)
            else:
                # Agent exports include type=chat, but the create API derives it.
                body = {k: v for k, v in item.items() if kind != 'agents' or k != 'type'}
                client.request('POST', '/api/agent_builder/' + kind, body)
            applied.append({'kind': kind, 'id': item['id'], 'operation': 'updated' if old else 'created'})
    if current:
        response = client.request('PUT', '/api/workflows/workflow/' + WORKFLOW_ID, {'yaml': WORKFLOW.read_text()})
    else:
        response = client.request('POST', '/api/workflows/workflow', {'id': WORKFLOW_ID, 'yaml': WORKFLOW.read_text()})
    require(response.get('valid') is True, 'Workflow did not validate; inspect the saved definition in Kibana')
    live = client.request('GET', '/api/workflows/workflow/' + WORKFLOW_ID)
    require(live.get('valid') and live.get('enabled'), 'Workflow is not ready')
    require(yaml.safe_load(live['yaml']) == yaml.safe_load(WORKFLOW.read_text()), 'Remote workflow differs from reviewed source')
    receipt = {'installed_at': now(), 'workflow_id': WORKFLOW_ID, 'workflow_valid': True, 'manual_only': True,
               'workflow_sha256': hashlib.sha256(WORKFLOW.read_bytes()).hexdigest(), 'assets': applied,
               'routing_or_notification_connectors_called': False,
               'credential_scope': 'Kibana caller identity; read-only tools and steps, not a separately scoped verifier credential'}
    save(ROOT / 'evidence/elastic-integration-install.json', receipt)
    print('Installed 3 agents, 3 read-only tools, and the manual review workflow; no routing or notification actions')


def validate_run(run_id):
    require(re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]{0,127}', run_id) is not None, 'Invalid run ID')


def recorded_decision(client, run_id):
    validate_run(run_id)
    result = client.request('POST', '/sloproof-ledger/_search', {
        'size': 2, 'track_total_hits': True,
        'query': {'bool': {'filter': [{'term': {'run_id': run_id}}, {'term': {'event': 'verdict'}}, {'term': {'actor': 'deterministic-verifier'}}]}}}, es=True)
    hits = result['hits']
    require(hits['total']['value'] == 1 and len(hits['hits']) == 1, 'Expected exactly one recorded deterministic verdict; review is blocked')
    hit = hits['hits'][0]
    source = hit['_source']
    require(source['verdict'] in {'PROVED', 'FAILED', 'INCONCLUSIVE'}, 'Invalid recorded verdict')
    require(source['detail']['run_id'] == run_id and source['detail']['verdict'] == source['verdict'] and source['detail']['contract_hash'] == source['contract_hash'], 'Inconsistent recorded decision')
    local = json.loads((ROOT / 'evidence' / (run_id + '-verdict.json')).read_text())
    require(source['detail'] == local, 'Local and remote verifier artifacts differ; stop for review')
    return hit['_id'], local


def check(client, run_id):
    definitions()
    doc_id, verdict = recorded_decision(client, run_id)
    params = {'run_id': run_id}
    checks = []
    for tool in read_definitions()[0]:
        if tool['id'] != 'sloproof_recorded_verdict':
            require(bool(verdict.get('windows')), 'This tool check needs a recorded observation window')
            first = verdict['windows'][0]
            tool_params = {**params, 'window_start': first['window_start'], 'window_end': first['window_end']}
        else:
            tool_params = params
        result = client.request('POST', '/api/agent_builder/tools/_execute', {'tool_id': tool['id'], 'tool_params': tool_params})
        rows = tool_rows(result)
        if tool['id'] == 'sloproof_recorded_verdict':
            require(len(rows) == 1 and rows[0]['run_id'] == run_id and rows[0]['verdict'] == verdict['verdict'] and rows[0]['contract_hash'] == verdict['contract_hash'], 'Tool returned a different recorded decision')
        else:
            candidate = [row for row in rows if row['version'] == 'v1']
            require(len(candidate) == 1 and candidate[0]['requests'] == first['requests'], 'Tool candidate cohort does not match the recorded window')
            if tool['id'] == 'sloproof_window_metrics':
                require(candidate[0]['success_rate'] == first['success_rate'] and math.isclose(candidate[0]['p95_ms'], first['p95_ms'], rel_tol=0.001), 'Tool metrics differ from recorded evidence')
            else:
                require(candidate[0]['config_hashes'] == 1 and candidate[0]['meets_min_samples'] is True, 'Candidate evidence is incomplete')
        checks.append({'tool_id': tool['id'], 'params': tool_params, 'response': result})
    save(ROOT / 'evidence' / (run_id + '-elastic-tools.json'), {'checked_at': now(), 'run_id': run_id, 'source_document_id': doc_id, 'checks': checks})
    print('Saved all three live tool responses for', run_id)


def tool_rows(response):
    results = response.get('results', [])
    require(not any(r.get('type') == 'error' for r in results), 'Agent Builder tool returned an error')
    tables = [r['data'] for r in results if r.get('type') == 'esql_results']
    require(len(tables) == 1, 'Expected one ES|QL result table')
    table = tables[0]
    names = [c['name'] for c in table['columns']]
    require(bool(table['values']), 'Tool returned no evidence')
    return [dict(zip(names, row)) for row in table['values']]


def review_path(run_id, attempt):
    validate_run(run_id)
    require(attempt >= 1, 'Attempt must be positive')
    suffix = '' if attempt == 1 else '-attempt-' + str(attempt)
    return ROOT / 'evidence' / (run_id + suffix + '-elastic-review.json')


def run(client, run_id, attempt):
    definitions()
    path = review_path(run_id, attempt)
    require(not path.exists(), 'Review artifact already exists; use collect or a new --attempt')
    doc_id, verdict = recorded_decision(client, run_id)
    live = client.request('GET', '/api/workflows/workflow/' + WORKFLOW_ID)
    require(live.get('valid') and live.get('enabled') and yaml.safe_load(live['yaml']) == yaml.safe_load(WORKFLOW.read_text()), 'Remote workflow is disabled, invalid, or differs from reviewed source')
    started = client.request('POST', '/api/workflows/workflow/' + WORKFLOW_ID + '/run', {'inputs': {'run_id': run_id}})
    artifact = {'run_id': run_id, 'attempt': attempt, 'started_at': now(), 'workflow_id': WORKFLOW_ID,
                'execution_id': started['workflowExecutionId'], 'source_document_id': doc_id,
                'recorded_verdict': verdict['verdict'], 'contract_hash': verdict['contract_hash'],
                'scope': 'Post-verification review; AI does not decide or route traffic', 'status': 'started',
                'workflow_sha256': hashlib.sha256(WORKFLOW.read_bytes()).hexdigest()}
    save(path, artifact, exclusive=True)
    print(json.dumps({'run_id': run_id, 'execution_id': artifact['execution_id'], 'artifact': str(path.relative_to(ROOT))}))


def collect(client, run_id, attempt):
    path = review_path(run_id, attempt)
    artifact = json.loads(path.read_text())
    execution = client.request('GET', '/api/workflows/executions/' + artifact['execution_id'] + '?includeInput=true&includeOutput=true')
    inputs = execution.get('input') or execution.get('context', {}).get('inputs', {})
    require(execution['workflowId'] == WORKFLOW_ID and inputs.get('run_id') == run_id, 'Execution identity mismatch')
    require(hashlib.sha256(execution['yaml'].encode()).hexdigest() == artifact['workflow_sha256'], 'Execution used a different workflow definition')
    doc_id, verdict = recorded_decision(client, run_id)
    require(doc_id == artifact['source_document_id'] and verdict['verdict'] == artifact['recorded_verdict'], 'Source decision changed during review')
    # Keep the run inputs, definition and step evidence, not host/user context.
    execution['context'] = {'inputs': inputs}
    for field in ('executedBy', 'spaceId', 'traceId', 'entryTransactionId'):
        execution.pop(field, None)
    artifact.update({'collected_at': now(), 'status': execution['status'], 'execution': execution})
    save(path, artifact)
    print(json.dumps({'run_id': run_id, 'status': execution['status'], 'steps': [{'name': s.get('stepId'), 'status': s['status']} for s in execution.get('stepExecutions', [])]}))
    return 0 if execution['status'] == 'completed' else 2


def ask(client, run_id, question, attempt):
    _, verdict = recorded_decision(client, run_id)
    require(bool(verdict.get('windows')), 'A bounded question needs a recorded observation window')
    first = verdict['windows'][0]
    frozen = client.request('GET', '/sloproof-contracts/_doc/' + run_id, es=True)['_source']
    require(frozen['run_id'] == run_id and frozen['contract_hash'] == verdict['contract_hash'], 'Frozen contract does not match the recorded decision')
    require(hashlib.sha256(frozen['contract_source'].encode()).hexdigest() == frozen['contract_hash'], 'Frozen contract source hash mismatch')
    require(json.loads(frozen['contract_source']) == frozen['contract'], 'Frozen contract body differs from its source')
    contract_context = {k: frozen['contract'][k] for k in ('criteria', 'observation', 'eligibility')}
    prompt = (f'Run {run_id}. Use window_start {first["window_start"]} inclusive and '
              f'window_end {first["window_end"]} exclusive. The following contract was retrieved '
              f'from the frozen record and its source SHA256 checked against the recorded '
              f'decision ({frozen["contract_hash"]}): {json.dumps(contract_context)}. '
              'Contract semantics: success_rate must be at least the minimum; p95 must '
              'be STRICTLY LESS THAN p95_latency_ms_max. Equality fails; never say <=. '
              'The completeness tool checks counts and configuration identities only, not '
              'time coverage, all integrity checks or full-run reconciliation. Do not '
              'claim it establishes full coverage or trustworthy/complete telemetry. '
              'Distinguish the existing three-window verdict from your one-window '
              f'comparison. Answer in at most 140 words and explicitly say p95 < {frozen["contract"]["criteria"]["p95_latency_ms_max"]} ms '
              'when discussing this contract. '
              + question)
    path = ROOT / 'evidence' / (run_id + '-elastic-answer-' + str(attempt) + '.json')
    require(attempt >= 1 and not path.exists(), 'Answer already exists or invalid attempt')
    response = client.request('POST', '/api/agent_builder/converse', {'agent_id': 'sloproof_briefer', 'input': prompt}, timeout=180)
    called = {s.get('tool_id') for s in response.get('steps', []) if s.get('type') == 'tool_call'}
    supported = {'sloproof_recorded_verdict', 'sloproof_window_metrics', 'sloproof_evidence_completeness'} <= called
    save(path, {'run_id': run_id, 'recorded_at': now(), 'agent_id': 'sloproof_briefer', 'input': prompt,
                'question': question, 'frozen_contract_context': contract_context, 'required_tools_called': supported,
                'response': response, 'scope': 'Advisory answer; source verdict unchanged'}, exclusive=True)
    require(supported, 'Answer retained but unsupported: required tool calls are missing')
    print('Saved Agent Builder answer to', path.relative_to(ROOT))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    sub.add_parser('validate')
    sub.add_parser('install')
    for command in ('check', 'run', 'collect', 'ask'):
        p = sub.add_parser(command)
        p.add_argument('run_id')
        if command != 'check':
            p.add_argument('--attempt', type=int, default=1)
        if command == 'ask':
            p.add_argument('question')
    args = parser.parse_args()
    definitions()
    if args.command == 'validate':
        print('Definitions validated: bounded queries, recorded-decision gate, and no write/routing/notification steps')
        return 0
    client = Client()
    if args.command == 'install':
        install(client)
    elif args.command == 'check':
        check(client, args.run_id)
    elif args.command == 'run':
        run(client, args.run_id, args.attempt)
    elif args.command == 'collect':
        return collect(client, args.run_id, args.attempt)
    else:
        ask(client, args.run_id, args.question, args.attempt)
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, RuntimeError) as exc:
        sys.exit(str(exc))
