package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestRouteProviderReturnsMatches(t *testing.T) {
	client := &fakeRouteClient{routes: []*gcp.Route{
		{Name: "default-route", DestRange: "0.0.0.0/0", Network: "default", NextHop: "default-internet-gateway", Priority: 1000},
		{Name: "peering-route", DestRange: "10.128.0.0/20", Network: "my-vpc", NextHop: "peering-connection", Priority: 100, Description: "peering to shared vpc"},
	}}

	provider := &RouteProvider{NewClient: func(ctx context.Context, project string) (RouteClient, error) {
		return client, nil
	}}

	// Search by name
	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "peering"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "peering-route" {
		t.Fatalf("expected 1 result for name search, got %d", len(results))
	}
}

func TestRouteProviderMatchesByDestRange(t *testing.T) {
	client := &fakeRouteClient{routes: []*gcp.Route{
		{Name: "default-route", DestRange: "0.0.0.0/0", Network: "default", NextHop: "default-internet-gateway"},
		{Name: "subnet-route", DestRange: "10.128.0.0/20", Network: "my-vpc", NextHop: "my-vpc"},
	}}

	provider := &RouteProvider{NewClient: func(ctx context.Context, project string) (RouteClient, error) {
		return client, nil
	}}

	// Search by dest range
	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "10.128"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "subnet-route" {
		t.Fatalf("expected 1 result for dest range search, got %d", len(results))
	}

	// Search by next hop
	results, err = provider.Search(context.Background(), "proj-a", Query{Term: "internet-gateway"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "default-route" {
		t.Fatalf("expected 1 result for next hop search, got %d", len(results))
	}
}

func TestRouteProviderMatchesByDescription(t *testing.T) {
	client := &fakeRouteClient{routes: []*gcp.Route{
		{Name: "custom-route", Description: "route to on-prem datacenter", DestRange: "192.168.0.0/16", Network: "prod-vpc", NextHop: "vpn-tunnel-1"},
	}}

	provider := &RouteProvider{NewClient: func(ctx context.Context, project string) (RouteClient, error) {
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "datacenter"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "custom-route" {
		t.Fatalf("expected 1 result for description search, got %d", len(results))
	}
}

func TestRouteProviderMatchesByNetwork(t *testing.T) {
	client := &fakeRouteClient{routes: []*gcp.Route{
		{Name: "route-a", DestRange: "10.0.0.0/8", Network: "prod-vpc", NextHop: "default-internet-gateway"},
		{Name: "route-b", DestRange: "10.0.0.0/8", Network: "dev-vpc", NextHop: "default-internet-gateway"},
	}}

	provider := &RouteProvider{NewClient: func(ctx context.Context, project string) (RouteClient, error) {
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "prod-vpc"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "route-a" {
		t.Fatalf("expected 1 result for network search, got %d", len(results))
	}
}

func TestRouteDetails(t *testing.T) {
	route := &gcp.Route{
		Name:        "custom-route",
		Description: "route to on-prem",
		DestRange:   "192.168.0.0/16",
		Network:     "prod-vpc",
		NextHop:     "vpn-tunnel-1",
		Priority:    100,
		RouteType:   "STATIC",
		Tags:        []string{"vpn-client", "on-prem"},
	}

	details := routeDetails(route)
	if details["destRange"] != "192.168.0.0/16" {
		t.Errorf("expected destRange, got %q", details["destRange"])
	}
	if details["network"] != "prod-vpc" {
		t.Errorf("expected network, got %q", details["network"])
	}
	if details["nextHop"] != "vpn-tunnel-1" {
		t.Errorf("expected nextHop, got %q", details["nextHop"])
	}
	if details["priority"] != "100" {
		t.Errorf("expected priority 100, got %q", details["priority"])
	}
	if details["routeType"] != "STATIC" {
		t.Errorf("expected routeType STATIC, got %q", details["routeType"])
	}
	if details["tags"] != "vpn-client, on-prem" {
		t.Errorf("expected tags, got %q", details["tags"])
	}
	if details["description"] != "route to on-prem" {
		t.Errorf("expected description, got %q", details["description"])
	}
}

func TestRouteDetailsEmpty(t *testing.T) {
	route := &gcp.Route{Name: "minimal-route"}
	details := routeDetails(route)
	if _, ok := details["description"]; ok {
		t.Error("expected no description key for empty description")
	}
	if _, ok := details["tags"]; ok {
		t.Error("expected no tags key for empty tags")
	}
	if _, ok := details["routeType"]; ok {
		t.Error("expected no routeType key for empty route type")
	}
}

func TestRouteProviderNilProvider(t *testing.T) {
	var provider *RouteProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

func TestRouteProviderPropagatesErrors(t *testing.T) {
	provider := &RouteProvider{NewClient: func(context.Context, string) (RouteClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &RouteProvider{NewClient: func(context.Context, string) (RouteClient, error) {
		return &fakeRouteClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

type fakeRouteClient struct {
	routes []*gcp.Route
	err    error
}

func (f *fakeRouteClient) ListRoutes(context.Context) ([]*gcp.Route, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.routes, nil
}
