package main

import (
	"os"
	"strconv"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// initializeVars initializes variables based on input via init container env variables.
// Global variables used here:
// podNamespace,
// aerospikeHeadlessServiceName,
// serviceDNSDomain,
// podName,
// hostIP,
// clusterName,
// autoGenerateNodeIds,
// hostNetworking,
// hostNetworkingExternalIP,
// nodePortServicesEnabled,
// nodePortServicesExternalIP,
// loadBalancerServicesEnabled,
// externalIPServicesEnabled,
// heartbeatPlainPort,
// servicePlainPort,
// fabricPlainPort,
// nodeIDPrefix,
// serviceTLSOnly,
// serviceCAFile,
// serviceCertFile,
// serviceKeyFile,
// serviceTLSName,
// serviceMutualAuth,
// serviceTLSPort,
// heartbeatTLSOnly,
// heartbeatCAFile,
// heartbeatCertFile,
// heartbeatKeyFile,
// heartbeatTLSName,
// heartbeatTLSPort,
// fabricTLSOnly,
// fabricCAFile,
// fabricCertFile,
// fabricKeyFile,
// fabricTLSName,
// fabricTLSPort
func initializeVars() {
	// pod namespace
	ns, ok := os.LookupEnv("POD_NAMESPACE")
	if ok {
		podNamespace = ns
	}

	// aerospike headless service
	service, ok := os.LookupEnv("SERVICE")
	if ok {
		aerospikeHeadlessServiceName = service
	}

	// cluster service dns domain
	domain, ok := os.LookupEnv("SERVICE_DNS_DOMAIN")
	if ok {
		serviceDNSDomain = domain
	}

	// pod name
	pod, ok := os.LookupEnv("POD_NAME")
	if ok {
		podName = pod
	}

	// If POD_NAME env variable doesn't exists, use hostname.
	if podName == "" {
		zap.S().Warn("POD_NAME env variable not set, using hostname instead.")
		hostname, err := os.Hostname()
		if err != nil {
			zap.S().Fatalf("Failed to get hostname: %v.", err)
		} else {
			podName = hostname
		}
	}

	// host IP
	ip, ok := os.LookupEnv("HOST_IP")
	if ok {
		hostIP = ip
	}

	// cluster name
	cn, ok := os.LookupEnv("CLUSTER_NAME")
	if ok {
		clusterName = cn
	}

	// auto generate node flag
	autogennodeid, ok := os.LookupEnv("AUTO_GENERATE_NODE_IDS")
	if ok {
		autoGenerateNodeIds = autogennodeid
	}

	// host network flag
	hostnetwork, ok := os.LookupEnv("HOST_NETWORKING")
	if ok {
		hostNetworking = hostnetwork
	}

	hostnetworkextip, ok := os.LookupEnv("HOST_NETWORKING_EXTERNAL_IP")
	if ok {
		hostNetworkingExternalIP = hostnetworkextip
	}

	// enable node port services flag
	nodeportservice, ok := os.LookupEnv("ENABLE_NODE_PORT_SERVICES")
	if ok {
		nodePortServicesEnabled = nodeportservice
	}

	nodeportserviceextip, ok := os.LookupEnv("ENABLE_NODE_PORT_SERVICES_EXTERNAL_IP")
	if ok {
		nodePortServicesExternalIP = nodeportserviceextip
	}

	// enable load balancer services flag
	loadbalancerservice, ok := os.LookupEnv("ENABLE_LOADBALANCER_SERVICES")
	if ok {
		loadBalancerServicesEnabled = loadbalancerservice
	}

	// enable external ip services flag
	loadbalancerserviceextip, ok := os.LookupEnv("ENABLE_EXTERNAL_IP_SERVICES")
	if ok {
		externalIPServicesEnabled = loadbalancerserviceextip
	}

	// hearbeat port
	hbport, ok := os.LookupEnv("HEARTBEAT_PORT")
	if ok {
		heartbeatPlainPort = hbport
	}

	// service port
	svcport, ok := os.LookupEnv("SERVICE_PORT")
	if ok {
		servicePlainPort = svcport
	}

	// fabric port
	fbport, ok := os.LookupEnv("FABRIC_PORT")
	if ok {
		fabricPlainPort = fbport
	}

	// node id prefix
	prefix, ok := os.LookupEnv("NODE_ID_PREFIX")
	if ok {
		_, err := strconv.ParseUint(prefix, 16, 64)
		if err == nil {
			nodeIDPrefix = prefix
		}
	}

	// Service TLS

	svctlsonly, ok := os.LookupEnv("SERVICE_TLS_ONLY")
	if ok {
		serviceTLSOnly = svctlsonly
	}

	svccafile, ok := os.LookupEnv("SERVICE_CA_FILE")
	if ok {
		serviceCAFile = svccafile
	}

	svccertfile, ok := os.LookupEnv("SERVICE_CERT_FILE")
	if ok {
		serviceCertFile = svccertfile
	}

	svckeyfile, ok := os.LookupEnv("SERVICE_KEY_FILE")
	if ok {
		serviceKeyFile = svckeyfile
	}

	svctlsname, ok := os.LookupEnv("SERVICE_TLS_NAME")
	if ok {
		serviceTLSName = svctlsname
	}

	svcmutualauth, ok := os.LookupEnv("SERVICE_MUTUAL_AUTH")
	if ok {
		serviceMutualAuth = svcmutualauth
	}

	svctlsport, ok := os.LookupEnv("SERVICE_TLS_PORT")
	if ok {
		serviceTLSPort = svctlsport
	}

	// Heartbeat TLS

	hbtlsonly, ok := os.LookupEnv("HEARTBEAT_TLS_ONLY")
	if ok {
		heartbeatTLSOnly = hbtlsonly
	}

	hbcafile, ok := os.LookupEnv("HEARTBEAT_CA_FILE")
	if ok {
		heartbeatCAFile = hbcafile
	}

	hbcertfile, ok := os.LookupEnv("HEARTBEAT_CERT_FILE")
	if ok {
		heartbeatCertFile = hbcertfile
	}

	hbkeyfile, ok := os.LookupEnv("HEARTBEAT_KEY_FILE")
	if ok {
		heartbeatKeyFile = hbkeyfile
	}

	hbtlsname, ok := os.LookupEnv("HEARTBEAT_TLS_NAME")
	if ok {
		heartbeatTLSName = hbtlsname
	}

	hbtlsport, ok := os.LookupEnv("HEARTBEAT_TLS_PORT")
	if ok {
		heartbeatTLSPort = hbtlsport
	}

	// Fabric TLS

	fbtlsonly, ok := os.LookupEnv("FABRIC_TLS_ONLY")
	if ok {
		fabricTLSOnly = fbtlsonly
	}

	fbcafile, ok := os.LookupEnv("FABRIC_CA_FILE")
	if ok {
		fabricCAFile = fbcafile
	}

	fbcertfile, ok := os.LookupEnv("FABRIC_CERT_FILE")
	if ok {
		fabricCertFile = fbcertfile
	}

	fbkeyfile, ok := os.LookupEnv("FABRIC_KEY_FILE")
	if ok {
		fabricKeyFile = fbkeyfile
	}

	fbtlsname, ok := os.LookupEnv("FABRIC_TLS_NAME")
	if ok {
		fabricTLSName = fbtlsname
	}

	fbtlsport, ok := os.LookupEnv("FABRIC_TLS_PORT")
	if ok {
		fabricTLSPort = fbtlsport
	}

	secenabled, ok := os.LookupEnv("SECURITY_ENABLED")
	if ok {
		securityEnabled = secenabled
	}

	huser, ok := os.LookupEnv("HELM_USERNAME")
	if ok {
		helmUsername = huser
	}

	hpass, ok := os.LookupEnv("HELM_PASSWORD")
	if ok {
		helmPassword = hpass
	}

	auser, ok := os.LookupEnv("ADMIN_USERNAME")
	if ok {
		adminUsername = auser
	}

	apass, ok := os.LookupEnv("ADMIN_PASSWORD")
	if ok {
		adminPassword = apass
	}

	amode, ok := os.LookupEnv("AUTH_MODE")
	if ok {
		authMode = amode
	}

	expenabled, ok := os.LookupEnv("AEROSPIKE_PROMETHEUS_EXPORTER_ENABLED")
	if ok {
		apeEnabled = expenabled
	}
}

// initializeConfigVolume prepares the config volume with necessary files and scripts.
// copies necessary configuration and license files from k8s configmap
func initializeConfigVolume(configMapMountPath, aerospikeConfigVolumePath, apeConfigVolumePath string) {
	// aerospike.template.conf
	err := copyFile(configMapMountPath+"/aerospike.template.conf", aerospikeConfigVolumePath+"/aerospike.template.conf")
	if err != nil {
		zap.S().Errorf("%v.", err)
	}

	// features.conf
	err = copyFile(configMapMountPath+"/features.conf", aerospikeConfigVolumePath+"/features.conf")
	if err != nil {
		zap.S().Errorf("%v.", err)
	}

	// ape.toml.template
	err = copyFile(configMapMountPath+"/ape.toml.template", apeConfigVolumePath+"/ape.toml.template")
	if err != nil {
		zap.S().Errorf("%v.", err)
	}

	// aku-adm
	srcFilePath := "/aku-adm"
	if _, err = os.Stat(configMapMountPath + "/aku-adm"); err == nil {
		srcFilePath = configMapMountPath + "/aku-adm"
	}

	err = copyFile(srcFilePath, aerospikeConfigVolumePath+"/aku-adm")
	if err != nil {
		zap.S().Errorf("%v.", err)
	}

	err = os.Chmod(aerospikeConfigVolumePath+"/aku-adm", 0777)
	if err != nil {
		zap.S().Errorf("%v.", err)
	}
}

// initializeLogging initializes zap logging.
// replaces global logger.
// returns zap sugared logger.
// only console logging.
func initializeLogging(logLevel string) *zap.SugaredLogger {
	config := zap.NewProductionConfig()

	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.DisableCaller = true
	config.DisableStacktrace = true
	config.Encoding = "console"
	config.OutputPaths = []string{"stdout"}

	logLevel = strings.ToLower(logLevel)
	switch logLevel {
	case "info":
		config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	case "debug":
		config.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
		config.DisableCaller = true
		config.DisableStacktrace = true
	default:
		config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	}

	logger, _ := config.Build()
	zap.ReplaceGlobals(logger)

	return logger.Sugar()
}
