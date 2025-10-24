package output

import (
	"testing"

	"github.com/kedare/compass/internal/gcp"
	"github.com/stretchr/testify/assert"
	"google.golang.org/api/compute/v1"
)

func TestResourceName(t *testing.T) {
	tests := []struct {
		name     string
		link     string
		expected string
	}{
		{
			name:     "full resource URL",
			link:     "projects/my-project/regions/us-central1/networks/my-network",
			expected: "my-network",
		},
		{
			name:     "simple name",
			link:     "my-network",
			expected: "my-network",
		},
		{
			name:     "empty string",
			link:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resourceName(tt.link)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClassifyStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		expected string
	}{
		{
			name:     "established status",
			status:   "ESTABLISHED",
			expected: statusClassGood,
		},
		{
			name:     "up status",
			status:   "UP",
			expected: statusClassGood,
		},
		{
			name:     "failed status",
			status:   "FAILED",
			expected: statusClassBad,
		},
		{
			name:     "down status",
			status:   "DOWN",
			expected: statusClassBad,
		},
		{
			name:     "error status",
			status:   "ERROR",
			expected: statusClassBad,
		},
		{
			name:     "provisioning status",
			status:   "PROVISIONING",
			expected: statusClassWarn,
		},
		{
			name:     "wait status",
			status:   "WAIT",
			expected: statusClassWarn,
		},
		{
			name:     "unknown status",
			status:   "SOME_UNKNOWN_STATUS",
			expected: statusClassNeutral,
		},
		{
			name:     "empty status",
			status:   "",
			expected: statusClassUnknown,
		},
		{
			name:     "lowercase established",
			status:   "established",
			expected: statusClassGood,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyStatus(tt.status)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestApplyStyle(t *testing.T) {
	tests := []struct {
		name      string
		textValue string
		style     string
	}{
		{
			name:      "good style",
			textValue: "UP",
			style:     statusClassGood,
		},
		{
			name:      "bad style",
			textValue: "DOWN",
			style:     statusClassBad,
		},
		{
			name:      "warn style",
			textValue: "WAIT",
			style:     statusClassWarn,
		},
		{
			name:      "neutral style",
			textValue: "UNKNOWN",
			style:     statusClassNeutral,
		},
		{
			name:      "empty text",
			textValue: "",
			style:     statusClassGood,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applyStyle(tt.textValue, tt.style)
			if tt.textValue == "" {
				assert.Equal(t, "", result)
			} else {
				// Just verify it returns a non-empty string (may include color codes)
				assert.NotEmpty(t, result)
			}
		})
	}
}

func TestColorTunnelName(t *testing.T) {
	tests := []struct {
		name     string
		tunnel   *gcp.VPNTunnelInfo
		expected string
	}{
		{
			name: "tunnel with good status",
			tunnel: &gcp.VPNTunnelInfo{
				Name:   "my-tunnel",
				Status: "ESTABLISHED",
			},
			expected: "my-tunnel", // Will be colored but still contains the name
		},
		{
			name:     "nil tunnel",
			tunnel:   nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := colorTunnelName(tt.tunnel)
			if tt.tunnel == nil {
				assert.Equal(t, "", result)
			} else {
				// Result should contain the tunnel name (may have color codes)
				assert.Contains(t, result, tt.tunnel.Name)
			}
		})
	}
}

func TestColorPeerName(t *testing.T) {
	tests := []struct {
		name     string
		peer     *gcp.BGPSessionInfo
		expected string
	}{
		{
			name: "enabled peer with good status",
			peer: &gcp.BGPSessionInfo{
				Name:          "peer-1",
				Enabled:       true,
				SessionStatus: "UP",
			},
			expected: "peer-1",
		},
		{
			name: "enabled peer with bad status",
			peer: &gcp.BGPSessionInfo{
				Name:          "peer-2",
				Enabled:       true,
				SessionStatus: "DOWN",
			},
			expected: "peer-2",
		},
		{
			name: "disabled peer",
			peer: &gcp.BGPSessionInfo{
				Name:          "peer-3",
				Enabled:       false,
				SessionStatus: "DOWN",
			},
			expected: "peer-3",
		},
		{
			name:     "nil peer",
			peer:     nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := colorPeerName(tt.peer)
			if tt.peer == nil {
				assert.Equal(t, "", result)
			} else {
				assert.Contains(t, result, tt.peer.Name)
			}
		})
	}
}

func TestColorPeerStatus(t *testing.T) {
	tests := []struct {
		name string
		peer *gcp.BGPSessionInfo
	}{
		{
			name: "peer with status and state",
			peer: &gcp.BGPSessionInfo{
				SessionStatus: "UP",
				SessionState:  "ESTABLISHED",
			},
		},
		{
			name: "peer with only status",
			peer: &gcp.BGPSessionInfo{
				SessionStatus: "UP",
			},
		},
		{
			name: "peer with empty status",
			peer: &gcp.BGPSessionInfo{
				SessionStatus: "",
			},
		},
		{
			name: "nil peer",
			peer: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := colorPeerStatus(tt.peer)
			assert.NotEmpty(t, result)
		})
	}
}

func TestFormatEndpointPair(t *testing.T) {
	tests := []struct {
		name     string
		peer     *gcp.BGPSessionInfo
		contains []string
	}{
		{
			name: "peer with both IPs and ASNs",
			peer: &gcp.BGPSessionInfo{
				LocalIP:  "169.254.0.1",
				PeerIP:   "169.254.0.2",
				LocalASN: 64512,
				PeerASN:  65001,
			},
			contains: []string{"169.254.0.1", "169.254.0.2", "AS64512", "AS65001"},
		},
		{
			name: "peer with missing local IP",
			peer: &gcp.BGPSessionInfo{
				LocalIP:  "",
				PeerIP:   "169.254.0.2",
				LocalASN: 64512,
				PeerASN:  65001,
			},
			contains: []string{"?", "169.254.0.2"},
		},
		{
			name:     "nil peer",
			peer:     nil,
			contains: []string{"?"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatEndpointPair(tt.peer)
			for _, expected := range tt.contains {
				assert.Contains(t, result, expected)
			}
		})
	}
}

func TestFormatIPSecPeers(t *testing.T) {
	tests := []struct {
		name     string
		tunnel   *gcp.VPNTunnelInfo
		contains []string
	}{
		{
			name: "tunnel with both IPs",
			tunnel: &gcp.VPNTunnelInfo{
				LocalGatewayIP: "35.186.82.3",
				PeerIP:         "203.0.113.1",
			},
			contains: []string{"35.186.82.3", "203.0.113.1"},
		},
		{
			name: "tunnel with BGP session fallback",
			tunnel: &gcp.VPNTunnelInfo{
				LocalGatewayIP: "",
				PeerIP:         "203.0.113.1",
				BgpSessions: []*gcp.BGPSessionInfo{
					{LocalIP: "169.254.0.1"},
				},
			},
			contains: []string{"169.254.0.1", "203.0.113.1"},
		},
		{
			name: "tunnel with missing IPs",
			tunnel: &gcp.VPNTunnelInfo{
				LocalGatewayIP: "",
				PeerIP:         "",
			},
			contains: []string{"?"},
		},
		{
			name:     "nil tunnel",
			tunnel:   nil,
			contains: []string{"?"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatIPSecPeers(tt.tunnel)
			for _, expected := range tt.contains {
				assert.Contains(t, result, expected)
			}
		})
	}
}

func TestFormatPeerDetail(t *testing.T) {
	tests := []struct {
		name     string
		peer     *gcp.BGPSessionInfo
		contains []string
	}{
		{
			name: "complete peer info",
			peer: &gcp.BGPSessionInfo{
				Name:            "peer-1",
				LocalIP:         "169.254.0.1",
				PeerIP:          "169.254.0.2",
				LocalASN:        64512,
				PeerASN:         65001,
				SessionStatus:   "UP",
				LearnedRoutes:   10,
				AdvertisedCount: 5,
			},
			contains: []string{"peer-1", "received 10", "advertised 5"},
		},
		{
			name:     "nil peer",
			peer:     nil,
			contains: []string{"?"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatPeerDetail(tt.peer)
			for _, expected := range tt.contains {
				assert.Contains(t, result, expected)
			}
		})
	}
}

func TestSortedGateways(t *testing.T) {
	gateways := []*gcp.VPNGatewayInfo{
		{Name: "gw-2", Region: "us-east1"},
		{Name: "gw-1", Region: "us-central1"},
		{Name: "gw-3", Region: "us-central1"},
	}

	sorted := sortedGateways(gateways)
	assert.Equal(t, "gw-1", sorted[0].Name)
	assert.Equal(t, "gw-3", sorted[1].Name)
	assert.Equal(t, "gw-2", sorted[2].Name)

	// Verify original slice is not modified
	assert.Equal(t, "gw-2", gateways[0].Name)
}

func TestSortedTunnels(t *testing.T) {
	tunnels := []*gcp.VPNTunnelInfo{
		{Name: "tunnel-2", Region: "us-east1"},
		{Name: "tunnel-1", Region: "us-central1"},
		{Name: "tunnel-3", Region: "us-central1"},
	}

	sorted := sortedTunnels(tunnels)
	assert.Equal(t, "tunnel-1", sorted[0].Name)
	assert.Equal(t, "tunnel-3", sorted[1].Name)
	assert.Equal(t, "tunnel-2", sorted[2].Name)

	// Verify original slice is not modified
	assert.Equal(t, "tunnel-2", tunnels[0].Name)
}

func TestSortedPeers(t *testing.T) {
	peers := []*gcp.BGPSessionInfo{
		{Name: "peer-2", RouterName: "router-2"},
		{Name: "peer-1", RouterName: "router-1"},
		{Name: "peer-3", RouterName: "router-1"},
	}

	sorted := sortedPeers(peers)
	assert.Equal(t, "peer-1", sorted[0].Name)
	assert.Equal(t, "peer-3", sorted[1].Name)
	assert.Equal(t, "peer-2", sorted[2].Name)

	// Verify original slice is not modified
	assert.Equal(t, "peer-2", peers[0].Name)
}

func TestTunnelPeerSummary(t *testing.T) {
	t.Run("nil tunnel", func(t *testing.T) {
		result := tunnelPeerSummary(nil)
		assert.Equal(t, displayNone, result)
	})

	t.Run("tunnel with no peers", func(t *testing.T) {
		tunnel := &gcp.VPNTunnelInfo{
			Name:        "tunnel-1",
			BgpSessions: []*gcp.BGPSessionInfo{},
		}
		result := tunnelPeerSummary(tunnel)
		assert.Equal(t, displayNone, result)
	})

	t.Run("tunnel with peers", func(t *testing.T) {
		tunnel := &gcp.VPNTunnelInfo{
			Name: "tunnel-1",
			BgpSessions: []*gcp.BGPSessionInfo{
				{
					Name:            "peer-1",
					LocalIP:         "169.254.0.1",
					PeerIP:          "169.254.0.2",
					SessionStatus:   "UP",
					LearnedRoutes:   5,
					AdvertisedCount: 3,
				},
			},
		}
		result := tunnelPeerSummary(tunnel)
		assert.Contains(t, result, "peer-1")
		assert.Contains(t, result, "received 5")
		assert.Contains(t, result, "advertised 3")
	})
}

func TestDisplayVPNOverview(t *testing.T) {
	overview := &gcp.VPNOverview{
		Gateways: []*gcp.VPNGatewayInfo{
			{
				Name:    "test-gw",
				Region:  "us-central1",
				Network: "projects/test/global/networks/default",
				Interfaces: []*compute.VpnGatewayVpnGatewayInterface{
					{Id: 0, IpAddress: "1.2.3.4"},
				},
				Tunnels: []*gcp.VPNTunnelInfo{},
			},
		},
	}

	t.Run("text format", func(t *testing.T) {
		err := DisplayVPNOverview(overview, "text", false)
		assert.NoError(t, err)
	})

	t.Run("table format", func(t *testing.T) {
		err := DisplayVPNOverview(overview, "table", false)
		assert.NoError(t, err)
	})

	t.Run("json format", func(t *testing.T) {
		err := DisplayVPNOverview(overview, "json", false)
		assert.NoError(t, err)
	})

	t.Run("nil overview", func(t *testing.T) {
		err := DisplayVPNOverview(nil, "text", false)
		assert.NoError(t, err)
	})
}

func TestDisplayVPNGateway(t *testing.T) {
	gw := &gcp.VPNGatewayInfo{
		Name:    "test-gw",
		Region:  "us-central1",
		Network: "projects/test/global/networks/default",
		Interfaces: []*compute.VpnGatewayVpnGatewayInterface{
			{Id: 0, IpAddress: "1.2.3.4"},
		},
		Tunnels: []*gcp.VPNTunnelInfo{},
	}

	t.Run("text format", func(t *testing.T) {
		err := DisplayVPNGateway(gw, "text")
		assert.NoError(t, err)
	})

	t.Run("table format", func(t *testing.T) {
		err := DisplayVPNGateway(gw, "table")
		assert.NoError(t, err)
	})

	t.Run("json format", func(t *testing.T) {
		err := DisplayVPNGateway(gw, "json")
		assert.NoError(t, err)
	})
}

func TestDisplayVPNTunnel(t *testing.T) {
	tunnel := &gcp.VPNTunnelInfo{
		Name:           "test-tunnel",
		Region:         "us-central1",
		Status:         "ESTABLISHED",
		RouterName:     "test-router",
		PeerIP:         "203.0.113.1",
		LocalGatewayIP: "35.186.82.3",
		BgpSessions:    []*gcp.BGPSessionInfo{},
	}

	t.Run("text format", func(t *testing.T) {
		err := DisplayVPNTunnel(tunnel, "text")
		assert.NoError(t, err)
	})

	t.Run("table format", func(t *testing.T) {
		err := DisplayVPNTunnel(tunnel, "table")
		assert.NoError(t, err)
	})

	t.Run("json format", func(t *testing.T) {
		err := DisplayVPNTunnel(tunnel, "json")
		assert.NoError(t, err)
	})
}
