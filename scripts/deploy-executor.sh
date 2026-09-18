#!/usr/bin/env bash
# Deploys the SLOProof recovery executor: an IAM role + Lambda (Function URL,
# no API Gateway) that patches checkout-route's weights, an EKS access entry
# mapping that role to the "sloproof-lambda-executor" k8s group, and the RBAC
# that group is scoped to (k8s/50-executor-rbac.yaml).
#
# Named distinctly from a pre-existing "sloproof-executor" k8s Role/RoleBinding
# (ServiceAccount-subject -- likely a teammate's IRSA/pod-based executor) so
# this doesn't fight over the same RBAC resource. The Lambda function and IAM
# role themselves keep the plain "sloproof-executor" name; only the k8s-side
# group/Role/RoleBinding are disambiguated.
#
# Idempotent: safe to re-run after editing infra/lambda/executor/handler.py.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/_guard.sh

require_cmd aws kubectl zip
require_context

FUNCTION_NAME="sloproof-executor"
ROLE_NAME="sloproof-executor"
K8S_GROUP="sloproof-lambda-executor"
ACCOUNT_ID="$(aws sts get-caller-identity --query Account --output text)"

echo "applying RBAC (k8s/50-executor-rbac.yaml)"
kubectl apply -f k8s/50-executor-rbac.yaml

echo "packaging handler"
RENDER="$(mktemp -d)"
trap 'rm -rf "$RENDER"' EXIT
cp infra/lambda/executor/handler.py "$RENDER/"
( cd "$RENDER" && zip -q function.zip handler.py )

echo "ensuring IAM role"
if ! aws iam get-role --role-name "$ROLE_NAME" >/dev/null 2>&1; then
  aws iam create-role --role-name "$ROLE_NAME" \
    --assume-role-policy-document file://infra/lambda/executor/trust-policy.json >/dev/null
fi

sed -e "s|__REGION__|${AWS_REGION}|g" \
    -e "s|__ACCOUNT_ID__|${ACCOUNT_ID}|g" \
    -e "s|__CLUSTER_NAME__|${CLUSTER_NAME}|g" \
    infra/lambda/executor/iam-policy.json > "$RENDER/iam-policy.json"
aws iam put-role-policy --role-name "$ROLE_NAME" --policy-name sloproof-executor-inline \
  --policy-document "file://$RENDER/iam-policy.json"

ROLE_ARN="arn:aws:iam::${ACCOUNT_ID}:role/${ROLE_NAME}"

# IAM propagation is eventually consistent; a fresh role can fail the
# create-function call below with an assume-role error for a minute or two.
echo "waiting for IAM role to propagate"
sleep 10

EXECUTOR_SHARED_SECRET="${EXECUTOR_SHARED_SECRET:-$(openssl rand -hex 24)}"
ENV_JSON="{\"Variables\":{\"CLUSTER_NAME\":\"${CLUSTER_NAME}\",\"NAMESPACE\":\"${NAMESPACE}\",\"ROUTE_NAME\":\"${ROUTE_NAME}\",\"EXECUTOR_SHARED_SECRET\":\"${EXECUTOR_SHARED_SECRET}\"}}"

echo "ensuring Lambda function"
if aws lambda get-function --function-name "$FUNCTION_NAME" >/dev/null 2>&1; then
  aws lambda update-function-code --function-name "$FUNCTION_NAME" \
    --zip-file "fileb://$RENDER/function.zip" >/dev/null
  aws lambda wait function-updated --function-name "$FUNCTION_NAME"
  aws lambda update-function-configuration --function-name "$FUNCTION_NAME" \
    --environment "$ENV_JSON" --timeout 30 >/dev/null
else
  aws lambda create-function --function-name "$FUNCTION_NAME" \
    --runtime python3.13 --handler handler.handler --role "$ROLE_ARN" \
    --zip-file "fileb://$RENDER/function.zip" \
    --environment "$ENV_JSON" --timeout 30 >/dev/null
fi
aws lambda wait function-active --function-name "$FUNCTION_NAME"

echo "ensuring Function URL (AuthType NONE -- Kibana's .webhook connector can't sign SigV4)"
if ! aws lambda get-function-url-config --function-name "$FUNCTION_NAME" >/dev/null 2>&1; then
  aws lambda create-function-url-config --function-name "$FUNCTION_NAME" --auth-type NONE >/dev/null
  aws lambda add-permission --function-name "$FUNCTION_NAME" \
    --statement-id FunctionURLAllowPublicAccess --action lambda:InvokeFunctionUrl \
    --principal "*" --function-url-auth-type NONE >/dev/null
fi
FUNCTION_URL="$(aws lambda get-function-url-config --function-name "$FUNCTION_NAME" --query FunctionUrl --output text)"

echo "ensuring EKS access entry (maps the role to k8s group ${K8S_GROUP})"
if ! aws eks describe-access-entry --cluster-name "$CLUSTER_NAME" --principal-arn "$ROLE_ARN" >/dev/null 2>&1; then
  aws eks create-access-entry --cluster-name "$CLUSTER_NAME" --principal-arn "$ROLE_ARN" \
    --type STANDARD --kubernetes-groups "$K8S_GROUP" >/dev/null
fi

echo
echo "executor URL:  $FUNCTION_URL"
echo "shared secret: (not printed -- see EXECUTOR_SHARED_SECRET in the Lambda console/env if you need it again)"
echo
echo "next: point the checkout-remediation-api Kibana connector at ${FUNCTION_URL%/}"
echo "      with header x-sloproof-executor-secret set to that secret."
