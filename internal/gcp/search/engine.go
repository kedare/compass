package search

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
)

const defaultProjectConcurrency = 4

var (
	// ErrNoProjects indicates that no projects were provided for the search.
	ErrNoProjects = errors.New("no projects available for search")
	// ErrEmptyQuery indicates that the query term is empty or whitespace.
	ErrEmptyQuery = errors.New("search term cannot be empty")
	// ErrNoProviders indicates that the engine has no active providers.
	ErrNoProviders = errors.New("no search providers configured")
)

// Engine coordinates running the configured providers across a set of projects.
type Engine struct {
	providers             []Provider
	MaxConcurrentProjects int
}

// NewEngine creates a search engine with the supplied providers.
func NewEngine(providers ...Provider) *Engine {
	return &Engine{providers: providers}
}

// Search executes the configured providers across the provided projects.
// It returns partial results even when some providers fail, collecting errors as warnings.
func (e *Engine) Search(ctx context.Context, projects []string, query Query) ([]Result, error) {
	output, err := e.SearchWithWarnings(ctx, projects, query)
	if err != nil {
		return nil, err
	}
	return output.Results, nil
}

// SearchWithWarnings executes the configured providers across the provided projects.
// Unlike Search, it returns both results and any warnings from failed providers.
// This allows callers to display partial results while informing users about failures.
func (e *Engine) SearchWithWarnings(ctx context.Context, projects []string, query Query) (*SearchOutput, error) {
	if e == nil {
		return nil, ErrNoProviders
	}

	if len(e.providers) == 0 {
		return nil, ErrNoProviders
	}

	trimmed := uniqueProjects(projects)
	if len(trimmed) == 0 {
		return nil, ErrNoProjects
	}

	if query.NormalizedTerm() == "" {
		return nil, ErrEmptyQuery
	}

	// Filter providers by type if type filter is specified.
	// This avoids making API calls for resource types we don't care about.
	activeProviders := filterProvidersByType(e.providers, query)
	if len(activeProviders) == 0 {
		// No providers match the requested types
		return &SearchOutput{}, nil
	}

	limit := e.MaxConcurrentProjects
	if limit <= 0 {
		limit = defaultProjectConcurrency
	}

	sem := make(chan struct{}, limit)
	var mu sync.Mutex
	var results []Result
	var warnings []SearchWarning

	var wg sync.WaitGroup
	for _, project := range trimmed {
		project := project
		wg.Add(1)
		go func() {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			// Check if context was cancelled
			if ctx.Err() != nil {
				return
			}

			for _, provider := range activeProviders {
				providerResults, err := provider.Search(ctx, project, query)
				if err != nil {
					// Record warning but continue with other providers
					mu.Lock()
					warnings = append(warnings, SearchWarning{
						Project:  project,
						Provider: provider.Kind(),
						Err:      err,
					})
					mu.Unlock()
					continue
				}

				if len(providerResults) == 0 {
					continue
				}

				mu.Lock()
				results = append(results, providerResults...)
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		if results[i].Project != results[j].Project {
			return results[i].Project < results[j].Project
		}
		if results[i].Type != results[j].Type {
			return results[i].Type < results[j].Type
		}
		if results[i].Name != results[j].Name {
			return results[i].Name < results[j].Name
		}
		return results[i].Location < results[j].Location
	})

	// Sort warnings for consistent output
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].Project != warnings[j].Project {
			return warnings[i].Project < warnings[j].Project
		}
		return warnings[i].Provider < warnings[j].Provider
	})

	return &SearchOutput{
		Results:  results,
		Warnings: warnings,
	}, nil
}

// uniqueProjects trims, deduplicates, and preserves order of project IDs.
func uniqueProjects(projects []string) []string {
	seen := make(map[string]struct{}, len(projects))
	ordered := make([]string, 0, len(projects))
	for _, project := range projects {
		project = strings.TrimSpace(project)
		if project == "" {
			continue
		}
		if _, exists := seen[project]; exists {
			continue
		}
		seen[project] = struct{}{}
		ordered = append(ordered, project)
	}
	return ordered
}

// filterProvidersByType returns only providers whose Kind matches the query's type filter.
// If no type filter is set, all providers are returned.
func filterProvidersByType(providers []Provider, query Query) []Provider {
	if len(query.Types) == 0 {
		return providers
	}

	filtered := make([]Provider, 0, len(providers))
	for _, provider := range providers {
		if query.MatchesType(provider.Kind()) {
			filtered = append(filtered, provider)
		}
	}

	return filtered
}
