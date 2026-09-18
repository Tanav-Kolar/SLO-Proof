#!/usr/bin/env bash
# Pushes the image to ECR and prints the immutable digest reference.
#
# Reads AWS config from the environment / your AWS profile. It never reads or
# writes credentials itself. Supply them with `aws configure --profile sloproof`
# or exported AWS_* variables, or an SSO session.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/_guard.sh

require_cmd aws docker
require_env AWS_REGION
# Derived rather than hand-set, so the registry can never point at another account.
AWS_ACCOUNT_ID="${AWS_ACCOUNT_ID:-$(aws sts get-caller-identity --query Account --output text)}"

IMAGE_TAG="${IMAGE_TAG:-$(git rev-parse --short HEAD 2>/dev/null || echo dev)}"
IMAGE_NAME="${IMAGE_NAME:-sloproof-checkout}"
LOCAL_REF="${IMAGE_NAME}:${IMAGE_TAG}"
REGISTRY="${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com"
REMOTE_REF="${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}"

echo "identity in use:"
aws sts get-caller-identity --output text --query 'Arn'

aws ecr describe-repositories --repository-names "${IMAGE_NAME}" --region "${AWS_REGION}" >/dev/null 2>&1 \
  || aws ecr create-repository --repository-name "${IMAGE_NAME}" --region "${AWS_REGION}" \
       --image-scanning-configuration scanOnPush=true >/dev/null

aws ecr get-login-password --region "${AWS_REGION}" | docker login --username AWS --password-stdin "${REGISTRY}"

docker tag "${LOCAL_REF}" "${REMOTE_REF}"
docker push "${REMOTE_REF}"

DIGEST="$(aws ecr describe-images --repository-name "${IMAGE_NAME}" --image-ids imageTag="${IMAGE_TAG}" \
  --region "${AWS_REGION}" --query 'imageDetails[0].imageDigest' --output text)"

echo
echo "Pin deployments to the digest, not the tag:"
echo "export CHECKOUT_IMAGE=${REGISTRY}/${IMAGE_NAME}@${DIGEST}"
