package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestTargetPoolProviderReturnsMatches(t *testing.T) {
	client := &fakeTargetPoolClient{pools: []*gcp.TargetPool{
		{Name: "prod-pool", Region: "us-central1", SessionAffinity: "NONE", InstanceCount: 5},
		{Name: "dev-pool", Region: "europe-west1", SessionAffinity: "CLIENT_IP", InstanceCount: 2},
	}}

	provider := &TargetPoolProvider{NewClient: func(ctx context.Context, project string) (TargetPoolClient, error) {
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

	if results[0].Type != KindTargetPool {
		t.Fatalf("expected type %s, got %s", KindTargetPool, results[0].Type)
	}

	if results[0].Location != "us-central1" {
		t.Fatalf("expected location us-central1, got %s", results[0].Location)
	}

	if results[0].Details["sessionAffinity"] != "NONE" {
		t.Fatalf("expected sessionAffinity NONE, got %s", results[0].Details["sessionAffinity"])
	}

	if results[0].Details["instances"] != "5" {
		t.Fatalf("expected instances 5, got %s", results[0].Details["instances"])
	}
}

func TestTargetPoolProviderPropagatesErrors(t *testing.T) {
	provider := &TargetPoolProvider{NewClient: func(context.Context, string) (TargetPoolClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &TargetPoolProvider{NewClient: func(context.Context, string) (TargetPoolClient, error) {
		return &fakeTargetPoolClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestTargetPoolProviderNilProvider(t *testing.T) {
	var provider *TargetPoolProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

type fakeTargetPoolClient struct {
	pools []*gcp.TargetPool
	err   error
}

func (f *fakeTargetPoolClient) ListTargetPools(context.Context) ([]*gcp.TargetPool, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.pools, nil
}
