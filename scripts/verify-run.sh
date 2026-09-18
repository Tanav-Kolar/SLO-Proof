#!/usr/bin/env bash
# Verify a completed Job and append the deterministic output to the ledger.
# The read-only verifier process never receives AWS, Kubernetes or writer keys.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/_guard.sh
require_context
require_env RUN_ID ELASTIC_CLOUD_ENDPOINT ELASTIC_VERIFIER_API_KEY ELASTIC_CONTROLLER_API_KEY
[[ "$RUN_ID" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]*$ ]] || exit 1
job="${LOADGEN_JOB:-loadgen-${RUN_ID}}"
state="$(kubectl -n checkout get job "$job" -o jsonpath='{.status.succeeded}')"
[[ "$state" == 1 ]] || { echo 'REFUSING: load generator Job has not completed' >&2; exit 1; }
mkdir -p evidence
kubectl -n checkout logs "job/$job" > "evidence/$RUN_ID-loadgen.log"
python3 - "$RUN_ID" <<'PY'
import json,pathlib,sys
run=sys.argv[1];s=pathlib.Path('evidence/'+run+'-loadgen.log').read_text()
i=s.find('{');summary,_=json.JSONDecoder().raw_decode(s[i:])
if summary['run_id']!=run:raise SystemExit('summary run mismatch')
pathlib.Path('evidence/'+run+'-summary.json').write_text(json.dumps(summary,indent=2)+'\n')
PY
status=0
scripts/control.sh verify --run-id "$RUN_ID" --summary "evidence/$RUN_ID-summary.json" > "evidence/$RUN_ID-verdict.json" || status=$?
[[ "$status" == 0 || "$status" == 2 ]] || exit "$status"
scripts/control.sh record --run-id "$RUN_ID" --input "evidence/$RUN_ID-verdict.json"
cat "evidence/$RUN_ID-verdict.json"
exit "$status"
