#!/usr/bin/env bash
# Returns the demo to its starting state: ordinary traffic 100% on v1, v1
# healthy, v2 back in the regression profile and ready to fail again.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/_guard.sh
require_context

echo "== restoring fault profiles =="
scripts/set-profile.sh v1 healthy
scripts/set-profile.sh v2 checkout-regression

echo "== restoring healthy baseline routing =="
scripts/set-weights.sh 100 0

echo
echo "reset complete. Ordinary traffic is on v1; v2 is armed but receiving none."
kubectl -n "$NAMESPACE" get deploy,svc,httproute
