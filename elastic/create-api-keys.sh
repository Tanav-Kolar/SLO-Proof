#!/usr/bin/env bash
# Creates the three scoped Elasticsearch API keys and writes them into .env.
#
# The separation is the core claim of the project: the component that decides
# whether to promote must not be able to write the evidence it reads. So the
# verifier key is read-only, and it is a different credential from the one that
# ingests and the one that writes the ledger.
#
#   sloproof-ingest      write  sloproof-requests
#   sloproof-verifier    READ   sloproof-requests, sloproof-contracts, sloproof-ledger
#   sloproof-controller  write  sloproof-contracts, sloproof-ledger
#
# AUTHENTICATION MATTERS HERE. An API key cannot mint a scoped child key:
# Elasticsearch only allows a *derived* key with an empty role descriptor, which
# would inherit every privilege of its parent and scope nothing. So this needs
# user credentials. Set ELASTIC_USERNAME and ELASTIC_PASSWORD in .env, or create
# the keys in Kibana; the script prints the exact role descriptors for that.
#
# Secrets are written straight into .env (gitignored). Only key IDs are printed,
# so secret values never reach a terminal transcript or a CI log.
set -euo pipefail
cd "$(dirname "$0")/.."

[[ -f .env ]] || { echo "REFUSING: no .env" >&2; exit 1; }
set -a; source .env; set +a
: "${ELASTIC_CLOUD_ENDPOINT:?not set in .env}"

mkdir -p .env.backups
cp .env ".env.backups/env-$(date -u +%Y%m%dT%H%M%SZ)"

if [[ -n "${ELASTIC_USERNAME:-}" && -n "${ELASTIC_PASSWORD:-}" ]]; then
  AUTH=(-u "$ELASTIC_USERNAME:$ELASTIC_PASSWORD")
  echo "authenticating as user $ELASTIC_USERNAME"
elif [[ -n "${ELASTIC_API_KEY:-}" ]]; then
  AUTH=(-H "Authorization: ApiKey $ELASTIC_API_KEY")
  echo "authenticating with ELASTIC_API_KEY (may not be able to scope; see below)"
else
  echo "REFUSING: set ELASTIC_USERNAME/ELASTIC_PASSWORD or ELASTIC_API_KEY in .env" >&2
  exit 1
fi

DESC_INGEST='{"ingest":{"cluster":["monitor"],"indices":[{"names":["sloproof-requests*"],"privileges":["create_index","write","create","index","view_index_metadata"]}]}}'
DESC_VERIFIER='{"verifier":{"cluster":["monitor"],"indices":[{"names":["sloproof-requests*","sloproof-contracts*","sloproof-ledger*"],"privileges":["read","view_index_metadata"]}]}}'
DESC_CONTROLLER='{"controller":{"cluster":["monitor"],"indices":[{"names":["sloproof-contracts*","sloproof-ledger*"],"privileges":["create_index","write","create","index","read","view_index_metadata"]},{"names":["sloproof-requests*"],"privileges":["read","view_index_metadata"]}]}}'

derived_key_error=0

make_key() {
  local name="$1" envvar="$2" descriptors="$3" body resp id secret

  body="$(python3 -c "
import json,sys
print(json.dumps({'name': sys.argv[1], 'role_descriptors': json.loads(sys.argv[2])}))" "$name" "$descriptors")"

  resp="$(curl -sS --max-time 30 -X POST "${AUTH[@]}" \
    -H 'Content-Type: application/json' \
    "$ELASTIC_CLOUD_ENDPOINT/_security/api_key" -d "$body")"

  id="$(printf '%s' "$resp" | python3 -c 'import sys,json
try: print(json.load(sys.stdin).get("id",""))
except Exception: print("")')"
  secret="$(printf '%s' "$resp" | python3 -c 'import sys,json
try: print(json.load(sys.stdin).get("encoded",""))
except Exception: print("")')"

  if [[ -z "$secret" ]]; then
    if printf '%s' "$resp" | grep -q 'derived api keys'; then
      derived_key_error=1
    else
      echo "  FAILED $name:" >&2
      printf '%s' "$resp" | head -c 300 >&2; echo >&2
    fi
    return 1
  fi

  python3 - "$envvar" "$secret" <<'PY'
import sys, pathlib
var, val = sys.argv[1], sys.argv[2]
p = pathlib.Path(".env")
lines = [l for l in p.read_text().splitlines() if not l.startswith(var + "=")]
lines.append(f"{var}={val}")
p.write_text("\n".join(lines) + "\n")
PY
  echo "  $name -> $envvar  (key id: $id)"
}

echo "creating scoped keys:"
ok=0
make_key sloproof-ingest     ELASTIC_INGEST_API_KEY     "$DESC_INGEST"     && ok=$((ok+1)) || true
make_key sloproof-verifier   ELASTIC_VERIFIER_API_KEY   "$DESC_VERIFIER"   && ok=$((ok+1)) || true
make_key sloproof-controller ELASTIC_CONTROLLER_API_KEY "$DESC_CONTROLLER" && ok=$((ok+1)) || true

if [[ $derived_key_error -eq 1 ]]; then
  cat >&2 <<'MSG'

CANNOT SCOPE KEYS WITH THE CURRENT CREDENTIAL.

Elasticsearch refused with "creating derived api keys requires an explicit role
descriptor that is empty". That is what happens when the caller is itself an API
key: a child key may only inherit its parent's privileges, never narrow them. A
key minted that way would give the verifier full write access, which defeats the
separation entirely -- so this script refuses to create one rather than produce a
credential that looks scoped and is not.

Two ways forward:

1. Put user credentials in .env and rerun:
     ELASTIC_USERNAME=...
     ELASTIC_PASSWORD=...

2. Create them in Kibana: Stack Management -> API keys -> Create API key ->
   "User API key" -> Control security privileges, and paste the role descriptor
   printed below for each key. Then add the encoded values to .env as
   ELASTIC_INGEST_API_KEY, ELASTIC_VERIFIER_API_KEY, ELASTIC_CONTROLLER_API_KEY.

Role descriptors:
MSG
  for pair in "sloproof-ingest:$DESC_INGEST" "sloproof-verifier:$DESC_VERIFIER" "sloproof-controller:$DESC_CONTROLLER"; do
    echo >&2; echo "  ${pair%%:*}:" >&2
    python3 -c "import json,sys;print(json.dumps(json.loads(sys.argv[1]),indent=2))" "${pair#*:}" | sed 's/^/    /' >&2
  done
  echo >&2
  exit 1
fi

echo
echo "$ok/3 keys written to .env (gitignored). Secrets were not printed."
[[ "$ok" -eq 3 ]] || exit 1
echo "Now prove the verifier really cannot write:  elastic/verify-key-scopes.sh"
