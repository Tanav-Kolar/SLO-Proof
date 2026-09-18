# Sourced by every script that talks to a cluster.
#
# Refuses to run unless SLOPROOF_KUBE_CONTEXT is set AND matches the current
# kubectl context. A hackathon laptop usually has several contexts; this makes
# applying the demo to the wrong cluster an explicit act rather than an accident.
set -euo pipefail

require_context() {
  local current
  current="$(kubectl config current-context 2>/dev/null || true)"

  if [[ -z "${SLOPROOF_KUBE_CONTEXT:-}" ]]; then
    echo "REFUSING: SLOPROOF_KUBE_CONTEXT is not set." >&2
    echo "  Current kubectl context is: ${current:-<none>}" >&2
    echo "  Set it explicitly, e.g.:  export SLOPROOF_KUBE_CONTEXT='${current:-my-eks-context}'" >&2
    exit 1
  fi

  if [[ "$current" != "$SLOPROOF_KUBE_CONTEXT" ]]; then
    echo "REFUSING: context mismatch." >&2
    echo "  SLOPROOF_KUBE_CONTEXT = $SLOPROOF_KUBE_CONTEXT" >&2
    echo "  current context       = ${current:-<none>}" >&2
    echo "  Run: kubectl config use-context '$SLOPROOF_KUBE_CONTEXT'" >&2
    exit 1
  fi

  echo "context: $current"
}

require_env() {
  local missing=0
  for v in "$@"; do
    if [[ -z "${!v:-}" ]]; then echo "REFUSING: $v is not set" >&2; missing=1; fi
  done
  [[ $missing -eq 0 ]] || exit 1
}

NAMESPACE="${NAMESPACE:-checkout}"
ROUTE_NAME="${ROUTE_NAME:-checkout-route}"
LEDGER_DIR="${LEDGER_DIR:-evidence}"
