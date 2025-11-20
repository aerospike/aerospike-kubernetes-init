# Aerospike Kubernetes Init

## Overview

Aerospike Kubernetes Init is a CLI utility which is used to help operator in deploying an Aerospike cluster. It provides
various functionalities such as server cold-restart, warm-restart, disk cleanup and Aerospike cluster status update. It 
runs in an init-container at the time of Aerospike cluster deployment to perform all pre-requisite steps.

## Building and quick start
### Build and push image

Run the following command with the appropriate name and version for the init image.

```shell
make docker-buildx-build-push IMG=aerospike/aerospike-kubernetes-init:2.4.0-dev2 VERSION=2.4.0-dev2
```

For using this new init image with Aerospike Kubernetes Operator, update the init image name and tag in AKO code base 
and build a new operator image.