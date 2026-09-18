"""Boundary regression tests for the Elastic review integration; no live APIs."""
import copy
import unittest

from manage import definitions, validate, validate_run, tool_rows


class IntegrationBoundaryTests(unittest.TestCase):
    def setUp(self):
        self.tools, self.agents, self.workflow = copy.deepcopy(definitions())

    def rejects(self):
        with self.assertRaises(ValueError):
            validate(self.tools, self.agents, self.workflow)

    def test_canonical_definitions(self):
        validate(self.tools, self.agents, self.workflow)

    def test_unbounded_query_rejected(self):
        self.tools[-1]['configuration']['query'] += '\n| LIMIT 1'
        self.rejects()

    def test_builtin_agent_capabilities_rejected(self):
        self.agents[0]['configuration']['enable_elastic_capabilities'] = True
        self.rejects()

    def test_extra_agent_tool_rejected(self):
        self.agents[0]['configuration']['tools'][0]['tool_ids'].append('platform.core.execute')
        self.rejects()

    def test_agent_preexecution_workflow_rejected(self):
        self.agents[0]['configuration']['workflow_ids'] = ['unreviewed-workflow']
        self.rejects()

    def test_missing_decision_gate_rejected(self):
        self.workflow['steps'][1]['condition'] = 'steps.read_verdict.output.hits.total.value >= 0'
        self.rejects()

    def test_ungated_agent_rejected(self):
        self.workflow['steps'].append({'name': 'guess', 'type': 'ai.agent', 'agent-id': 'sloproof_verifier'})
        self.rejects()

    def test_write_and_external_action_steps_rejected(self):
        for kind in ('elasticsearch.index', 'http', 'slack_api.postMessage'):
            with self.subTest(kind=kind):
                self.workflow['steps'].append({'name': 'write_or_act', 'type': kind})
                self.rejects()
                self.workflow['steps'].pop()

    def test_automatic_trigger_rejected(self):
        self.workflow['triggers'].append({'type': 'alerting.ruleEventsGenerated'})
        self.rejects()

    def test_wrong_actor_rejected(self):
        self.workflow['steps'][0]['with']['query']['bool']['filter'][-1]['term']['actor'] = 'agent'
        self.rejects()

    def test_run_id_injection_rejected(self):
        for value in ('../secrets', '', 'run" OR true', 'a' * 129):
            with self.subTest(value=value), self.assertRaises(ValueError):
                validate_run(value)

    def test_http_200_tool_error_is_not_success(self):
        with self.assertRaises(ValueError):
            tool_rows({'results': [{'type': 'error', 'data': {'message': 'query failed'}}]})

    def test_empty_tool_table_is_not_success(self):
        with self.assertRaises(ValueError):
            tool_rows({'results': [{'type': 'esql_results', 'data': {'columns': [], 'values': []}}]})


if __name__ == '__main__':
    unittest.main()
