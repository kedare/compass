package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestCloudSQLProviderReturnsMatches(t *testing.T) {
	client := &fakeCloudSQLClient{instances: []*gcp.CloudSQLInstance{
		{Name: "prod-database", Region: "us-central1", DatabaseVersion: "POSTGRES_14", Tier: "db-custom-4-16384", State: "RUNNABLE"},
		{Name: "dev-db", Region: "europe-west1", DatabaseVersion: "MYSQL_8_0", Tier: "db-f1-micro", State: "STOPPED"},
	}}

	provider := &CloudSQLProvider{NewClient: func(ctx context.Context, project string) (CloudSQLClient, error) {
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

	if results[0].Name != "prod-database" {
		t.Fatalf("expected prod-database, got %s", results[0].Name)
	}

	if results[0].Type != KindCloudSQLInstance {
		t.Fatalf("expected type %s, got %s", KindCloudSQLInstance, results[0].Type)
	}

	if results[0].Details["version"] != "POSTGRES_14" {
		t.Fatalf("expected version POSTGRES_14, got %s", results[0].Details["version"])
	}

	if results[0].Details["state"] != "RUNNABLE" {
		t.Fatalf("expected state RUNNABLE, got %s", results[0].Details["state"])
	}
}

func TestCloudSQLProviderPropagatesErrors(t *testing.T) {
	provider := &CloudSQLProvider{NewClient: func(context.Context, string) (CloudSQLClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &CloudSQLProvider{NewClient: func(context.Context, string) (CloudSQLClient, error) {
		return &fakeCloudSQLClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestCloudSQLProviderNilProvider(t *testing.T) {
	var provider *CloudSQLProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

type fakeCloudSQLClient struct {
	instances []*gcp.CloudSQLInstance
	err       error
}

func (f *fakeCloudSQLClient) ListCloudSQLInstances(context.Context) ([]*gcp.CloudSQLInstance, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.instances, nil
}
