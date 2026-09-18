#!/usr/bin/env bash
# Tears the demo down. Load balancers are released BEFORE the cluster is
# deleted: deleting a cluster out from under a Service of type LoadBalancer
# leaves an orphaned, still-billing NLB behind.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/_guard.sh

require_cmd aws eksctl kubectl
require_context

echo "== removing application and gateway (releases the NLB) =="
kubectl delete -f k8s/ --ignore-not-found --wait=true || true
helm uninstall envoy-gateway -n envoy-gateway-system --wait 2>/dev/null || true

echo "== waiting for load balancers to be released =="
for _ in $(seq 1 30); do
  REMAINING="$(kubectl get svc -A -o json \
    | python3 -c 'import json,sys;print(sum(1 for i in json.load(sys.stdin)["items"] if i["spec"].get("type")=="LoadBalancer"))')"
  [[ "$REMAINING" == "0" ]] && break
  echo "  $REMAINING LoadBalancer service(s) still present"
  sleep 10
done

echo "== deleting cluster =="
eksctl delete cluster --name "$CLUSTER_NAME" --region "$AWS_REGION" --wait

echo
echo "confirm nothing is still billing:"
echo "  aws elbv2 describe-load-balancers --region $AWS_REGION --query 'LoadBalancers[].LoadBalancerName'"
echo "  aws ec2 describe-instances --region $AWS_REGION --filters Name=tag:project,Values=sloproof --query 'Reservations[].Instances[].State.Name'"
