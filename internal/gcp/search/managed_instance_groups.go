package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/kedare/compass/internal/gcp"
)

// MIGClientFactory creates a Compute Engine client scoped to a project.
type MIGClientFactory func(ctx context.Context, project string) (MIGClient, error)

// MIGClient exposes the subset of gcp.Client used by the MIG searcher.
type MIGClient interface {
	ListManagedInstanceGroups(ctx context.Context, location string) ([]gcp.ManagedInstanceGroup, error)
}

// MIGProvider searches Managed Instance Groups for query matches.
type MIGProvider struct {
	NewClient MIGClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *MIGProvider) Kind() ResourceKind {
	return KindManagedInstanceGroup
}

// Search implements the Provider interface.
func (p *MIGProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	migs, err := client.ListManagedInstanceGroups(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list managed instance groups in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(migs))
	for _, mig := range migs {
		if !query.MatchesAny(mig.Name, mig.Description, mig.BaseInstanceName, mig.InstanceTemplate) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindManagedInstanceGroup,
			Name:     mig.Name,
			Project:  project,
			Location: mig.Location,
			Details:  migDetails(&mig),
		})
	}

	return matches, nil
}

// migDetails extracts display metadata for a Managed Instance Group.
func migDetails(mig *gcp.ManagedInstanceGroup) map[string]string {
	details := make(map[string]string)

	if mig.IsRegional {
		details["scope"] = "regional"
	} else {
		details["scope"] = "zonal"
	}

	if mig.Description != "" {
		details["description"] = mig.Description
	}

	// Size info
	details["targetSize"] = fmt.Sprintf("%d", mig.TargetSize)
	if mig.CurrentSize > 0 {
		details["currentSize"] = fmt.Sprintf("%d", mig.CurrentSize)
	}

	// Instance template
	if mig.InstanceTemplate != "" {
		details["instanceTemplate"] = mig.InstanceTemplate
	}

	// Base instance name
	if mig.BaseInstanceName != "" {
		details["baseInstanceName"] = mig.BaseInstanceName
	}

	// Status
	if mig.IsStable {
		details["status"] = "stable"
	} else {
		details["status"] = "updating"
	}

	// Update policy
	if mig.UpdateType != "" {
		details["updateType"] = mig.UpdateType
	}
	if mig.MaxSurge != "" {
		details["maxSurge"] = mig.MaxSurge
	}
	if mig.MaxUnavailable != "" {
		details["maxUnavailable"] = mig.MaxUnavailable
	}

	// Named ports
	if len(mig.NamedPorts) > 0 {
		var ports []string
		for name, port := range mig.NamedPorts {
			ports = append(ports, fmt.Sprintf("%s:%d", name, port))
		}
		details["namedPorts"] = strings.Join(ports, ", ")
	}

	// Target zones (for regional MIGs)
	if len(mig.TargetZones) > 0 {
		details["targetZones"] = strings.Join(mig.TargetZones, ", ")
	}

	return details
}
