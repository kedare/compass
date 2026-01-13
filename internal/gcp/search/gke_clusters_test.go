package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestGKEClusterProviderReturnsMatches(t *testing.T) {
	client := &fakeGKEClusterClient{clusters: []*gcp.GKECluster{
		{Name: "prod-cluster", Location: "us-central1", Status: "RUNNING", CurrentMasterVersion: "1.27.3-gke.1700", NodeCount: 10},
		{Name: "dev-cluster", Location: "europe-west1-b", Status: "RUNNING", CurrentMasterVersion: "1.26.5-gke.1400", NodeCount: 3},
	}}

	provider := &GKEClusterProvider{NewClient: func(ctx context.Context, project string) (GKEClusterClient, error) {
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

	if results[0].Name != "prod-cluster" {
		t.Fatalf("expected prod-cluster, got %s", results[0].Name)
	}

	if results[0].Type != KindGKECluster {
		t.Fatalf("expected type %s, got %s", KindGKECluster, results[0].Type)
	}

	if results[0].Details["status"] != "RUNNING" {
		t.Fatalf("expected status RUNNING, got %s", results[0].Details["status"])
	}

	if results[0].Details["nodes"] != "10" {
		t.Fatalf("expected nodes 10, got %s", results[0].Details["nodes"])
	}
}

func TestGKEClusterProviderPropagatesErrors(t *testing.T) {
	provider := &GKEClusterProvider{NewClient: func(context.Context, string) (GKEClusterClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &GKEClusterProvider{NewClient: func(context.Context, string) (GKEClusterClient, error) {
		return &fakeGKEClusterClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestGKEClusterProviderNilProvider(t *testing.T) {
	var provider *GKEClusterProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

type fakeGKEClusterClient struct {
	clusters []*gcp.GKECluster
	err      error
}

func (f *fakeGKEClusterClient) ListGKEClusters(context.Context) ([]*gcp.GKECluster, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.clusters, nil
}
