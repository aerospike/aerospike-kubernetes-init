package main

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/sets"
)

// getPeers find peers of the aerospike deployment by doing a DNS lookup using the headless service name
func getPeers(retryCount int, retryPeriod time.Duration) ([]string, error) {
	// Sanity check
	if podName == "" || podNamespace == "" || aerospikeHeadlessServiceName == "" || serviceDNSDomain == "" {
		return nil, fmt.Errorf("variables empty, can't proceed")
	}

	// Generate FQDN for self
	domainName := strings.Join([]string{podNamespace, "svc", serviceDNSDomain}, ".")
	selfName := strings.Join([]string{podName, aerospikeHeadlessServiceName, domainName}, ".")
	zap.S().Infof("Self: %s", selfName)

	retryAttempt := 0
	peersList := []string{}
	for currPeers := sets.NewString(); retryAttempt < retryCount; retryAttempt = retryAttempt + 1 {
		// Lookup aerospike headless service
		newPeers, err := doLookup(aerospikeHeadlessServiceName)
		if err != nil {
			zap.S().Warnf("Error during DNS lookup: %v. Retrying.", err)
			time.Sleep(retryPeriod)
			continue
		}

		if newPeers.Equal(currPeers) || !newPeers.Has(selfName) {
			zap.S().Warnf("Unable to locate self during DNS lookup. Retrying.")
			time.Sleep(retryPeriod)
			continue
		}

		peersList = newPeers.List()
		sort.Strings(peersList)

		// Update new peers
		currPeers = newPeers

		// Print new peers list
		zap.S().Info("Found self during DNS lookup. Peers list updated.")
		for _, peer := range peersList {
			zap.S().Debug(peer)
		}

		// Success
		break
	}

	// Check if retries exhausted
	if retryAttempt >= retryCount {
		return nil, fmt.Errorf("retries exhausted")
	}

	// Sanity check
	if len(peersList) == 0 {
		return nil, fmt.Errorf("peers list is empty")
	}

	// Success
	return peersList, nil
}

// doLookup performs DNS lookup to resolve serviceName into unique pod FQDNs
func doLookup(serviceName string) (sets.String, error) {
	endpoints := sets.NewString()

	_, records, err := net.LookupSRV("", "", serviceName)
	if err != nil {
		return endpoints, err
	}

	for _, record := range records {
		// Exclude the "." (dot) at the end in the SRV records.
		endpoint := fmt.Sprintf("%v", record.Target[:len(record.Target)-1])
		// ipList, _ := net.LookupIP(endpoint)
		endpoints.Insert(endpoint)
	}

	return endpoints, nil
}
