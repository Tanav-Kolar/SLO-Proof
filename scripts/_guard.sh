# Sourced by every script that talks to a cluster.
#
# Two protections, both required by SERVICE_ARCHITECTURE.md section 6:
#
#   1. KUBECONFIG is pinned to a dedicated file inside the repo. No script ever
#      reads the ambient kubeconfig, so a stray context on the laptop cannot be
#      targeted even by accident.
#   2. The context inside that file must match SLOPROOF_KUBE_CONTEXT, which the
#      operator sets explicitly.
#
# This machine's ambient contexts are unrelated production clusters. An
# unqualified kubectl apply from here would target someone's real infrastructure.
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"

# Pinned, gitignored, and exported so every kubectl and helm call inherits it.
export KUBECONFIG="${SLOPROOF_KUBECONFIG:-$REPO_ROOT/.kube/sloproof.config}"

NAMESPACE="${NAMESPACE:-checkout}"
ROUTE_NAME="${ROUTE_NAME:-checkout-route}"
LEDGER_DIR="${LEDGER_DIR:-evidence}"
CLUSTER_NAME="${CLUSTER_NAME:-sloproof}"
APP_LABEL="${APP_LABEL:-app=checkout}"
AWS_REGION="${AWS_REGION:-us-west-2}"

require_kubeconfig() {
  if [[ ! -f "$KUBECONFIG" ]]; then
    echo "REFUSING: no pinned kubeconfig at $KUBECONFIG" >&2
    echo "  Create the cluster first:  scripts/eks-up.sh" >&2
    exit 1
  fi
}

# The EKS kubeconfig authenticates by exec'ing `aws eks get-token`. Without AWS
# credentials in the environment that surfaces as an opaque "Unable to connect
# to the server", which reads like a cluster outage rather than a missing
# profile. Fail with the real reason instead.
require_aws() {
  if ! aws sts get-caller-identity >/dev/null 2>&1; then
    echo "REFUSING: AWS credentials do not resolve." >&2
    echo "  kubectl authenticates to EKS by running the aws CLI, so this would" >&2
    echo "  otherwise look like the cluster being unreachable." >&2
    echo "  Set AWS_PROFILE (and AWS_SHARED_CREDENTIALS_FILE if not in ~/.aws)." >&2
    exit 1
  fi
}

require_context() {
  require_kubeconfig
  require_aws
  local current
  current="$(kubectl config current-context 2>/dev/null || true)"

  if [[ -z "${SLOPROOF_KUBE_CONTEXT:-}" ]]; then
    echo "REFUSING: SLOPROOF_KUBE_CONTEXT is not set." >&2
    echo "  Context in the pinned kubeconfig is: ${current:-<none>}" >&2
    echo "  Set it explicitly:  export SLOPROOF_KUBE_CONTEXT='${current:-<context>}'" >&2
    exit 1
  fi

  if [[ "$current" != "$SLOPROOF_KUBE_CONTEXT" ]]; then
    echo "REFUSING: context mismatch inside the pinned kubeconfig." >&2
    echo "  SLOPROOF_KUBE_CONTEXT = $SLOPROOF_KUBE_CONTEXT" >&2
    echo "  current context       = ${current:-<none>}" >&2
    exit 1
  fi

  echo "kubeconfig: $KUBECONFIG"
  echo "context:    $current"
}

require_env() {
  local missing=0
  for v in "$@"; do
    if [[ -z "${!v:-}" ]]; then echo "REFUSING: $v is not set" >&2; missing=1; fi
  done
  [[ $missing -eq 0 ]] || exit 1
}

require_cmd() {
  local missing=0
  for c in "$@"; do
    command -v "$c" >/dev/null 2>&1 || { echo "REFUSING: $c is not installed" >&2; missing=1; }
  done
  [[ $missing -eq 0 ]] || { echo "  brew install eksctl helm" >&2; exit 1; }
}
