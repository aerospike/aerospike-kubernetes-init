package pkg

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientGoScheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newFakeClientWithNodes(t *testing.T, nodes ...*corev1.Node) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := clientGoScheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to build scheme: %v", err)
	}

	objects := make([]client.Object, 0, len(nodes))
	for _, node := range nodes {
		objects = append(objects, node)
	}

	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func node(name string, labels map[string]string, addresses ...corev1.NodeAddress) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Status: corev1.NodeStatus{
			Addresses: addresses,
		},
	}
}

func address(addrType corev1.NodeAddressType, ip string) corev1.NodeAddress {
	return corev1.NodeAddress{
		Type:    addrType,
		Address: ip,
	}
}

func TestGetHostIPS(t *testing.T) {
	const (
		ipv6Internal = "fd00::10"
		ipv6External = "2001:db8::10"
		ipv4Internal = "10.0.0.10"
		ipv4External = "192.0.2.10"
	)

	testCases := []struct {
		name            string
		hostIP          string
		wantInternalIP  string
		wantExternalIP  string
		wantAccessIP    string
		wantAlternateIP string
		nodes           []*corev1.Node
	}{
		{
			name:   "IPv6 single-stack node",
			hostIP: ipv6Internal,
			nodes: []*corev1.Node{
				node("ipv6-node", nil,
					address(corev1.NodeInternalIP, ipv6Internal),
					address(corev1.NodeExternalIP, ipv6External),
				),
			},
			wantInternalIP: ipv6Internal,
			wantExternalIP: ipv6External,
		},
		{
			name:   "IPv4 single-stack node",
			hostIP: ipv4Internal,
			nodes: []*corev1.Node{
				node("ipv4-node", nil,
					address(corev1.NodeInternalIP, ipv4Internal),
					address(corev1.NodeExternalIP, ipv4External),
				),
			},
			wantInternalIP: ipv4Internal,
			wantExternalIP: ipv4External,
		},
		{
			name:   "hostname addresses and malformed IPs are ignored",
			hostIP: ipv6Internal,
			nodes: []*corev1.Node{
				node("noisy-node", nil,
					address(corev1.NodeHostName, "worker-0"),
					address(corev1.NodeInternalIP, "not-an-ip"),
					address(corev1.NodeInternalIP, ipv6Internal),
				),
			},
			wantInternalIP: ipv6Internal,
			// No parsable external address, so the host IP is kept.
			wantExternalIP: ipv6Internal,
		},
		{
			name:   "configured access labels are read from the matching node",
			hostIP: ipv6Internal,
			nodes: []*corev1.Node{
				node("labelled-node", map[string]string{
					configuredAccessIPLabel:          "2001:db8::beef",
					configuredAlternateAccessIPLabel: "2001:db8::cafe",
				},
					address(corev1.NodeInternalIP, ipv6Internal),
				),
			},
			wantInternalIP:  ipv6Internal,
			wantExternalIP:  ipv6Internal,
			wantAccessIP:    "2001:db8::beef",
			wantAlternateIP: "2001:db8::cafe",
		},
		{
			name:   "only the node holding the host IP is used",
			hostIP: ipv6Internal,
			nodes: []*corev1.Node{
				node("other-node", nil,
					address(corev1.NodeInternalIP, "fd00::99"),
					address(corev1.NodeExternalIP, "2001:db8::99"),
				),
				node("matching-node", nil,
					address(corev1.NodeInternalIP, ipv6Internal),
					address(corev1.NodeExternalIP, ipv6External),
				),
			},
			wantInternalIP: ipv6Internal,
			wantExternalIP: ipv6External,
		},
		{
			name:           "no matching node leaves the host IP untouched",
			hostIP:         ipv6Internal,
			nodes:          []*corev1.Node{node("unrelated-node", nil, address(corev1.NodeInternalIP, "fd00::99"))},
			wantInternalIP: ipv6Internal,
			wantExternalIP: ipv6Internal,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			k8sClient := newFakeClientWithNodes(t, testCase.nodes...)

			internalIP, externalIP, accessIP, alternateIP, err := getHostIPS(
				context.Background(), k8sClient, testCase.hostIP,
			)
			if err != nil {
				t.Fatalf("getHostIPS returned an error: %v", err)
			}

			if internalIP != testCase.wantInternalIP {
				t.Errorf("internalIP = %q, want %q", internalIP, testCase.wantInternalIP)
			}

			if externalIP != testCase.wantExternalIP {
				t.Errorf("externalIP = %q, want %q", externalIP, testCase.wantExternalIP)
			}

			if accessIP != testCase.wantAccessIP {
				t.Errorf("configuredAccessIP = %q, want %q", accessIP, testCase.wantAccessIP)
			}

			if alternateIP != testCase.wantAlternateIP {
				t.Errorf("configuredAlternateAccessIP = %q, want %q", alternateIP, testCase.wantAlternateIP)
			}
		})
	}
}
