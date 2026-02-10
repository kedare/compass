package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestGKENodePoolProviderReturnsMatches(t *testing.T) {
	client := &fakeGKENodePoolClient{pools: []*gcp.GKENodePool{
		{Name: "prod-pool", Location: "us-central1", ClusterName: "prod-cluster", MachineType: "e2-standard-4", NodeCount: 5, Status: "RUNNING"},
		{Name: "dev-pool", Location: "europe-west1-b", ClusterName: "dev-cluster", MachineType: "e2-medium", NodeCount: 2, Status: "RUNNING"},
	}}

	provider := &GKENodePoolProvider{NewClient: func(ctx context.Context, project string) (GKENodePoolClient, error) {
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

	if results[0].Name != "prod-pool" {
		t.Fatalf("expected prod-pool, got %s", results[0].Name)
	}

	if results[0].Type != KindGKENodePool {
		t.Fatalf("expected type %s, got %s", KindGKENodePool, results[0].Type)
	}

	if results[0].Details["cluster"] != "prod-cluster" {
		t.Fatalf("expected cluster prod-cluster, got %s", results[0].Details["cluster"])
	}

	if results[0].Details["machineType"] != "e2-standard-4" {
		t.Fatalf("expected machineType e2-standard-4, got %s", results[0].Details["machineType"])
	}

	if results[0].Details["nodes"] != "5" {
		t.Fatalf("expected nodes 5, got %s", results[0].Details["nodes"])
	}
}

func TestGKENodePoolProviderMatchesByDetailFields(t *testing.T) {
	client := &fakeGKENodePoolClient{pools: []*gcp.GKENodePool{
		{Name: "pool-a", ClusterName: "web-cluster", MachineType: "e2-standard-4", NodeCount: 5},
		{Name: "pool-b", ClusterName: "api-cluster", MachineType: "n2-highmem-8", NodeCount: 3},
	}}

	provider := &GKENodePoolProvider{NewClient: func(ctx context.Context, project string) (GKENodePoolClient, error) {
		return client, nil
	}}

	// Search by cluster name
	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "web-cluster"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "pool-a" {
		t.Fatalf("expected 1 result for cluster name search, got %d", len(results))
	}

	// Search by machine type
	results, err = provider.Search(context.Background(), "proj-a", Query{Term: "n2-highmem"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "pool-b" {
		t.Fatalf("expected 1 result for machine type search, got %d", len(results))
	}
}

func TestGKENodePoolProviderPropagatesErrors(t *testing.T) {
	provider := &GKENodePoolProvider{NewClient: func(context.Context, string) (GKENodePoolClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &GKENodePoolProvider{NewClient: func(context.Context, string) (GKENodePoolClient, error) {
		return &fakeGKENodePoolClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestGKENodePoolProviderNilProvider(t *testing.T) {
	var provider *GKENodePoolProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

type fakeGKENodePoolClient struct {
	pools []*gcp.GKENodePool
	err   error
}

func (f *fakeGKENodePoolClient) ListGKENodePools(context.Context) ([]*gcp.GKENodePool, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.pools, nil
}
