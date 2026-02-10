package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestSnapshotProviderReturnsMatches(t *testing.T) {
	client := &fakeSnapshotClient{snapshots: []*gcp.Snapshot{
		{Name: "prod-backup-20231201", DiskSizeGb: 500, StorageBytes: 1024 * 1024 * 1024 * 50, Status: "READY", SourceDisk: "prod-data-disk"},
		{Name: "dev-snapshot", DiskSizeGb: 100, StorageBytes: 1024 * 1024 * 1024 * 10, Status: "READY", SourceDisk: "dev-disk"},
	}}

	provider := &SnapshotProvider{NewClient: func(ctx context.Context, project string) (SnapshotClient, error) {
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

	if results[0].Name != "prod-backup-20231201" {
		t.Fatalf("expected prod-backup-20231201, got %s", results[0].Name)
	}

	if results[0].Type != KindSnapshot {
		t.Fatalf("expected type %s, got %s", KindSnapshot, results[0].Type)
	}

	if results[0].Location != "global" {
		t.Fatalf("expected location global, got %s", results[0].Location)
	}

	if results[0].Details["diskSizeGb"] != "500" {
		t.Fatalf("expected diskSizeGb 500, got %s", results[0].Details["diskSizeGb"])
	}

	if results[0].Details["status"] != "READY" {
		t.Fatalf("expected status READY, got %s", results[0].Details["status"])
	}

	if results[0].Details["sourceDisk"] != "prod-data-disk" {
		t.Fatalf("expected sourceDisk prod-data-disk, got %s", results[0].Details["sourceDisk"])
	}
}

func TestSnapshotProviderMatchesBySourceDisk(t *testing.T) {
	client := &fakeSnapshotClient{snapshots: []*gcp.Snapshot{
		{Name: "snap-a", SourceDisk: "boot-disk-prod", Status: "READY"},
		{Name: "snap-b", SourceDisk: "data-disk-dev", Status: "READY"},
	}}

	provider := &SnapshotProvider{NewClient: func(ctx context.Context, project string) (SnapshotClient, error) {
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "data-disk-dev"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "snap-b" {
		t.Fatalf("expected 1 result for source disk search, got %d", len(results))
	}
}

func TestSnapshotProviderPropagatesErrors(t *testing.T) {
	provider := &SnapshotProvider{NewClient: func(context.Context, string) (SnapshotClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &SnapshotProvider{NewClient: func(context.Context, string) (SnapshotClient, error) {
		return &fakeSnapshotClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestSnapshotProviderNilProvider(t *testing.T) {
	var provider *SnapshotProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

type fakeSnapshotClient struct {
	snapshots []*gcp.Snapshot
	err       error
}

func (f *fakeSnapshotClient) ListSnapshots(context.Context) ([]*gcp.Snapshot, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.snapshots, nil
}
