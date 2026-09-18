#!/usr/bin/env bash
# Runs a saved ES|QL query and prints the result as a table.
#
# Usage: elastic/query.sh <query-name> [name=value ...]
#   elastic/query.sh incident-timeline run_id=demo-1
#   elastic/query.sh window-metrics run_id=demo-1 \
#       window_start=2026-09-18T06:28:00Z window_end=2026-09-18T06:29:00Z
#
# Uses the read-only verifier key when one exists, so the query path is exercised
# with the same credential the verifier will use.
set -euo pipefail
cd "$(dirname "$0")/.."

[[ $# -ge 1 ]] || { echo "usage: $0 <query-name> [name=value ...]" >&2; ls -1 elastic/queries | sed 's/\.esql$//' | sed 's/^/  /' >&2; exit 1; }
NAME="$1"; shift
FILE="elastic/queries/${NAME}.esql"
[[ -f "$FILE" ]] || { echo "no such query: $FILE" >&2; exit 1; }

set -a; source .env; set +a
KEY="${ELASTIC_VERIFIER_API_KEY:-$ELASTIC_API_KEY}"

python3 - "$FILE" "$ELASTIC_CLOUD_ENDPOINT" "$KEY" "$@" <<'PY'
import json, sys, urllib.request
qfile, endpoint, key = sys.argv[1], sys.argv[2], sys.argv[3]
params = [{k: v} for k, v in (a.split("=", 1) for a in sys.argv[4:])]

body = json.dumps({"query": open(qfile).read(), "params": params}).encode()
req = urllib.request.Request(endpoint + "/_query", data=body, method="POST",
    headers={"Authorization": "ApiKey " + key, "Content-Type": "application/json"})
try:
    with urllib.request.urlopen(req, timeout=90) as r:
        d = json.load(r)
except urllib.error.HTTPError as e:
    sys.exit("ES|QL error: " + e.read().decode()[:600])

cols = [c["name"] for c in d["columns"]]
rows = d["values"]
if not rows:
    print("(no rows -- an empty result is missing evidence, not zero errors)")
    sys.exit(0)

def fmt(v):
    if isinstance(v, float): return f"{v:.2f}"
    return "" if v is None else str(v)

w = [max(len(c), *(len(fmt(r[i])) for r in rows)) for i, c in enumerate(cols)]
print("  ".join(c.ljust(w[i]) for i, c in enumerate(cols)))
print("  ".join("-" * w[i] for i in range(len(cols))))
for r in rows:
    print("  ".join(fmt(v).ljust(w[i]) for i, v in enumerate(r)))
print(f"\n{len(rows)} rows")
PY
