#!/usr/bin/env bash
# Installs Envoy Gateway, which brings the Gateway API CRDs with it and
# provisions an AWS NLB for its listener Service.
#
# Chosen over the AWS Load Balancer Controller because it needs no IAM policy,
# no IRSA service account, and no separate CRD install -- one helm release. The
# load balancer is still provisioned by AWS. Move to the AWS Load Balancer
# Controller later if an ALB specifically is wanted; the HTTPRoute manifests do
# not change, only GATEWAY_CLASS_NAME.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/_guard.sh

require_cmd helm kubectl
require_context

CHART="oci://docker.io/envoyproxy/gateway-helm"
# Unpinned by default so this keeps working as versions move. Pin it with
# ENVOY_GATEWAY_VERSION once a working version is known, and record it.
VERSION_ARG=()
[[ -n "${ENVOY_GATEWAY_VERSION:-}" ]] && VERSION_ARG=(--version "$ENVOY_GATEWAY_VERSION")

helm upgrade --install envoy-gateway "$CHART" "${VERSION_ARG[@]}" \
  --namespace envoy-gateway-system --create-namespace \
  --set config.envoyGateway.provider.kubernetes.envoyService.annotations."service\.beta\.kubernetes\.io/aws-load-balancer-type"=nlb \
  --wait --timeout 10m

RESOLVED="$(helm list -n envoy-gateway-system -o json | python3 -c 'import json,sys;print(json.load(sys.stdin)[0]["chart"])')"
echo "installed: $RESOLVED"

kubectl wait --namespace envoy-gateway-system \
  --for=condition=Available deployment/envoy-gateway --timeout=300s

echo
echo "available GatewayClasses:"
kubectl get gatewayclass

mkdir -p "$LEDGER_DIR"
cat > "$LEDGER_DIR/gateway-install.json" <<JSON
{
  "event": "gateway_controller_installed",
  "chart": "$RESOLVED",
  "gateway_class_expected": "eg",
  "installed_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
JSON

echo
echo "next:"
echo "  export GATEWAY_CLASS_NAME=eg"
echo "  scripts/deploy.sh"
