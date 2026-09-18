#!/usr/bin/env bash
# Creates the three SLOProof index templates and their concrete indices.
#
# Regular indices, not data streams, on purpose: data streams force auto-generated
# document ids, and the ingest path needs request_id as a stable _id so a retried
# bulk item overwrites rather than double-counting.
#
# Reads ELASTIC_CLOUD_ENDPOINT and ELASTIC_API_KEY from .env. Never prints them.
set -euo pipefail
cd "$(dirname "$0")/.."

[[ -f .env ]] || { echo "REFUSING: no .env" >&2; exit 1; }
set -a; source .env; set +a
: "${ELASTIC_CLOUD_ENDPOINT:?not set in .env}"
: "${ELASTIC_API_KEY:?not set in .env}"

es() {
  local method="$1" path="$2"; shift 2
  curl -sS --max-time 30 -X "$method" \
    -H "Authorization: ApiKey $ELASTIC_API_KEY" \
    -H 'Content-Type: application/json' \
    "$ELASTIC_CLOUD_ENDPOINT$path" "$@"
}

for name in requests contracts ledger; do
  echo -n "template sloproof-$name: "
  es PUT "/_index_template/sloproof-$name" \
     --data-binary "@elastic/templates/sloproof-$name.json" \
    | python3 -c 'import sys,json;d=json.load(sys.stdin);print("ok" if d.get("acknowledged") else d)'
done

for name in requests contracts ledger; do
  echo -n "index sloproof-$name: "
  code="$(es HEAD "/sloproof-$name" -o /dev/null -w '%{http_code}')"
  if [[ "$code" == "200" ]]; then
    echo "already exists"
  else
    es PUT "/sloproof-$name" \
      | python3 -c 'import sys,json;d=json.load(sys.stdin);print("created" if d.get("acknowledged") else d)'
  fi
done

echo
echo "mapped fields on sloproof-requests:"
es GET "/sloproof-requests/_mapping" \
  | python3 -c 'import sys,json;d=json.load(sys.stdin);m=list(d.values())[0]["mappings"];print(" ",", ".join(sorted(m["properties"])));print("  dynamic:",m.get("dynamic"))'
