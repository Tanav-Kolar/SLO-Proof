#!/usr/bin/env python3
"""Operator-only /version snapshots using the explicitly pinned kubeconfig."""
import json,os,pathlib,re,subprocess,sys,time,urllib.request
root=pathlib.Path(__file__).resolve().parent.parent
expected=root/'.kube/sloproof.config'
if pathlib.Path(os.environ.get('KUBECONFIG','')).resolve()!=expected.resolve():
 sys.exit('REFUSING: snapshot requires the repository-pinned operator kubeconfig')
context=subprocess.check_output(['kubectl','config','current-context'],text=True).strip()
if not os.environ.get('SLOPROOF_KUBE_CONTEXT') or context!=os.environ['SLOPROOF_KUBE_CONTEXT']:
 sys.exit('REFUSING: context mismatch')
run,profile=sys.argv[1:]
if not re.fullmatch('[a-z0-9][a-z0-9-]{0,44}',run):sys.exit('invalid run ID')
pods=json.loads(subprocess.check_output(['kubectl','-n','checkout','get','pods','-l','app=checkout,version=v1','-o','json']))['items']
ready=[p for p in pods if not p['metadata'].get('deletionTimestamp') and any(c['type']=='Ready' and c['status']=='True' for c in p.get('status',{}).get('conditions',[]))]
if not ready:sys.exit('No ready candidate pods')
snapshots=[]
for pod in ready:
 name=pod['metadata']['name']
 # 0 chooses a free local port. Parse the port-forward readiness line.
 proc=subprocess.Popen(['kubectl','-n','checkout','port-forward','pod/'+name,':8080','--address=127.0.0.1'],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
 try:
  import selectors
  selector=selectors.DefaultSelector();selector.register(proc.stdout,selectors.EVENT_READ)
  deadline=time.monotonic()+20;port=None
  while time.monotonic()<deadline:
   if not selector.select(timeout=1):continue
   line=proc.stdout.readline()
   match=re.search(r'Forwarding from 127\.0\.0\.1:(\d+) -> 8080',line)
   if match:port=match.group(1);break
   if proc.poll() is not None:break
  if port is None:raise RuntimeError('port-forward did not become ready')
  with urllib.request.urlopen('http://127.0.0.1:'+port+'/version',timeout=10) as r:version=json.load(r)
  if version['version']!='v1' or version['fault_profile']!=profile:raise RuntimeError('unexpected live candidate profile')
  snapshots.append({'pod':name,'version_response':version})
 finally:
  proc.terminate()
  try:proc.wait(timeout=5)
  except subprocess.TimeoutExpired:proc.kill();proc.wait()
if len({p['version_response']['config_hash'] for p in snapshots})!=1:sys.exit('Mixed candidate configurations')
doc={'run_id':run,'config_hash':snapshots[0]['version_response']['config_hash'],'pods':snapshots}
(root/'evidence'/f'{run}-candidate.json').write_text(json.dumps(doc,indent=2)+'\n')
print('Captured matching /version responses from',len(snapshots),'candidate pods')
