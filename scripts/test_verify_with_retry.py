"""Exercise bounded retry orchestration without cloud or routing credentials."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('retry_runner', Path(__file__).with_name('verify-with-retry.py'))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class RetryOrchestrationTests(unittest.TestCase):
    def execute(self, outcomes, retry_allowed=True):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / 'evidence').mkdir()
            calls, pauses = [], []
            clock = [0.0]
            original = iter(outcomes)
            def fake_run(command, **kwargs):
                calls.append(command)
                if command[0].endswith('/verifier'):
                    self.assertEqual(set(kwargs['env']), {'PATH','ELASTIC_CLOUD_ENDPOINT','ELASTIC_VERIFIER_API_KEY'})
                    outcome = next(original)
                    value = {'run_id':'unit-run','verdict':outcome,'reason':'HTTP 503' if outcome=='INCONCLUSIVE' else 'measured result',
                             'contract_hash':'same-contract','receipt':{'route_effective_at':'same-observed-anchor'},
                             'windows':[{'window_start':'same-start','window_end':'same-end'}]}
                    json.dump(value,kwargs['stdout'])
                    return subprocess.CompletedProcess(command, 0 if outcome=='PROVED' else 2)
                self.assertTrue(command[0].endswith('/rollout-policy'))
                return subprocess.CompletedProcess(command,0,json.dumps({'evidence_retry':{'allowed':retry_allowed}}),'')
            def sleep(seconds):
                pauses.append(seconds)
                clock[0] += seconds
            env = {'PATH':os.environ['PATH'],'ELASTIC_CLOUD_ENDPOINT':'https://unit.invalid','ELASTIC_VERIFIER_API_KEY':'unit-read-key'}
            with patch.object(runner,'ROOT',root), patch.object(runner.sys,'argv',['verify-with-retry.py','unit-run']), patch.dict(os.environ,env), patch.object(runner.subprocess,'run',side_effect=fake_run), patch.object(runner.time,'sleep',side_effect=sleep), patch.object(runner.time,'monotonic',side_effect=lambda:clock[0]):
                result=runner.main()
                final=json.loads((root/'evidence/unit-run-verdict.json').read_text())
                audit=json.loads((root/'evidence/unit-run-verification-retries.json').read_text())
                with self.assertRaises(SystemExit): runner.main()
            return result,final,audit,calls,pauses

    def test_temporary_delay_recovers_without_changing_anchor_or_executing_route(self):
        status,final,audit,calls,pauses=self.execute(['INCONCLUSIVE','PROVED'])
        self.assertEqual((status,final['verdict'],pauses),(0,'PROVED',[30]))
        self.assertEqual(final['receipt']['route_effective_at'],'same-observed-anchor')
        self.assertEqual(audit['routing_actions'],0)
        self.assertEqual(len(audit['attempts']),2)
        verifier_calls=[c for c in calls if c[0].endswith('/verifier')]
        self.assertEqual(verifier_calls[0],verifier_calls[1])

    def test_retry_budget_cannot_loop_forever(self):
        status,final,audit,_,pauses=self.execute(['INCONCLUSIVE']*3)
        self.assertEqual((status,final['verdict'],pauses),(2,'INCONCLUSIVE',[30,30]))
        self.assertTrue(audit['retry_budget_exhausted'])
        self.assertEqual(len(audit['attempts']),3)

    def test_failed_result_stops_even_if_retry_policy_says_yes(self):
        status,final,audit,_,pauses=self.execute(['FAILED'])
        self.assertEqual((status,final['verdict'],pauses),(2,'FAILED',[]))
        self.assertEqual(len(audit['attempts']),1)

    def test_unrepairable_evidence_is_not_retried(self):
        status,_,audit,_,pauses=self.execute(['INCONCLUSIVE'],retry_allowed=False)
        self.assertEqual((status,len(audit['attempts']),pauses),(2,1,[]))


if __name__ == '__main__': unittest.main()
