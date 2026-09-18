#!/usr/bin/env python3
"""Bounded read-only evidence retries; never repeat a routing action or shift windows."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]


def main():
    run = sys.argv[1]
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]{0,127}', run):
        raise SystemExit('Invalid run ID')
    final = ROOT / 'evidence' / (run + '-verdict.json')
    audit = ROOT / 'evidence' / (run + '-verification-retries.json')
    if final.exists() or audit.exists():
        raise SystemExit('Refusing to replace a recorded verdict or retry history')
    env = {key: os.environ[key] for key in ('PATH', 'ELASTIC_CLOUD_ENDPOINT', 'ELASTIC_VERIFIER_API_KEY')}
    attempts = []
    deadline = time.monotonic() + 120
    for attempt in range(1, 4):
        path = ROOT / 'evidence' / f'{run}-verification-attempt-{attempt}.json'
        with path.open('x') as output:
            result = subprocess.run([str(ROOT / 'build/verifier'), '--run-id', run,
                '--summary', str(ROOT / 'evidence' / (run + '-summary.json'))],
                cwd=ROOT, env=env, stdout=output, timeout=min(45, max(1, deadline - time.monotonic())))
        if result.returncode not in (0, 2):
            raise SystemExit('Verifier process failed; routing remains held')
        verdict = json.loads(path.read_text())
        if verdict.get('run_id') != run:
            raise SystemExit('Verifier run identity mismatch')
        classification = subprocess.run([str(ROOT / 'build/rollout-policy'), '--input', str(path), '--retry-only'],
            cwd=ROOT, env={'PATH': env['PATH']}, capture_output=True, text=True, timeout=min(5, max(0.1, deadline - time.monotonic())))
        retry = json.loads(classification.stdout)['evidence_retry'] if classification.returncode == 0 else {'allowed': False}
        attempts.append({'attempt': attempt, 'artifact_source': str(path.relative_to(ROOT)),
                         'verdict': verdict['verdict'], 'reason': verdict['reason'], 'retry_policy': retry})
        # A conclusive result ends immediately. Max two additional attempts,
        # separated by 30 seconds, even if malformed policy claims otherwise.
        exhausted = attempt == 3 or time.monotonic() + 35 >= deadline
        if verdict['verdict'] != 'INCONCLUSIVE' or not retry.get('allowed') or exhausted:
            with final.open('x') as output:
                json.dump(verdict, output, indent=2); output.write('\n')
            with audit.open('x') as output:
                json.dump({'run_id': run, 'attempts': attempts,
                    'final_verdict': verdict['verdict'], 'retry_budget_exhausted': exhausted and verdict['verdict'] == 'INCONCLUSIVE',
                    'maximum_elapsed_seconds': 120,
                    'same_frozen_windows': True, 'routing_actions': 0}, output, indent=2)
                output.write('\n')
            return result.returncode
        print(f'Evidence delay: holding routing; rechecking the same frozen windows in 30 seconds ({attempt}/2).', file=sys.stderr, flush=True)
        time.sleep(30)
    raise AssertionError('unreachable')


if __name__ == '__main__':
    sys.exit(main())
