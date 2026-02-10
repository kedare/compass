package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestSubnetProviderReturnsMatches(t *testing.T) {
	client := &fakeSubnetClient{subnets: []*gcp.Subnet{
		{Name: "prod-subnet", Region: "us-central1", IPCidrRange: "10.0.0.0/24", Network: "prod-vpc", Purpose: "PRIVATE"},
		{Name: "dev-subnet", Region: "europe-west1", IPCidrRange: "10.1.0.0/24", Network: "dev-vpc", Purpose: "PRIVATE"},
	}}

	provider := &SubnetProvider{NewClient: func(ctx context.Context, project string) (SubnetClient, error) {
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

	if results[0].Name != "prod-subnet" {
		t.Fatalf("expected prod-subnet, got %s", results[0].Name)
	}

	if results[0].Type != KindSubnet {
		t.Fatalf("expected type %s, got %s", KindSubnet, results[0].Type)
	}

	if results[0].Location != "us-central1" {
		t.Fatalf("expected location us-central1, got %s", results[0].Location)
	}

	if results[0].Details["cidr"] != "10.0.0.0/24" {
		t.Fatalf("expected cidr 10.0.0.0/24, got %s", results[0].Details["cidr"])
	}

	if results[0].Details["network"] != "prod-vpc" {
		t.Fatalf("expected network prod-vpc, got %s", results[0].Details["network"])
	}
}

func TestSubnetProviderMatchesByDetailFields(t *testing.T) {
	client := &fakeSubnetClient{subnets: []*gcp.Subnet{
		{Name: "subnet-a", Region: "us-central1", IPCidrRange: "10.0.0.0/24", Network: "vpc-alpha"},
		{Name: "subnet-b", Region: "us-central1", IPCidrRange: "10.1.0.0/24", Network: "vpc-beta"},
	}}

	provider := &SubnetProvider{NewClient: func(ctx context.Context, project string) (SubnetClient, error) {
		return client, nil
	}}

	// Search by CIDR
	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "10.1.0"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "subnet-b" {
		t.Fatalf("expected 1 result for CIDR search, got %d", len(results))
	}

	// Search by network
	results, err = provider.Search(context.Background(), "proj-a", Query{Term: "vpc-alpha"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "subnet-a" {
		t.Fatalf("expected 1 result for network search, got %d", len(results))
	}
}

func TestSubnetProviderPropagatesErrors(t *testing.T) {
	provider := &SubnetProvider{NewClient: func(context.Context, string) (SubnetClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &SubnetProvider{NewClient: func(context.Context, string) (SubnetClient, error) {
		return &fakeSubnetClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestSubnetProviderNilProvider(t *testing.T) {
	var provider *SubnetProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

type fakeSubnetClient struct {
	subnets []*gcp.Subnet
	err     error
}

func (f *fakeSubnetClient) ListSubnets(context.Context) ([]*gcp.Subnet, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.subnets, nil
}
