package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// DiskClientFactory creates a Compute Engine client scoped to a project.
type DiskClientFactory func(ctx context.Context, project string) (DiskClient, error)

// DiskClient exposes the subset of gcp.Client used by the disk searcher.
type DiskClient interface {
	ListDisks(ctx context.Context) ([]*gcp.Disk, error)
}

// DiskProvider searches Compute Engine disks for query matches.
type DiskProvider struct {
	NewClient DiskClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *DiskProvider) Kind() ResourceKind {
	return KindDisk
}

// Search implements the Provider interface.
func (p *DiskProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	disks, err := client.ListDisks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list disks in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(disks))
	for _, disk := range disks {
		if disk == nil || !query.MatchesAny(disk.Name, disk.Type) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindDisk,
			Name:     disk.Name,
			Project:  project,
			Location: disk.Zone,
			Details:  diskDetails(disk),
		})
	}

	return matches, nil
}

// diskDetails extracts display metadata for a Compute Engine disk.
func diskDetails(disk *gcp.Disk) map[string]string {
	details := make(map[string]string)

	if disk.SizeGb > 0 {
		details["sizeGb"] = fmt.Sprintf("%d", disk.SizeGb)
	}

	if disk.Type != "" {
		details["type"] = disk.Type
	}

	if disk.Status != "" {
		details["status"] = disk.Status
	}

	return details
}
