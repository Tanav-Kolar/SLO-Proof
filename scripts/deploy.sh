#!/usr/bin/env bash
# Renders and applies the checkout workloads and routing.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/_guard.sh

require_context
require_env CHECKOUT_IMAGE GATEWAY_CLASS_NAME
RUN_ID="${RUN_ID:-run-$(date -u +%Y%m%dT%H%M%SZ)}"

RENDER="$(mktemp -d)"
trap 'rm -rf "$RENDER"' EXIT

for f in k8s/*.yaml; do
  sed -e "s|__CHECKOUT_IMAGE__|${CHECKOUT_IMAGE}|g" \
      -e "s|__GATEWAY_CLASS_NAME__|${GATEWAY_CLASS_NAME}|g" \
      -e "s|__RUN_ID__|${RUN_ID}|g" \
      "$f" > "$RENDER/$(basename "$f")"
done

if grep -rq '__[A-Z_]*__' "$RENDER"; then
  echo "REFUSING: unsubstituted placeholders remain:" >&2
  grep -ro '__[A-Z_]*__' "$RENDER" | sort -u >&2
  exit 1
fi

echo "run_id: ${RUN_ID}"
echo "image:  ${CHECKOUT_IMAGE}"
kubectl apply -f "$RENDER"

for d in $(kubectl -n "$NAMESPACE" get deploy -l "$APP_LABEL" -o name); do
  kubectl -n "$NAMESPACE" rollout status "$d" --timeout=180s
done

echo
echo "Ordinary traffic starts 100% on v1 (healthy baseline)."
echo "Preflight each version directly:  curl -H 'x-sloproof-target: v2' <gateway>/checkout"
