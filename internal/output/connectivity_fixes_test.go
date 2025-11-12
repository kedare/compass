package output

import (
	"testing"

	"github.com/kedare/compass/internal/gcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newFixContext(protocol string, srcIP string, dstIP string, dstPort int64, projectID string) fixContext {
	return fixContext{
		result: &gcp.ConnectivityTestResult{
			Protocol:  protocol,
			ProjectID: projectID,
		},
		details: &gcp.ReachabilityDetails{
			Traces: []*gcp.Trace{},
		},
		source: &gcp.EndpointInfo{
			IPAddress: srcIP,
		},
		destination: &gcp.EndpointInfo{
			IPAddress: dstIP,
			Port:      dstPort,
		},
		reverse: false,
	}
}

func TestViewerPermissionChecker(t *testing.T) {
	t.Parallel()
	checker := &viewerPermissionChecker{}
	ctx := newFixContext("TCP", "10.0.0.1", "10.0.0.2", 443, "project-1")

	t.Run("detects missing permissions", func(t *testing.T) {
		step := &gcp.TraceStep{
			State: "VIEWER_PERMISSION_MISSING",
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "Insufficient permissions")
		assert.Contains(t, suggestion, "compute.networkViewer")
	})

	t.Run("returns empty for normal step", func(t *testing.T) {
		step := &gcp.TraceStep{
			State: "APPLY_ROUTE",
		}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})
}

func TestAbortCauseChecker(t *testing.T) {
	t.Parallel()
	checker := &abortCauseChecker{}
	ctx := newFixContext("TCP", "10.0.0.1", "10.0.0.2", 443, "project-1")

	t.Run("detects abort cause", func(t *testing.T) {
		step := &gcp.TraceStep{
			AbortCause: "Configuration error detected",
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "Analysis aborted")
		assert.Contains(t, suggestion, "Configuration error detected")
	})

	t.Run("returns empty when no abort", func(t *testing.T) {
		step := &gcp.TraceStep{}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})
}

func TestEgressFirewallChecker(t *testing.T) {
	t.Parallel()
	checker := &egressFirewallChecker{}
	ctx := newFixContext("TCP", "10.0.0.1", "10.0.0.2", 443, "project-1")

	t.Run("detects egress firewall block", func(t *testing.T) {
		step := &gcp.TraceStep{
			Firewall:   "egress-deny-all",
			CausesDrop: true,
			State:      "APPLY_EGRESS_FIREWALL_RULE",
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "Egress firewall rule")
		assert.Contains(t, suggestion, "10.0.0.2:443")
	})

	t.Run("returns empty for ingress rule", func(t *testing.T) {
		step := &gcp.TraceStep{
			Firewall:   "ingress-deny-all",
			CausesDrop: true,
			State:      "APPLY_INGRESS_FIREWALL_RULE",
		}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})

	t.Run("returns empty when not dropping", func(t *testing.T) {
		step := &gcp.TraceStep{
			Firewall:   "egress-allow",
			CausesDrop: false,
			State:      "APPLY_EGRESS_FIREWALL_RULE",
		}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})
}

func TestIngressFirewallChecker(t *testing.T) {
	t.Parallel()
	checker := &ingressFirewallChecker{}
	ctx := newFixContext("TCP", "10.0.0.1", "10.0.0.2", 443, "project-1")

	t.Run("detects ingress firewall block", func(t *testing.T) {
		step := &gcp.TraceStep{
			Firewall:   "ingress-deny-all",
			CausesDrop: true,
			State:      "APPLY_INGRESS_FIREWALL_RULE",
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "Add firewall rule allowing TCP traffic")
		assert.Contains(t, suggestion, "from 10.0.0.1")
		assert.Contains(t, suggestion, "to 10.0.0.2:443")
	})

	t.Run("returns empty for egress rule", func(t *testing.T) {
		step := &gcp.TraceStep{
			Firewall:   "egress-deny-all",
			CausesDrop: true,
			State:      "APPLY_EGRESS_FIREWALL_RULE",
		}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})
}

func TestBlackholeRouteChecker(t *testing.T) {
	t.Parallel()
	checker := &blackholeRouteChecker{}
	ctx := newFixContext("TCP", "10.0.0.1", "10.0.0.2", 443, "project-1")

	t.Run("detects blackhole route", func(t *testing.T) {
		step := &gcp.TraceStep{
			Route:            "default-route",
			CausesDrop:       true,
			RouteNextHopType: "NEXT_HOP_BLACKHOLE",
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "blackhole")
		assert.Contains(t, suggestion, "Update route")
	})

	t.Run("returns empty for valid route", func(t *testing.T) {
		step := &gcp.TraceStep{
			Route:            "default-route",
			CausesDrop:       false,
			RouteNextHopType: "NEXT_HOP_INSTANCE",
		}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})
}

func TestVPNTunnelChecker(t *testing.T) {
	t.Parallel()
	checker := &vpnTunnelChecker{}
	ctx := newFixContext("TCP", "10.0.0.1", "192.168.1.1", 443, "project-1")

	t.Run("detects VPN tunnel issue", func(t *testing.T) {
		step := &gcp.TraceStep{
			Route:            "vpn-route",
			CausesDrop:       true,
			RouteNextHopType: "NEXT_HOP_VPN_TUNNEL",
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "VPN tunnel")
		assert.Contains(t, suggestion, "IKE configuration")
	})

	t.Run("returns empty when tunnel is working", func(t *testing.T) {
		step := &gcp.TraceStep{
			Route:            "vpn-route",
			CausesDrop:       false,
			RouteNextHopType: "NEXT_HOP_VPN_TUNNEL",
		}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})
}

func TestInterconnectChecker(t *testing.T) {
	t.Parallel()
	checker := &interconnectChecker{}
	ctx := newFixContext("TCP", "10.0.0.1", "192.168.1.1", 443, "project-1")

	t.Run("detects Interconnect issue", func(t *testing.T) {
		step := &gcp.TraceStep{
			Route:            "interconnect-route",
			CausesDrop:       true,
			RouteNextHopType: "NEXT_HOP_INTERCONNECT",
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "Interconnect")
		assert.Contains(t, suggestion, "VLAN attachment")
	})
}

func TestVPCPeeringChecker(t *testing.T) {
	t.Parallel()
	checker := &vpcPeeringChecker{}
	ctx := newFixContext("TCP", "10.0.0.1", "10.1.0.1", 443, "project-1")

	t.Run("detects VPC peering subnet issue", func(t *testing.T) {
		step := &gcp.TraceStep{
			Route:      "peering-route",
			CausesDrop: true,
			RouteType:  "PEERING_SUBNET",
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "VPC peering")
		assert.Contains(t, suggestion, "import/export")
	})

	t.Run("detects VPC peering static route issue", func(t *testing.T) {
		step := &gcp.TraceStep{
			Route:      "peering-route",
			CausesDrop: true,
			RouteType:  "PEERING_STATIC",
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "VPC peering")
	})

	t.Run("detects VPC peering dynamic route issue", func(t *testing.T) {
		step := &gcp.TraceStep{
			Route:      "peering-route",
			CausesDrop: true,
			RouteType:  "PEERING_DYNAMIC",
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "VPC peering")
	})

	t.Run("returns empty for other route types", func(t *testing.T) {
		step := &gcp.TraceStep{
			Route:      "static-route",
			CausesDrop: true,
			RouteType:  "STATIC",
		}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})
}

func TestGenericRouteChecker(t *testing.T) {
	t.Parallel()
	checker := &genericRouteChecker{}

	t.Run("suggests forward route fix", func(t *testing.T) {
		ctx := newFixContext("TCP", "10.0.0.1", "10.0.0.2", 443, "project-1")
		step := &gcp.TraceStep{
			Route:      "default",
			CausesDrop: true,
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "routing configuration")
		assert.NotContains(t, suggestion, "return-path")
	})

	t.Run("suggests return route fix", func(t *testing.T) {
		ctx := newFixContext("TCP", "10.0.0.1", "10.0.0.2", 443, "project-1")
		ctx.reverse = true
		step := &gcp.TraceStep{
			Route:      "default",
			CausesDrop: true,
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "return-path")
		assert.Contains(t, suggestion, "10.0.0.1")
	})

	t.Run("returns empty when not dropping", func(t *testing.T) {
		ctx := newFixContext("TCP", "10.0.0.1", "10.0.0.2", 443, "project-1")
		step := &gcp.TraceStep{
			Route:      "default",
			CausesDrop: false,
		}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})
}

func TestLoadBalancerChecker(t *testing.T) {
	t.Parallel()
	checker := &loadBalancerChecker{}
	ctx := newFixContext("TCP", "10.0.0.1", "10.0.0.2", 443, "project-1")

	t.Run("detects load balancer issue", func(t *testing.T) {
		step := &gcp.TraceStep{
			LoadBalancer: "my-lb",
			CausesDrop:   true,
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "Load balancer")
		assert.Contains(t, suggestion, "health")
	})

	t.Run("returns empty when LB is healthy", func(t *testing.T) {
		step := &gcp.TraceStep{
			LoadBalancer: "my-lb",
			CausesDrop:   false,
		}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})
}

func TestSharedVPCChecker(t *testing.T) {
	t.Parallel()
	checker := &sharedVPCChecker{}
	ctx := newFixContext("TCP", "10.0.0.1", "10.0.0.2", 443, "project-1")

	t.Run("detects cross-project issue", func(t *testing.T) {
		step := &gcp.TraceStep{
			ProjectID:  "project-2",
			CausesDrop: true,
			Firewall:   "some-fw",
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "Shared VPC")
		assert.Contains(t, suggestion, "different project")
	})

	t.Run("returns empty for same project", func(t *testing.T) {
		step := &gcp.TraceStep{
			ProjectID:  "project-1",
			CausesDrop: true,
			Firewall:   "some-fw",
		}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})
}

func TestDropReasonChecker(t *testing.T) {
	t.Parallel()
	checker := &dropReasonChecker{}
	ctx := newFixContext("TCP", "10.0.0.1", "10.0.0.2", 443, "project-1")

	t.Run("shows drop reason", func(t *testing.T) {
		step := &gcp.TraceStep{
			CausesDrop: true,
			DropReason: "Packet exceeded MTU",
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "Traffic dropped")
		assert.Contains(t, suggestion, "Packet exceeded MTU")
	})

	t.Run("returns empty when no drop reason", func(t *testing.T) {
		step := &gcp.TraceStep{
			CausesDrop: true,
		}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})
}

func TestRFC1918InternetGatewayChecker(t *testing.T) {
	t.Parallel()
	checker := &rfc1918InternetGatewayChecker{}

	t.Run("detects RFC1918 IP with internet gateway", func(t *testing.T) {
		ctx := newFixContext("TCP", "10.0.0.1", "192.168.1.1", 443, "project-1")
		step := &gcp.TraceStep{
			Route:            "default-route",
			RouteNextHopType: "NEXT_HOP_INTERNET_GATEWAY",
			CausesDrop:       false,
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "192.168.1.1")
		assert.Contains(t, suggestion, "RFC1918")
		assert.Contains(t, suggestion, "Internet Gateway")
	})

	t.Run("returns empty for public IP", func(t *testing.T) {
		ctx := newFixContext("TCP", "10.0.0.1", "8.8.8.8", 443, "project-1")
		step := &gcp.TraceStep{
			Route:            "default-route",
			RouteNextHopType: "NEXT_HOP_INTERNET_GATEWAY",
			CausesDrop:       false,
		}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})

	t.Run("returns empty when dropping", func(t *testing.T) {
		ctx := newFixContext("TCP", "10.0.0.1", "192.168.1.1", 443, "project-1")
		step := &gcp.TraceStep{
			Route:            "default-route",
			RouteNextHopType: "NEXT_HOP_INTERNET_GATEWAY",
			CausesDrop:       true,
		}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})
}

func TestMissingNATChecker(t *testing.T) {
	t.Parallel()
	checker := &missingNATChecker{}

	t.Run("detects missing NAT for public destination", func(t *testing.T) {
		ctx := newFixContext("TCP", "10.0.0.1", "8.8.8.8", 443, "project-1")
		step := &gcp.TraceStep{
			Instance:      "my-instance",
			HasExternalIP: false,
		}
		suggestion := checker.Check(ctx, step)
		assert.Contains(t, suggestion, "no external IP")
		assert.Contains(t, suggestion, "Cloud NAT")
	})

	t.Run("returns empty when instance has external IP", func(t *testing.T) {
		ctx := newFixContext("TCP", "10.0.0.1", "8.8.8.8", 443, "project-1")
		step := &gcp.TraceStep{
			Instance:      "my-instance",
			HasExternalIP: true,
		}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})

	t.Run("returns empty for private destination", func(t *testing.T) {
		ctx := newFixContext("TCP", "10.0.0.1", "192.168.1.1", 443, "project-1")
		step := &gcp.TraceStep{
			Instance:      "my-instance",
			HasExternalIP: false,
		}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})

	t.Run("returns empty when NAT exists in trace", func(t *testing.T) {
		ctx := newFixContext("TCP", "10.0.0.1", "8.8.8.8", 443, "project-1")
		ctx.details.Traces = []*gcp.Trace{
			{
				Steps: []*gcp.TraceStep{
					{HasNAT: true},
				},
			},
		}
		step := &gcp.TraceStep{
			Instance:      "my-instance",
			HasExternalIP: false,
		}
		suggestion := checker.Check(ctx, step)
		assert.Empty(t, suggestion)
	})
}

func TestGetAllCheckers(t *testing.T) {
	t.Parallel()

	checkers := getAllCheckers()
	require.NotEmpty(t, checkers)

	// Verify all checkers implement the interface
	for i, checker := range checkers {
		assert.NotNil(t, checker, "checker at index %d should not be nil", i)
	}

	// Verify specific checkers are present
	var foundViewerPermission, foundFirewall, foundRoute bool
	for _, checker := range checkers {
		switch checker.(type) {
		case *viewerPermissionChecker:
			foundViewerPermission = true
		case *ingressFirewallChecker:
			foundFirewall = true
		case *genericRouteChecker:
			foundRoute = true
		}
	}

	assert.True(t, foundViewerPermission, "should include viewerPermissionChecker")
	assert.True(t, foundFirewall, "should include firewall checker")
	assert.True(t, foundRoute, "should include route checker")
}

func TestIsPrivateIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ip       string
		expected bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.0.1", true},
		{"192.168.255.255", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"172.15.0.1", false},
		{"172.32.0.1", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			result := isPrivateIP(tt.ip)
			assert.Equal(t, tt.expected, result, "isPrivateIP(%q) should return %v", tt.ip, tt.expected)
		})
	}
}

func TestSplitLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "single line",
			input:    "hello",
			expected: []string{"hello"},
		},
		{
			name:     "multiple lines",
			input:    "line1\nline2\nline3",
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "trailing newline",
			input:    "line1\nline2\n",
			expected: []string{"line1", "line2"},
		},
		{
			name:     "empty lines",
			input:    "line1\n\nline3",
			expected: []string{"line1", "", "line3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitLines(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
