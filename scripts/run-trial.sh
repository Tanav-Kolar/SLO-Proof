#!/usr/bin/env bash
# Operator-driven, one complete trial. Leaves routing unchanged after verification.
# Explicit setup changes fault profiles before freezing; the executor only changes
# route weights. Every run needs a unique ID and scoped credentials.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/_guard.sh
[[ $# == 2 ]] || { echo 'usage: scripts/run-trial.sh healthy|bad-recovery <unique-run-id>' >&2; exit 1; }
profile="$1"
case "$profile" in healthy|bad-recovery) ;; *) echo 'Unsupported scenario' >&2; exit 1 ;; esac
export RUN_ID="$2"
[[ "$RUN_ID" =~ ^[a-z0-9][a-z0-9-]{0,44}$ ]] || { echo 'Use a lowercase DNS-safe run ID, at most 45 characters' >&2; exit 1; }
# Check credentials before any cluster mutation or profile change.
require_env ELASTIC_CLOUD_ENDPOINT ELASTIC_INGEST_API_KEY ELASTIC_VERIFIER_API_KEY ELASTIC_CONTROLLER_API_KEY LOADGEN_IMAGE
[[ "$LOADGEN_IMAGE" == *@sha256:* ]] || { echo 'Digest-pinned LOADGEN_IMAGE required' >&2; exit 1; }
require_context
mkdir -p build evidence
(cd src/control && go build -o ../../build/verifier ./cmd/verifier && go build -o ../../build/controller ./cmd/controller)
# Prove the read credential's actual permissions before changing anything.
env -i PATH="$PATH" ELASTIC_CLOUD_ENDPOINT="$ELASTIC_CLOUD_ENDPOINT" ELASTIC_VERIFIER_API_KEY="$ELASTIC_VERIFIER_API_KEY" build/verifier --check-scope
python3 - <<'PY'
import json,subprocess,os,urllib.request,urllib.error
jobs=json.loads(subprocess.check_output(['kubectl','-n','checkout','get','jobs','-l','app=sloproof-loadgen','-o','json']))['items']
if any(j['metadata']['name']=='loadgen-'+os.environ['RUN_ID'] for j in jobs):
 raise SystemExit('REFUSING: a Job already used this run ID')
if any(not j.get('status',{}).get('completionTime') and not j.get('status',{}).get('failed') for j in jobs):
 raise SystemExit('REFUSING: another load generator Job is active or pending')
req=urllib.request.Request(os.environ['ELASTIC_CLOUD_ENDPOINT']+'/sloproof-contracts/_doc/'+os.environ['RUN_ID'],headers={'Authorization':'ApiKey '+os.environ['ELASTIC_VERIFIER_API_KEY']})
try:
 with urllib.request.urlopen(req,timeout=20):pass
except urllib.error.HTTPError as e:
 if e.code!=404:raise
else:raise SystemExit('REFUSING: run ID already has a frozen contract')
PY
scripts/setup-executor.sh
# Scenario setup is an explicit operator action, before the trial is frozen.
scripts/set-profile.sh v1 "$profile"
scripts/set-profile.sh v2 checkout-regression
scripts/set-weights.sh 0 100

# Query /version on every ready v1 pod through a pinned port-forward. Reject
# mixed configurations, and preserve real provenance before contract freeze.
python3 scripts/snapshot-candidate.py "$RUN_ID" "$profile"
config_hash="$(python3 - "$RUN_ID" <<'PY'
import json,sys
print(json.load(open('evidence/'+sys.argv[1]+'-candidate.json'))['config_hash'])
PY
)"

export LOADGEN_JOB="loadgen-${RUN_ID}" DURATION=300s WARMUP=0s RPS=100
# Start traffic while still on the incident version. Image pulls and scheduling
# therefore cannot consume the frozen route-convergence allowance.
scripts/run-loadgen.sh
kubectl -n checkout wait --for=condition=Ready pod -l "job-name=$LOADGEN_JOB" --timeout=120s
scripts/control.sh freeze --run-id "$RUN_ID" --config-hash "$config_hash" > "evidence/$RUN_ID-contract.json"
if ! scripts/control.sh trial --run-id "$RUN_ID" > "evidence/$RUN_ID-route.json"; then
  echo 'Trial action refused or failed; no retry. Collecting the Job evidence so the verifier can record the missing receipt.' >&2
fi

# Wait for the generator to finish; the observer derives its timestamp from
# the original post-patch traffic, not from the clock at which we query it.
for attempt in $(seq 1 10); do
  if kubectl -n checkout wait --for=condition=complete "job/$LOADGEN_JOB" --timeout=40s; then break; fi
  failed="$(kubectl -n checkout get job "$LOADGEN_JOB" -o jsonpath='{.status.failed}')"
  [[ -z "$failed" || "$failed" == 0 ]] || { echo 'Load generator failed; no automatic route change' >&2; exit 1; }
  echo 'Still collecting real evidence; routing remains at the trial split.'
done
if ! scripts/control.sh observe --run-id "$RUN_ID" > "evidence/$RUN_ID-effective.json"; then
  echo 'No effective receipt established; verifier must treat the run as INCONCLUSIVE.' >&2
fi
status=0
scripts/verify-run.sh || status=$?
[[ "$status" == 0 || "$status" == 2 ]] || exit "$status"
python3 scripts/render-evidence.py
printf '\nTrial finished. Routing has NOT been automatically changed. Review the verdict and choose the next operator action.\n'
exit "$status"
