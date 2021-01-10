package main

import (
	flag "github.com/spf13/pflag"
	"time"
)

// Defaults
var (
	// Don't change unless necessary
	configMapMountPath        string = "/configs"
	aerospikeConfigVolumePath string = "/etc/aerospike"
	apeConfigVolumePath       string = "/etc/aerospike-prometheus-exporter"

	// Defaults, that need to be overriden through ENV variables
	podNamespace                 string = "default"
	aerospikeHeadlessServiceName string = "aerospike"
	serviceDNSDomain             string = "cluster.local"
	nodeIDPrefix                 string = "a"

	// Dynamically populated variables
	podName                     string
	hostIP                      string
	clusterName                 string
	autoGenerateNodeIds         string = "true"
	hostNetworking              string = "false"
	hostNetworkingExternalIP    string = "false"
	nodePortServicesEnabled     string = "false"
	nodePortServicesExternalIP  string = "false"
	loadBalancerServicesEnabled string = "false"
	externalIPServicesEnabled   string = "false"

	serviceTLSOnly    string
	serviceCAFile     string
	serviceCertFile   string
	serviceKeyFile    string
	serviceTLSName    string
	serviceMutualAuth string
	serviceTLSPort    string
	servicePlainPort  string = "3000"

	heartbeatTLSOnly   string
	heartbeatCAFile    string
	heartbeatCertFile  string
	heartbeatKeyFile   string
	heartbeatTLSName   string
	heartbeatTLSPort   string
	heartbeatPlainPort string = "3002"

	fabricTLSOnly   string
	fabricCAFile    string
	fabricCertFile  string
	fabricKeyFile   string
	fabricTLSName   string
	fabricTLSPort   string
	fabricPlainPort string = "3001"

	securityEnabled string = "false"
	helmUsername    string = "helm_operator"
	helmPassword    string = "helm_operator"
	adminUsername   string = "admin"
	adminPassword   string = "admin"
	authMode        string = "internal"

	// Aerospike Prometheus Exporter
	apeEnabled string = "false"
)

const (
	dnsLookupRetryPeriod = 1 * time.Second // 1 second
	dnsLookupRetryCount  = 60              // Limit retries to 60 attempts ~ 1 min
)

// Main
func main() {
	// Command line inputs
	logLevel := flag.String("log-level", "info", "Specify logging level")
	flag.Parse()

	// Initialize logging
	sugarLogger := initializeLogging(*logLevel)
	defer sugarLogger.Sync()

	sugarLogger.Info("Welcome to Aerospike Init Container.")

	// Initialize variables
	sugarLogger.Info("Initializing variables.")
	initializeVars()

	// Intialize config volume
	sugarLogger.Info("Initializing config volume.")
	initializeConfigVolume(configMapMountPath, aerospikeConfigVolumePath, apeConfigVolumePath)

	// Find peers and prepare aerospike config file
	sugarLogger.Info("Finding peers.")
	peers, err := getPeers(dnsLookupRetryCount, dnsLookupRetryPeriod)
	if err != nil {
		sugarLogger.Fatalf("Error while finding peers: %v.", err)
	}

	// Prepare aerospike configuration file
	sugarLogger.Info("Preparing Aerospike configuration file.")
	err = prepareAerospikeConfig(peers)
	if err != nil {
		sugarLogger.Fatalf("Error while preparing aerospike config: %v.", err)
	}

	// Prepare exporter configuration file
	err = prepareAerospikePrometheusExporterConfig()
	if err != nil {
		sugarLogger.Fatalf("Error while preparing exporter config: %v.", err)
	}

	sugarLogger.Info("Init container successfully executed.")
}
