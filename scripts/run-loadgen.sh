#!/usr/bin/env bash
# Caller exports credentials; this script never reads AWS credential files.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/_guard.sh
require_context
require_env LOADGEN_IMAGE RUN_ID ELASTIC_CLOUD_ENDPOINT ELASTIC_INGEST_API_KEY
[[ "$LOADGEN_IMAGE" == *@sha256:* ]] || { echo 'REFUSING: pin LOADGEN_IMAGE to a digest' >&2; exit 1; }
export LOADGEN_JOB="${LOADGEN_JOB:-loadgen-${RUN_ID}}"
export RPS="${RPS:-100}" DURATION="${DURATION:-240s}" WARMUP="${WARMUP:-10s}"
gateway_service="$(kubectl -n envoy-gateway-system get svc -l gateway.envoyproxy.io/owning-gateway-name=checkout-gateway,gateway.envoyproxy.io/owning-gateway-namespace=checkout -o jsonpath='{.items[0].metadata.name}')"
[[ -n "$gateway_service" ]] || { echo 'No in-cluster gateway Service found' >&2; exit 1; }
export TARGET_URL="http://${gateway_service}.envoy-gateway-system.svc.cluster.local/checkout"
# Pipe the secret through stdin: no secret manifest on disk or credentials in argv.
python3 - <<'PY' | kubectl apply -f -
import base64,json,os
print(json.dumps({'apiVersion':'v1','kind':'Secret','metadata':{'name':'sloproof-loadgen','namespace':'checkout'},'type':'Opaque','data':{k:base64.b64encode(os.environ[k].encode()).decode() for k in ['ELASTIC_CLOUD_ENDPOINT','ELASTIC_INGEST_API_KEY']}}))
PY
python3 - <<'PY' | kubectl create -f -
import os,re
s=open('k8s/50-loadgen.yaml').read()
for k in ['LOADGEN_JOB','LOADGEN_IMAGE','RUN_ID','TARGET_URL','RPS','DURATION','WARMUP']:
    v=os.environ[k]
    if not re.fullmatch(r'[A-Za-z0-9:/@._-]+',v): raise SystemExit('invalid '+k)
    s=s.replace('__'+k+'__',v)
if re.search(r'__[A-Z_]+__',s): raise SystemExit('unresolved placeholder')
print(s)
PY
echo "Run $RUN_ID targets $TARGET_URL"
