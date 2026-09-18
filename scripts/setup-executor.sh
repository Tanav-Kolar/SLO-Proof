#!/usr/bin/env bash
# Operator bootstraps a short-lived, route-only executor identity.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/_guard.sh
require_context
kubectl apply -f - <<'YAML'
apiVersion: v1
kind: ServiceAccount
metadata:
  name: sloproof-executor
  namespace: checkout
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: sloproof-executor
  namespace: checkout
rules:
  - apiGroups: [gateway.networking.k8s.io]
    resources: [httproutes]
    resourceNames: [checkout-route]
    verbs: [get, patch]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: sloproof-executor
  namespace: checkout
subjects:
  - kind: ServiceAccount
    name: sloproof-executor
    namespace: checkout
roleRef:
  kind: Role
  name: sloproof-executor
  apiGroup: rbac.authorization.k8s.io
YAML
# Neither token nor kubeconfig is printed; both are confined to ignored .kube/.
python3 - <<'PY'
import json,os,subprocess,pathlib
cfg=json.loads(subprocess.check_output(['kubectl','config','view','--minify','--raw','-o','json']))
token=subprocess.check_output(['kubectl','-n','checkout','create','token','sloproof-executor','--duration=1h'],text=True).strip()
cluster=cfg['clusters'][0];cluster['name']='sloproof'
out={'apiVersion':'v1','kind':'Config','clusters':[cluster],'users':[{'name':'executor','user':{'token':token}}],'contexts':[{'name':'sloproof-executor','context':{'cluster':'sloproof','user':'executor','namespace':'checkout'}}],'current-context':'sloproof-executor'}
p=pathlib.Path('.kube/executor.config');fd=os.open(p,os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o600)
with os.fdopen(fd,'w') as f:json.dump(out,f)
os.chmod(p,0o600)
print('Created .kube/executor.config (one-hour requested lifetime, route-only RBAC).')
PY
