#!/usr/bin/env bash
# Builds the checkout image. Defaults to linux/amd64 because EKS managed nodes
# are typically amd64 while the build laptop is often arm64.
set -euo pipefail
cd "$(dirname "$0")/.."

PLATFORM="${PLATFORM:-linux/amd64}"
IMAGE_TAG="${IMAGE_TAG:-$(git rev-parse --short HEAD 2>/dev/null || echo dev)}"
IMAGE_NAME="${IMAGE_NAME:-sloproof-checkout}"
LOCAL_REF="${IMAGE_NAME}:${IMAGE_TAG}"

echo "building ${LOCAL_REF} for ${PLATFORM}"
BUILD_CONTEXT="${BUILD_CONTEXT:-src/checkout}"
docker build --platform "${PLATFORM}" -t "${LOCAL_REF}" "$BUILD_CONTEXT"

echo
echo "built: ${LOCAL_REF}"
docker image inspect "${LOCAL_REF}" --format 'size: {{.Size}} bytes'
echo "next:  scripts/push-ecr.sh"
