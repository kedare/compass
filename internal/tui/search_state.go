package tui

import (
	"context"
	"sync"

	"github.com/kedare/compass/internal/gcp/search"
)

// This file contains state management structures for the search view.

// SearchViewState encapsulates all state variables for the search view.
// This improves testability and makes state management explicit.
type SearchViewState struct {
	// Core data
	AllResults  []searchEntry
	AllWarnings []search.SearchWarning
	SearchError error

	// UI state
	ModalOpen     bool
	FilterMode    bool
	CurrentFilter string
	FuzzyMode     bool

	// Search context
	CurrentSearchTerm string
	RecordedAffinity  map[string]struct{}

	// Search history
	SearchHistory []string
	HistoryIndex  int

	// Sync primitives
	ResultsMu    sync.Mutex
	IsSearching  bool
	SearchCancel context.CancelFunc
}

// NewSearchViewState creates a new search view state with sensible defaults.
func NewSearchViewState() *SearchViewState {
	return &SearchViewState{
		AllResults:       []searchEntry{},
		AllWarnings:      []search.SearchWarning{},
		RecordedAffinity: make(map[string]struct{}),
		HistoryIndex:     -1,
	}
}
