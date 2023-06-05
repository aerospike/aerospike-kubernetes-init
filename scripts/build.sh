#!/bin/bash

set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
ROOT_DIR="$DIR/.."

cd "$ROOT_DIR"

# For non-tag github triggers, only test docker build. Do not push docker images
if [ "$REF_TYPE" != 'tag' ]; then
	make docker-buildx-build IMG="$IMG_BASE":"$BRANCH"
	exit 0
fi

# Push docker image to dockerhub
make docker-buildx-build-push IMG="$IMG_BASE":"$TAG"

# Push docker image to ECR for tegssting
ECR_IMG="$AWS_ECR"/"$IMG_BASE":"$TAG"
make docker-buildx-build-push IMG="$ECR_IMG"

# Push docker image to Quay. Here docker manifest is created separately to tag each child manifest with individual arch related tag
QUAY_IMG=quay.io/"$IMG_BASE":"$TAG"
make docker-buildx-build-push-openshift IMG="$QUAY_IMG"-amd64 PLATFORMS=linux/amd64
make docker-buildx-build-push-openshift IMG="$QUAY_IMG"-arm64 PLATFORMS=linux/arm64
docker manifest create IMG="$QUAY_IMG" IMG="$QUAY_IMG"-arm64 IMG="$QUAY_IMG"-amd64
docker manifest push IMG="$QUAY_IMG"
