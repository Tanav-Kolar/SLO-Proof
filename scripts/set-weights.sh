#!/usr/bin/env bash
# Sets the ordinary-traffic split and writes a routing receipt.
#
# Usage: scripts/set-weights.sh <v1-weight> <v2-weight>
#   healthy baseline   100 0
#   incident             0 100
#   recovery trial      10 90
#   promotion          100 0
#
# The receipt records both the API-acceptance time and the weights before and
# after. Observation windows anchor to route_effective_at, which the load
# generator confirms from served traffic -- an accepted patch is not evidence
# that traffic actually moved.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/_guard.sh

require_context
[[ $# -eq 2 ]] || { echo "usage: $0 <v1-weight> <v2-weight>" >&2; exit 1; }
V1="$1"; V2="$2"

BEFORE="$(kubectl -n "$NAMESPACE" get httproute "$ROUTE_NAME" -o json)"

PATCH="$(printf '%s' "$BEFORE" | python3 -c '
import json, sys
route = json.load(sys.stdin)
rules = route["spec"]["rules"]
idx = [i for i, r in enumerate(rules)
       if not any("headers" in m for m in r.get("matches", []))]
if len(idx) != 1:
    sys.exit(f"expected exactly one ordinary (header-free) rule, found {idx}")
# Preserve whatever backends the route already names and change only their
# weights. Hardcoding backend names here would both break reuse on another
# service and silently clobber a backend someone else had edited.
refs = [dict(r) for r in rules[idx[0]]["backendRefs"]]
weights = [int(w) for w in sys.argv[1:]]
if len(refs) != len(weights):
    sys.exit(f"route has {len(refs)} backends but {len(weights)} weights were given")
for ref, w in zip(refs, weights):
    ref["weight"] = w
print(json.dumps([{"op": "replace", "path": f"/spec/rules/{idx[0]}/backendRefs",
                   "value": refs}]))
' "$V1" "$V2")"

AFTER="$(kubectl -n "$NAMESPACE" patch httproute "$ROUTE_NAME" --type=json -p "$PATCH" -o json)"

mkdir -p "$LEDGER_DIR"
RECEIPT="$LEDGER_DIR/route-$(date -u +%Y%m%dT%H%M%SZ).json"
printf '%s' "$BEFORE" | python3 -c '
import json, sys, datetime
before = json.load(sys.stdin)
after = json.loads(sys.argv[1])
def weights(route):
    rules = route["spec"]["rules"]
    i = [n for n, r in enumerate(rules) if not any("headers" in m for m in r.get("matches", []))][0]
    return {b["name"]: b.get("weight") for b in rules[i]["backendRefs"]}
json.dump({
    "event": "route_weights_changed",
    "route_uid": before["metadata"]["uid"],
    "resource_version_before": before["metadata"]["resourceVersion"],
    "resource_version_after": after["metadata"]["resourceVersion"],
    "weights_before": weights(before),
    "weights_after": weights(after),
    "api_accepted_at": datetime.datetime.now(datetime.UTC).isoformat(),
    "route_effective_at": None,
    "note": "route_effective_at must be filled from observed served traffic, not from this timestamp."
}, open(sys.argv[2], "w"), indent=2)
' "$AFTER" "$RECEIPT"

echo "weights -> v1=${V1} v2=${V2}"
echo "receipt: $RECEIPT"
cat "$RECEIPT"
