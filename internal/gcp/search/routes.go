package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/kedare/compass/internal/gcp"
)

// RouteClient exposes the subset of gcp.Client used by the route searcher.
type RouteClient interface {
	ListRoutes(ctx context.Context) ([]*gcp.Route, error)
}

// RouteProvider searches VPC routes for query matches.
type RouteProvider struct {
	NewClient func(ctx context.Context, project string) (RouteClient, error)
}

// Kind returns the resource kind this provider handles.
func (p *RouteProvider) Kind() ResourceKind {
	return KindRoute
}

// Search implements the Provider interface.
func (p *RouteProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	routes, err := client.ListRoutes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list routes in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(routes))
	for _, route := range routes {
		if route == nil || !query.MatchesAny(route.Name, route.Description, route.DestRange, route.Network, route.NextHop) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindRoute,
			Name:     route.Name,
			Project:  project,
			Location: "global",
			Details:  routeDetails(route),
		})
	}

	return matches, nil
}

// routeDetails extracts display metadata for a VPC route.
func routeDetails(route *gcp.Route) map[string]string {
	details := make(map[string]string)

	if route.DestRange != "" {
		details["destRange"] = route.DestRange
	}

	if route.Network != "" {
		details["network"] = route.Network
	}

	if route.NextHop != "" {
		details["nextHop"] = route.NextHop
	}

	if route.Priority > 0 {
		details["priority"] = fmt.Sprintf("%d", route.Priority)
	}

	if route.RouteType != "" {
		details["routeType"] = route.RouteType
	}

	if len(route.Tags) > 0 {
		details["tags"] = strings.Join(route.Tags, ", ")
	}

	if route.Description != "" {
		details["description"] = route.Description
	}

	return details
}
