package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestHealthCheckProviderReturnsMatches(t *testing.T) {
	client := &fakeHealthCheckClient{checks: []*gcp.HealthCheck{
		{Name: "prod-http-check", Type: "HTTP", Port: 80},
		{Name: "dev-tcp-check", Type: "TCP", Port: 443},
	}}

	provider := &HealthCheckProvider{NewClient: func(ctx context.Context, project string) (HealthCheckClient, error) {
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

	if results[0].Name != "prod-http-check" {
		t.Fatalf("expected prod-http-check, got %s", results[0].Name)
	}

	if results[0].Type != KindHealthCheck {
		t.Fatalf("expected type %s, got %s", KindHealthCheck, results[0].Type)
	}

	if results[0].Location != "global" {
		t.Fatalf("expected location global, got %s", results[0].Location)
	}

	if results[0].Details["type"] != "HTTP" {
		t.Fatalf("expected type HTTP, got %s", results[0].Details["type"])
	}

	if results[0].Details["port"] != "80" {
		t.Fatalf("expected port 80, got %s", results[0].Details["port"])
	}
}

func TestHealthCheckProviderPropagatesErrors(t *testing.T) {
	provider := &HealthCheckProvider{NewClient: func(context.Context, string) (HealthCheckClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &HealthCheckProvider{NewClient: func(context.Context, string) (HealthCheckClient, error) {
		return &fakeHealthCheckClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestHealthCheckProviderNilProvider(t *testing.T) {
	var provider *HealthCheckProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

type fakeHealthCheckClient struct {
	checks []*gcp.HealthCheck
	err    error
}

func (f *fakeHealthCheckClient) ListHealthChecks(context.Context) ([]*gcp.HealthCheck, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.checks, nil
}
