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
make docker-buildx-build-push IMG="$IMG_BASE":"$TAG"

# Push docker image to ECR for testing
ECR_IMG="$AWS_ECR"/"$IMG_BASE":"$TAG"
make docker-buildx-build-push IMG="$ECR_IMG"

# Push docker image to Quay with non-root user
QUAY_IMG=quay.io/"$IMG_BASE":"$TAG"
make docker-buildx-build-push-openshift IMG="$QUAY_IMG"
