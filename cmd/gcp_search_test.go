package cmd

import (
	"context"
	"testing"

	"github.com/kedare/compass/internal/gcp/search"
)

func TestResolveSearchProjectsPrefersFlag(t *testing.T) {
	prev := project
	project = "my-project"
	t.Cleanup(func() { project = prev })

	projects, err := resolveSearchProjects()
	if err != nil {
		t.Fatalf("resolveSearchProjects failed: %v", err)
	}

	if len(projects) != 1 || projects[0] != "my-project" {
		t.Fatalf("expected project from flag, got %#v", projects)
	}
}

func TestResolveSearchProjectsUsesCache(t *testing.T) {
	prevProject := project
	project = ""
	prevProvider := cachedProjectsProvider
	cachedProjectsProvider = func() ([]string, bool, error) {
		return []string{"a", "a", ""}, true, nil
	}
	t.Cleanup(func() {
		project = prevProject
		cachedProjectsProvider = prevProvider
	})

	projects, err := resolveSearchProjects()
	if err != nil {
		t.Fatalf("resolveSearchProjects failed: %v", err)
	}

	if len(projects) != 1 || projects[0] != "a" {
		t.Fatalf("expected deduplicated cached project, got %#v", projects)
	}
}

func TestResolveSearchProjectsErrorsWithoutCache(t *testing.T) {
	prevProject := project
	project = ""
	prevProvider := cachedProjectsProvider
	cachedProjectsProvider = func() ([]string, bool, error) {
		return nil, false, nil
	}
	t.Cleanup(func() {
		project = prevProject
		cachedProjectsProvider = prevProvider
	})

	if _, err := resolveSearchProjects(); err == nil {
		t.Fatal("expected error when cache disabled")
	}
}

func TestGCPSearchCommandInvokesEngine(t *testing.T) {
	prevSearchFactory := searchEngineFactory
	prevProviderFactory := instanceProviderFactory
	prevProject := project
	prevCacheProvider := cachedProjectsProvider
	project = ""
	instanceProviderFactory = func() search.Provider { return nil }
	cachedProjectsProvider = func() ([]string, bool, error) { return []string{"proj-a"}, true, nil }
	called := false
	searchEngineFactory = func(_ ...search.Provider) resourceSearchEngine {
		return searchEngineFunc(func(ctx context.Context, projects []string, query search.Query) ([]search.Result, error) {
			called = true
			if query.Term != "piou" {
				t.Fatalf("unexpected query: %s", query.Term)
			}
			if len(projects) != 1 || projects[0] != "proj-a" {
				t.Fatalf("unexpected projects: %#v", projects)
			}

			return []search.Result{{Type: search.KindComputeInstance, Name: "piou", Project: "proj-a"}}, nil
		})
	}

	t.Cleanup(func() {
		searchEngineFactory = prevSearchFactory
		instanceProviderFactory = prevProviderFactory
		project = prevProject
		cachedProjectsProvider = prevCacheProvider
	})

	if err := gcpSearchCmd.RunE(gcpSearchCmd, []string{"piou"}); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !called {
		t.Fatal("expected engine to be invoked")
	}
}

func TestFormatResultDetails(t *testing.T) {
	details := map[string]string{"status": "RUNNING", "internalIP": "10.0.0.1"}
	formatted := formatResultDetails(details)
	if formatted != "internalIP=10.0.0.1, status=RUNNING" {
		t.Fatalf("unexpected formatted value: %s", formatted)
	}

	if formatResultDetails(nil) != "" {
		t.Fatal("expected empty string for nil details")
	}
}

type searchEngineFunc func(ctx context.Context, projects []string, query search.Query) ([]search.Result, error)

func (f searchEngineFunc) Search(ctx context.Context, projects []string, query search.Query) ([]search.Result, error) {
	return f(ctx, projects, query)
}
