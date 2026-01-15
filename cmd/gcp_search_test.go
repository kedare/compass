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

	projects, err := resolveSearchProjects("test", nil)
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
	cachedProjectsProvider = func(searchTerm string, resourceTypes []string) ([]string, bool, error) {
		return []string{"a", "a", ""}, true, nil
	}
	t.Cleanup(func() {
		project = prevProject
		cachedProjectsProvider = prevProvider
	})

	projects, err := resolveSearchProjects("test", nil)
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
	cachedProjectsProvider = func(searchTerm string, resourceTypes []string) ([]string, bool, error) {
		return nil, false, nil
	}
	t.Cleanup(func() {
		project = prevProject
		cachedProjectsProvider = prevProvider
	})

	if _, err := resolveSearchProjects("test", nil); err == nil {
		t.Fatal("expected error when cache disabled")
	}
}

func TestGCPSearchCommandInvokesEngine(t *testing.T) {
	// Disable spinner to avoid race conditions
	prevUseSpinner := useSpinner
	useSpinner = false
	t.Cleanup(func() { useSpinner = prevUseSpinner })

	prevSearchFactory := searchEngineFactory
	prevProviderFactory := instanceProviderFactory
	prevProject := project
	prevCacheProvider := cachedProjectsProvider
	project = ""
	instanceProviderFactory = func() search.Provider { return nil }
	cachedProjectsProvider = func(searchTerm string, resourceTypes []string) ([]string, bool, error) {
		return []string{"proj-a"}, true, nil
	}
	called := false
	searchEngineFactory = func(_ int, _ ...search.Provider) resourceSearchEngine {
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

func (f searchEngineFunc) SearchWithWarnings(ctx context.Context, projects []string, query search.Query) (*search.SearchOutput, error) {
	results, err := f(ctx, projects, query)
	if err != nil {
		return nil, err
	}
	return &search.SearchOutput{Results: results}, nil
}

func TestParseSearchTypes(t *testing.T) {
	t.Run("no filters returns nil", func(t *testing.T) {
		result, err := parseSearchTypes(nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Fatalf("expected nil, got %v", result)
		}
	})

	t.Run("only include types", func(t *testing.T) {
		result, err := parseSearchTypes([]string{"compute.instance", "compute.disk"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 types, got %d", len(result))
		}
		if result[0] != search.KindComputeInstance || result[1] != search.KindDisk {
			t.Fatalf("unexpected types: %v", result)
		}
	})

	t.Run("only exclude types", func(t *testing.T) {
		result, err := parseSearchTypes(nil, []string{"compute.instance"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should return all types except compute.instance
		allKinds := search.AllResourceKinds()
		expectedLen := len(allKinds) - 1
		if len(result) != expectedLen {
			t.Fatalf("expected %d types, got %d", expectedLen, len(result))
		}
		// Verify compute.instance is not in the result
		for _, kind := range result {
			if kind == search.KindComputeInstance {
				t.Fatal("compute.instance should be excluded")
			}
		}
	})

	t.Run("include and exclude types", func(t *testing.T) {
		result, err := parseSearchTypes(
			[]string{"compute.instance", "compute.disk", "storage.bucket"},
			[]string{"compute.disk"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 types, got %d", len(result))
		}
		// Should have instance and bucket but not disk
		hasInstance := false
		hasBucket := false
		for _, kind := range result {
			if kind == search.KindComputeInstance {
				hasInstance = true
			}
			if kind == search.KindBucket {
				hasBucket = true
			}
			if kind == search.KindDisk {
				t.Fatal("compute.disk should be excluded")
			}
		}
		if !hasInstance || !hasBucket {
			t.Fatalf("expected instance and bucket, got %v", result)
		}
	})

	t.Run("invalid include type", func(t *testing.T) {
		_, err := parseSearchTypes([]string{"invalid.type"}, nil)
		if err == nil {
			t.Fatal("expected error for invalid type")
		}
	})

	t.Run("invalid exclude type", func(t *testing.T) {
		_, err := parseSearchTypes(nil, []string{"invalid.type"})
		if err == nil {
			t.Fatal("expected error for invalid type")
		}
	})

	t.Run("empty strings are ignored", func(t *testing.T) {
		result, err := parseSearchTypes([]string{"compute.instance", "", "  "}, []string{"", "compute.disk"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 1 || result[0] != search.KindComputeInstance {
			t.Fatalf("expected only compute.instance, got %v", result)
		}
	})
}
