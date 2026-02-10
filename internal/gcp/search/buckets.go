package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// BucketClientFactory creates a Cloud Storage client scoped to a project.
type BucketClientFactory func(ctx context.Context, project string) (BucketClient, error)

// BucketClient exposes the subset of gcp.Client used by the bucket searcher.
type BucketClient interface {
	ListBuckets(ctx context.Context) ([]*gcp.Bucket, error)
}

// BucketProvider searches Cloud Storage buckets for query matches.
type BucketProvider struct {
	NewClient BucketClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *BucketProvider) Kind() ResourceKind {
	return KindBucket
}

// Search implements the Provider interface.
func (p *BucketProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	buckets, err := client.ListBuckets(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list buckets in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(buckets))
	for _, bucket := range buckets {
		if bucket == nil || !query.MatchesAny(bucket.Name, bucket.StorageClass) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindBucket,
			Name:     bucket.Name,
			Project:  project,
			Location: bucket.Location,
			Details:  bucketDetails(bucket),
		})
	}

	return matches, nil
}

// bucketDetails extracts display metadata for a Cloud Storage bucket.
func bucketDetails(bucket *gcp.Bucket) map[string]string {
	details := make(map[string]string)

	if bucket.StorageClass != "" {
		details["storageClass"] = bucket.StorageClass
	}

	if bucket.LocationType != "" {
		details["locationType"] = bucket.LocationType
	}

	return details
}
