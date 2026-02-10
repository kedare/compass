package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
	"google.golang.org/api/compute/v1"
)

func TestVPNGatewayProviderReturnsMatches(t *testing.T) {
	client := &fakeVPNGatewayClient{gateways: []*gcp.VPNGatewayInfo{
		{Name: "vpn-gateway-prod", Region: "us-central1", Network: "projects/proj-a/global/networks/my-vpc"},
		{Name: "vpn-gateway-dev", Region: "europe-west1", Network: "projects/proj-a/global/networks/dev-vpc"},
	}}

	provider := &VPNGatewayProvider{NewClient: func(ctx context.Context, project string) (VPNGatewayClient, error) {
		if project != "proj-a" {
			t.Fatalf("unexpected project %s", project)
		}
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "prod"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Name != "vpn-gateway-prod" {
		t.Fatalf("expected vpn-gateway-prod, got %s", results[0].Name)
	}

	if results[0].Type != KindVPNGateway {
		t.Fatalf("expected type %s, got %s", KindVPNGateway, results[0].Type)
	}

	if results[0].Location != "us-central1" {
		t.Fatalf("expected location us-central1, got %s", results[0].Location)
	}

	if results[0].Details["network"] != "my-vpc" {
		t.Fatalf("expected network my-vpc, got %s", results[0].Details["network"])
	}
}

func TestVPNGatewayProviderMatchesByNetwork(t *testing.T) {
	client := &fakeVPNGatewayClient{gateways: []*gcp.VPNGatewayInfo{
		{Name: "gw-a", Region: "us-central1", Network: "prod-vpc"},
		{Name: "gw-b", Region: "us-central1", Network: "staging-vpc"},
	}}

	provider := &VPNGatewayProvider{NewClient: func(ctx context.Context, project string) (VPNGatewayClient, error) {
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "staging-vpc"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "gw-b" {
		t.Fatalf("expected 1 result for network search, got %d", len(results))
	}
}

func TestVPNGatewayProviderPropagatesErrors(t *testing.T) {
	provider := &VPNGatewayProvider{NewClient: func(context.Context, string) (VPNGatewayClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &VPNGatewayProvider{NewClient: func(context.Context, string) (VPNGatewayClient, error) {
		return &fakeVPNGatewayClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestVPNGatewayProviderNilProvider(t *testing.T) {
	var provider *VPNGatewayProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

func TestVPNGatewayDetails(t *testing.T) {
	gw := &gcp.VPNGatewayInfo{
		Name:    "test-gateway",
		Network: "projects/proj/global/networks/my-network",
		Interfaces: []*compute.VpnGatewayVpnGatewayInterface{
			{}, {},
		},
		Tunnels: []*gcp.VPNTunnelInfo{
			{Name: "tunnel-1"},
		},
	}

	details := vpnGatewayDetails(gw)

	if details["network"] != "my-network" {
		t.Errorf("expected network 'my-network', got %q", details["network"])
	}

	if details["interfaces"] != "2" {
		t.Errorf("expected interfaces '2', got %q", details["interfaces"])
	}

	if details["tunnels"] != "1" {
		t.Errorf("expected tunnels '1', got %q", details["tunnels"])
	}
}

type fakeVPNGatewayClient struct {
	gateways []*gcp.VPNGatewayInfo
	err      error
}

func (f *fakeVPNGatewayClient) ListVPNGateways(context.Context) ([]*gcp.VPNGatewayInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.gateways, nil
}
