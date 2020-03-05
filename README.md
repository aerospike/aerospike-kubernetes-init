# Aerospike Kubernetes InitContainer Image


### Steps to build

1. Clone this repository,
```
git clone https://github.com/aerospike/aerospike-kubernetes-init.git
cd aerospike-kubernetes-init
```

2. Pull Submodules,
```
git submodule update --init
```

3. Build docker image,
```
docker build . -t aerospike/aerospike-kubernetes-init
```
