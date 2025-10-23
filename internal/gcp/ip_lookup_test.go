package gcp

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/api/compute/v1"
)

func TestInstanceIPMatches(t *testing.T) {
	instance := &compute.Instance{
		NetworkInterfaces: []*compute.NetworkInterface{
			{
				Name:      "nic0",
				NetworkIP: "10.0.0.1",
				AccessConfigs: []*compute.AccessConfig{
					{
						Name:  "External NAT",
						NatIP: "203.0.113.10",
					},
				},
			},
			{
				Name:      "nic1",
				NetworkIP: "10.0.1.5",
			},
		},
	}

	internalMatches := instanceIPMatches(instance, net.ParseIP("10.0.0.1"))
	require.Len(t, internalMatches, 1)
	require.Equal(t, IPAssociationInstanceInternal, internalMatches[0].kind)

	externalMatches := instanceIPMatches(instance, net.ParseIP("203.0.113.10"))
	require.Len(t, externalMatches, 1)
	require.Equal(t, IPAssociationInstanceExternal, externalMatches[0].kind)
}

func TestInstanceIPMatchesIPv6(t *testing.T) {
	instance := &compute.Instance{
		NetworkInterfaces: []*compute.NetworkInterface{
			{
				Name:        "nic0",
				Ipv6Address: "2600:abcd::5",
				AccessConfigs: []*compute.AccessConfig{
					{NatIP: "2600:abcd::100"},
				},
			},
		},
	}

	internal := instanceIPMatches(instance, net.ParseIP("2600:abcd::5"))
	require.Len(t, internal, 1)
	require.Equal(t, IPAssociationInstanceInternal, internal[0].kind)

	external := instanceIPMatches(instance, net.ParseIP("2600:abcd::100"))
	require.Len(t, external, 1)
	require.Equal(t, IPAssociationInstanceExternal, external[0].kind)
}

func TestDescribeForwardingRule(t *testing.T) {
	rule := &compute.ForwardingRule{
		LoadBalancingScheme: "EXTERNAL",
		PortRange:           "80-81",
		Target:              "https://www.googleapis.com/compute/v1/projects/proj/regions/us-central1/targetHttpsProxies/proxy-1",
	}

	desc := describeForwardingRule(rule)
	require.NotEmpty(t, desc)
	require.Contains(t, desc, "scheme=external")
	require.Contains(t, desc, "ports=80-81")
	require.Contains(t, desc, "target=proxy-1")
}

func TestDescribeAddress(t *testing.T) {
	addr := &compute.Address{
		Status:      "IN_USE",
		Purpose:     "GCE_ENDPOINT",
		NetworkTier: "PREMIUM",
		AddressType: "EXTERNAL",
	}

	desc := describeAddress(addr)
	require.NotEmpty(t, desc)

	for _, want := range []string{"status=in_use", "purpose=gce_endpoint", "tier=premium", "type=external"} {
		require.Contains(t, desc, want)
	}
}

func TestLocationFromScope(t *testing.T) {
	tests := map[string]string{
		"zones/us-central1-a":   "us-central1-a",
		"regions/us-central1":   "us-central1",
		"global":                "global",
		"projects/p/global":     "global",
		"custom/scope":          "scope",
		"projects/x/regions/r1": "r1",
	}

	for scope, want := range tests {
		require.Equal(t, want, locationFromScope(scope))
	}
}

func TestLastComponent(t *testing.T) {
	tests := map[string]string{
		"projects/p/regions/r1/targets/t1": "t1",
		"simple-resource":                  "simple-resource",
		"":                                 "",
	}

	for input, want := range tests {
		require.Equal(t, want, lastComponent(input))
	}
}

func TestEqualIP(t *testing.T) {
	ipv4 := net.ParseIP("192.0.2.10")
	require.True(t, equalIP("192.0.2.10", ipv4))
	require.False(t, equalIP("192.0.2.11", ipv4))

	ipv6 := net.ParseIP("2001:db8::1")
	require.True(t, equalIP("2001:0db8:0:0::1", ipv6))
	require.False(t, equalIP("not-an-ip", ipv6))
}
