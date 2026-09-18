#!/usr/bin/env bash
# Assert real HTTP authorization results. Missing keys or unexpected statuses
# fail the command; a printed list of unexamined statuses is not a proof.
set -euo pipefail
cd "$(dirname "$0")/.."
set -a; source .env; set +a
python3 - <<'PY'
import datetime,json,os,pathlib,sys,urllib.request,urllib.error,uuid
needed=['ELASTIC_CLOUD_ENDPOINT','ELASTIC_VERIFIER_API_KEY','ELASTIC_INGEST_API_KEY','ELASTIC_CONTROLLER_API_KEY']
missing=[k for k in needed if not os.environ.get(k)]
if missing:sys.exit('REFUSING: missing '+', '.join(missing))
base=os.environ['ELASTIC_CLOUD_ENDPOINT'];probe='scope-probe-'+uuid.uuid4().hex
now=datetime.datetime.now(datetime.timezone.utc).isoformat()
record={'run_id':probe,'request_id':probe,'traffic_class':'synthetic','service':'checkout','version':'probe','status':200,'outcome':'success','success':True,'duration_ms':1,'@timestamp':now,'started_at':now,'ended_at':now,'generator_id':'scope-probe','config_hash':'probe','target_host':'probe'}
ledger={'@timestamp':now,'run_id':probe,'event':'scope_probe','actor':'operator'}
checks=[
 ('verifier reads requests','VERIFIER','POST','/sloproof-requests/_count',{},200),
 ('verifier reads contracts','VERIFIER','POST','/sloproof-contracts/_count',{},200),
 ('verifier reads ledger','VERIFIER','POST','/sloproof-ledger/_count',{},200),
 ('verifier cannot write requests','VERIFIER','PUT','/sloproof-requests/_doc/'+probe,record,403),
 ('verifier cannot write contracts','VERIFIER','PUT','/sloproof-contracts/_doc/'+probe,{'run_id':probe},403),
 ('verifier cannot write ledger','VERIFIER','PUT','/sloproof-ledger/_doc/'+probe,ledger,403),
 ('ingest cannot write ledger','INGEST','PUT','/sloproof-ledger/_doc/'+probe,ledger,403),
 ('ingest writes requests','INGEST','PUT','/sloproof-requests/_doc/'+probe,record,201),
 ('controller cannot write requests','CONTROLLER','PUT','/sloproof-requests/_doc/'+probe+'-controller',record,403),
 ('controller reads request observations','CONTROLLER','POST','/sloproof-requests/_count',{},200),
 ('controller writes ledger','CONTROLLER','PUT','/sloproof-ledger/_doc/'+probe,ledger,201),
]
failed=False
results=[]
for label,key,method,path,body,expected in checks:
 req=urllib.request.Request(base+path,method=method,data=json.dumps(body).encode(),headers={'Authorization':'ApiKey '+os.environ['ELASTIC_'+key+'_API_KEY'],'Content-Type':'application/json'})
 try:
  with urllib.request.urlopen(req,timeout=30) as r:status=r.status
 except urllib.error.HTTPError as e:status=e.code
 print(label+': HTTP '+str(status)+' (expected '+str(expected)+')')
 results.append({'check':label,'actual_http_status':status,'expected_http_status':expected,'passed':status==expected})
 if status!=expected:failed=True
# Probe records are synthetic and identified by a unique run ID; retain them as
# inspectable authorization evidence, outside all trial runs and observations.
artifact=pathlib.Path('evidence/key-scopes.json')
artifact.parent.mkdir(exist_ok=True)
artifact.write_text(json.dumps({'checked_at':now,'probe_run_id':probe,'passed':not failed,'checks':results},indent=2)+'\n')
print('Saved sanitized permission results to '+str(artifact))
sys.exit(1 if failed else 0)
PY
