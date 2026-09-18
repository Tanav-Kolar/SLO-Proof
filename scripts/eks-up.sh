#!/usr/bin/env bash
# Creates the EKS cluster from infra/eks/cluster.yaml into a pinned kubeconfig.
#
# Billable and slow: EKS control plane by the hour, two t3.medium nodes, and
# later one load balancer. Roughly 15-20 minutes. Run scripts/eks-down.sh after.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/_guard.sh

require_cmd aws eksctl kubectl

echo "AWS identity:"
aws sts get-caller-identity --query 'Arn' --output text

if aws eks describe-cluster --name "$CLUSTER_NAME" --region "$AWS_REGION" >/dev/null 2>&1; then
  echo "cluster $CLUSTER_NAME already exists; writing kubeconfig only"
else
  echo "creating cluster $CLUSTER_NAME in $AWS_REGION (15-20 min)"
  eksctl create cluster -f infra/eks/cluster.yaml --kubeconfig "$KUBECONFIG"
fi

mkdir -p "$(dirname "$KUBECONFIG")"
aws eks update-kubeconfig --name "$CLUSTER_NAME" --region "$AWS_REGION" --kubeconfig "$KUBECONFIG"

CTX="$(kubectl config current-context)"
echo
echo "cluster ready."
echo "  export SLOPROOF_KUBE_CONTEXT='$CTX'"
echo "next: scripts/install-gateway.sh"
kubectl get nodes
