#!/usr/bin/env bash
# Verify all three profiles on isolated local ports, without cloud credentials.
set -euo pipefail
cd "$(dirname "$0")/.."
SMOKE_DIR="$(mktemp -d)"
trap 'rm -rf "$SMOKE_DIR"' EXIT
BIN="$SMOKE_DIR/checkout"
(cd src/checkout && go build -o "$BIN" .)
echo "built local checkout test binary"

run_profile() {
  local profile="$1" version="$2" port run_id pid ready="" status=0
  port="$(python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(('127.0.0.1',0))
    print(sock.getsockname()[1])
PY
)"
  run_id="smoke-$$-${profile}"
  SERVICE_VERSION="$version" FAULT_PROFILE="$profile" PORT="$port" \
    OTEL_SDK_DISABLED=true READINESS_DELAY_MS=300 SHUTDOWN_GRACE_MS=100 RUN_ID="$run_id" \
    "$BIN" >"$SMOKE_DIR/$profile.log" 2>&1 &
  pid=$!
  for _ in $(seq 1 100); do
    kill -0 "$pid" 2>/dev/null || break
    if curl -sf "http://127.0.0.1:$port/readyz" >/dev/null 2>&1; then ready=yes; break; fi
    sleep 0.2
  done
  if [[ -z "$ready" ]]; then
    echo "FAILED: $profile did not start on its isolated port" >&2
    cat "$SMOKE_DIR/$profile.log" >&2
    kill -TERM "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    return 1
  fi
  printf '%-22s ' "$profile"
  python3 - "$port" "$profile" "$run_id" "$version" <<'PY' || status=$?
import sys,json,time,urllib.request,urllib.error
from concurrent.futures import ThreadPoolExecutor
port,profile,run_id,version=sys.argv[1:]
with urllib.request.urlopen(f'http://127.0.0.1:{port}/version',timeout=5) as r:identity=json.load(r)
if (identity['run_id'],identity['fault_profile'],identity['version'])!=(run_id,profile,version):
    sys.exit('FAILED: port belongs to a different checkout process')
n,conc=300,40
def one(_):
    started=time.perf_counter()
    req=urllib.request.Request(f'http://127.0.0.1:{port}/checkout',method='POST')
    try:
        with urllib.request.urlopen(req,timeout=10) as r:code=r.status;r.read()
    except urllib.error.HTTPError as e:code=e.code;e.read()
    except Exception:code=0
    return code,(time.perf_counter()-started)*1000
with ThreadPoolExecutor(max_workers=conc) as pool:results=list(pool.map(one,range(n)))
latencies=sorted(ms for _,ms in results);rate=sum(code==200 for code,_ in results)/n
p95=latencies[min(int(.95*n),n-1)]
expected={'healthy':rate>=.99 and p95<300,
          'checkout-regression':.7<=rate<=.9 and p95>=800,
          'bad-recovery':rate>=.99 and p95>=300}[profile]
print(json.dumps({'success_rate':round(rate,4),'p95_ms':round(p95,1),'expected_behavior':expected}))
if not expected:sys.exit('FAILED: profile did not satisfy its expected contract behavior')
PY
  kill -TERM "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  return "$status"
}
run_profile healthy v1
run_profile checkout-regression v2
run_profile bad-recovery v1
