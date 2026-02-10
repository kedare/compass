package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestVPNTunnelProviderReturnsMatches(t *testing.T) {
	client := &fakeVPNTunnelClient{tunnels: []*gcp.VPNTunnelInfo{
		{
			Name:           "tunnel-to-office",
			Region:         "us-central1",
			Status:         "ESTABLISHED",
			PeerIP:         "203.0.113.1",
			LocalGatewayIP: "35.1.2.3",
			IkeVersion:     2,
			GatewayLink:    "projects/proj/regions/us-central1/vpnGateways/my-gateway",
		},
		{
			Name:       "tunnel-to-datacenter",
			Region:     "europe-west1",
			Status:     "NEGOTIATION_FAILURE",
			PeerIP:     "198.51.100.1",
			IkeVersion: 1,
		},
	}}

	provider := &VPNTunnelProvider{NewClient: func(ctx context.Context, project string) (VPNTunnelClient, error) {
		if project != "proj-a" {
			t.Fatalf("unexpected project %s", project)
		}
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "office"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Name != "tunnel-to-office" {
		t.Fatalf("expected tunnel-to-office, got %s", results[0].Name)
	}

	if results[0].Type != KindVPNTunnel {
		t.Fatalf("expected type %s, got %s", KindVPNTunnel, results[0].Type)
	}

	if results[0].Location != "us-central1" {
		t.Fatalf("expected location us-central1, got %s", results[0].Location)
	}

	if results[0].Details["status"] != "ESTABLISHED" {
		t.Fatalf("expected status ESTABLISHED, got %s", results[0].Details["status"])
	}

	if results[0].Details["peerIP"] != "203.0.113.1" {
		t.Fatalf("expected peerIP 203.0.113.1, got %s", results[0].Details["peerIP"])
	}

	if results[0].Details["ikeVersion"] != "2" {
		t.Fatalf("expected ikeVersion 2, got %s", results[0].Details["ikeVersion"])
	}

	if results[0].Details["gateway"] != "my-gateway" {
		t.Fatalf("expected gateway my-gateway, got %s", results[0].Details["gateway"])
	}
}

func TestVPNTunnelProviderMatchesByDetailFields(t *testing.T) {
	client := &fakeVPNTunnelClient{tunnels: []*gcp.VPNTunnelInfo{
		{Name: "tunnel-a", Region: "us-central1", PeerIP: "203.0.113.1", LocalGatewayIP: "35.1.2.3"},
		{Name: "tunnel-b", Region: "us-central1", PeerIP: "198.51.100.1", LocalGatewayIP: "35.4.5.6"},
	}}

	provider := &VPNTunnelProvider{NewClient: func(ctx context.Context, project string) (VPNTunnelClient, error) {
		return client, nil
	}}

	// Search by peer IP
	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "203.0.113"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "tunnel-a" {
		t.Fatalf("expected 1 result for peer IP search, got %d", len(results))
	}

	// Search by local gateway IP
	results, err = provider.Search(context.Background(), "proj-a", Query{Term: "35.4.5"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "tunnel-b" {
		t.Fatalf("expected 1 result for local IP search, got %d", len(results))
	}
}

func TestVPNTunnelProviderPropagatesErrors(t *testing.T) {
	provider := &VPNTunnelProvider{NewClient: func(context.Context, string) (VPNTunnelClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &VPNTunnelProvider{NewClient: func(context.Context, string) (VPNTunnelClient, error) {
		return &fakeVPNTunnelClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestVPNTunnelProviderNilProvider(t *testing.T) {
	var provider *VPNTunnelProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

func TestVPNTunnelDetails(t *testing.T) {
	tunnel := &gcp.VPNTunnelInfo{
		Name:           "test-tunnel",
		Status:         "ESTABLISHED",
		PeerIP:         "192.0.2.1",
		LocalGatewayIP: "35.1.2.3",
		IkeVersion:     2,
		GatewayLink:    "projects/proj/regions/us-central1/vpnGateways/prod-gateway",
	}

	details := vpnTunnelDetails(tunnel)

	if details["status"] != "ESTABLISHED" {
		t.Errorf("expected status 'ESTABLISHED', got %q", details["status"])
	}

	if details["peerIP"] != "192.0.2.1" {
		t.Errorf("expected peerIP '192.0.2.1', got %q", details["peerIP"])
	}

	if details["localIP"] != "35.1.2.3" {
		t.Errorf("expected localIP '35.1.2.3', got %q", details["localIP"])
	}

	if details["ikeVersion"] != "2" {
		t.Errorf("expected ikeVersion '2', got %q", details["ikeVersion"])
	}

	if details["gateway"] != "prod-gateway" {
		t.Errorf("expected gateway 'prod-gateway', got %q", details["gateway"])
	}
}

func TestVPNTunnelDetailsEmptyFields(t *testing.T) {
	tunnel := &gcp.VPNTunnelInfo{
		Name: "test-tunnel",
	}

	details := vpnTunnelDetails(tunnel)

	if _, ok := details["status"]; ok {
		t.Error("expected status to be absent for empty value")
	}

	if _, ok := details["peerIP"]; ok {
		t.Error("expected peerIP to be absent for empty value")
	}

	if _, ok := details["ikeVersion"]; ok {
		t.Error("expected ikeVersion to be absent for zero value")
	}
}

type fakeVPNTunnelClient struct {
	tunnels []*gcp.VPNTunnelInfo
	err     error
}

func (f *fakeVPNTunnelClient) ListVPNTunnels(context.Context) ([]*gcp.VPNTunnelInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.tunnels, nil
}
