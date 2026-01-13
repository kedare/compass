package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestMIGProviderReturnsMatches(t *testing.T) {
	client := &fakeMIGClient{migs: []gcp.ManagedInstanceGroup{
		{
			Name:             "prod-mig",
			Location:         "us-central1",
			IsRegional:       true,
			TargetSize:       10,
			CurrentSize:      10,
			IsStable:         true,
			InstanceTemplate: "prod-template",
		},
		{
			Name:             "dev-mig",
			Location:         "europe-west1-a",
			IsRegional:       false,
			TargetSize:       3,
			CurrentSize:      3,
			IsStable:         true,
			InstanceTemplate: "dev-template",
		},
	}}

	provider := &MIGProvider{NewClient: func(ctx context.Context, project string) (MIGClient, error) {
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

	if results[0].Name != "prod-mig" {
		t.Fatalf("expected prod-mig, got %s", results[0].Name)
	}

	if results[0].Type != KindManagedInstanceGroup {
		t.Fatalf("expected type %s, got %s", KindManagedInstanceGroup, results[0].Type)
	}

	if results[0].Details["scope"] != "regional" {
		t.Fatalf("expected scope regional, got %s", results[0].Details["scope"])
	}

	if results[0].Details["targetSize"] != "10" {
		t.Fatalf("expected targetSize 10, got %s", results[0].Details["targetSize"])
	}
}

func TestMIGProviderPropagatesErrors(t *testing.T) {
	provider := &MIGProvider{NewClient: func(context.Context, string) (MIGClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &MIGProvider{NewClient: func(context.Context, string) (MIGClient, error) {
		return &fakeMIGClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestMIGProviderNilProvider(t *testing.T) {
	var provider *MIGProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

func TestMIGDetails(t *testing.T) {
	mig := &gcp.ManagedInstanceGroup{
		Name:             "test-mig",
		IsRegional:       false,
		IsStable:         false,
		TargetSize:       5,
		CurrentSize:      3,
		InstanceTemplate: "my-template",
		BaseInstanceName: "test-instance",
		Description:      "Test MIG",
		UpdateType:       "PROACTIVE",
		MaxSurge:         "1",
		MaxUnavailable:   "0",
		NamedPorts:       map[string]int64{"http": 80, "https": 443},
		TargetZones:      []string{"us-central1-a", "us-central1-b"},
	}

	details := migDetails(mig)

	if details["scope"] != "zonal" {
		t.Errorf("expected scope 'zonal', got %q", details["scope"])
	}

	if details["status"] != "updating" {
		t.Errorf("expected status 'updating', got %q", details["status"])
	}

	if details["targetSize"] != "5" {
		t.Errorf("expected targetSize '5', got %q", details["targetSize"])
	}

	if details["currentSize"] != "3" {
		t.Errorf("expected currentSize '3', got %q", details["currentSize"])
	}

	if details["updateType"] != "PROACTIVE" {
		t.Errorf("expected updateType 'PROACTIVE', got %q", details["updateType"])
	}
}

type fakeMIGClient struct {
	migs []gcp.ManagedInstanceGroup
	err  error
}

func (f *fakeMIGClient) ListManagedInstanceGroups(context.Context, string) ([]gcp.ManagedInstanceGroup, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.migs, nil
}
