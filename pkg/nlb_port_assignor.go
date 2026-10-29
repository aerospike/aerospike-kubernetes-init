package pkg

import (
	goctx "context"
	"encoding/json"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
)

// nlbPortOffsetsAnnotation stores port offsets keyed by "<rackID>-<ordinal>".
// Rack revision is not part of the key, so a revision roll keeps the external port.
// The operator reads the same annotation to program the cluster NLB Service.
const nlbPortOffsetsAnnotation = "aerospike.com/nlb-port-offsets"

// maxTCPPort is the highest legal TCP port. Offsets that push an advertised port
// past this value are not assigned.
const maxTCPPort = int32(65535)

const (
	// nlbPortOffsetMin is the minimum assignable offset (1; 0 is the unset sentinel).
	nlbPortOffsetMin = int32(1)
	// nlbMaxNodes is Aerospike's maximum supported node count.
	nlbMaxNodes = int32(256)
	// nlbPortOffsetSearchBound is the upper bound of the offset search range.
	// It is larger than nlbMaxNodes to guarantee 256 allocatable offsets even after
	// skipping offsets that collide with reserved ports (service, heartbeat, fabric,
	// admin — plain and TLS = 8 ports max). Adding 8 ensures we always find 256 clean offsets.
	nlbPortOffsetSearchBound = nlbMaxNodes + 8
)

// isNLBNetworkType returns true if any access type is clusterNLB.
func isNLBNetworkType(np asdbv1.AerospikeNetworkPolicy) bool {
	return np.AccessType == asdbv1.AerospikeNetworkTypeNLB ||
		np.AlternateAccessType == asdbv1.AerospikeNetworkTypeNLB ||
		np.TLSAccessType == asdbv1.AerospikeNetworkTypeNLB ||
		np.TLSAlternateAccessType == asdbv1.AerospikeNetworkTypeNLB
}

// nlbReservedPorts returns the set of all base Aerospike ports that no NLB
// advertised port (servicePort+offset or adminPort+offset) may collide with.
// This includes the base service, heartbeat, fabric, and admin ports (plain + TLS).
func (initp *InitParams) nlbReservedPorts() map[int32]bool {
	reserved := map[int32]bool{}

	for _, p := range []int32{
		initp.networkInfo.servicePort,
		initp.networkInfo.serviceTLSPort,
		initp.networkInfo.heartBeatPort,
		initp.networkInfo.heartBeatTLSPort,
		initp.networkInfo.fabricPort,
		initp.networkInfo.fabricTLSPort,
		initp.networkInfo.adminPort,
		initp.networkInfo.adminTLSPort,
	} {
		if p != 0 {
			reserved[p] = true
		}
	}

	return reserved
}

// nlbOffsetCollidesWithReserved returns true if the candidate offset would make any
// NLB advertised port (service or admin, plain or TLS) land on a reserved base port.
func (initp *InitParams) nlbOffsetCollidesWithReserved(offset int32, reserved map[int32]bool) bool {
	// Service port checks
	if p := initp.networkInfo.servicePort; p != 0 && reserved[p+offset] {
		return true
	}

	if p := initp.networkInfo.serviceTLSPort; p != 0 && reserved[p+offset] {
		return true
	}

	// Admin port checks — admin also gets a unique NLB port (adminPort + offset)
	if p := initp.networkInfo.adminPort; p != 0 && reserved[p+offset] {
		return true
	}

	if p := initp.networkInfo.adminTLSPort; p != 0 && reserved[p+offset] {
		return true
	}

	return false
}

// nlbBasePort returns the TLS-aware service port used as the base for NLB offset
// arithmetic. ServicePort in status = nlbBasePort() + nlbPortOffset, so on restart
// offset = ServicePort - nlbBasePort().
func (initp *InitParams) nlbBasePort() int32 {
	if initp.networkInfo.serviceTLSPort != 0 {
		return initp.networkInfo.serviceTLSPort
	}

	return initp.networkInfo.servicePort
}

// nlbAdminBasePort returns the TLS-aware admin port used as the NLB admin base.
func (initp *InitParams) nlbAdminBasePort() int32 {
	if initp.networkInfo.adminTLSPort != 0 {
		return initp.networkInfo.adminTLSPort
	}

	return initp.networkInfo.adminPort
}

// nlbStatusAccessType is the access type represented by status.pods[].servicePort.
// TLS wins when a service TLS port is configured, matching the pre-NLB status convention.
func (initp *InitParams) nlbStatusAccessType() asdbv1.AerospikeNetworkType {
	if initp.networkInfo.serviceTLSPort != 0 {
		return initp.aeroCluster.Spec.AerospikeNetworkPolicy.TLSAccessType
	}

	return initp.aeroCluster.Spec.AerospikeNetworkPolicy.AccessType
}

// useNLBStatusPort reports whether status service/admin ports should carry the NLB
// offset. Alternate-only clusterNLB still assigns an offset for aerospike.conf, but
// status.servicePort keeps the primary access port.
func (initp *InitParams) useNLBStatusPort() bool {
	return initp.nlbStatusAccessType() == asdbv1.AerospikeNetworkTypeNLB &&
		!asdbv1.GetBool(initp.aeroCluster.Spec.PodSpec.MultiPodPerHost) &&
		initp.nlbPortOffset > 0
}

// computeServicePort returns the value to store in status.pods[].servicePort.
//   - primary access is clusterNLB and multiPodPerHost=false: nlbBasePort() + offset
//   - Other node-network types: mapped / NodePort value
//   - Pod-network types:        0
func (initp *InitParams) computeServicePort() int32 {
	if initp.useNLBStatusPort() {
		return initp.nlbBasePort() + initp.nlbPortOffset
	}

	if initp.isNodeNetwork() {
		if initp.networkInfo.serviceTLSPort != 0 {
			return initp.networkInfo.mappedServiceTLSPort
		}

		return initp.networkInfo.mappedServicePort
	}

	return 0
}

// computeServiceAdminPort returns the value to store in status.pods[].serviceAdminPort.
// An unset admin port stays 0. The NLB offset is applied only when the primary access
// type is clusterNLB, and only when an admin listen port is configured.
func (initp *InitParams) computeServiceAdminPort() int32 {
	if initp.useNLBStatusPort() && initp.nlbAdminBasePort() != 0 {
		return initp.nlbAdminBasePort() + initp.nlbPortOffset
	}

	if initp.isNodeNetwork() {
		if initp.networkInfo.adminTLSPort != 0 {
			return initp.networkInfo.mappedAdminTLSPort
		}

		return initp.networkInfo.mappedAdminPort
	}

	return 0
}

// nlbOffsetValid reports whether offset can be advertised without colliding with a
// reserved listen port or exceeding the TCP port range.
func (initp *InitParams) nlbOffsetValid(offset int32) bool {
	if offset < nlbPortOffsetMin || offset > nlbPortOffsetSearchBound {
		return false
	}

	if initp.nlbOffsetCollidesWithReserved(offset, initp.nlbReservedPorts()) {
		return false
	}

	for _, base := range []int32{
		initp.networkInfo.servicePort,
		initp.networkInfo.serviceTLSPort,
		initp.networkInfo.adminPort,
		initp.networkInfo.adminTLSPort,
	} {
		if base != 0 && base+offset > maxTCPPort {
			return false
		}
	}

	return true
}

// nlbOffsetKey is "<rackID>-<ordinal>". Revision is ignored so cluster-1-a-0 and
// cluster-1-b-0 share one offset across a rack revision roll.
func nlbOffsetKey(clusterName, podName string) (string, error) {
	rackID, _, err := utils.GetRackIDAndRevisionFromPodName(clusterName, podName)
	if err != nil {
		return "", err
	}

	parts := strings.Split(podName, "-")
	ordinal := parts[len(parts)-1]

	return fmt.Sprintf("%d-%s", rackID, ordinal), nil
}

func parseNLBOffsets(cluster *asdbv1.AerospikeCluster) map[string]int32 {
	offsets := map[string]int32{}
	if cluster.Annotations == nil {
		return offsets
	}

	raw := cluster.Annotations[nlbPortOffsetsAnnotation]
	if raw == "" {
		return offsets
	}

	if err := json.Unmarshal([]byte(raw), &offsets); err != nil || offsets == nil {
		return map[string]int32{}
	}

	return offsets
}

// resolveNLBPortOffset is the entry point for NLB port offset management.
// It is a no-op for non-NLB clusters and multiPodPerHost=true.
// A stored offset is reused only when it is still inside the assignable range and
// does not collide with a reserved listen port.
func (initp *InitParams) resolveNLBPortOffset(ctx goctx.Context) error {
	if !isNLBNetworkType(initp.aeroCluster.Spec.AerospikeNetworkPolicy) {
		return nil
	}

	// For multiPodPerHost=true the NodePort is already the unique per-pod NLB port
	// and is captured in ServicePort by the existing NodePort service mechanism.
	if asdbv1.GetBool(initp.aeroCluster.Spec.PodSpec.MultiPodPerHost) {
		return nil
	}

	key, err := nlbOffsetKey(initp.aeroCluster.Name, initp.podName)
	if err != nil {
		return fmt.Errorf("nlb offset key for pod %s: %w", initp.podName, err)
	}

	if off, ok := parseNLBOffsets(initp.aeroCluster)[key]; ok && initp.nlbOffsetValid(off) {
		initp.logger.Info("NLB port offset already assigned", "pod", initp.podName, "offset", off)
		initp.nlbPortOffset = off

		return nil
	}

	return initp.assignNLBPortOffset(ctx, key)
}

// legacyNLBOffset recovers an offset stored by the previous ServicePort scheme.
// Values outside the assignable range (NodePorts, a lowered service port, TLS removed)
// are rejected so the pod takes a fresh offset instead of keeping a bad one.
func (initp *InitParams) legacyNLBOffset(servicePort int32) int32 {
	base := initp.nlbBasePort()
	if servicePort <= base {
		return 0
	}

	offset := servicePort - base
	if !initp.nlbOffsetValid(offset) {
		return 0
	}

	return offset
}

// assignNLBPortOffset picks a free offset and records it under the rack-ordinal key.
// Optimistic concurrency on the cluster annotation serialises concurrent pod starts.
func (initp *InitParams) assignNLBPortOffset(ctx goctx.Context, key string) error {
	var assignedOffset int32

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cluster := &asdbv1.AerospikeCluster{}
		if err := initp.k8sClient.Get(ctx, types.NamespacedName{
			Name:      initp.aeroCluster.Name,
			Namespace: initp.namespace,
		}, cluster); err != nil {
			return err
		}

		offsets := parseNLBOffsets(cluster)
		if off, ok := offsets[key]; ok && initp.nlbOffsetValid(off) {
			assignedOffset = off

			return nil
		}

		used := map[int32]bool{}

		for otherKey, off := range offsets {
			if otherKey == key {
				continue
			}

			if off >= nlbPortOffsetMin && off <= nlbPortOffsetSearchBound {
				used[off] = true
			}
		}

		for podName, ps := range cluster.Status.Pods {
			podKey, keyErr := nlbOffsetKey(cluster.Name, podName)
			if keyErr != nil || podKey == key {
				continue
			}

			if _, stored := offsets[podKey]; stored {
				continue
			}

			if off := initp.legacyNLBOffset(ps.ServicePort); off != 0 {
				used[off] = true
			}
		}

		chosen := int32(0)
		if ps, ok := cluster.Status.Pods[initp.podName]; ok {
			if off := initp.legacyNLBOffset(ps.ServicePort); off != 0 && !used[off] {
				chosen = off
			}
		}

		if chosen == 0 {
			for i := nlbPortOffsetMin; i <= nlbPortOffsetSearchBound; i++ {
				if !initp.nlbOffsetValid(i) || used[i] {
					continue
				}

				chosen = i

				break
			}
		}

		if chosen == 0 {
			return fmt.Errorf(
				"clusterNLB: no free port offset available (cluster has reached the %d-node limit)",
				nlbMaxNodes,
			)
		}

		base := cluster.DeepCopy()
		if cluster.Annotations == nil {
			cluster.Annotations = map[string]string{}
		}

		offsets[key] = chosen

		raw, err := json.Marshal(offsets)
		if err != nil {
			return err
		}

		cluster.Annotations[nlbPortOffsetsAnnotation] = string(raw)

		if err := initp.k8sClient.Patch(ctx, cluster, client.MergeFrom(base)); err != nil {
			if apierrors.IsConflict(err) {
				initp.logger.V(1).Info("Retrying NLB port offset assignment after conflict",
					"pod", initp.podName, "offset", chosen)
			}

			return err
		}

		assignedOffset = chosen

		return nil
	})
	if err != nil {
		return fmt.Errorf("assign NLB port offset for pod %s: %w", initp.podName, err)
	}

	initp.logger.Info("Assigned NLB port offset", "pod", initp.podName, "offset", assignedOffset)
	initp.nlbPortOffset = assignedOffset

	return nil
}

// nlbAdvertisedPort returns the port to advertise for clusterNLB access.
// For multiPodPerHost=true: the NodePort (already in mappedServicePort from getPorts()).
// For multiPodPerHost=false: servicePort + nlbPortOffset.
func (initp *InitParams) nlbAdvertisedPort(servicePort, mappedServicePort int32) int32 {
	if asdbv1.GetBool(initp.aeroCluster.Spec.PodSpec.MultiPodPerHost) {
		return mappedServicePort // NodePort already resolved by setIPAndPorts()
	}

	return servicePort + initp.nlbPortOffset
}

// nlbAddressForType returns the NLB FQDN/IP configured for the given address type.
func nlbAddressForType(np asdbv1.AerospikeNetworkPolicy, addressType string) string {
	switch addressType {
	case access:
		return np.NLBAccessAddress
	case alternateAccess:
		return np.NLBAlternateAccessAddress
	case tlsAccess:
		return np.NLBTLSAccessAddress
	case tlsAlternateAccess:
		return np.NLBTLSAlternateAccessAddress
	}

	return ""
}
