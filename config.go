package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	lbServicePollPeriod     = 10 * time.Second // 10 seconds
	lbServicePollRetryLimit = 30               // 30 attempts ~ 5 mins
)

// prepareAerospikeConfig updates aerospike config template with dynamic config requirements.
// Sets cluster-name
// Sets node-id
// Sets alternate-access-address if required
// Populates heartbeat config(mesh-seed-address-port)
// TODO: Replace all this with an aerospike config file generator
func prepareAerospikeConfig(peersList []string) (err error) {
	// Read aerospike config template file
	aerospikeConfigFile := aerospikeConfigVolumePath + "/aerospike.template.conf"
	lines, err := fileToLines(aerospikeConfigFile)
	if err != nil {
		return fmt.Errorf("error reading file %s: %v", aerospikeConfigFile, err)
	}

	// Fetch and add serviceaccount CA certificate to pool
	caCert, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
	if err != nil {
		return fmt.Errorf("error adding service account CA certificate to pool: %v", err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	// HTTP client to query kubernetes API server
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
		},
	}

	// Fetch serviceaccount token
	token, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return fmt.Errorf("error reading service account token: %v", err)
	}
	tokenString := string(token)

	// Regex to locate lines in config file
	firstServiceStanzaRegexp := regexp.MustCompile(`^service\s+{`)
	secondServiceStanzaRegexp := regexp.MustCompile(`service\s+{`)
	heartbeatStanzaRegexp := regexp.MustCompile(`heartbeat\s+{`)
	fabricStanzaRegexp := regexp.MustCompile(`fabric\s+{`)
	infoStanzaRegexp := regexp.MustCompile(`info\s+{`)
	networkStanzaRegexp := regexp.MustCompile(`network\s+{`)
	securityStanzaRegexp := regexp.MustCompile(`security\s+{`)
	addressLineRegexp := regexp.MustCompile(`address\s`)
	portLineRegexp := regexp.MustCompile(`port\s`)
	skipAddressAndPort := false
	securityStanzaExists := false

	// Update aerospike config template file
	fileContent := ""
	for _, line := range lines {
		// skip address and port if tlsOnly is enabled
		if skipAddressAndPort {
			if addressLineRegexp.MatchString(line) || portLineRegexp.MatchString(line) {
				continue
			}
		}

		if securityStanzaRegexp.MatchString(line) {
			securityStanzaExists = true
		}

		fileContent += line
		fileContent += "\n"

		// Write node-id and cluster-name to service config
		if firstServiceStanzaRegexp.MatchString(line) {
			// Set cluster-name
			if clusterName != "" {
				fileContent += "\tcluster-name " + clusterName + "\n"
			}

			// Set node-id
			if autoGenerateNodeIds == "true" {
				if podName != "" {
					r := regexp.MustCompile("([^-]+$)")
					fileContent += "\tnode-id " + nodeIDPrefix + r.FindString(podName) + "\n"
				}
			}

			continue
		}

		// Write TLS configurations
		if networkStanzaRegexp.MatchString(line) {
			var tlsNames = make(map[string]bool)

			if serviceTLSName != "" {
				_, ok := tlsNames[serviceTLSName]
				if !ok {
					tlsNames[serviceTLSName] = true
					fileContent += "\ttls " + serviceTLSName + " {\n"

					path, err := getCertFilePath(aerospikeConfigVolumePath, serviceCertFile, serviceTLSName+"-service-cert.pem")
					if err != nil {
						return err
					}
					fileContent += "\t\tcert-file " + path + "\n"

					path, err = getCertFilePath(aerospikeConfigVolumePath, serviceCAFile, serviceTLSName+"-service-cacert.pem")
					if err != nil {
						return err
					}
					fileContent += "\t\tca-file " + path + "\n"

					path, err = getCertFilePath(aerospikeConfigVolumePath, serviceKeyFile, serviceTLSName+"-service-key.pem")
					if err != nil {
						return err
					}
					fileContent += "\t\tkey-file " + path + "\n"
					fileContent += "\t}" + "\n"
				}
			}

			if heartbeatTLSName != "" {
				_, ok := tlsNames[heartbeatTLSName]
				if !ok {
					tlsNames[heartbeatTLSName] = true
					fileContent += "\ttls " + heartbeatTLSName + " {\n"

					path, err := getCertFilePath(aerospikeConfigVolumePath, heartbeatCertFile, heartbeatTLSName+"-heartbeat-cert.pem")
					if err != nil {
						return err
					}
					fileContent += "\t\tcert-file " + path + "\n"

					path, err = getCertFilePath(aerospikeConfigVolumePath, heartbeatCAFile, heartbeatTLSName+"-heartbeat-cacert.pem")
					if err != nil {
						return err
					}
					fileContent += "\t\tca-file " + path + "\n"

					path, err = getCertFilePath(aerospikeConfigVolumePath, heartbeatKeyFile, heartbeatTLSName+"-heartbeat-key.pem")
					if err != nil {
						return err
					}
					fileContent += "\t\tkey-file " + path + "\n"
					fileContent += "\t}" + "\n"
				}
			}

			if fabricTLSName != "" {
				_, ok := tlsNames[fabricTLSName]
				if !ok {
					tlsNames[fabricTLSName] = true
					fileContent += "\ttls " + fabricTLSName + " {\n"

					path, err := getCertFilePath(aerospikeConfigVolumePath, fabricCertFile, fabricTLSName+"-fabric-cert.pem")
					if err != nil {
						return err
					}
					fileContent += "\t\tcert-file " + path + "\n"

					path, err = getCertFilePath(aerospikeConfigVolumePath, fabricCAFile, fabricTLSName+"-fabric-cacert.pem")
					if err != nil {
						return err
					}
					fileContent += "\t\tca-file " + path + "\n"

					path, err = getCertFilePath(aerospikeConfigVolumePath, fabricKeyFile, fabricTLSName+"-heartbeat-key.pem")
					if err != nil {
						return err
					}
					fileContent += "\t\tkey-file " + path + "\n"
					fileContent += "\t}" + "\n"
				}
			}
		}

		// Write alternate access address and access port
		if secondServiceStanzaRegexp.MatchString(line) {
			skipAddressAndPort = false
			if serviceTLSOnly == "true" {
				skipAddressAndPort = true
			}

			if serviceTLSName != "" {
				fileContent += "\t\ttls-address any\n"
				fileContent += "\t\ttls-port " + serviceTLSPort + "\n"

				fileContent += "\t\ttls-name " + serviceTLSName + "\n"
				if serviceMutualAuth == "true" {
					fileContent += "\t\ttls-authenticate-client " + serviceTLSName + "\n"
				}
			}

			// Host Networking
			if hostNetworking == "true" {
				// curl --cacert /var/run/secrets/kubernetes.io/serviceaccount/ca.crt -H "Authorization: Bearer xxTOKENxx" "https://kubernetes.default.svc/api/v1/nodes"
				nodeResp, err := sendHTTPRequest(client, "https://kubernetes.default.svc/api/v1/nodes/", "GET", tokenString)
				if err != nil {
					return fmt.Errorf("error while fetching nodes: %v", err)
				}

				nodeIP := hostIP
				if hostNetworkingExternalIP == "true" {
					nodeIP = getNodeIP(nodeResp, hostIP)
				}

				if nodeIP != "" {
					if serviceTLSOnly != "true" {
						fileContent += "\t\talternate-access-address " + nodeIP + "\n"
						fileContent += "\t\talternate-access-port " + servicePlainPort + "\n"
					}

					if serviceTLSName != "" {
						fileContent += "\t\ttls-alternate-access-address " + nodeIP + "\n"
						fileContent += "\t\ttls-alternate-access-port " + serviceTLSPort + "\n"
					}
				}
			}

			// Node port services
			if nodePortServicesEnabled == "true" {
				// curl --cacert /var/run/secrets/kubernetes.io/serviceaccount/ca.crt -H "Authorization: Bearer xxTOKENxx" "https://kubernetes.default.svc/api/v1/nodes"
				nodeResp, err := sendHTTPRequest(client, "https://kubernetes.default.svc/api/v1/nodes/", "GET", tokenString)
				if err != nil {
					return fmt.Errorf("error while fetching nodes: %v", err)
				}

				nodeIP := hostIP
				if nodePortServicesExternalIP == "true" {
					nodeIP = getNodeIP(nodeResp, hostIP)
				}

				if serviceTLSOnly != "true" {
					// curl --cacert /var/run/secrets/kubernetes.io/serviceaccount/ca.crt -H 'Authorization: Bearer xxTOKENxx' https://kubernetes.default.svc/api/v1/namespaces/default/services/nodeport-anton-aerospike-enterprise-0
					serviceResp, err := sendHTTPRequest(client, "https://kubernetes.default.svc/api/v1/namespaces/"+podNamespace+"/services/nodeport-"+podName, "GET", tokenString)
					if err != nil {
						return fmt.Errorf("error while fetching nodeport service: %v", err)
					}

					nodePlainPort, err := getNodePort(serviceResp, "aerospike-plain")
					if err != nil {
						return fmt.Errorf("error getting nodePort(Plain): %v", err)
					}

					if nodeIP != "" && nodePlainPort != 0 {
						fileContent += "\t\talternate-access-address " + nodeIP + "\n"
						fileContent += "\t\talternate-access-port " + strconv.Itoa(nodePlainPort) + "\n"
					}
				}

				if serviceTLSName != "" {
					// curl --cacert /var/run/secrets/kubernetes.io/serviceaccount/ca.crt -H 'Authorization: Bearer xxTOKENxx' https://kubernetes.default.svc/api/v1/namespaces/default/services/nodeport-anton-aerospike-enterprise-0
					serviceResp, err := sendHTTPRequest(client, "https://kubernetes.default.svc/api/v1/namespaces/"+podNamespace+"/services/nodeport-"+podName, "GET", tokenString)
					if err != nil {
						return fmt.Errorf("error while fetching nodeport service: %v", err)
					}

					nodeTLSPort, err := getNodePort(serviceResp, "aerospike-tls")
					if err != nil {
						return fmt.Errorf("error getting nodePort(TLS): %v", err)
					}

					if nodeIP != "" && nodeTLSPort != 0 {
						fileContent += "\t\ttls-alternate-access-address " + nodeIP + "\n"
						fileContent += "\t\ttls-alternate-access-port " + strconv.Itoa(nodeTLSPort) + "\n"
					}
				}
			}

			// Loadbalancer services
			if loadBalancerServicesEnabled == "true" {
				if serviceTLSOnly != "true" {
					loadbalancerIP := ""
					loadbalancerPort := 0
					for attempt := 0; attempt < lbServicePollRetryLimit; attempt++ {
						// curl --cacert /var/run/secrets/kubernetes.io/serviceaccount/ca.crt -H 'Authorization: Bearer xxTOKENxx' https://kubernetes.default.svc/api/v1/namespaces/default/services/loadbalancer-anton-aerospike-enterprise-0
						serviceResp, err := sendHTTPRequest(client, "https://kubernetes.default.svc/api/v1/namespaces/"+podNamespace+"/services/loadbalancer-"+podName, "GET", tokenString)
						if err != nil {
							zap.S().Warnf("Error while fetching load balancer service: %v. Retrying.", err)
							time.Sleep(lbServicePollPeriod)
							continue
						}

						loadbalancerIP, loadbalancerPort, err = getLoadBalancerIPAndPort(serviceResp, "aerospike-plain")
						if err != nil {
							zap.S().Warnf("Error getting loadbalancer IP and port: %v. Retrying.", err)
							time.Sleep(lbServicePollPeriod)
							continue
						}

						break
					}

					if loadbalancerIP != "" && loadbalancerPort != 0 {
						fileContent += "\t\talternate-access-address " + loadbalancerIP + "\n"
						fileContent += "\t\talternate-access-port " + strconv.Itoa(loadbalancerPort) + "\n"
					}
				}

				if serviceTLSName != "" {
					loadbalancerTLSIP := ""
					loadbalancerTLSPort := 0
					for attempt := 0; attempt < lbServicePollRetryLimit; attempt++ {
						// curl --cacert /var/run/secrets/kubernetes.io/serviceaccount/ca.crt -H 'Authorization: Bearer xxTOKENxx' https://kubernetes.default.svc/api/v1/namespaces/default/services/loadbalancer-anton-aerospike-enterprise-0
						serviceResp, err := sendHTTPRequest(client, "https://kubernetes.default.svc/api/v1/namespaces/"+podNamespace+"/services/loadbalancer-"+podName, "GET", tokenString)
						if err != nil {
							zap.S().Warnf("Error while fetching load balancer service: %v. Retrying.", err)
							time.Sleep(lbServicePollPeriod)
							continue
						}

						loadbalancerTLSIP, loadbalancerTLSPort, err = getLoadBalancerIPAndPort(serviceResp, "aerospike-tls")
						if err != nil {
							zap.S().Warnf("Error getting loadbalancer IP and port: %v. Retrying.", err)
							time.Sleep(lbServicePollPeriod)
							continue
						}

						break
					}

					if loadbalancerTLSIP != "" && loadbalancerTLSPort != 0 {
						fileContent += "\t\ttls-alternate-access-address " + loadbalancerTLSIP + "\n"
						fileContent += "\t\ttls-alternate-access-port " + strconv.Itoa(loadbalancerTLSPort) + "\n"
					}
				}
			}

			// External IP services
			if externalIPServicesEnabled == "true" {
				if serviceTLSOnly != "true" {
					// curl --cacert /var/run/secrets/kubernetes.io/serviceaccount/ca.crt -H 'Authorization: Bearer xxTOKENxx' https://kubernetes.default.svc/api/v1/namespaces/default/services/extip-anton-aerospike-enterprise-0
					serviceResp, err := sendHTTPRequest(client, "https://kubernetes.default.svc/api/v1/namespaces/"+podNamespace+"/services/extip-"+podName, "GET", tokenString)
					if err != nil {
						return fmt.Errorf("error while fetching external ip service: %v", err)
					}

					extIP, extPort, err := getExtIPPort(serviceResp, "aerospike-plain")
					if err != nil {
						return fmt.Errorf("error getting external ip and port: %v", err)
					}

					if extIP != "" && extPort != 0 {
						fileContent += "\t\talternate-access-address " + extIP + "\n"
						fileContent += "\t\talternate-access-port " + strconv.Itoa(extPort) + "\n"
					}
				}

				if serviceTLSName != "" {
					// curl --cacert /var/run/secrets/kubernetes.io/serviceaccount/ca.crt -H 'Authorization: Bearer xxTOKENxx' https://kubernetes.default.svc/api/v1/namespaces/default/services/extip-anton-aerospike-enterprise-0
					serviceResp, err := sendHTTPRequest(client, "https://kubernetes.default.svc/api/v1/namespaces/"+podNamespace+"/services/extip-"+podName, "GET", tokenString)
					if err != nil {
						return fmt.Errorf("error while fetching external ip service: %v", err)
					}

					extIP, extPort, err := getExtIPPort(serviceResp, "aerospike-tls")
					if err != nil {
						return fmt.Errorf("error getting external ip and port: %v", err)
					}

					if extIP != "" && extPort != 0 {
						fileContent += "\t\ttls-alternate-access-address " + extIP + "\n"
						fileContent += "\t\ttls-alternate-access-port " + strconv.Itoa(extPort) + "\n"
					}
				}
			}

			continue
		}

		// Write peers to heartbeat config
		if heartbeatStanzaRegexp.MatchString(line) {
			skipAddressAndPort = false
			if heartbeatTLSOnly == "true" {
				skipAddressAndPort = true
			}

			if heartbeatTLSOnly != "true" {
				for _, peer := range peersList {
					fileContent += "\t\tmesh-seed-address-port " + peer + " " + heartbeatPlainPort + "\n"
				}
			}

			if heartbeatTLSName != "" {
				fileContent += "\t\ttls-address any\n"
				fileContent += "\t\ttls-port " + heartbeatTLSPort + "\n"
				fileContent += "\t\ttls-name " + heartbeatTLSName + "\n"

				for _, peer := range peersList {
					fileContent += "\t\ttls-mesh-seed-address-port " + peer + " " + heartbeatTLSPort + "\n"
				}
			}

			continue
		}

		// Write fabric configuration
		if fabricStanzaRegexp.MatchString(line) {
			skipAddressAndPort = false
			if fabricTLSOnly == "true" {
				skipAddressAndPort = true
			}

			if fabricTLSName != "" {
				fileContent += "\t\ttls-address any\n"
				fileContent += "\t\ttls-port " + fabricTLSPort + "\n"
				fileContent += "\t\ttls-name " + fabricTLSName + "\n"
			}
		}

		// Write info configuration
		if infoStanzaRegexp.MatchString(line) {
			skipAddressAndPort = false
		}
	}

	// Write security configuration
	if securityEnabled == "true" && !securityStanzaExists {
		fileContent += "security {\n"
		fileContent += "\tenable-security true\n"
		fileContent += "}\n\n"
	}

	// Write updated aerospike config template file
	err = ioutil.WriteFile(aerospikeConfigFile, []byte(fileContent), 0644)
	if err != nil {
		return fmt.Errorf("error writing to file %s: %v", aerospikeConfigFile, err)
	}

	// Success
	return nil
}

func getCertFilePath(configMountPoint string, certFile string, fileName string) (string, error) {
	if certFile == "" {
		return "", fmt.Errorf("certificate file name empty")
	}

	parsedCertFile := strings.Split(certFile, ":")

	switch len(parsedCertFile) {
	case 1:
		return certFile, nil
	case 2:
		switch parsedCertFile[0] {
		case "file":
			return parsedCertFile[1], nil
		case "b64enc":
			path, err := storeb64EncodedFileAndGetPath(configMountPoint, parsedCertFile[1], fileName)
			if err != nil {
				return "", err
			}

			return path, nil
		default:
			return "", fmt.Errorf("invalid option while parsing cert file: %s", parsedCertFile[0])
		}
	}

	// Should not reach here
	return "", fmt.Errorf("unable to parse cert file: %s", certFile)
}

func storeb64EncodedFileAndGetPath(configMountPoint string, b64EncodedValue string, fileName string) (string, error) {
	err := os.MkdirAll(configMountPoint+"/certs", 0755)
	if err != nil {
		return "", fmt.Errorf("unable to create directory %s: %v", configMountPoint+"/certs", err)
	}

	decodedData, err := base64.StdEncoding.DecodeString(b64EncodedValue)
	if err != nil {
		return "", fmt.Errorf("unable to decode base 64 encoded data: %v", err)
	}

	err = ioutil.WriteFile(configMountPoint+"/certs/"+fileName, decodedData, 0755)
	if err != nil {
		return "", fmt.Errorf("unable to write certificate file: %v", err)
	}

	return configMountPoint + "/certs/" + fileName, nil
}

// send HTTP request and return response
func sendHTTPRequest(client *http.Client, url string, method string, bearerToken string) (resp *http.Response, err error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		zap.S().Errorf("Error while creating HTTP request: %v.", err)
	}
	req.Header.Set("Authorization", "Bearer "+string(bearerToken))

	return client.Do(req)
}

// ServiceResponse represents the response message when querying kubernetes service
// Contains "spec" and "status" fields
type ServiceResponse struct {
	Specifications Spec `json:"spec,omitempty"`
	Status         Stat `json:"status,omitempty"`
}

// Stat represents "status" field in service response
// Contains "loadBalancer" field
type Stat struct {
	LoadBalancer Ingresses `json:"loadBalancer,omitempty"`
}

// Ingresses represents the each loadBalancer
// Contains "ingress" field
type Ingresses struct {
	Ingress []Address `json:"ingress,omitempty"`
}

// Address represents the endpoints of the loadbalancer
// Contains "ip" and "hostname" fields
//
// IP is set for load-balancer ingress points that are IP based
// (typically GCE or OpenStack load-balancers)
//
// Hostname is set for load-balancer ingress points that are DNS based
// (typically AWS load-balancers)
type Address struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
}

// Spec represents "spec" field in service response
// Contains "ports" and "externalIPs" fields
type Spec struct {
	Ports       []Port   `json:"ports,omitempty"`
	ExternalIPs []string `json:"externalIPs,omitempty"`
}

// Port represents the "port" field in service spec
type Port struct {
	Name       string `json:"name,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	AppPort    int    `json:"port,omitempty"`
	TargetPort int    `json:"targetPort,omitempty"`
	NodePort   int    `json:"nodePort,omitempty"`
}

func getNodePort(resp *http.Response, name string) (int, error) {
	decoder := json.NewDecoder(resp.Body)
	sr := &ServiceResponse{}

	err := decoder.Decode(sr)
	if err != nil {
		zap.S().Warnf("Unable to decode request: %v.", err)
		return 0, err
	}

	for _, v := range sr.Specifications.Ports {
		if v.Name == name {
			return v.NodePort, nil
		}
	}

	return 0, fmt.Errorf("unable to locate nodeport: %s", name)
}

func getExtIPPort(resp *http.Response, name string) (string, int, error) {
	decoder := json.NewDecoder(resp.Body)
	sr := &ServiceResponse{}

	err := decoder.Decode(sr)
	if err != nil {
		return "", 0, err
	}

	for _, v := range sr.Specifications.Ports {
		if v.Name == name {
			return sr.Specifications.ExternalIPs[0], v.AppPort, nil
		}
	}

	return "", 0, fmt.Errorf("unable to locate port: %s", name)
}

func getLoadBalancerIPAndPort(resp *http.Response, name string) (string, int, error) {
	decoder := json.NewDecoder(resp.Body)
	sr := &ServiceResponse{}

	err := decoder.Decode(sr)
	if err != nil {
		return "", 0, err
	}

	if len(sr.Status.LoadBalancer.Ingress) == 0 {
		return "", 0, fmt.Errorf("no ingress IPs found")
	}

	for _, v := range sr.Specifications.Ports {
		if v.Name == name {
			// IP is set for load-balancer ingress points that are IP based
			// (typically GCE or OpenStack load-balancers)
			//
			// Hostname is set for load-balancer ingress points that are DNS based
			// (typically AWS load-balancers)
			//
			// Assume preference for IP over hostname
			if sr.Status.LoadBalancer.Ingress[0].IP != "" {
				return sr.Status.LoadBalancer.Ingress[0].IP, v.AppPort, nil
			}

			if sr.Status.LoadBalancer.Ingress[0].Hostname != "" {
				hostName := sr.Status.LoadBalancer.Ingress[0].Hostname
				// Try to resolve the loadbalancer dns name here
				// Aerospike might throw error if the dns name is not resolvable
				for attempt := 0; attempt < lbServicePollRetryLimit; attempt++ {
					_, err := net.LookupIP(hostName)
					if err != nil {
						zap.S().Warnf("unable to resolve loadbalancer hostname %s. Retrying.", hostName)
						time.Sleep(lbServicePollPeriod)
						continue
					}

					return hostName, v.AppPort, nil
				}

				return "", 0, fmt.Errorf("invalid hostname for the loadbalancer")
			}

			zap.S().Error("Port matched, but no IP or hostname found for the loadbalancer.")
		}
	}

	return "", 0, fmt.Errorf("unable to locate port: %s", name)
}

// NodeResponse represents the response when querying kubernetes nodes
// Contains "items" field which in-turn contains each node object
type NodeResponse struct {
	Nodes []Node `json:"items,omitempty"`
}

// Node represents the kubernetes node
// Contains "status" field
type Node struct {
	Status Addresses `json:"status,omitempty"`
}

// Addresses represents the list of addresses for a kubernetes nodes
type Addresses struct {
	IPAddresses []IPAddress `json:"addresses,omitempty"`
}

// IPAddress field represents individual address for a kubernetes node
// Contains type and ip address
type IPAddress struct {
	Type    string `json:"type,omitempty"`
	Address string `json:"address,omitempty"`
}

func getNodeIP(resp *http.Response, hostIP string) string {
	decoder := json.NewDecoder(resp.Body)
	nr := &NodeResponse{}

	err := decoder.Decode(nr)
	if err != nil {
		zap.S().Warnf("Unable to decode request: %v.", err)
		return hostIP
	}

	extIP := ""
	intIP := ""
	for _, node := range nr.Nodes {
		for _, ipa := range node.Status.IPAddresses {
			if ipa.Type == "ExternalIP" {
				extIP = ipa.Address
			}

			if ipa.Type == "InternalIP" {
				intIP = ipa.Address
			}
		}

		if intIP == hostIP {
			break
		}
	}

	if extIP == "" {
		zap.S().Warnf("Unable to fetch external IP of the instance.")
		return hostIP
	}

	return extIP
}

// prepareAerospikePrometheusExporterConfig updates exporter config template with dynamic config requirements.
// Only for service TLS and security configuration.
func prepareAerospikePrometheusExporterConfig() error {
	if apeEnabled != "true" {
		return nil
	}

	if serviceTLSOnly != "true" && securityEnabled != "true" {
		return nil
	}

	zap.S().Info("Preparing Exporter configuration file.")

	// Read exporter config template file
	apeConfigFile := apeConfigVolumePath + "/ape.toml.template"
	lines, err := fileToLines(apeConfigFile)
	if err != nil {
		return fmt.Errorf("error reading file %s: %v", apeConfigFile, err)
	}

	// Regex to locate lines in config file
	serverCertConfigRegexp := regexp.MustCompile(`^cert_file="\${AS_CERT_FILE}"`)
	caCertConfigRegexp := regexp.MustCompile(`^root_ca="\${AS_ROOT_CA}"`)
	keyCertConfigRegexp := regexp.MustCompile(`^key_file="\${AS_KEY_FILE}"`)
	tlsNameConfigRegexp := regexp.MustCompile(`^node_tls_name="\${AS_NODE_TLS_NAME}"`)
	tlsPortConfigRegexp := regexp.MustCompile(`^db_port=\${AS_PORT}`)

	securityAuthModeConfigRegexp := regexp.MustCompile(`^auth_mode="\${AS_AUTH_MODE}"`)
	securityUsernameConfigRegexp := regexp.MustCompile(`^user="\${AS_AUTH_USER}"`)
	securityPasswordConfigRegexp := regexp.MustCompile(`^password="\${AS_AUTH_PASSWORD}"`)

	// Update exporter config template file
	fileContent := ""
	for _, line := range lines {
		if serviceTLSOnly == "true" {
			if serverCertConfigRegexp.MatchString(line) {
				path, err := getCertFilePath(apeConfigVolumePath, serviceCertFile, serviceTLSName+"-service-cert.pem")
				if err != nil {
					return err
				}

				fileContent += "cert_file=\"" + path + "\"\n"
				continue
			}

			if caCertConfigRegexp.MatchString(line) {
				path, err := getCertFilePath(apeConfigVolumePath, serviceCAFile, serviceTLSName+"-service-cacert.pem")
				if err != nil {
					return err
				}

				fileContent += "root_ca=\"" + path + "\"\n"
				continue
			}

			if keyCertConfigRegexp.MatchString(line) {
				path, err := getCertFilePath(apeConfigVolumePath, serviceKeyFile, serviceTLSName+"-service-key.pem")
				if err != nil {
					return err
				}

				fileContent += "key_file=\"" + path + "\"\n"
				continue
			}

			if tlsNameConfigRegexp.MatchString(line) {
				fileContent += "node_tls_name=\"" + serviceTLSName + "\"\n"
				continue
			}

			if tlsPortConfigRegexp.MatchString(line) {
				fileContent += "db_port=" + serviceTLSPort + "\n"
				continue
			}
		}

		if securityEnabled == "true" {
			if securityAuthModeConfigRegexp.MatchString(line) {
				fileContent += "auth_mode=\"" + authMode + "\"\n"
				continue
			}

			if securityUsernameConfigRegexp.MatchString(line) {
				fileContent += "user=\"" + adminUsername + "\"\n"
				continue
			}

			if securityPasswordConfigRegexp.MatchString(line) {
				fileContent += "password=\"" + adminPassword + "\"\n"
				continue
			}
		}

		fileContent += line
		fileContent += "\n"
	}

	// Write updated exporter config template file
	err = ioutil.WriteFile(apeConfigFile, []byte(fileContent), 0644)
	if err != nil {
		return fmt.Errorf("error writing to file %s: %v", apeConfigFile, err)
	}

	// Success
	return nil
}
