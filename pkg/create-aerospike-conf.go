package pkg

import (
	"bufio"
	_ "embed"
	"fmt"
	"os"
	"strconv"
	"strings"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"gopkg.in/yaml.v3"
)

//go:embed fips-confs/openssl.cnf
var opensslCnf []byte

//go:embed fips-confs/fips.cnf
var fipsCnf []byte

const (
	aerospikeTemplateConf = "/etc/aerospike/aerospike.template.yaml"
	aerospikeYAML         = "/etc/aerospike/aerospike.yaml"
	aerospikeConf         = "/etc/aerospike/aerospike.conf"
	peers                 = "/etc/aerospike/peers"
	aerospikeOpensslCnf   = "/etc/aerospike/openssl.cnf"
	aerospikeFipsCnf      = "/etc/aerospike/fips.cnf"
	access                = "access"
	alternateAccess       = "alternate-access"
	tlsAccess             = "tls-access"
	tlsAlternateAccess    = "tls-alternate-access"

	// minServerNativeYAMLVersion is the minimum Aerospike server version that
	// supports the native YAML config format (aerospike.yaml).
	minServerNativeYAMLVersion = "8.1.1"
)

func (initp *InitParams) createAerospikeConf() error {
	data, err := os.ReadFile(aerospikeTemplateConf)
	if err != nil {
		return err
	}

	var config map[string]interface{}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse template YAML: %w", err)
	}

	// Update node-id in service section
	if svc, ok := config["service"].(map[string]interface{}); ok {
		if nodeID, ok := svc["node-id"].(string); ok && nodeID == "ENV_NODE_ID" {
			svc["node-id"] = initp.nodeID
		}
	}

	network := getOrCreateSubMap(config, "network")

	if initp.networkInfo.servicePort != 0 {
		initp.substituteEndpoint(
			network, initp.networkInfo.networkPolicy.AccessType, access,
			initp.networkInfo.configureAccessIP, initp.networkInfo.customAccessNetworkIPs)
		initp.substituteEndpoint(
			network, initp.networkInfo.networkPolicy.AlternateAccessType, alternateAccess,
			initp.networkInfo.configuredAlterAccessIP, initp.networkInfo.customAlternateAccessNetworkIPs)
	}

	if initp.networkInfo.serviceTLSPort != 0 {
		initp.substituteEndpoint(
			network, initp.networkInfo.networkPolicy.TLSAccessType, tlsAccess,
			initp.networkInfo.configureAccessIP, initp.networkInfo.customTLSAccessNetworkIPs)
		initp.substituteEndpoint(
			network, initp.networkInfo.networkPolicy.TLSAlternateAccessType, tlsAlternateAccess,
			initp.networkInfo.configuredAlterAccessIP, initp.networkInfo.customTLSAlternateAccessNetworkIPs)
	}

	fabric := getOrCreateSubMap(network, "fabric")

	if initp.networkInfo.fabricPort != 0 &&
		initp.networkInfo.networkPolicy.FabricType == asdbv1.AerospikeNetworkTypeCustomInterface {
		fabric["address"] = toInterfaceSlice(initp.networkInfo.customFabricNetworkIPs)
	}

	if initp.networkInfo.fabricTLSPort != 0 &&
		initp.networkInfo.networkPolicy.TLSFabricType == asdbv1.AerospikeNetworkTypeCustomInterface {
		fabric["tls-address"] = toInterfaceSlice(initp.networkInfo.customTLSFabricNetworkIPs)
	}

	heartbeat := getOrCreateSubMap(network, "heartbeat")

	readFile, err := os.Open(peers)
	if err != nil {
		return err
	}

	defer readFile.Close()

	fileScanner := bufio.NewScanner(readFile)

	// Update mesh seeds in the configuration
	for fileScanner.Scan() {
		peer := fileScanner.Text()
		if strings.Contains(peer, initp.podName) {
			continue
		}

		if initp.networkInfo.heartBeatPort != 0 {
			appendToStringList(heartbeat, "mesh-seed-address-ports",
				fmt.Sprintf("%s:%d", peer, initp.networkInfo.heartBeatPort))
		}

		if initp.networkInfo.heartBeatTLSPort != 0 {
			appendToStringList(heartbeat, "tls-mesh-seed-address-ports",
				fmt.Sprintf("%s:%d", peer, initp.networkInfo.heartBeatTLSPort))
		}
	}

	// If host networking is used, force heartbeat and fabric to advertise the
	// network interface bound to the K8s node's host network.
	if initp.networkInfo.hostNetwork {
		if initp.networkInfo.heartBeatPort != 0 {
			heartbeat["address"] = initp.networkInfo.podIP
		}

		if initp.networkInfo.heartBeatTLSPort != 0 {
			heartbeat["tls-address"] = initp.networkInfo.podIP
		}

		if initp.networkInfo.fabricPort != 0 {
			fabric["address"] = initp.networkInfo.podIP
		}

		if initp.networkInfo.fabricTLSPort != 0 {
			fabric["tls-address"] = initp.networkInfo.podIP
		}
	}

	// Update namespace sections with rack-id from pod annotation
	initp.updateNamespaceRackID(config)

	out, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config YAML: %w", err)
	}

	// Remove LDAP escape sequences if any
	outStr := strings.ReplaceAll(string(out), "$${_DNE}{un}", "${un}")
	outStr = strings.ReplaceAll(outStr, "$${_DNE}{dn}", "${dn}")

	if err := os.Remove(aerospikeTemplateConf); err != nil {
		return err
	}

	initp.logger.Info(fmt.Sprintf("Generated aerospike YAML config %s: \n%s", aerospikeYAML, outStr))

	// For servers >= 8.1.1 the native YAML file is used directly.
	// For older servers, convert the YAML to the legacy .conf format.
	serverVersion, err := asdbv1.GetImageVersion(initp.aeroCluster.Spec.Image)
	if err != nil {
		return fmt.Errorf("failed to get server version from image %q: %w", initp.aeroCluster.Spec.Image, err)
	}

	if serverSupportsNativeYAML(serverVersion) {
		if err = os.WriteFile(aerospikeYAML, []byte(outStr), 0644); err != nil { //nolint:gocritic,gosec // file permission
			return err
		}

		initp.logger.Info(fmt.Sprintf("Server version %s supports native YAML, using %s", serverVersion, aerospikeYAML))
		return nil
	}

	initp.logger.Info(fmt.Sprintf("Server version %s does not support native YAML, converting to %s", serverVersion, aerospikeConf))

	confStr, err := nativeYAMLToConf([]byte(outStr), initp.logger)
	if err != nil {
		return fmt.Errorf("failed to convert native YAML to conf: %w", err)
	}

	if err = os.WriteFile(aerospikeConf, []byte(confStr), 0644); err != nil { //nolint:gocritic,gosec // file permission
		return fmt.Errorf("failed to write %s: %w", aerospikeConf, err)
	}

	// aerospikeYAML is intentionally kept alongside aerospikeConf so that
	// manageVolumesAndUpdateStatus can always read the YAML for the pod annotation.
	initp.logger.Info(fmt.Sprintf("Converted native YAML to legacy conf: %s", aerospikeConf))

	return nil
}

func (initp *InitParams) createAerospikeOpensslAndFipsCnf() error {
	initp.logger.Info("Creating openssl.cnf and fips.cnf files")
	//nolint:gocritic,gosec // file permission
	if err := os.WriteFile(aerospikeOpensslCnf, opensslCnf, 0644); err != nil {
		return fmt.Errorf("failed to write openssl.cnf: %v", err)
	}

	//nolint:gocritic,gosec // file permission
	if err := os.WriteFile(aerospikeFipsCnf, fipsCnf, 0644); err != nil {
		return fmt.Errorf("failed to write fips.cnf: %v", err)
	}

	initp.logger.Info("Successfully created openssl.cnf and fips.cnf files")

	return nil
}

// updateNamespaceRackID replaces rack-id field in each namespace section of the config map
// using the value from pod annotation "aerospike.com/override-rack-id".
// Only proceeds if EnableRackIDOverride is set to true in AerospikeCluster CR spec.
// Only replaces rack-id if it already exists in the namespace, does not add if missing.
func (initp *InitParams) updateNamespaceRackID(config map[string]interface{}) {
	if !asdbv1.GetBool(initp.aeroCluster.Spec.EnableRackIDOverride) {
		initp.logger.Info("EnableRackIDOverride not set, skipping rack-id update")
		return
	}

	initp.logger.Info("Updating namespace sections with override rack-id", "rack-id", initp.overrideRackID)

	namespaces, ok := config["namespaces"].(map[string]interface{})
	if !ok {
		return
	}

	for _, nsVal := range namespaces {
		ns, ok := nsVal.(map[string]interface{})
		if !ok {
			continue
		}

		if _, exists := ns["rack-id"]; exists {
			ns["rack-id"] = initp.overrideRackID
		}
	}
}

// substituteEndpoint computes the access endpoints based on network policy and writes
// them directly into the network.service section of the config map.
// The computed values are also stored on networkInfo to update the node status later.
func (initp *InitParams) substituteEndpoint(network map[string]interface{}, networkType asdbv1.AerospikeNetworkType,
	addressType, configuredIP string, interfaceIPs []string) {
	var (
		accessAddress []string
		accessPort    int32
	)

	servicePort := initp.networkInfo.servicePort
	mappedServicePort := initp.networkInfo.mappedServicePort

	if addressType == tlsAccess || addressType == tlsAlternateAccess {
		servicePort = initp.networkInfo.serviceTLSPort
		mappedServicePort = initp.networkInfo.mappedServiceTLSPort
	}

	//nolint:exhaustive // fallback to default
	switch networkType {
	case asdbv1.AerospikeNetworkTypePod:
		accessAddress = append(accessAddress, initp.networkInfo.podIP)
		accessPort = servicePort

	case asdbv1.AerospikeNetworkTypeHostInternal:
		accessAddress = append(accessAddress, initp.networkInfo.internalIP)
		accessPort = mappedServicePort

	case asdbv1.AerospikeNetworkTypeHostExternal:
		accessAddress = append(accessAddress, initp.networkInfo.externalIP)
		accessPort = mappedServicePort

	case asdbv1.AerospikeNetworkTypeConfigured:
		if configuredIP == "" {
			initp.logger.Error(fmt.Errorf("configureIP missing"),
				fmt.Sprintf("Please set %s and %s node label to use NetworkPolicy configuredIP for "+
					"access and alternateAccess addresses", configuredAccessIPLabel, configuredAlternateAccessIPLabel))
			os.Exit(1)
		}

		accessAddress = append(accessAddress, configuredIP)
		accessPort = mappedServicePort

	case asdbv1.AerospikeNetworkTypeCustomInterface:
		accessAddress = interfaceIPs
		accessPort = servicePort

	default:
		accessAddress = append(accessAddress, initp.networkInfo.podIP)
		accessPort = servicePort
	}

	// Store computed address to update the status later.
	switch addressType {
	case access:
		initp.networkInfo.globalAddressesAndPorts.globalAccessAddress = accessAddress
		initp.networkInfo.globalAddressesAndPorts.globalAccessPort = accessPort

	case alternateAccess:
		initp.networkInfo.globalAddressesAndPorts.globalAlternateAccessAddress = accessAddress
		initp.networkInfo.globalAddressesAndPorts.globalAlternateAccessPort = accessPort

	case tlsAccess:
		initp.networkInfo.globalAddressesAndPorts.globalTLSAccessAddress = accessAddress
		initp.networkInfo.globalAddressesAndPorts.globalTLSAccessPort = accessPort

	case tlsAlternateAccess:
		initp.networkInfo.globalAddressesAndPorts.globalTLSAlternateAccessAddress = accessAddress
		initp.networkInfo.globalAddressesAndPorts.globalTLSAlternateAccessPort = accessPort
	}

	// Write computed addresses and port into the YAML config map.
	svc := getOrCreateSubMap(network, "service")
	svc[addressType+"-addresses"] = toInterfaceSlice(accessAddress)
	svc[addressType+"-port"] = int(accessPort)
}

func getOrCreateSubMap(parent map[string]interface{}, key string) map[string]interface{} {
	if sub, ok := parent[key].(map[string]interface{}); ok {
		return sub
	}

	sub := map[string]interface{}{}
	parent[key] = sub

	return sub
}

func toInterfaceSlice(ss []string) []interface{} {
	out := make([]interface{}, len(ss))
	for i, s := range ss {
		out[i] = s
	}

	return out
}

func appendToStringList(m map[string]interface{}, key, value string) {
	if existing, ok := m[key].([]interface{}); ok {
		m[key] = append(existing, value)
	} else {
		m[key] = []interface{}{value}
	}
}

// serverSupportsNativeYAML returns true when the given server version is at
// least minServerNativeYAMLVersion (8.1.1). Only the major.minor.patch
// components are compared; the build suffix (e.g. the fourth component) is
// ignored.
func serverSupportsNativeYAML(serverVersion string) bool {
	return compareVersions(serverVersion, minServerNativeYAMLVersion) >= 0
}

// compareVersions compares two dot-separated version strings by their first
// three numeric components (major.minor.patch). It returns -1, 0, or 1 when
// a is less than, equal to, or greater than b respectively.
func compareVersions(a, b string) int {
	aParts := strings.SplitN(a, ".", 4)
	bParts := strings.SplitN(b, ".", 4)

	for i := range 3 {
		var aNum, bNum int

		if i < len(aParts) {
			aNum, _ = strconv.Atoi(aParts[i])
		}

		if i < len(bParts) {
			bNum, _ = strconv.Atoi(bParts[i])
		}

		if aNum < bNum {
			return -1
		}

		if aNum > bNum {
			return 1
		}
	}

	return 0
}
