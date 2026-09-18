#!/usr/bin/env bash
# Proves the credential separation instead of asserting it.
#
# D7 claims the verifier is credentially separate from the thing that wants to
# promote. That claim is only worth anything if the verifier key demonstrably
# fails to write. This script attempts each forbidden action and reports the
# HTTP status, so a judge can see 403 rather than take the design on trust.
set -euo pipefail
cd "$(dirname "$0")/.."

[[ -f .env ]] || { echo "REFUSING: no .env" >&2; exit 1; }
set -a; source .env; set +a
: "${ELASTIC_CLOUD_ENDPOINT:?not set in .env}"

probe() {
  local label="$1" key="$2" method="$3" path="$4" data="${5:-}"
  if [[ -z "$key" ]]; then
    printf '  %-46s %s\n' "$label" "SKIPPED (key not in .env)"
    return
  fi
  local args=(-sS -o /dev/null -w '%{http_code}' --max-time 20 -X "$method"
              -H "Authorization: ApiKey $key" -H 'Content-Type: application/json')
  [[ -n "$data" ]] && args+=(-d "$data")
  printf '  %-46s HTTP %s\n' "$label" "$(curl "${args[@]}" "$ELASTIC_CLOUD_ENDPOINT$path")"
}

echo "expected: reads 200, forbidden writes 403"
echo
echo "verifier key (must read everything, write nothing):"
probe "read sloproof-requests"          "${ELASTIC_VERIFIER_API_KEY:-}"   POST /sloproof-requests/_count
probe "read sloproof-contracts"         "${ELASTIC_VERIFIER_API_KEY:-}"   POST /sloproof-contracts/_count
probe "WRITE sloproof-ledger (must 403)" "${ELASTIC_VERIFIER_API_KEY:-}"  POST "/sloproof-ledger/_doc" '{"event":"scope_probe"}'
probe "WRITE sloproof-requests (must 403)" "${ELASTIC_VERIFIER_API_KEY:-}" POST "/sloproof-requests/_doc" '{"run_id":"scope_probe"}'

echo
echo "ingest key (must write requests only):"
probe "write sloproof-requests"          "${ELASTIC_INGEST_API_KEY:-}"    POST "/sloproof-requests/_doc" '{"run_id":"scope-probe","request_id":"scope-probe","traffic_class":"synthetic","service":"checkout","version":"probe","status":200,"outcome":"success","success":true,"duration_ms":1.0,"@timestamp":"2026-01-01T00:00:00Z","started_at":"2026-01-01T00:00:00Z","ended_at":"2026-01-01T00:00:00Z","generator_id":"probe","config_hash":"probe","target_host":"probe"}'
probe "WRITE sloproof-ledger (must 403)"  "${ELASTIC_INGEST_API_KEY:-}"   POST "/sloproof-ledger/_doc" '{"event":"scope_probe"}'

echo
echo "controller key (must write contracts and ledger):"
probe "write sloproof-ledger"            "${ELASTIC_CONTROLLER_API_KEY:-}" POST "/sloproof-ledger/_doc" '{"event":"scope_probe"}'
probe "WRITE sloproof-requests (must 403)" "${ELASTIC_CONTROLLER_API_KEY:-}" POST "/sloproof-requests/_doc" '{"run_id":"scope_probe"}'

echo
echo "Any 'must 403' line showing 2xx means the separation is not real."
