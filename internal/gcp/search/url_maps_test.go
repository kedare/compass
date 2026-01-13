package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestURLMapProviderReturnsMatches(t *testing.T) {
	client := &fakeURLMapClient{urlMaps: []*gcp.URLMap{
		{Name: "prod-lb-url-map", DefaultService: "prod-backend", HostRuleCount: 3, PathMatcherCount: 2},
		{Name: "dev-url-map", DefaultService: "dev-backend", HostRuleCount: 1, PathMatcherCount: 1},
	}}

	provider := &URLMapProvider{NewClient: func(ctx context.Context, project string) (URLMapClient, error) {
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

	if results[0].Name != "prod-lb-url-map" {
		t.Fatalf("expected prod-lb-url-map, got %s", results[0].Name)
	}

	if results[0].Type != KindURLMap {
		t.Fatalf("expected type %s, got %s", KindURLMap, results[0].Type)
	}

	if results[0].Location != "global" {
		t.Fatalf("expected location global, got %s", results[0].Location)
	}

	if results[0].Details["defaultService"] != "prod-backend" {
		t.Fatalf("expected defaultService prod-backend, got %s", results[0].Details["defaultService"])
	}

	if results[0].Details["hostRules"] != "3" {
		t.Fatalf("expected hostRules 3, got %s", results[0].Details["hostRules"])
	}
}

func TestURLMapProviderPropagatesErrors(t *testing.T) {
	provider := &URLMapProvider{NewClient: func(context.Context, string) (URLMapClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &URLMapProvider{NewClient: func(context.Context, string) (URLMapClient, error) {
		return &fakeURLMapClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestURLMapProviderNilProvider(t *testing.T) {
	var provider *URLMapProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

type fakeURLMapClient struct {
	urlMaps []*gcp.URLMap
	err     error
}

func (f *fakeURLMapClient) ListURLMaps(context.Context) ([]*gcp.URLMap, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.urlMaps, nil
}
