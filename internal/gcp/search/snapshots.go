package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// SnapshotClientFactory creates a Compute Engine client scoped to a project.
type SnapshotClientFactory func(ctx context.Context, project string) (SnapshotClient, error)

// SnapshotClient exposes the subset of gcp.Client used by the snapshot searcher.
type SnapshotClient interface {
	ListSnapshots(ctx context.Context) ([]*gcp.Snapshot, error)
}

// SnapshotProvider searches Compute Engine snapshots for query matches.
type SnapshotProvider struct {
	NewClient SnapshotClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *SnapshotProvider) Kind() ResourceKind {
	return KindSnapshot
}

// Search implements the Provider interface.
func (p *SnapshotProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	snapshots, err := client.ListSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list snapshots in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot == nil || !query.MatchesAny(snapshot.Name, snapshot.SourceDisk) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindSnapshot,
			Name:     snapshot.Name,
			Project:  project,
			Location: "global",
			Details:  snapshotDetails(snapshot),
		})
	}

	return matches, nil
}

// snapshotDetails extracts display metadata for a Compute Engine snapshot.
func snapshotDetails(snapshot *gcp.Snapshot) map[string]string {
	details := make(map[string]string)

	if snapshot.DiskSizeGb > 0 {
		details["diskSizeGb"] = fmt.Sprintf("%d", snapshot.DiskSizeGb)
	}

	if snapshot.StorageBytes > 0 {
		details["storageMb"] = fmt.Sprintf("%d", snapshot.StorageBytes/(1024*1024))
	}

	if snapshot.Status != "" {
		details["status"] = snapshot.Status
	}

	if snapshot.SourceDisk != "" {
		details["sourceDisk"] = snapshot.SourceDisk
	}

	return details
}
