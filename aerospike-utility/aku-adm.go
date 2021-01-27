package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	aerospike "github.com/aerospike/aerospike-client-go"
	goversion "github.com/hashicorp/go-version"
	flag "github.com/spf13/pflag"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	securityEnabled string = "false"
	helmUsername    string = "helm_operator"
	helmPassword    string = "helm_operator"
	adminUsername   string = "admin"
	adminPassword   string = "admin"
	authMode        string = "internal"

	servicePlainPort string = "3000"

	serviceTLSEnabled string
	serviceCAFile     string
	serviceCertFile   string
	serviceKeyFile    string
	serviceTLSName    string
	serviceTLSPort    string

	serviceMutualAuth string

	aerospikeConfigVolumePath string = "/etc/aerospike"

	myPodIP string
)

func main() {
	logLevel := flag.String("log-level", "info", "Specify logging level")
	operation := flag.String("operation", "", "Specify operation post-start or pre-stop")
	host := flag.String("host", "localhost", "Aerospike seed IP to connect")

	flag.Parse()

	sugarLogger := initializeLogging(*logLevel)
	defer sugarLogger.Sync()

	sugarLogger.Info("Welcome to Aerospike Kubernetes Utility.")

	initVars()
	performOperation(*operation, *host)
}

// Creates and initializes aerospike client object
func createAerospikeClient(host string, username string, password string) *aerospike.Client {
	var client *aerospike.Client
	var err error
	zap.S().Info("Initializing Aerospike client.")

	for {
		client, err = initAerospikeClient(host, username, password)
		if err != nil {
			// During post start, need to wait for the aerospike to start. So sleep and retry.
			zap.S().Errorf("Unable to initialize Aerospike client: %v. Retrying.", err)
			time.Sleep(1 * time.Second)
			continue
		}

		zap.S().Info("Aerospike client initialized.")
		break
	}

	return client
}

// Perform specific operation - post-start, pre-stop and liveness probe
func performOperation(op string, host string) {
	switch op {
	case "post-start":
		if securityEnabled != "true" {
			zap.S().Info("Security disabled. No post-start operation.")
			break
		}
		client := createAerospikeClient(host, adminUsername, adminPassword)
		performPostStartOp(client)
		client.Close()
		break
	case "pre-stop":
		client := createAerospikeClient(host, helmUsername, helmPassword)
		performPreStopOp(client)
		client.Close()
		break
	case "liveness":
		performLivenessProbeOp(host, adminUsername, adminPassword)
		break
	case "pre-stop-community":
		client := createAerospikeClient(host, "", "")
		performPreStopOpCommunity(client)
		client.Close()
		break
	default:
		zap.S().Fatal("Unknown operation.")
	}

	return
}

// Pre stop logic (for community edition)
// Check if cluster is stable (without migrations)
// TODO: Add cluster_size and cluster_key validations
func performPreStopOpCommunity(client *aerospike.Client) {
	var activeNodes []*aerospike.Node

	infoPolicy := aerospike.NewInfoPolicy()
	infoPolicy.Timeout = 10 * time.Second

	zap.S().Info("Starting pre-stop operation.")

	// check cluster stable
	command := "cluster-stable:ignore-migrations=no;"
	for {
		activeNodes = client.GetNodes()
		retry := false

		for _, node := range activeNodes {
			out, err := node.RequestInfo(infoPolicy, command)
			if err != nil {
				zap.S().Errorf("Error during cluster-stable check on node %s: %v. Retrying.", node.GetName(), err)
				retry = true
				break
			}

			if out[command] == "ERROR::unstable-cluster" {
				zap.S().Warn("Unstable cluster. Retrying.")
				retry = true
				break
			}

			zap.S().Debugf("Node %s: %v.", node.GetName(), out[command])
		}

		if retry {
			time.Sleep(1 * time.Second)
			continue
		}

		zap.S().Info("Cluster is stable.")
		break
	}

	zap.S().Info("Pre-stop operation complete.")
	return
}

// Liveness probe check
// Performs an info call "build" on localhost
func performLivenessProbeOp(host, adminUsername, adminPassword string) {
	zap.S().Info("Starting Liveness check.")

	command := "build"
	success := false

	var connection *aerospike.Connection
	var err error

	for i := 0; i < 3; i++ {
		if connection == nil {
			connection, err = initAerospikeConnection(host, adminUsername, adminPassword)
			if err != nil {
				zap.S().Errorf("Error while creating connection to %s: %v. Retrying.", host, err)
				time.Sleep(1 * time.Second)
				continue
			}
		}

		out, err := aerospike.RequestInfo(connection, command)
		if err != nil {
			zap.S().Errorf("Error while fetching build version: %v. Retrying.", err)
			time.Sleep(1 * time.Second)
			continue
		}

		zap.S().Debugf("Response: %s.", out[command])
		success = true
		break
	}

	if !success {
		zap.S().Fatal("Liveness check failed.")
	}

	zap.S().Info("Liveness check success.")
	return
}

// Pre stop logic
// Check if cluster is stable,
// Quiesce self and validate pending_quiesce state
// Recluster and validate effective_is_quiesce state
// Validate no throughput and no proxies on this node
// TODO: Add cluster_size and cluster_key validations
// TODO: Add validation - Non-quiesced nodes should not be a destination of proxy transactions
func performPreStopOp(client *aerospike.Client) {
	var activeNodes []*aerospike.Node

	infoPolicy := aerospike.NewInfoPolicy()
	infoPolicy.Timeout = 10 * time.Second

	zap.S().Info("Starting pre-stop operation.")

	// check cluster stable
	command := "cluster-stable:ignore-migrations=no;"
	for {
		activeNodes = client.GetNodes()
		retry := false

		for _, node := range activeNodes {
			out, err := node.RequestInfo(infoPolicy, command)
			if err != nil {
				zap.S().Errorf("Error during cluster-stable check on node %s: %v. Retrying.", node.GetName(), err)
				retry = true
				break
			}

			if out[command] == "ERROR::unstable-cluster" {
				zap.S().Warn("Unstable cluster. Retrying.")
				retry = true
				break
			}

			zap.S().Debugf("Node %s: %v.", node.GetName(), out[command])
		}

		if retry {
			time.Sleep(1 * time.Second)
			continue
		}

		zap.S().Info("Cluster is stable.")
		break
	}

	// quiesce
	command = "quiesce:"
	for {
		activeNodes = client.GetNodes()
		retry := false

		for _, node := range activeNodes {
			hostName := node.GetHost().Name
			if hostName == "localhost" || hostName == "127.0.0.1" || hostName == myPodIP {
				zap.S().Infof("Quiescing self %s.", hostName)

				out, err := node.RequestInfo(infoPolicy, command)
				if err != nil {
					zap.S().Errorf("Error while quiescing self %s: %v. Retrying.", hostName, err)
					retry = true
					break
				}

				for {
					out, err = node.RequestInfo(infoPolicy, "namespaces")
					if err != nil {
						zap.S().Errorf("Error while fetching namespaces: %v. Retrying.", err)
						time.Sleep(1 * time.Second)
						continue
					}

					selectedNamespace := ""
					if len(out["namespaces"]) > 0 {
						selectedNamespace = strings.Split(out["namespaces"], ";")[0]
					}

					if len(selectedNamespace) > 0 {
						stats, err := node.RequestInfo(infoPolicy, fmt.Sprintf("namespace/%s", selectedNamespace))
						if err != nil {
							zap.S().Errorf("Error while requesting namespace statistics: %v. Retrying.", err)
							time.Sleep(1 * time.Second)
							continue
						}

						metrics := parseStats(stats[fmt.Sprintf("namespace/%s", selectedNamespace)], ";")

						if metrics["pending_quiesce"] != "true" {
							zap.S().Warn("Waiting for pending_quiesce to be true. Retrying.")
							time.Sleep(1 * time.Second)
							continue
						}

						zap.S().Debug("pending_quiesce is true.")
						break
					}

					break
				}

				break
			}
		}

		if retry {
			time.Sleep(1 * time.Second)
			continue
		}

		zap.S().Info("Quiesced self successfully.")
		break
	}

	// recluster
	command = "recluster:"
	for {
		activeNodes = client.GetNodes()
		retry := false

		for _, node := range activeNodes {
			_, err := node.RequestInfo(infoPolicy, command)
			if err != nil {
				zap.S().Errorf("Error while issuing recluster to %s: %v. Retrying.", node.GetHost().Name, err)
				retry = true
				break
			}
		}

		if retry {
			time.Sleep(1 * time.Second)
			continue
		}

		zap.S().Info("Recluster issued.")
		break
	}

	time.Sleep(5 * time.Second)

	// Validate quiesce state after recluster
	// make sure quiesce went through properly, if not, abort.
	for {
		activeNodes = client.GetNodes()
		retry := false
		pendingQuiesceTrueInAllNodes := true

		for _, node := range activeNodes {
			out, err := node.RequestInfo(infoPolicy, "namespaces")
			if err != nil {
				zap.S().Errorf("Error while fetching namespaces: %v. Retrying.", err)
				retry = true
				break
			}

			selectedNamespace := ""
			if len(out["namespaces"]) > 0 {
				selectedNamespace = strings.Split(out["namespaces"], ";")[0]
			}

			if len(selectedNamespace) > 0 {
				stats, err := node.RequestInfo(infoPolicy, fmt.Sprintf("namespace/%s", selectedNamespace))
				if err != nil {
					zap.S().Errorf("Error while requesting namespace statistics: %v. Retrying.", err)
					retry = true
					break
				}

				metrics := parseStats(stats[fmt.Sprintf("namespace/%s", selectedNamespace)], ";")
				if metrics["pending_quiesce"] == "false" {
					pendingQuiesceTrueInAllNodes = false
					break
				}
			}
		}

		if retry {
			time.Sleep(1 * time.Second)
			continue
		}

		if pendingQuiesceTrueInAllNodes {
			zap.S().Error("pending_quiesce is true on all nodes. already in pre-stop, stopping pod.")
			// Quiesce was probably ignored. Abort.
			cmd := "cluster-stable:ignore-migrations=no;"
			for {
				nodes := client.GetNodes()
				shouldRetry := false

				for _, node := range nodes {
					res, err := node.RequestInfo(infoPolicy, cmd)
					if err != nil {
						zap.S().Errorf("Error during cluster-stable check on node %s: %v. Retrying.", node.GetName(), err)
						shouldRetry = true
						break
					}

					if res[cmd] == "ERROR::unstable-cluster" {
						zap.S().Warn("Unstable cluster. Retrying.")
						shouldRetry = true
						break
					}

					zap.S().Debugf("Node %s: %v", node.GetName(), res[cmd])
				}

				if shouldRetry {
					time.Sleep(1 * time.Second)
					continue
				}

				zap.S().Info("Cluster is stable.")
				break
			}

			zap.S().Info("Pre-stop operation successful.")
			return
		}

		break
	}

	// Check effective_is_quiesced
	for {
		activeNodes = client.GetNodes()
		retry := false

		for _, node := range activeNodes {
			hostName := node.GetHost().Name
			if hostName == "localhost" || hostName == "127.0.0.1" || hostName == myPodIP {
				out, err := node.RequestInfo(infoPolicy, "namespaces")
				if err != nil {
					zap.S().Errorf("Error while fetching namespaces: %v. Retrying.", err)
					retry = true
					break
				}

				selectedNamespace := ""
				if len(out["namespaces"]) > 0 {
					selectedNamespace = strings.Split(out["namespaces"], ";")[0]
				}

				if len(selectedNamespace) > 0 {
					stats, err := node.RequestInfo(infoPolicy, fmt.Sprintf("namespace/%s", selectedNamespace))
					if err != nil {
						zap.S().Errorf("Error while requesting namespace statistics: %v. Retrying.", err)
						retry = true
						break
					}

					metrics := parseStats(stats[fmt.Sprintf("namespace/%s", selectedNamespace)], ";")

					if metrics["effective_is_quiesced"] != "true" {
						zap.S().Warn("Waiting for effective_is_quiesced to be true. Retrying.")
						retry = true
						break
					}

					zap.S().Info("effective_is_quiesced is true.")
					break
				}
			}
		}

		if retry {
			time.Sleep(1 * time.Second)
			continue
		}

		break
	}

	// verify no throughput on this node
	for {
		activeNodes = client.GetNodes()
		retry := false

		for _, node := range activeNodes {
			hostName := node.GetHost().Name

			if hostName == "localhost" || hostName == "127.0.0.1" || hostName == myPodIP {
				out, err := node.RequestInfo(infoPolicy, "build")
				if err != nil {
					zap.S().Errorf("Error while fetching build version: %v. Retrying.", err)
					retry = true
					break
				}

				latencyCommand, err := getLatencyCommand(out["build"])
				if err != nil {
					zap.S().Errorf("Error while requesting latency command: %v. Retrying.", err)
					retry = true
					break
				}

				latencyStats, err := node.RequestInfo(infoPolicy, latencyCommand)
				if err != nil {
					zap.S().Errorf("Error while fetching latency stats: %v. Retrying.", err)
					retry = true
					break
				}

				var opsPerSec float64 = 0.0
				if latencyCommand == "latencies:" {
					opsPerSec, err = getOpsPerSecNew(latencyStats["latencies:"])
					if err != nil {
						zap.S().Errorf("Error while calculating ops/sec (new): %v. Retrying.", err)
						retry = true
						break
					}
				} else {
					opsPerSec, err = getOpsPerSecLegacy(latencyStats["latency:"])
					if err != nil {
						zap.S().Errorf("Error while calculating ops/sec (legacy): %v. Retrying.", err)
						retry = true
						break
					}
				}

				zap.S().Debugf("Total Ops/sec: %f.", opsPerSec)

				if opsPerSec != 0.0 {
					zap.S().Warn("Current throughput non zero. Retrying.")
					retry = true
					break
				}

				break
			}
		}

		if retry {
			time.Sleep(5 * time.Second)
			continue
		}

		zap.S().Info("No active transactions observed on this node.")
		break
	}

	// verify no proxies on this node
	totalProxies := 0
	for {
		activeNodes = client.GetNodes()
		retry := false

		for _, node := range activeNodes {
			hostName := node.GetHost().Name

			if hostName == "localhost" || hostName == "127.0.0.1" || hostName == myPodIP {
				out, err := node.RequestInfo(infoPolicy, "namespaces")
				if err != nil {
					zap.S().Errorf("Error while fetching namespaces: %v. Retrying.", err)
					retry = true
					break
				}

				namespacesList := strings.Split(out["namespaces"], ";")
				if len(namespacesList) > 0 {
					total := 0
					for _, ns := range namespacesList {
						stats, err := node.RequestInfo(infoPolicy, fmt.Sprintf("namespace/%s", ns))
						if err != nil {
							zap.S().Errorf("Error while fetching namespace statistics for %s: %v. Retrying.", ns, err)
							retry = true
							break
						}

						metrics := parseStats(stats[fmt.Sprintf("namespace/%s", ns)], ";")

						i, _ := strconv.Atoi(metrics["client_proxy_complete"])
						total += i

						i, _ = strconv.Atoi(metrics["client_proxy_error"])
						total += i

						i, _ = strconv.Atoi(metrics["client_proxy_timeout"])
						total += i

						i, _ = strconv.Atoi(metrics["batch_sub_proxy_complete"])
						total += i

						i, _ = strconv.Atoi(metrics["batch_sub_proxy_error"])
						total += i

						i, _ = strconv.Atoi(metrics["batch_sub_proxy_timeout"])
						total += i
					}

					zap.S().Debugf("Previous total proxies: %d, Current total proxies: %d.", totalProxies, total)

					if totalProxies != total {
						zap.S().Warn("Proxies diff non zero. Retrying.")
						totalProxies = total
						retry = true
						break
					}
				}

				break
			}
		}

		if retry {
			time.Sleep(5 * time.Second)
			continue
		}

		zap.S().Info("No ongoing proxies observed on this node.")
		break
	}

	zap.S().Info("Pre-stop operation successful.")
	return
}

// Get latency command based on the aerospike build version
// "latencies:" for 5.1 and above
// "latency:" for older versions
func getLatencyCommand(ver string) (string, error) {
	ref := "5.1.0.0"

	version, err := goversion.NewVersion(ver)
	if err != nil {
		zap.S().Errorf("Error parsing build version %s: %v.", ver, err)
		return "", err
	}

	refVersion, err := goversion.NewVersion(ref)
	if err != nil {
		zap.S().Errorf("Error parsing reference version %s: %v.", ref, err)
		return "", err
	}

	if version.GreaterThanOrEqual(refVersion) {
		return "latencies:", nil
	}

	return "latency:", nil
}

// Post start operation
// creates a sys-admin user which can be used for pre stop operation.
// (only when security is enabled)
func performPostStartOp(client *aerospike.Client) {
	zap.S().Info("Starting post-start operation.")

	adminPolicy := aerospike.NewAdminPolicy()
	for i := 0; i < 3; i++ {
		err := client.CreateUser(adminPolicy, helmUsername, helmPassword, []string{"sys-admin"})
		if err != nil {
			if err.Error() == "User already exists" {
				zap.S().Info("User already exists.")
				break
			}

			zap.S().Errorf("Unable to create user %s: %v. Retrying.", helmUsername, err)
			time.Sleep(1 * time.Second)
			continue
		}

		zap.S().Info("Created a `sys-admin` user for further management.")
		break
	}

	zap.S().Info("Post-start operation complete.")
	return
}

// Initializes zap logging
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
