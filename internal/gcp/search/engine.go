package search

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
)

// SearchProgress contains information about the current search progress.
type SearchProgress struct {
	TotalRequests     int    // Total number of provider requests to make
	CompletedRequests int    // Number of completed requests
	PendingRequests   int    // Number of pending requests
	CurrentProject    string // Current project being searched
	CurrentProvider   string // Current provider being used
}

// ResultCallback is called when new results are available during streaming search.
// The callback receives a slice of new results and progress information.
// If the callback returns an error, the search will be stopped.
type ResultCallback func(results []Result, progress SearchProgress) error

const defaultProjectConcurrency = 8

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

// SearchStreaming executes search and calls the callback as results become available.
// This allows progressive display of results in the UI.
// The search can be cancelled via context, and existing results are preserved.
// Returns all accumulated results and warnings when complete.
func (e *Engine) SearchStreaming(ctx context.Context, projects []string, query Query, callback ResultCallback) (*SearchOutput, error) {
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
	activeProviders := filterProvidersByType(e.providers, query)
	if len(activeProviders) == 0 {
		return &SearchOutput{}, nil
	}

	limit := e.MaxConcurrentProjects
	if limit <= 0 {
		limit = defaultProjectConcurrency
	}

	// Calculate total requests (projects × providers)
	totalRequests := len(trimmed) * len(activeProviders)
	var completedRequests int

	sem := make(chan struct{}, limit)
	var mu sync.Mutex
	var results []Result
	var warnings []SearchWarning

	// Split projects into priority batches for better affinity-based ordering
	// Search top projects first to show relevant results faster
	priorityBatchSize := 5 // Search top 5 projects first
	if len(trimmed) < priorityBatchSize {
		priorityBatchSize = len(trimmed)
	}

	searchProjectBatch := func(projects []string) {
		var wg sync.WaitGroup
		for _, project := range projects {
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
					// Check cancellation before each provider
					if ctx.Err() != nil {
						return
					}

					providerResults, err := provider.Search(ctx, project, query)

					// Update completed count regardless of result
					mu.Lock()
					completedRequests++
					currentCompleted := completedRequests
					mu.Unlock()

					if err != nil {
						// Record warning but continue with other providers
						mu.Lock()
						warnings = append(warnings, SearchWarning{
							Project:  project,
							Provider: provider.Kind(),
							Err:      err,
						})
						mu.Unlock()

						// Still send progress update even on error
						if callback != nil {
							progress := SearchProgress{
								TotalRequests:     totalRequests,
								CompletedRequests: currentCompleted,
								PendingRequests:   totalRequests - currentCompleted,
								CurrentProject:    project,
								CurrentProvider:   string(provider.Kind()),
							}
							_ = callback(nil, progress)
						}
						continue
					}

					// Call the callback with new results and progress
					mu.Lock()
					results = append(results, providerResults...)
					currentResults := make([]Result, len(providerResults))
					copy(currentResults, providerResults)
					mu.Unlock()

					// Send progress update with new results
					if callback != nil {
						progress := SearchProgress{
							TotalRequests:     totalRequests,
							CompletedRequests: currentCompleted,
							PendingRequests:   totalRequests - currentCompleted,
							CurrentProject:    project,
							CurrentProvider:   string(provider.Kind()),
						}
						if err := callback(currentResults, progress); err != nil {
							// Callback requested stop - just return
							return
						}
					}
				}
			}()
		}
		wg.Wait()
	}

	// Search high-priority projects first
	priorityBatch := trimmed[:priorityBatchSize]
	searchProjectBatch(priorityBatch)

	// Then search remaining projects
	if len(trimmed) > priorityBatchSize {
		remainingBatch := trimmed[priorityBatchSize:]
		searchProjectBatch(remainingBatch)
	}

	// Sort results for consistent output
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

// GetProviderKinds returns all resource kinds supported by the engine's providers.
func (e *Engine) GetProviderKinds() []ResourceKind {
	if e == nil {
		return nil
	}

	kinds := make([]ResourceKind, len(e.providers))
	for i, p := range e.providers {
		kinds[i] = p.Kind()
	}
	return kinds
}
