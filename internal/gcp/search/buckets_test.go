package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestBucketProviderReturnsMatches(t *testing.T) {
	client := &fakeBucketClient{buckets: []*gcp.Bucket{
		{Name: "prod-data-bucket", Location: "US", StorageClass: "STANDARD", LocationType: "multi-region"},
		{Name: "dev-bucket", Location: "EU", StorageClass: "NEARLINE", LocationType: "multi-region"},
	}}

	provider := &BucketProvider{NewClient: func(ctx context.Context, project string) (BucketClient, error) {
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

	if results[0].Name != "prod-data-bucket" {
		t.Fatalf("expected prod-data-bucket, got %s", results[0].Name)
	}

	if results[0].Type != KindBucket {
		t.Fatalf("expected type %s, got %s", KindBucket, results[0].Type)
	}

	if results[0].Location != "US" {
		t.Fatalf("expected location US, got %s", results[0].Location)
	}

	if results[0].Details["storageClass"] != "STANDARD" {
		t.Fatalf("expected storageClass STANDARD, got %s", results[0].Details["storageClass"])
	}
}

func TestBucketProviderMatchesByStorageClass(t *testing.T) {
	client := &fakeBucketClient{buckets: []*gcp.Bucket{
		{Name: "archive-bucket", Location: "US", StorageClass: "COLDLINE"},
		{Name: "hot-bucket", Location: "US", StorageClass: "STANDARD"},
	}}

	provider := &BucketProvider{NewClient: func(ctx context.Context, project string) (BucketClient, error) {
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "COLDLINE"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "archive-bucket" {
		t.Fatalf("expected 1 result for storage class search, got %d", len(results))
	}
}

func TestBucketProviderPropagatesErrors(t *testing.T) {
	provider := &BucketProvider{NewClient: func(context.Context, string) (BucketClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &BucketProvider{NewClient: func(context.Context, string) (BucketClient, error) {
		return &fakeBucketClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestBucketProviderNilProvider(t *testing.T) {
	var provider *BucketProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

type fakeBucketClient struct {
	buckets []*gcp.Bucket
	err     error
}

func (f *fakeBucketClient) ListBuckets(context.Context) ([]*gcp.Bucket, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.buckets, nil
}
