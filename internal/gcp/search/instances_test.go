package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestInstanceProviderReturnsMatches(t *testing.T) {
	client := &fakeInstanceClient{instances: []*gcp.Instance{
		{Name: "alpha", Project: "proj-a", Zone: "us", Status: "RUNNING"},
		{Name: "piou-alpha", Project: "proj-a", Zone: "us", Status: "RUNNING", InternalIP: "10.0.0.1"},
	}}

	provider := &InstanceProvider{NewClient: func(ctx context.Context, project string) (InstanceClient, error) {
		if project != "proj-a" {
			t.Fatalf("unexpected project %s", project)
		}
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "PiOu"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Details["internalIP"] != "10.0.0.1" {
		t.Fatalf("expected details to include internal IP, got %#v", results[0].Details)
	}
}

func TestInstanceProviderMatchesByIP(t *testing.T) {
	client := &fakeInstanceClient{instances: []*gcp.Instance{
		{Name: "web-server-01", Project: "proj-a", Zone: "us-central1-a", Status: "RUNNING", InternalIP: "10.0.0.1", ExternalIP: "35.1.2.3"},
		{Name: "db-server-01", Project: "proj-a", Zone: "us-central1-b", Status: "RUNNING", InternalIP: "10.0.0.2", MachineType: "n2-standard-4"},
	}}

	provider := &InstanceProvider{NewClient: func(ctx context.Context, project string) (InstanceClient, error) {
		return client, nil
	}}

	// Search by internal IP
	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "10.0.0.1"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "web-server-01" {
		t.Fatalf("expected 1 result for IP search, got %d", len(results))
	}

	// Search by external IP
	results, err = provider.Search(context.Background(), "proj-a", Query{Term: "35.1.2"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "web-server-01" {
		t.Fatalf("expected 1 result for external IP search, got %d", len(results))
	}

	// Search by machine type
	results, err = provider.Search(context.Background(), "proj-a", Query{Term: "n2-standard"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "db-server-01" {
		t.Fatalf("expected 1 result for machine type search, got %d", len(results))
	}
}

func TestInstanceProviderPropagatesErrors(t *testing.T) {
	provider := &InstanceProvider{NewClient: func(context.Context, string) (InstanceClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &InstanceProvider{NewClient: func(context.Context, string) (InstanceClient, error) {
		return &fakeInstanceClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

type fakeInstanceClient struct {
	instances []*gcp.Instance
	err       error
}

func (f *fakeInstanceClient) ListInstances(context.Context, string) ([]*gcp.Instance, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.instances, nil
}
