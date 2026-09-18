#!/usr/bin/env python3
"""Produce a portable, read-only demo from saved measurements and verifier output.

No network calls, credentials, routing actions, fabricated windows or verdicts.
"""
import datetime,hashlib,json,pathlib
root=pathlib.Path(__file__).resolve().parent.parent
evidence=root/'evidence'
contract=json.loads((root/'contracts/checkout-slo.json').read_text())
measurements=[]
metrics=evidence/'d9-window-metrics.txt'
summary=evidence/'d9-healthy-summary.json'
if metrics.exists() and summary.exists():
 s=json.loads(summary.read_text())
 for line in metrics.read_text().splitlines():
  fields=line.split()
  if len(fields)==6 and fields[0]=='v1':
   measurements.append({'run_id':s['run_id'],'version':'v1','requests':int(fields[1]),'success_rate':float(fields[3]),'p95_ms':float(fields[5]),'completed':s['completed'],'indexed_ok':s['indexed_ok'],'dropped':s['dropped_arrivals'],'reconciled':s['reconciled'],'kind':'Measured latency check · setup credential used','source':'evidence/d9-window-metrics.txt','query':(root/'elastic/queries/window-metrics.esql').read_text(),'params':{'run_id':s['run_id'],'window_start':'2026-09-18T07:00:00Z','window_end':'2026-09-18T08:00:00Z'}})
verdicts=[]
for path in sorted(evidence.glob('*-verdict.json')):
 try:v=json.loads(path.read_text())
 except (ValueError,OSError):continue
 if v.get('verdict') not in ('PROVED','FAILED','INCONCLUSIVE') or not v.get('run_id'):continue
 # Preserve the complete source, including queries, receipts and missing fields.
 v['artifact_source']='evidence/'+path.name
 verdicts.append(v)
verdicts.sort(key=lambda v:({'FAILED':0,'PROVED':1,'INCONCLUSIVE':2}[v['verdict']],v['run_id']))
negative=evidence/'verifier-unscoped-refusal.json'
checks=[]
if negative.exists():
 d=json.loads(negative.read_text());checks.append({'name':'Unrestricted verifier credential rejected','result':d['verdict'],'reason':d['reason'],'at':d['verified_at'],'scope':'Credential check only; no SLO verdict'})
agents=[]
briefings={}
scope_check=evidence/'key-scopes.json'
if scope_check.exists():
 d=json.loads(scope_check.read_text())
 checks.append({'name':'Scoped runtime credentials: '+('PASS' if d['passed'] else 'FAIL'),'reason':f"{sum(c['passed'] for c in d['checks'])}/{len(d['checks'])} authorization checks passed",'at':d['checked_at'],'scope':'Actual HTTP permission checks; source: evidence/key-scopes.json','detail':d})
for path in sorted(evidence.glob('*-agents.json')):
 d=json.loads(path.read_text())
 d['artifact_source']='evidence/'+path.name
 briefings.setdefault(d['run_id'],[]).append(d)
for run,attempts in sorted(briefings.items()):
 attempts.sort(key=lambda d:d.get('attempt',1))
 latest=attempts[-1]
 latest['earlier_briefings']=[d['artifact_source'] for d in attempts[:-1]]
 agents.append(latest)
elastic_reviews=[]
review_groups={}
for path in sorted(evidence.glob('*-elastic-review.json')):
 d=json.loads(path.read_text())
 d['artifact_source']='evidence/'+path.name
 review_groups.setdefault(d['run_id'],[]).append(d)
for run,attempts in sorted(review_groups.items()):
 attempts.sort(key=lambda d:d.get('attempt',1))
 latest=attempts[-1]
 latest['earlier_reviews']=[d['artifact_source'] for d in attempts[:-1]]
 elastic_reviews.append(latest)
rollout_policies=[]
for path in sorted(evidence.glob('*-rollout-policy.json')):
 d=json.loads(path.read_text())
 d['artifact_source']='evidence/'+path.name
 rollout_policies.append(d)
elastic_answer_groups={}
for path in sorted(evidence.glob('*-elastic-answer-*.json')):
 d=json.loads(path.read_text())
 d['artifact_source']='evidence/'+path.name
 d['attempt']=int(path.stem.rsplit('-',1)[1])
 elastic_answer_groups.setdefault(d['run_id'],[]).append(d)
elastic_answers=[]
for run,attempts in sorted(elastic_answer_groups.items()):
 attempts.sort(key=lambda d:d['attempt'])
 latest=attempts[-1]
 latest['earlier_answers']=[d['artifact_source'] for d in attempts[:-1]]
 elastic_answers.append(latest)
payload={'generated_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'contract':contract,'contract_hash':hashlib.sha256((root/'contracts/checkout-slo.json').read_bytes()).hexdigest(),'measurements':measurements,'verdicts':verdicts,'checks':checks,'agents':agents,'elastic_reviews':elastic_reviews,'elastic_answers':elastic_answers,'rollout_policies':rollout_policies}
# Escape HTML-significant characters before embedding JSON in a script node.
serialized=json.dumps(payload,ensure_ascii=True).replace('&','\\u0026').replace('<','\\u003c').replace('>','\\u003e')
for source,target in [('evidence-template.html','evidence.html'),('showcase-template.html','index.html'),('process-template.html','process.html')]:
 template=(root/'demo'/source).read_text()
 out=root/'demo'/target
 out.write_text(template.replace('__EVIDENCE_JSON__',serialized))
 print(out)
