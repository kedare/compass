package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestBackendServiceProviderReturnsMatches(t *testing.T) {
	client := &fakeBackendServiceClient{services: []*gcp.BackendService{
		{Name: "prod-backend", Region: "us-central1", Protocol: "HTTP", LoadBalancingScheme: "EXTERNAL", HealthCheckCount: 1, BackendCount: 3},
		{Name: "dev-backend", Region: "europe-west1", Protocol: "HTTPS", LoadBalancingScheme: "INTERNAL", HealthCheckCount: 2, BackendCount: 1},
	}}

	provider := &BackendServiceProvider{NewClient: func(ctx context.Context, project string) (BackendServiceClient, error) {
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

	if results[0].Name != "prod-backend" {
		t.Fatalf("expected prod-backend, got %s", results[0].Name)
	}

	if results[0].Type != KindBackendService {
		t.Fatalf("expected type %s, got %s", KindBackendService, results[0].Type)
	}

	if results[0].Details["protocol"] != "HTTP" {
		t.Fatalf("expected protocol HTTP, got %s", results[0].Details["protocol"])
	}

	if results[0].Details["backends"] != "3" {
		t.Fatalf("expected backends 3, got %s", results[0].Details["backends"])
	}
}

func TestBackendServiceProviderPropagatesErrors(t *testing.T) {
	provider := &BackendServiceProvider{NewClient: func(context.Context, string) (BackendServiceClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &BackendServiceProvider{NewClient: func(context.Context, string) (BackendServiceClient, error) {
		return &fakeBackendServiceClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestBackendServiceProviderNilProvider(t *testing.T) {
	var provider *BackendServiceProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

type fakeBackendServiceClient struct {
	services []*gcp.BackendService
	err      error
}

func (f *fakeBackendServiceClient) ListBackendServices(context.Context) ([]*gcp.BackendService, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.services, nil
}
