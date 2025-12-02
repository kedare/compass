package search

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
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
func (e *Engine) Search(ctx context.Context, projects []string, query Query) ([]Result, error) {
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

	limit := e.MaxConcurrentProjects
	if limit <= 0 {
		limit = defaultProjectConcurrency
	}

	sem := make(chan struct{}, limit)
	var mu sync.Mutex
	var results []Result

	eg, egCtx := errgroup.WithContext(ctx)
	for _, project := range trimmed {
		project := project
		eg.Go(func() error {
			sem <- struct{}{}
			defer func() { <-sem }()

			for _, provider := range e.providers {
				providerResults, err := provider.Search(egCtx, project, query)
				if err != nil {
					return fmt.Errorf("%s: %w", project, err)
				}

				if len(providerResults) == 0 {
					continue
				}

				mu.Lock()
				results = append(results, providerResults...)
				mu.Unlock()
			}

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

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

	return results, nil
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
