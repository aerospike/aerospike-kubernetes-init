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

# This script writes out an aerospike config using a list of newline seperated
# peer DNS names it accepts through stdin.

# /etc/aerospike is assumed to be a shared volume so we can modify aerospike.conf as required


set -x
set -e

CFG=/etc/aerospike/aerospike.template.conf
HB_PORT=${HB_PORT:-3002}

# Set cluster-name if not set
# --**-- Used By Aerospike Helm Chart, DO NOT CHANGE --**--
if [ ! -z $CLUSTER_NAME ] && [ "$CLUSTER_NAME" != "" ]
then
	if ! grep -q "cluster-name" ${CFG}
	then
		sed -i "/service[[:blank:]]*{/{p;s/.*/1/;H;g;/^\(\n1\)\{1\}$/s//\tcluster-name $CLUSTER_NAME/p;d}" ${CFG}
	else
		printf "cluster-name is already set! \n"
	fi
fi

# Auto generate Node IDs and add to config
# --**-- Used By Aerospike Helm Chart, DO NOT CHANGE --**--
if [ "$AUTO_GENERATE_NODE_IDS" = true ]
then
    if ! grep -q "node-id" ${CFG}
    then
        INDEX=${POD_NAME##*-}
        sed -i "/service[[:blank:]]*{/{p;s/.*/1/;H;g;/^\(\n1\)\{1\}$/s//\tnode-id a$INDEX/p;d}" ${CFG}
    else
        printf "AUTO_GENERATE_NODE_IDS is true but node-id is already configured! \n"
    fi
fi

# If node port services are enabled, get instance IP and set as alternate-access-address and nodePort as alternate-access-port
# --**-- Used By Aerospike Helm Chart, DO NOT CHANGE --**--
if [ "$ENABLE_NODE_PORT_SERVICES" = true ]
then
	set +e
	KUBE_TOKEN=$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)
	ret=$(curl --write-out "%{http_code}\n" --silent --output /dev/null -sSk -H "Authorization: Bearer $KUBE_TOKEN" https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_PORT_443_TCP_PORT/api/v1/namespaces/$POD_NAMESPACE/services/nodeport-$POD_NAME)
	set -e
	if [ $ret = 200 ]
	then
		NODEPORT=$(curl -sSk -H "Authorization: Bearer $KUBE_TOKEN" https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_PORT_443_TCP_PORT/api/v1/namespaces/$POD_NAMESPACE/services/nodeport-$POD_NAME | jq -r '.spec.ports' | grep nodePort | awk '{print $2}')

		if [ "$PLATFORM" = "gke" ]
		then
			set +e
			ret2=$(curl --write-out "%{http_code}\n" --silent --output /dev/null -H "Metadata-Flavor: Google" http://169.254.169.254/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip)
			set -e
			if [ $ret2 = 200 ]
			then
				EXT_IP=$(curl -H "Metadata-Flavor: Google" http://169.254.169.254/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip)
			fi
		elif [ "$PLATFORM" = "eks" ]
		then
			set +e
			ret2=$(curl --write-out "%{http_code}\n" --silent --output /dev/null http://169.254.169.254/latest/meta-data/public-ipv4)
			set -e
			if [ $ret2 = 200 ]
			then
				EXT_IP=$(curl http://169.254.169.254/latest/meta-data/public-ipv4)
			fi
		fi

		# If cannot get external IP set to host IP
		if [ -z $EXT_IP ] || [ "$EXT_IP" = "" ]
		then
			EXT_IP=$HOST_IP
		fi

		if [ ! -z $EXT_IP ] && [ "$EXT_IP" != "" ] && [ ! -z $NODEPORT ] && [ "$NODEPORT" != "" ]
		then
			echo "External IP:$EXT_IP"
			sed -i "/service[[:blank:]]*{/{p;s/.*/1/;H;g;/^\(\n1\)\{2\}$/s//\t\talternate-access-address ${EXT_IP}/p;d}" ${CFG}
			sed -i "/service[[:blank:]]*{/{p;s/.*/1/;H;g;/^\(\n1\)\{2\}$/s//\t\talternate-access-port ${NODEPORT}/p;d}" ${CFG}
		fi
	fi
fi

# If loadbalancer services are enabled, get loadbalancer IP and Port and set as alternate-access-address and alternate-access-port
# --**-- Used By Aerospike Helm Chart, DO NOT CHANGE --**--
if [ "$ENABLE_LOADBALANCER_SERVICES" = true ]
then
	set +e
	KUBE_TOKEN=$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)
	ret=$(curl --write-out "%{http_code}\n" --silent --output /dev/null -sSk -H "Authorization: Bearer $KUBE_TOKEN" https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_PORT_443_TCP_PORT/api/v1/namespaces/$POD_NAMESPACE/services/loadbalancer-$POD_NAME)
	set -e
	if [ $ret = 200 ]
	then
		LBPORT=$(curl -sSk -H "Authorization: Bearer $KUBE_TOKEN" https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_PORT_443_TCP_PORT/api/v1/namespaces/$POD_NAMESPACE/services/loadbalancer-$POD_NAME | jq -r '.spec.ports' | grep port | awk '{print $2}')
		LBPORT=$(echo "$LBPORT" | tr -d ',')
		EXT_IP=$(curl -sSk -H "Authorization: Bearer $KUBE_TOKEN" https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_PORT_443_TCP_PORT/api/v1/namespaces/$POD_NAMESPACE/services/loadbalancer-$POD_NAME | jq .status.loadBalancer.ingress | grep "ip" | awk '{print $2}')
		EXT_IP=$(echo "$EXT_IP" | tr -d '"')
		if [ ! -z $EXT_IP ] && [ "$EXT_IP" != "" ] && [ ! -z $LBPORT ] && [ "$LBPORT" != "" ]
		then
			echo "External IP:$EXT_IP"
			sed -i "/service[[:blank:]]*{/{p;s/.*/1/;H;g;/^\(\n1\)\{2\}$/s//\t\talternate-access-address ${EXT_IP}/p;d}" ${CFG}
			sed -i "/service[[:blank:]]*{/{p;s/.*/1/;H;g;/^\(\n1\)\{2\}$/s//\t\talternate-access-port ${LBPORT}/p;d}" ${CFG}
		fi
	fi
fi

# External IP Services
# --**-- Used By Aerospike Helm Chart, DO NOT CHANGE --**--
if [ "$ENABLE_EXT_IP_SERVICES" = true ]
then
	set +e
	KUBE_TOKEN=$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)
	ret=$(curl --write-out "%{http_code}\n" --silent --output /dev/null -sSk -H "Authorization: Bearer $KUBE_TOKEN" https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_PORT_443_TCP_PORT/api/v1/namespaces/$POD_NAMESPACE/services/extip-$POD_NAME)
	set -e
	if [ $ret = 200 ]
	then
		EXT_IP_PORT=$(curl -sSk -H "Authorization: Bearer $KUBE_TOKEN" https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_PORT_443_TCP_PORT/api/v1/namespaces/$POD_NAMESPACE/services/extip-$POD_NAME | jq -r '.spec.ports' | grep port | awk '{print $2}')
		EXT_IP_PORT=$(echo "$EXT_IP_PORT" | tr -d ',')
		EXT_IP=$(curl -sSk -H "Authorization: Bearer $KUBE_TOKEN" https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_PORT_443_TCP_PORT/api/v1/namespaces/$POD_NAMESPACE/services/extip-$POD_NAME | jq .spec.externalIPs | grep -oE "\b([0-9]{1,3}\.){3}[0-9]{1,3}\b")
		if [ ! -z $EXT_IP ] && [ "$EXT_IP" != "" ] && [ ! -z $EXT_IP_PORT ] && [ "$EXT_IP_PORT" != "" ]
		then
			echo "External IP:$EXT_IP"
			sed -i "/service[[:blank:]]*{/{p;s/.*/1/;H;g;/^\(\n1\)\{2\}$/s//\t\talternate-access-address ${EXT_IP}/p;d}" ${CFG}
			sed -i "/service[[:blank:]]*{/{p;s/.*/1/;H;g;/^\(\n1\)\{2\}$/s//\t\talternate-access-port ${EXT_IP_PORT}/p;d}" ${CFG}
		fi
	fi
fi

# For GKE/EKS assign external IP to alternate-access-address
# if hostnetworking is enabled
# --**-- Used By Aerospike Helm Chart, DO NOT CHANGE --**--
if [ "$HOST_NETWORK" = true ]
then
	if [ "$PLATFORM" = "gke" ]
	then
		set +e
		ret=$(curl --write-out "%{http_code}\n" --silent --output /dev/null -H "Metadata-Flavor: Google" http://169.254.169.254/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip)
		set -e
		if [ $ret = 200 ]
		then
			EXT_IP=$(curl -H "Metadata-Flavor: Google" http://169.254.169.254/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip)
			if [ ! -z $EXT_IP ] && [ "$EXT_IP" != "" ]
			then
				echo "External IP:$EXT_IP"
				sed -i "/service[[:blank:]]*{/{p;s/.*/1/;H;g;/^\(\n1\)\{2\}$/s//\t\talternate-access-address ${EXT_IP}/p;d}" ${CFG}
			fi
		fi
	elif [ "$PLATFORM" = "eks" ]
	then
		set +e
		ret=$(curl --write-out "%{http_code}\n" --silent --output /dev/null http://169.254.169.254/latest/meta-data/public-ipv4)
		set -e
		if [ $ret = 200 ]
		then
			EXT_IP=$(curl http://169.254.169.254/latest/meta-data/public-ipv4)
			if [ ! -z $EXT_IP ] && [ "$EXT_IP" != "" ]
			then
				echo "External IP: $EXT_IP"
				sed -i "/service[[:blank:]]*{/{p;s/.*/1/;H;g;/^\(\n1\)\{2\}$/s//\t\talternate-access-address ${EXT_IP}/p;d}" ${CFG}
			fi
		fi
	fi
fi

function join {
    local IFS="$1"; shift; echo "$*";
}

HOSTNAME=$(hostname)
# Parse out cluster name, formatted as: petset_name-index
IFS='-' read -ra ADDR <<< "$(hostname)"
CLUSTER_NAME="${ADDR[0]}"

while read -ra LINE; do
    if [[ "${LINE}" == *"${HOSTNAME}"* ]]; then
        MY_NAME=$LINE
    fi
    PEERS=("${PEERS[@]}" $LINE)
done

for PEER in "${PEERS[@]}"; do
	sed -i -e "/mesh-seed-placeholder/a \\\t\tmesh-seed-address-port ${PEER} ${HB_PORT}" ${CFG}
done


# don't need a restart, we're just writing the conf in case there's an
# unexpected restart on the node.
