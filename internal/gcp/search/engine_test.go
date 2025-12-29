package search

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestEngineSearchAggregatesResults(t *testing.T) {
	provider := &stubProvider{
		responses: map[string][]Result{
			"b": {{Project: "b", Type: KindComputeInstance, Name: "beta", Location: "us-central1-a"}},
			"a": {{Project: "a", Type: KindComputeInstance, Name: "alpha", Location: "europe-west1-b"}},
		},
	}

	engine := NewEngine(provider)
	results, err := engine.Search(context.Background(), []string{"b", "a", "a"}, Query{Term: "a"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Project != "a" || results[1].Project != "b" {
		t.Fatalf("unexpected ordering: %#v", results)
	}

	if provider.calls["a"] != 1 || provider.calls["b"] != 1 {
		t.Fatalf("expected provider to be called once per project, got %#v", provider.calls)
	}
}

func TestEngineSearchValidatesInputs(t *testing.T) {
	engine := NewEngine(&stubProvider{})
	if _, err := engine.Search(context.Background(), nil, Query{Term: "foo"}); !errors.Is(err, ErrNoProjects) {
		t.Fatalf("expected ErrNoProjects, got %v", err)
	}

	if _, err := engine.Search(context.Background(), []string{"a"}, Query{}); !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("expected ErrEmptyQuery, got %v", err)
	}

	emptyEngine := &Engine{}
	if _, err := emptyEngine.Search(context.Background(), []string{"a"}, Query{Term: "a"}); !errors.Is(err, ErrNoProviders) {
		t.Fatalf("expected ErrNoProviders, got %v", err)
	}
}

func TestEngineSearchCollectsWarningsOnProviderErrors(t *testing.T) {
	provider := &stubProvider{
		errProject: "b",
		responses: map[string][]Result{
			"a": {{Project: "a", Type: KindComputeInstance, Name: "alpha", Location: "us-central1-a"}},
		},
	}
	engine := NewEngine(provider)

	// Search should succeed with partial results
	output, err := engine.SearchWithWarnings(context.Background(), []string{"a", "b"}, Query{Term: "x"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should have one result from project "a"
	if len(output.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(output.Results))
	}

	// Should have one warning from project "b"
	if len(output.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(output.Warnings))
	}

	if output.Warnings[0].Project != "b" {
		t.Fatalf("expected warning for project 'b', got %q", output.Warnings[0].Project)
	}
}

func TestEngineSearchReturnsEmptyResultsOnAllProviderErrors(t *testing.T) {
	provider := &stubProvider{errProject: "a"}
	engine := NewEngine(provider)

	// Search should succeed but with no results, only warnings
	output, err := engine.SearchWithWarnings(context.Background(), []string{"a"}, Query{Term: "x"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(output.Results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(output.Results))
	}

	if len(output.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(output.Warnings))
	}
}

type stubProvider struct {
	responses  map[string][]Result
	errProject string
	calls      map[string]int
	mu         sync.Mutex
	kind       ResourceKind
}

func (s *stubProvider) Kind() ResourceKind {
	if s.kind == "" {
		return KindComputeInstance
	}
	return s.kind
}

func (s *stubProvider) Search(_ context.Context, project string, query Query) ([]Result, error) {
	s.mu.Lock()
	if s.calls == nil {
		s.calls = make(map[string]int)
	}
	s.calls[project]++
	s.mu.Unlock()

	if project == s.errProject {
		return nil, errors.New("boom")
	}

	if s.responses == nil {
		return nil, nil
	}

	return s.responses[project], nil
}
