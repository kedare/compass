// Package search provides modular search capabilities for GCP resources.
package search

import (
	"context"
	"strings"
)

// ResourceKind identifies the category of a searchable resource.
type ResourceKind string

const (
	// KindComputeInstance represents a Compute Engine VM instance.
	KindComputeInstance ResourceKind = "compute.instance"
)

// Query captures the user supplied search term and matching behaviour.
type Query struct {
	Term string
}

// NormalizedTerm returns the lowercase trimmed representation of the query.
func (q Query) NormalizedTerm() string {
	return strings.ToLower(strings.TrimSpace(q.Term))
}

// Matches reports whether the provided value satisfies the query.
func (q Query) Matches(value string) bool {
	normalized := q.NormalizedTerm()
	if normalized == "" {
		return false
	}

	return strings.Contains(strings.ToLower(value), normalized)
}

// Result describes a single resource that matched the query.
type Result struct {
	Type     ResourceKind
	Name     string
	Project  string
	Location string
	Details  map[string]string
}

// Provider defines a search backend for a specific resource type.
type Provider interface {
	// Search inspects resources in the provided project and returns matches.
	Search(ctx context.Context, project string, query Query) ([]Result, error)
}
