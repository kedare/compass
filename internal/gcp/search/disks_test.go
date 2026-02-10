package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestDiskProviderReturnsMatches(t *testing.T) {
	client := &fakeDiskClient{disks: []*gcp.Disk{
		{Name: "prod-data-disk", Zone: "us-central1-a", SizeGb: 500, Type: "pd-ssd", Status: "READY"},
		{Name: "dev-disk", Zone: "europe-west1-b", SizeGb: 100, Type: "pd-standard", Status: "READY"},
	}}

	provider := &DiskProvider{NewClient: func(ctx context.Context, project string) (DiskClient, error) {
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

	if results[0].Name != "prod-data-disk" {
		t.Fatalf("expected prod-data-disk, got %s", results[0].Name)
	}

	if results[0].Type != KindDisk {
		t.Fatalf("expected type %s, got %s", KindDisk, results[0].Type)
	}

	if results[0].Location != "us-central1-a" {
		t.Fatalf("expected location us-central1-a, got %s", results[0].Location)
	}

	if results[0].Details["sizeGb"] != "500" {
		t.Fatalf("expected sizeGb 500, got %s", results[0].Details["sizeGb"])
	}

	if results[0].Details["type"] != "pd-ssd" {
		t.Fatalf("expected type pd-ssd, got %s", results[0].Details["type"])
	}
}

func TestDiskProviderMatchesByType(t *testing.T) {
	client := &fakeDiskClient{disks: []*gcp.Disk{
		{Name: "fast-disk", Zone: "us-central1-a", Type: "pd-ssd", SizeGb: 500, Status: "READY"},
		{Name: "cheap-disk", Zone: "us-central1-a", Type: "pd-standard", SizeGb: 1000, Status: "READY"},
	}}

	provider := &DiskProvider{NewClient: func(ctx context.Context, project string) (DiskClient, error) {
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "pd-ssd"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "fast-disk" {
		t.Fatalf("expected 1 result for disk type search, got %d", len(results))
	}
}

func TestDiskProviderPropagatesErrors(t *testing.T) {
	provider := &DiskProvider{NewClient: func(context.Context, string) (DiskClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &DiskProvider{NewClient: func(context.Context, string) (DiskClient, error) {
		return &fakeDiskClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestDiskProviderNilProvider(t *testing.T) {
	var provider *DiskProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

type fakeDiskClient struct {
	disks []*gcp.Disk
	err   error
}

func (f *fakeDiskClient) ListDisks(context.Context) ([]*gcp.Disk, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.disks, nil
}
