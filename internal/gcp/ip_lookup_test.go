package gcp

import (
	"net"
	"strings"
	"testing"

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
	if len(internalMatches) != 1 {
		t.Fatalf("expected 1 internal match, got %d", len(internalMatches))
	}

	if internalMatches[0].kind != IPAssociationInstanceInternal {
		t.Fatalf("expected internal match kind, got %s", internalMatches[0].kind)
	}

	externalMatches := instanceIPMatches(instance, net.ParseIP("203.0.113.10"))
	if len(externalMatches) != 1 {
		t.Fatalf("expected 1 external match, got %d", len(externalMatches))
	}

	if externalMatches[0].kind != IPAssociationInstanceExternal {
		t.Fatalf("expected external match kind, got %s", externalMatches[0].kind)
	}
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
	if len(internal) != 1 {
		t.Fatalf("expected 1 IPv6 internal match, got %d", len(internal))
	}

	if internal[0].kind != IPAssociationInstanceInternal {
		t.Fatalf("expected internal kind for IPv6, got %s", internal[0].kind)
	}

	external := instanceIPMatches(instance, net.ParseIP("2600:abcd::100"))
	if len(external) != 1 {
		t.Fatalf("expected 1 IPv6 external match, got %d", len(external))
	}

	if external[0].kind != IPAssociationInstanceExternal {
		t.Fatalf("expected external kind for IPv6, got %s", external[0].kind)
	}
}

func TestDescribeForwardingRule(t *testing.T) {
	rule := &compute.ForwardingRule{
		LoadBalancingScheme: "EXTERNAL",
		PortRange:           "80-81",
		Target:              "https://www.googleapis.com/compute/v1/projects/proj/regions/us-central1/targetHttpsProxies/proxy-1",
	}

	desc := describeForwardingRule(rule)
	if desc == "" {
		t.Fatal("expected non-empty description")
	}

	if want := "scheme=external"; !contains(desc, want) {
		t.Fatalf("expected description to contain %q, got %q", want, desc)
	}

	if want := "ports=80-81"; !contains(desc, want) {
		t.Fatalf("expected description to contain %q, got %q", want, desc)
	}

	if want := "target=proxy-1"; !contains(desc, want) {
		t.Fatalf("expected description to contain %q, got %q", want, desc)
	}
}

func TestDescribeAddress(t *testing.T) {
	addr := &compute.Address{
		Status:      "IN_USE",
		Purpose:     "GCE_ENDPOINT",
		NetworkTier: "PREMIUM",
		AddressType: "EXTERNAL",
	}

	desc := describeAddress(addr)
	if desc == "" {
		t.Fatal("expected non-empty description")
	}

	for _, want := range []string{"status=in_use", "purpose=gce_endpoint", "tier=premium", "type=external"} {
		if !contains(desc, want) {
			t.Fatalf("expected description to contain %q, got %q", want, desc)
		}
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
		if got := locationFromScope(scope); got != want {
			t.Fatalf("locationFromScope(%q) = %q, want %q", scope, got, want)
		}
	}
}

func TestLastComponent(t *testing.T) {
	tests := map[string]string{
		"projects/p/regions/r1/targets/t1": "t1",
		"simple-resource":                  "simple-resource",
		"":                                 "",
	}

	for input, want := range tests {
		if got := lastComponent(input); got != want {
			t.Fatalf("lastComponent(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestEqualIP(t *testing.T) {
	ipv4 := net.ParseIP("192.0.2.10")
	if !equalIP("192.0.2.10", ipv4) {
		t.Fatal("expected IPv4 values to match")
	}

	if equalIP("192.0.2.11", ipv4) {
		t.Fatal("unexpected IPv4 match")
	}

	ipv6 := net.ParseIP("2001:db8::1")
	if !equalIP("2001:0db8:0:0::1", ipv6) {
		t.Fatal("expected IPv6 values to match despite formatting")
	}

	if equalIP("not-an-ip", ipv6) {
		t.Fatal("unexpected match for invalid IP string")
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
