#!/bin/bash

set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
ROOT_DIR="$DIR/.."

cd "$ROOT_DIR"

# For non-tag github triggers, only test docker build. Do not push docker images
if [ "$REF_TYPE" != 'tag' ]; then
	make docker-buildx-build IMG="$IMG_BASE":"$TAG"
	exit 0
fi

# Push docker image to dockerhub
make docker-buildx-build-push IMG="$IMG_BASE":"$TAG" EXTRA_TAG="$IMG_BASE":latest

# Push docker image to ECR for testing
ECR_IMG_BASE="$AWS_ECR"/"$IMG_BASE"
make docker-buildx-build-push IMG="$ECR_IMG":"$TAG" EXTRA_TAG="$ECR_IMG_BASE":latest

# Push docker image to Quay with non-root user
QUAY_IMG_BASE=quay.io/"$IMG_BASE"
make docker-buildx-build-push-openshift IMG="$QUAY_IMG_BASE":"$TAG" EXTRA_TAG="$QUAY_IMG_BASE":latest
