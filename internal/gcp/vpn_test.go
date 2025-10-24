package gcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/api/compute/v1"
)

func TestSameResource(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected bool
	}{
		{
			name:     "matching links",
			a:        "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway",
			b:        "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway",
			expected: true,
		},
		{
			name:     "matching links with trailing slash",
			a:        "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway/",
			b:        "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway",
			expected: true,
		},
		{
			name:     "different links",
			a:        "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/gateway-1",
			b:        "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/gateway-2",
			expected: false,
		},
		{
			name:     "empty first string",
			a:        "",
			b:        "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway",
			expected: false,
		},
		{
			name:     "empty second string",
			a:        "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway",
			b:        "",
			expected: false,
		},
		{
			name:     "both empty",
			a:        "",
			b:        "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sameResource(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeLink(t *testing.T) {
	tests := []struct {
		name     string
		link     string
		expected string
	}{
		{
			name:     "link with trailing slash",
			link:     "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway/",
			expected: "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway",
		},
		{
			name:     "link without trailing slash",
			link:     "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway",
			expected: "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway",
		},
		{
			name:     "link with whitespace",
			link:     "  https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway  ",
			expected: "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway",
		},
		{
			name:     "empty link",
			link:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeLink(tt.link)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertVpnGateway(t *testing.T) {
	t.Run("valid gateway", func(t *testing.T) {
		gw := &compute.VpnGateway{
			Name:        "my-gateway",
			Network:     "https://www.googleapis.com/compute/v1/projects/my-project/global/networks/default",
			Description: "Test gateway",
			SelfLink:    "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway",
			Labels: map[string]string{
				"env": "test",
			},
			VpnInterfaces: []*compute.VpnGatewayVpnGatewayInterface{
				{Id: 0, IpAddress: "1.2.3.4"},
				{Id: 1, IpAddress: "5.6.7.8"},
			},
		}

		result := convertVpnGateway("us-central1", gw)
		assert.NotNil(t, result)
		assert.Equal(t, "my-gateway", result.Name)
		assert.Equal(t, "us-central1", result.Region)
		assert.Equal(t, gw.Network, result.Network)
		assert.Equal(t, "Test gateway", result.Description)
		assert.Equal(t, gw.SelfLink, result.SelfLink)
		assert.Equal(t, "test", result.Labels["env"])
		assert.Len(t, result.Interfaces, 2)
		assert.Empty(t, result.Tunnels)
	})

	t.Run("nil gateway", func(t *testing.T) {
		result := convertVpnGateway("us-central1", nil)
		assert.Nil(t, result)
	})
}

func TestConvertVpnTunnel(t *testing.T) {
	t.Run("valid tunnel", func(t *testing.T) {
		tunnel := &compute.VpnTunnel{
			Name:                "my-tunnel",
			Description:         "Test tunnel",
			SelfLink:            "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnTunnels/my-tunnel",
			PeerIp:              "203.0.113.1",
			Status:              "ESTABLISHED",
			DetailedStatus:      "Tunnel is up and running",
			IkeVersion:          2,
			Router:              "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/routers/my-router",
			VpnGateway:          "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway",
			VpnGatewayInterface: 0,
		}

		result := convertVpnTunnel("us-central1", tunnel)
		assert.NotNil(t, result)
		assert.Equal(t, "my-tunnel", result.Name)
		assert.Equal(t, "us-central1", result.Region)
		assert.Equal(t, "Test tunnel", result.Description)
		assert.Equal(t, "203.0.113.1", result.PeerIP)
		assert.Equal(t, "ESTABLISHED", result.Status)
		assert.Equal(t, "Tunnel is up and running", result.DetailedStatus)
		assert.Equal(t, int64(2), result.IkeVersion)
		assert.Equal(t, "my-router", result.RouterName)
		assert.Equal(t, "us-central1", result.RouterRegion)
		assert.Equal(t, tunnel.VpnGateway, result.GatewayLink)
		assert.Equal(t, int64(0), result.GatewayInterface)
		assert.Empty(t, result.BgpSessions)
	})

	t.Run("nil tunnel", func(t *testing.T) {
		result := convertVpnTunnel("us-central1", nil)
		assert.Nil(t, result)
	})
}

func TestScopeSuffix(t *testing.T) {
	tests := []struct {
		name     string
		scope    string
		expected string
	}{
		{
			name:     "full scope path",
			scope:    "regions/us-central1",
			expected: "us-central1",
		},
		{
			name:     "simple value",
			scope:    "us-central1",
			expected: "us-central1",
		},
		{
			name:     "long scope path",
			scope:    "projects/my-project/regions/us-west1",
			expected: "us-west1",
		},
		{
			name:     "empty scope",
			scope:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scopeSuffix(tt.scope)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResourceName(t *testing.T) {
	tests := []struct {
		name     string
		link     string
		expected string
	}{
		{
			name:     "full resource link",
			link:     "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway",
			expected: "my-gateway",
		},
		{
			name:     "simple name",
			link:     "my-gateway",
			expected: "my-gateway",
		},
		{
			name:     "empty link",
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

func TestRegionFromResource(t *testing.T) {
	tests := []struct {
		name     string
		link     string
		expected string
	}{
		{
			name:     "link with region",
			link:     "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway",
			expected: "",
		},
		{
			name:     "link without region",
			link:     "https://www.googleapis.com/compute/v1/projects/my-project/global/networks/default",
			expected: "",
		},
		{
			name:     "empty link",
			link:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := regionFromResource(tt.link)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name     string
		values   []string
		expected string
	}{
		{
			name:     "first is non-empty",
			values:   []string{"first", "second", "third"},
			expected: "first",
		},
		{
			name:     "second is first non-empty",
			values:   []string{"", "second", "third"},
			expected: "second",
		},
		{
			name:     "all empty",
			values:   []string{"", "", ""},
			expected: "",
		},
		{
			name:     "whitespace ignored",
			values:   []string{"  ", "value", ""},
			expected: "value",
		},
		{
			name:     "no values",
			values:   []string{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := firstNonEmpty(tt.values...)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCopyLabels(t *testing.T) {
	t.Run("nil source", func(t *testing.T) {
		result := copyLabels(nil)
		assert.Nil(t, result)
	})

	t.Run("empty source", func(t *testing.T) {
		result := copyLabels(map[string]string{})
		assert.Nil(t, result)
	})

	t.Run("copy labels", func(t *testing.T) {
		src := map[string]string{
			"env":  "test",
			"team": "infrastructure",
		}
		result := copyLabels(src)
		assert.Equal(t, src, result)
		// Ensure it's a copy, not the same map
		result["env"] = "prod"
		assert.Equal(t, "test", src["env"])
	})
}

func TestNameFilter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name",
			input:    "my-gateway",
			expected: `name eq "my-gateway"`,
		},
		{
			name:     "name with quotes",
			input:    `gateway-"test"`,
			expected: `name eq "gateway-\"test\""`,
		},
		{
			name:     "empty name",
			input:    "",
			expected: `name eq ""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nameFilter(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGatewayReference(t *testing.T) {
	tests := []struct {
		name           string
		link           string
		fallbackRegion string
		expectedRegion string
		expectedName   string
	}{
		{
			name:           "full gateway link",
			link:           "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/vpnGateways/my-gateway",
			fallbackRegion: "us-west1",
			expectedRegion: "us-central1",
			expectedName:   "my-gateway",
		},
		{
			name:           "empty link with fallback",
			link:           "",
			fallbackRegion: "us-west1",
			expectedRegion: "us-west1",
			expectedName:   "",
		},
		{
			name:           "link without region",
			link:           "vpnGateways/my-gateway",
			fallbackRegion: "us-west1",
			expectedRegion: "us-west1",
			expectedName:   "my-gateway",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			region, name := gatewayReference(tt.link, tt.fallbackRegion)
			assert.Equal(t, tt.expectedRegion, region)
			assert.Equal(t, tt.expectedName, name)
		})
	}
}

func TestRouterReference(t *testing.T) {
	tests := []struct {
		name           string
		link           string
		fallbackRegion string
		expectedRegion string
		expectedName   string
	}{
		{
			name:           "full router link",
			link:           "https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/routers/my-router",
			fallbackRegion: "us-west1",
			expectedRegion: "us-central1",
			expectedName:   "my-router",
		},
		{
			name:           "empty link with fallback",
			link:           "",
			fallbackRegion: "us-west1",
			expectedRegion: "us-west1",
			expectedName:   "",
		},
		{
			name:           "link without region",
			link:           "routers/my-router",
			fallbackRegion: "us-west1",
			expectedRegion: "us-west1",
			expectedName:   "my-router",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			region, name := routerReference(tt.link, tt.fallbackRegion)
			assert.Equal(t, tt.expectedRegion, region)
			assert.Equal(t, tt.expectedName, name)
		})
	}
}

func TestExtractDestRanges(t *testing.T) {
	t.Run("nil routes", func(t *testing.T) {
		result := extractDestRanges(nil)
		assert.Nil(t, result)
	})

	t.Run("empty routes", func(t *testing.T) {
		result := extractDestRanges([]*compute.Route{})
		assert.Nil(t, result)
	})

	t.Run("routes with dest ranges", func(t *testing.T) {
		routes := []*compute.Route{
			{DestRange: "10.0.0.0/8"},
			{DestRange: "192.168.0.0/16"},
			{DestRange: ""},
			nil,
		}
		result := extractDestRanges(routes)
		assert.Equal(t, []string{"10.0.0.0/8", "192.168.0.0/16"}, result)
	})

	t.Run("all empty or nil routes", func(t *testing.T) {
		routes := []*compute.Route{
			{DestRange: ""},
			nil,
			{DestRange: "  "},
		}
		result := extractDestRanges(routes)
		assert.Nil(t, result)
	})
}
