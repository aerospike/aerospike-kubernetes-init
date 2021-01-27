package main

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	aerospike "github.com/aerospike/aerospike-client-go"

	"go.uber.org/zap"
)

// Read certificate file and abort if any errors
// Returns file content as byte array
func readCertFile(filename string) []byte {
	dataBytes, err := ioutil.ReadFile(filename)
	if err != nil {
		zap.S().Fatalf("Failed to read certificate or key file `%s` : `%s`", filename, err)
	}

	return dataBytes
}

// Initialize Aerospike client
func initAerospikeClient(host string, username string, password string) (*aerospike.Client, error) {
	clientPolicy := aerospike.NewClientPolicy()
	tlsConfig := initTLSConfig()

	if securityEnabled == "true" {
		clientPolicy.User = username
		clientPolicy.Password = password

		if authMode == "external" {
			clientPolicy.AuthMode = aerospike.AuthModeExternal
		}
	}

	clientPolicy.Timeout = 5 * time.Second
	clientPolicy.TlsConfig = tlsConfig

	port := servicePlainPort
	tlsName := ""
	if clientPolicy.TlsConfig != nil {
		port = serviceTLSPort
		tlsName = serviceTLSName
	}
	portInt, _ := strconv.Atoi(port)

	server := aerospike.NewHost(host, portInt)
	server.TLSName = tlsName
	zap.S().Debugf("Connecting to aerospike node %s:%d.", host, portInt)

	client, err := aerospike.NewClientWithPolicyAndHost(clientPolicy, server)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// Create a connection to Aerospike node
func initAerospikeConnection(host string, username string, password string) (*aerospike.Connection, error) {
	clientPolicy := aerospike.NewClientPolicy()
	tlsConfig := initTLSConfig()

	if securityEnabled == "true" {
		clientPolicy.User = username
		clientPolicy.Password = password

		if authMode == "external" {
			clientPolicy.AuthMode = aerospike.AuthModeExternal
		}
	}

	// only one connection
	clientPolicy.ConnectionQueueSize = 1
	clientPolicy.Timeout = 5 * time.Second
	clientPolicy.TlsConfig = tlsConfig

	port := servicePlainPort
	tlsName := ""
	if clientPolicy.TlsConfig != nil {
		port = serviceTLSPort
		tlsName = serviceTLSName
	}
	portInt, _ := strconv.Atoi(port)

	server := aerospike.NewHost(host, portInt)
	server.TLSName = tlsName
	zap.S().Debugf("Connecting to aerospike node %s:%d.", host, portInt)

	connection, err := aerospike.NewConnection(clientPolicy, server)
	if err != nil {
		return nil, err
	}

	if clientPolicy.RequiresAuthentication() {
		if err := connection.Login(clientPolicy); err != nil {
			return nil, err
		}
	}

	return connection, nil
}

// Initialize TLS config
func initTLSConfig() *tls.Config {
	var tlsConfig *tls.Config

	if serviceTLSEnabled == "true" {
		serverPool, err := x509.SystemCertPool()
		if serverPool == nil || err != nil {
			zap.S().Debugf("Adding system certificates to the cert pool failed: %s.", err)
			serverPool = x509.NewCertPool()
		}

		if len(serviceCAFile) > 0 {
			path, err := getCertFilePath(aerospikeConfigVolumePath, serviceCAFile, serviceTLSName+"-service-cacert.pem")
			if err != nil {
				zap.S().Fatal("Unable to get certificate file path: %v.", err)
			}

			// Try to load system CA certs and add them to the system cert pool
			caCert := readCertFile(path)

			zap.S().Debugf("Adding server certificate `%s` to the pool.", path)
			serverPool.AppendCertsFromPEM(caCert)
		}

		var clientPool []tls.Certificate
		if len(serviceCertFile) > 0 || len(serviceKeyFile) > 0 {
			certPath, err := getCertFilePath(aerospikeConfigVolumePath, serviceCertFile, serviceTLSName+"-service-cert.pem")
			if err != nil {
				zap.S().Fatal("Unable to get certificate file path: %v.", err)
			}

			keyPath, err := getCertFilePath(aerospikeConfigVolumePath, serviceKeyFile, serviceTLSName+"-service-key.pem")
			if err != nil {
				zap.S().Fatal("Unable to get key file path: %v.", err)
			}

			// Read Cert and Key files
			certFileBytes := readCertFile(certPath)
			keyFileBytes := readCertFile(keyPath)

			// Decode PEM data
			keyBlock, _ := pem.Decode(keyFileBytes)
			certBlock, _ := pem.Decode(certFileBytes)

			if keyBlock == nil || certBlock == nil {
				zap.S().Fatalf("Unable to decode PEM data for `%s` or `%s`.", keyPath, certPath)
			}

			// Encode PEM data
			keyPEM := pem.EncodeToMemory(keyBlock)
			certPEM := pem.EncodeToMemory(certBlock)

			if keyPEM == nil || certPEM == nil {
				zap.S().Fatalf("Unable to encode PEM data for `%s` or `%s`.", keyPath, certPath)
			}

			cert, err := tls.X509KeyPair(certPEM, keyPEM)

			if err != nil {
				zap.S().Fatalf("Unable to add client certificate `%s` and key file `%s` to the pool: `%s`.", certPath, keyPath, err)
			}

			zap.S().Debugf("Adding client certificate `%s` to the pool.", certPath)
			clientPool = append(clientPool, cert)
		}

		tlsConfig = &tls.Config{
			Certificates:             clientPool,
			RootCAs:                  serverPool,
			InsecureSkipVerify:       false,
			PreferServerCipherSuites: true,
		}
		tlsConfig.BuildNameToCertificate()
	}

	return tlsConfig
}

// Get certificate file path
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
			return configMountPoint + "/certs/" + fileName, nil
		default:
			return "", fmt.Errorf("Invalid option while parsing cert file: %s", parsedCertFile[0])
		}
	}

	// Should not reach here
	return "", fmt.Errorf("Unable to parse cert file: %s", certFile)
}

// Update global variables from ENV variable inputs
func initVars() {
	zap.S().Info("Initializing variables.")

	podIP, ok := os.LookupEnv("MY_POD_IP")
	if ok {
		myPodIP = podIP
	}

	secEnabled, ok := os.LookupEnv("SECURITY_ENABLED")
	if ok {
		securityEnabled = secEnabled
	}

	helmusr, ok := os.LookupEnv("HELM_USERNAME")
	if ok {
		helmUsername = helmusr
	}

	helmpass, ok := os.LookupEnv("HELM_PASSWORD")
	if ok {
		helmPassword = helmpass
	}

	adminusr, ok := os.LookupEnv("ADMIN_USERNAME")
	if ok {
		adminUsername = adminusr
	}

	adminpass, ok := os.LookupEnv("ADMIN_PASSWORD")
	if ok {
		adminPassword = adminpass
	}

	auth, ok := os.LookupEnv("AUTH_MODE")
	if ok {
		authMode = auth
	}

	tlsEnabled, ok := os.LookupEnv("SERVICE_TLS_ENABLED")
	if ok {
		serviceTLSEnabled = tlsEnabled
	}

	tlsCAFile, ok := os.LookupEnv("SERVICE_CA_FILE")
	if ok {
		serviceCAFile = tlsCAFile
	}

	tlsCertFile, ok := os.LookupEnv("SERVICE_CERT_FILE")
	if ok {
		serviceCertFile = tlsCertFile
	}

	tlsKeyFile, ok := os.LookupEnv("SERVICE_KEY_FILE")
	if ok {
		serviceKeyFile = tlsKeyFile
	}

	tlsName, ok := os.LookupEnv("SERVICE_TLS_NAME")
	if ok {
		serviceTLSName = tlsName
	}

	tlsMutualAuth, ok := os.LookupEnv("SERVICE_MUTUAL_AUTH")
	if ok {
		serviceMutualAuth = tlsMutualAuth
	}

	tlsPort, ok := os.LookupEnv("SERVICE_TLS_PORT")
	if ok {
		serviceTLSPort = tlsPort
	}

	plainPort, ok := os.LookupEnv("SERVICE_PLAIN_PORT")
	if ok {
		servicePlainPort = plainPort
	}
}

// InfoParser provides a reader for Aerospike cluster's response for any of the metric
type InfoParser struct {
	*bufio.Reader
}

// NewInfoParser provides an instance of the InfoParser
func NewInfoParser(s string) *InfoParser {
	return &InfoParser{bufio.NewReader(strings.NewReader(s))}
}

// PeekAndExpect checks if the expected value is present without advancing the reader
func (ip *InfoParser) PeekAndExpect(s string) error {
	bytes, err := ip.Peek(len(s))
	if err != nil {
		return err
	}

	v := string(bytes)
	if v != s {
		return fmt.Errorf("InfoParser: Wrong value. Peek expected %s, but found %s", s, v)
	}

	return nil
}

// Expect validates the expected value against the one returned by the InfoParser
// This advances the reader by length of the input string.
func (ip *InfoParser) Expect(s string) error {
	bytes := make([]byte, len(s))

	v, err := ip.Read(bytes)
	if err != nil {
		return err
	}

	if string(bytes) != s {
		return fmt.Errorf("InfoParser: Wrong value. Expected %s, found %d", s, v)
	}

	return nil
}

// ReadUntil reads bytes from the InfoParser by handeling some edge-cases
func (ip *InfoParser) ReadUntil(delim byte) (string, error) {
	v, err := ip.ReadBytes(delim)

	switch len(v) {
	case 0:
		return string(v), err
	case 1:
		if v[0] == delim {
			return "", err
		}
		return string(v), err
	}

	return string(v[:len(v)-1]), err
}

// Get ops/sec
// Format (with and without latency data)
// {test}-read:10:17:37-GMT,ops/sec,>1ms,>8ms,>64ms;10:17:47,29648.2,3.44,0.08,0.00;
// error-no-data-yet-or-back-too-small;
// or,
// {test}-write:;
func getOpsPerSecLegacy(s string) (opsPerSec float64, err error) {
	ip := NewInfoParser(s)
	for {
		if err := ip.Expect("{"); err != nil {
			// it's an error string, read to next section
			if _, err := ip.ReadUntil(';'); err != nil {
				break
			}
			continue
		}

		// namespace name
		_, err := ip.ReadUntil('}')
		if err != nil {
			break
		}

		if err := ip.Expect("-"); err != nil {
			break
		}

		// operation (read, write etc.)
		_, err = ip.ReadUntil(':')
		if err != nil {
			break
		}

		// Might be an empty output if there's no latency data (in 5.1), so continue to next section
		if err := ip.PeekAndExpect(";"); err == nil {
			if err := ip.Expect(";"); err != nil {
				break
			}
			continue
		}

		// Ignore timestamp
		_, err = ip.ReadUntil(',')
		if err != nil {
			break
		}

		// Ignore labels
		_, err = ip.ReadUntil(';')
		if err != nil {
			break
		}

		// Ignore timestamp
		_, err = ip.ReadUntil(',')
		if err != nil {
			break
		}

		// Read bucket values
		bucketValuesStr, err := ip.ReadUntil(';')
		if err != nil && err != io.EOF {
			break
		}
		bucketValues := strings.Split(bucketValuesStr, ",")

		val, err := strconv.ParseFloat(bucketValues[0], 64)
		if err != nil {
			break
		}

		opsPerSec += val
	}

	return opsPerSec, nil
}

// Get ops/sec
// Format (with and without latency data)
// {test}-write:msec,4234.9,28.75,7.40,1.63,0.26,0.03,0.00,0.00,0.00,0.00,0.00,0.00,0.00,0.00,0.00,0.00,0.00,0.00;
// {test}-read:;
func getOpsPerSecNew(s string) (opsPerSec float64, err error) {
	ip := NewInfoParser(s)
	for {
		if err = ip.Expect("{"); err != nil {
			if _, err = ip.ReadUntil(';'); err != nil {
				break
			}
			continue
		}

		// namespace name
		_, err = ip.ReadUntil('}')
		if err != nil {
			break
		}

		if err = ip.Expect("-"); err != nil {
			break
		}

		// operation (read, write etc.)
		_, err = ip.ReadUntil(':')
		if err != nil {
			break
		}

		// Might be an empty output due to no latency data available, so continue to next section
		if err = ip.PeekAndExpect(";"); err == nil {
			if err = ip.Expect(";"); err != nil {
				break
			}
			continue
		}

		// time unit - msec or usec
		_, err = ip.ReadUntil(',')
		if err != nil {
			break
		}

		// Read bucket values
		bucketValuesStr, err := ip.ReadUntil(';')
		if err != nil && err != io.EOF {
			break
		}
		bucketValues := strings.Split(bucketValuesStr, ",")

		val, err := strconv.ParseFloat(bucketValues[0], 64)
		if err != nil {
			break
		}

		opsPerSec += val
	}

	return opsPerSec, nil
}

func parseStats(s, sep string) map[string]string {
	stats := make(map[string]string, strings.Count(s, sep)+1)
	s2 := strings.Split(s, sep)
	for _, s := range s2 {
		list := strings.SplitN(s, "=", 2)
		switch len(list) {
		case 0, 1:
		case 2:
			stats[list[0]] = list[1]
		default:
			stats[list[0]] = strings.Join(list[1:], "=")
		}
	}

	return stats
}
