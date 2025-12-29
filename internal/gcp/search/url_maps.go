package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// URLMapClientFactory creates a Compute Engine client scoped to a project.
type URLMapClientFactory func(ctx context.Context, project string) (URLMapClient, error)

// URLMapClient exposes the subset of gcp.Client used by the URL map searcher.
type URLMapClient interface {
	ListURLMaps(ctx context.Context) ([]*gcp.URLMap, error)
}

// URLMapProvider searches URL maps for query matches.
type URLMapProvider struct {
	NewClient URLMapClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *URLMapProvider) Kind() ResourceKind {
	return KindURLMap
}

// Search implements the Provider interface.
func (p *URLMapProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	urlMaps, err := client.ListURLMaps(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list URL maps in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(urlMaps))
	for _, urlMap := range urlMaps {
		if urlMap == nil || !query.Matches(urlMap.Name) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindURLMap,
			Name:     urlMap.Name,
			Project:  project,
			Location: "global",
			Details:  urlMapDetails(urlMap),
		})
	}

	return matches, nil
}

// urlMapDetails extracts display metadata for a URL map.
func urlMapDetails(urlMap *gcp.URLMap) map[string]string {
	details := make(map[string]string)

	if urlMap.DefaultService != "" {
		details["defaultService"] = urlMap.DefaultService
	}

	if urlMap.HostRuleCount > 0 {
		details["hostRules"] = fmt.Sprintf("%d", urlMap.HostRuleCount)
	}

	if urlMap.PathMatcherCount > 0 {
		details["pathMatchers"] = fmt.Sprintf("%d", urlMap.PathMatcherCount)
	}

	return details
}
