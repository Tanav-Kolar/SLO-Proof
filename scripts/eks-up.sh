#!/usr/bin/env bash
# Creates the EKS cluster from infra/eks/cluster.yaml into a pinned kubeconfig.
#
# Billable and slow: EKS control plane by the hour, two t3.medium nodes, and
# later one load balancer. Roughly 15-20 minutes. Run scripts/eks-down.sh after.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/_guard.sh

require_cmd aws eksctl kubectl

# eksctl writes the kubeconfig at the end of creation; the directory has to
# exist before then or a 20-minute create fails on the last step.
mkdir -p "$(dirname "$KUBECONFIG")"

echo "AWS identity:"
aws sts get-caller-identity --query 'Arn' --output text

if aws eks describe-cluster --name "$CLUSTER_NAME" --region "$AWS_REGION" >/dev/null 2>&1; then
  echo "cluster $CLUSTER_NAME already exists; writing kubeconfig only"
else
  echo "creating cluster $CLUSTER_NAME in $AWS_REGION (15-20 min)"
  RENDERED="$(mktemp)"
  trap 'rm -f "$RENDERED"' EXIT
  sed -e "s|__AWS_REGION__|${AWS_REGION}|g" \
      -e "s|__CLUSTER_NAME__|${CLUSTER_NAME}|g" \
      infra/eks/cluster.yaml > "$RENDERED"
  if grep -q '__[A-Z_]*__' "$RENDERED"; then
    echo "REFUSING: unsubstituted placeholders in the cluster config" >&2
    grep -o '__[A-Z_]*__' "$RENDERED" | sort -u >&2
    exit 1
  fi
  eksctl create cluster -f "$RENDERED" --kubeconfig "$KUBECONFIG"
fi

aws eks update-kubeconfig --name "$CLUSTER_NAME" --region "$AWS_REGION" --kubeconfig "$KUBECONFIG"

CTX="$(kubectl config current-context)"
echo
echo "cluster ready."
echo "  export SLOPROOF_KUBE_CONTEXT='$CTX'"
echo "next: scripts/install-gateway.sh"
kubectl get nodes
