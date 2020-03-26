#! /bin/bash

# ------------------------------------------------------------------------------
# Copyright 2012-2020 Aerospike, Inc.
#
# Portions may be licensed to Aerospike, Inc. under one or more contributor
# license agreements.
#
# Licensed under the Apache License, Version 2.0 (the "License"); you may not
# use this file except in compliance with the License. You may obtain a copy of
# the License at http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
# WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
# License for the specific language governing permissions and limitations under
# the License.
# ------------------------------------------------------------------------------


CONFIG_VOLUME="/etc/aerospike"
APE_CONFIG_VOLUME="/etc/aerospike-prometheus-exporter"
NAMESPACE=${POD_NAMESPACE:-default}
K8_SERVICE=${SERVICE:-aerospike}
SERVICE_DNS_DOMAIN=${SERVICE_DNS_DOMAIN:-cluster.local}

for i in "$@"
do
case $i in
    -c=*|--config=*)
    CONFIG_VOLUME="${i#*=}"
    shift
    ;;
    *)
    # unknown option
    ;;
esac
done

echo installing aerospike.conf into "${CONFIG_VOLUME}"
mkdir -p "${CONFIG_VOLUME}"

# chown -R aerospike:aerospike "${CONFIG_VOLUME}"

# If there's an aerospike config template in configmap, pull it.
# --**-- Used By Aerospike Helm Chart, DO NOT CHANGE --**--
if [ -f /configs/aerospike.template.conf ]; then
    cp /configs/aerospike.template.conf "${CONFIG_VOLUME}"/
fi

# If there's a feature-key-file in configmap, pull it.
# --**-- Used By Aerospike Helm Chart, DO NOT CHANGE --**--
if [ -f /configs/features.conf ]; then
    cp /configs/features.conf "${CONFIG_VOLUME}"/
fi

# Util script for aerospike-enterprise helm chart.
# --**-- Used By Aerospike Helm Chart, DO NOT CHANGE --**--
if [ -f /aerospike.sh ]; then
    cp /aerospike.sh "${CONFIG_VOLUME}"/
fi

# Prefer util script from configmap. Overwrite the original one from init container image.
# --**-- Used By Aerospike Helm Chart, DO NOT CHANGE --**--
if [ -f /configs/aerospike.sh ]; then
    cp /configs/aerospike.sh "${CONFIG_VOLUME}"/
fi

# Prefer on-start.sh script from configmap. Overwrite the original one from init container image.
# --**-- Used By Aerospike Helm Chart, DO NOT CHANGE --**--
if [ -f /configs/on-start.sh ]; then
    cp /configs/on-start.sh /on-start.sh
    chmod +x /on-start.sh
fi

# If there's an aerospike-prometheus-exporter config template in configmap, pull it.
# --**-- Used By Aerospike Helm Chart, DO NOT CHANGE --**--
if [ -f /configs/ape.toml.template ]; then
    cp /configs/ape.toml.template "${APE_CONFIG_VOLUME}"/
fi

/peer-finder -on-start=/on-start.sh -service=${K8_SERVICE} -ns=${NAMESPACE} -domain=${SERVICE_DNS_DOMAIN}
