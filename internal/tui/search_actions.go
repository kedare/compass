package tui

import (
	"context"

	"github.com/kedare/compass/internal/cache"
	"github.com/rivo/tview"
)

// This file contains action handler registry and implementations extracted from search_view.go.
//
// Architecture Overview:
// ---------------------
// This file defines the foundation for a registry-based action handler system.
// Instead of large if/else chains in the main keyboard handler, resource-specific
// actions can be registered here and dispatched dynamically.
//
// Current State:
//   - Foundation structures defined (SearchActionContext, SearchResourceHandler)
//   - Registry pattern established (searchHandlerRegistry map)
//   - Ready for handler implementations
//
// Future Implementation:
//   - Create handler functions for each resource type (e.g., handleInstanceDetails)
//   - Register handlers using RegisterHandlers() for each ResourceKind
//   - Replace if/else chains in search_view.go with GetHandler() lookups
//   - Benefits: Reduced complexity, easier testing, cleaner extension points
//
// Example usage (not yet implemented):
//   RegisterHandlers(string(search.KindComputeInstance), map[rune]SearchActionHandler{
//       'd': handleInstanceDetails,
//       's': handleInstanceSSH,
//       'b': handleBrowserOpen,
//   })

// SearchActionContext extends ActionContext with search-specific state.
type SearchActionContext struct {
	*ActionContext
	State             *SearchViewState
	UI                *SearchViewUI
	Cache             *cache.Cache
	CurrentSearchTerm string
	OnAffinityTouch   func(project, resourceType string)
}

// SearchViewUI encapsulates UI components for easier passing to handlers.
type SearchViewUI struct {
	App          *tview.Application
	Table        *tview.Table
	SearchInput  *tview.InputField
	FilterInput  *tview.InputField
	Status       *tview.TextView
	ProgressText *tview.TextView
	WarningsPane *tview.TextView
	Flex         *tview.Flex
	Ctx          context.Context
}

// SearchActionHandler is a function that handles a specific action on a search entry.
type SearchActionHandler func(ctx *SearchActionContext, entry *searchEntry) error

// SearchResourceHandler defines handlers for a specific resource type.
type SearchResourceHandler struct {
	ResourceType string
	Handlers     map[rune]SearchActionHandler
}

// Global registry for resource-specific action handlers.
var searchHandlerRegistry = make(map[string]*SearchResourceHandler)

// RegisterHandlers registers handlers for a resource type.
func RegisterHandlers(resourceType string, handlers map[rune]SearchActionHandler) {
	searchHandlerRegistry[resourceType] = &SearchResourceHandler{
		ResourceType: resourceType,
		Handlers:     handlers,
	}
}

// GetHandler retrieves the handler for a specific resource type and action key.
func GetHandler(resourceType string, key rune) SearchActionHandler {
	if handler, ok := searchHandlerRegistry[resourceType]; ok {
		if fn, ok := handler.Handlers[key]; ok {
			return fn
		}
	}
	return nil
}
