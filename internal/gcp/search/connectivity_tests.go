package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// ConnectivityTestClientFactory creates a connectivity test client scoped to a project.
type ConnectivityTestClientFactory func(ctx context.Context, project string) (ConnectivityTestClient, error)

// ConnectivityTestClient exposes the subset of gcp.ConnectivityClient used by the searcher.
type ConnectivityTestClient interface {
	ListTests(ctx context.Context, filter string) ([]*gcp.ConnectivityTestResult, error)
}

// ConnectivityTestProvider searches connectivity tests for query matches.
type ConnectivityTestProvider struct {
	NewClient ConnectivityTestClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *ConnectivityTestProvider) Kind() ResourceKind {
	return KindConnectivityTest
}

// Search implements the Provider interface.
func (p *ConnectivityTestProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	tests, err := client.ListTests(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list connectivity tests in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(tests))
	for _, test := range tests {
		if test == nil {
			continue
		}

		// Build searchable fields
		searchFields := []string{test.Name, test.DisplayName, test.Description, test.Protocol}

		// Add source endpoint info
		if test.Source != nil {
			searchFields = append(searchFields, test.Source.Instance, test.Source.IPAddress, test.Source.Network)
		}

		// Add destination endpoint info
		if test.Destination != nil {
			searchFields = append(searchFields, test.Destination.Instance, test.Destination.IPAddress, test.Destination.Network)
		}

		if !query.MatchesAny(searchFields...) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindConnectivityTest,
			Name:     test.Name,
			Project:  test.ProjectID,
			Location: "global",
			Details:  connectivityTestDetails(test),
		})
	}

	return matches, nil
}

// connectivityTestDetails extracts display metadata for a connectivity test.
func connectivityTestDetails(test *gcp.ConnectivityTestResult) map[string]string {
	details := map[string]string{
		"displayName": test.DisplayName,
		"protocol":    test.Protocol,
	}

	if test.ReachabilityDetails != nil {
		details["result"] = test.ReachabilityDetails.Result
	}

	if test.Source != nil && test.Source.IPAddress != "" {
		details["source"] = test.Source.IPAddress
	}

	if test.Destination != nil && test.Destination.IPAddress != "" {
		details["destination"] = test.Destination.IPAddress
	}

	return details
}
