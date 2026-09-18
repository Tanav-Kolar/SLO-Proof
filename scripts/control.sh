#!/usr/bin/env bash
# Build both executables; verifier runs with an allowlisted environment.
set -euo pipefail
cd "$(dirname "$0")/.."
[[ $# -ge 1 ]] || { echo 'usage: scripts/control.sh verify|freeze|trial|advance|observe|record|policy|route [flags]' >&2; exit 1; }
mkdir -p build
(cd src/control && go build -o ../../build/verifier ./cmd/verifier && go build -o ../../build/controller ./cmd/controller)
mode="$1"; shift
if [[ "$mode" == verify ]]; then
  : "${ELASTIC_CLOUD_ENDPOINT:?required}" "${ELASTIC_VERIFIER_API_KEY:?scoped read-only key required}"
  exec env -i PATH="$PATH" ELASTIC_CLOUD_ENDPOINT="$ELASTIC_CLOUD_ENDPOINT" ELASTIC_VERIFIER_API_KEY="$ELASTIC_VERIFIER_API_KEY" build/verifier "$@"
fi
: "${ELASTIC_CLOUD_ENDPOINT:?required}" "${ELASTIC_CONTROLLER_API_KEY:?scoped controller key required}"
exec env -i PATH="$PATH" ELASTIC_CLOUD_ENDPOINT="$ELASTIC_CLOUD_ENDPOINT" ELASTIC_CONTROLLER_API_KEY="$ELASTIC_CONTROLLER_API_KEY" build/controller "$mode" "$@"
