#!/usr/bin/env bash
# Changes a version's fault profile. OPERATOR ACTION ONLY.
#
# Usage: scripts/set-profile.sh <v1|v2> <healthy|checkout-regression|bad-recovery>
#
# This is deliberately a separate script from set-weights.sh, run under the
# operator's identity. The recovery executor changes route weights and nothing
# else; it must not be able to cause the incident it is trusted to repair.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/_guard.sh

require_context
[[ $# -eq 2 ]] || { echo "usage: $0 <v1|v2> <healthy|checkout-regression|bad-recovery>" >&2; exit 1; }
VERSION="$1"; PROFILE="$2"

case "$VERSION" in v1|v2) ;; *) echo "version must be v1 or v2" >&2; exit 1 ;; esac
case "$PROFILE" in healthy|checkout-regression|bad-recovery) ;;
  *) echo "unknown profile: $PROFILE" >&2; exit 1 ;; esac

kubectl -n "$NAMESPACE" patch configmap "checkout-${VERSION}-config" \
  --type merge -p "{\"data\":{\"FAULT_PROFILE\":\"${PROFILE}\"}}"

# Config is read at process start, so the profile change needs a rollout.
kubectl -n "$NAMESPACE" rollout restart "deploy/checkout-${VERSION}"
kubectl -n "$NAMESPACE" rollout status "deploy/checkout-${VERSION}" --timeout=180s

mkdir -p "$LEDGER_DIR"
RECEIPT="$LEDGER_DIR/profile-$(date -u +%Y%m%dT%H%M%SZ).json"
cat > "$RECEIPT" <<JSON
{
  "event": "fault_profile_changed",
  "version": "${VERSION}",
  "profile": "${PROFILE}",
  "changed_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "actor": "operator"
}
JSON
echo "receipt: $RECEIPT"
echo "confirm the live behaviour hash:"
echo "  kubectl -n $NAMESPACE exec deploy/checkout-${VERSION} -- wget -qO- localhost:8080/version   # no shell in image; use port-forward instead"
echo "  kubectl -n $NAMESPACE port-forward deploy/checkout-${VERSION} 8080:8080 & curl -s localhost:8080/version"
