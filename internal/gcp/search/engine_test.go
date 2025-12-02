package search

import (
	"context"
	"errors"
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

func TestEngineSearchPropagatesErrors(t *testing.T) {
	provider := &stubProvider{errProject: "b"}
	engine := NewEngine(provider)
	_, err := engine.Search(context.Background(), []string{"a", "b"}, Query{Term: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
}

type stubProvider struct {
	responses  map[string][]Result
	errProject string
	calls      map[string]int
}

func (s *stubProvider) Search(_ context.Context, project string, query Query) ([]Result, error) {
	if s.calls == nil {
		s.calls = make(map[string]int)
	}
	s.calls[project]++

	if project == s.errProject {
		return nil, errors.New("boom")
	}

	if s.responses == nil {
		return nil, nil
	}

	return s.responses[project], nil
}
