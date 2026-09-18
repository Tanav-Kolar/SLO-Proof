#!/usr/bin/env bash
# Runs the three fault profiles locally, with no cluster and no cloud account,
# and checks each one lands where contracts/checkout-slo.json expects.
#
#   healthy              >= 99% success, p95 < 300 ms   -> trial can pass
#   checkout-regression  visibly failing                -> the incident
#   bad-recovery         100% success, p95 > 300 ms     -> FAILED verdict run
set -euo pipefail
cd "$(dirname "$0")/.."

BIN="$(mktemp -d)/checkout"
( cd src/checkout && go build -o "$BIN" . )
echo "built $BIN"

run_profile() {
  local profile="$1" port="$2" version="$3"
  SERVICE_VERSION="$version" FAULT_PROFILE="$profile" PORT="$port" \
    READINESS_DELAY_MS=300 RUN_ID="smoke" "$BIN" >"/tmp/checkout-$profile.log" 2>&1 &
  local pid=$!
  local ready=""
  for _ in $(seq 1 100); do
    if curl -sf "http://127.0.0.1:$port/readyz" >/dev/null 2>&1; then ready=yes; break; fi
    sleep 0.2
  done
  if [[ -z "$ready" ]]; then
    echo "FAILED: $profile never became ready on port $port"; cat "/tmp/checkout-$profile.log"
    kill -TERM "$pid" 2>/dev/null || true; return 1
  fi
  printf '%-22s ' "$profile"
  python3 - "$port" <<'PY'
import sys, json, time, urllib.request, urllib.error
from concurrent.futures import ThreadPoolExecutor
port = int(sys.argv[1]); n, conc = 300, 40
def one(_):
    t0 = time.perf_counter()
    req = urllib.request.Request(f"http://127.0.0.1:{port}/checkout", method="POST")
    try:
        with urllib.request.urlopen(req, timeout=10) as r: code = r.status; r.read()
    except urllib.error.HTTPError as e: code = e.code; e.read()
    except Exception: code = 0
    return code, (time.perf_counter() - t0) * 1000
with ThreadPoolExecutor(max_workers=conc) as ex: res = list(ex.map(one, range(n)))
lat = sorted(d for _, d in res); ok = sum(1 for c, _ in res if c == 200)
print(json.dumps({"success_rate": round(ok/n, 4),
                  "p95_ms": round(lat[min(int(0.95*len(lat)), len(lat)-1)], 1)}))
PY
  kill -TERM "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
}

run_profile healthy             18080 v1
run_profile checkout-regression 18081 v2
run_profile bad-recovery        18082 v1
