package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestVPCNetworkProviderReturnsMatches(t *testing.T) {
	client := &fakeVPCNetworkClient{networks: []*gcp.VPCNetwork{
		{Name: "prod-vpc", AutoCreateSubnetworks: false, SubnetCount: 10},
		{Name: "dev-vpc", AutoCreateSubnetworks: true, SubnetCount: 5},
	}}

	provider := &VPCNetworkProvider{NewClient: func(ctx context.Context, project string) (VPCNetworkClient, error) {
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

	if results[0].Name != "prod-vpc" {
		t.Fatalf("expected prod-vpc, got %s", results[0].Name)
	}

	if results[0].Type != KindVPCNetwork {
		t.Fatalf("expected type %s, got %s", KindVPCNetwork, results[0].Type)
	}

	if results[0].Location != "global" {
		t.Fatalf("expected location global, got %s", results[0].Location)
	}

	if results[0].Details["mode"] != "custom" {
		t.Fatalf("expected mode custom, got %s", results[0].Details["mode"])
	}

	if results[0].Details["subnets"] != "10" {
		t.Fatalf("expected subnets 10, got %s", results[0].Details["subnets"])
	}
}

func TestVPCNetworkProviderAutoMode(t *testing.T) {
	client := &fakeVPCNetworkClient{networks: []*gcp.VPCNetwork{
		{Name: "auto-vpc", AutoCreateSubnetworks: true, SubnetCount: 3},
	}}

	provider := &VPCNetworkProvider{NewClient: func(ctx context.Context, project string) (VPCNetworkClient, error) {
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "auto"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Details["mode"] != "auto" {
		t.Fatalf("expected mode auto, got %s", results[0].Details["mode"])
	}
}

func TestVPCNetworkProviderPropagatesErrors(t *testing.T) {
	provider := &VPCNetworkProvider{NewClient: func(context.Context, string) (VPCNetworkClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &VPCNetworkProvider{NewClient: func(context.Context, string) (VPCNetworkClient, error) {
		return &fakeVPCNetworkClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestVPCNetworkProviderNilProvider(t *testing.T) {
	var provider *VPCNetworkProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

type fakeVPCNetworkClient struct {
	networks []*gcp.VPCNetwork
	err      error
}

func (f *fakeVPCNetworkClient) ListVPCNetworks(context.Context) ([]*gcp.VPCNetwork, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.networks, nil
}
