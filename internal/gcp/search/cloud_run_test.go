package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestCloudRunProviderReturnsMatches(t *testing.T) {
	client := &fakeCloudRunClient{services: []*gcp.CloudRunService{
		{Name: "prod-api", Region: "us-central1", URL: "https://prod-api-xyz.run.app", LatestRevision: "prod-api-00001"},
		{Name: "dev-service", Region: "europe-west1", URL: "https://dev-service-abc.run.app", LatestRevision: "dev-service-00003"},
	}}

	provider := &CloudRunProvider{NewClient: func(ctx context.Context, project string) (CloudRunClient, error) {
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

	if results[0].Name != "prod-api" {
		t.Fatalf("expected prod-api, got %s", results[0].Name)
	}

	if results[0].Type != KindCloudRunService {
		t.Fatalf("expected type %s, got %s", KindCloudRunService, results[0].Type)
	}

	if results[0].Details["url"] != "https://prod-api-xyz.run.app" {
		t.Fatalf("expected url https://prod-api-xyz.run.app, got %s", results[0].Details["url"])
	}

	if results[0].Details["revision"] != "prod-api-00001" {
		t.Fatalf("expected revision prod-api-00001, got %s", results[0].Details["revision"])
	}
}

func TestCloudRunProviderPropagatesErrors(t *testing.T) {
	provider := &CloudRunProvider{NewClient: func(context.Context, string) (CloudRunClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &CloudRunProvider{NewClient: func(context.Context, string) (CloudRunClient, error) {
		return &fakeCloudRunClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestCloudRunProviderNilProvider(t *testing.T) {
	var provider *CloudRunProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

type fakeCloudRunClient struct {
	services []*gcp.CloudRunService
	err      error
}

func (f *fakeCloudRunClient) ListCloudRunServices(context.Context) ([]*gcp.CloudRunService, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.services, nil
}
