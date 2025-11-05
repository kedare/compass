package tui

import (
	"context"
	"sync"

	"github.com/rivo/tview"
)

// PageStack manages a stack of components for navigation
type PageStack struct {
	pages  *tview.Pages
	stack  []Component
	app    *App
	mx     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

// NewPageStack creates a new page stack
func NewPageStack(app *App) *PageStack {
	ctx, cancel := context.WithCancel(context.Background())
	return &PageStack{
		pages:  tview.NewPages(),
		stack:  make([]Component, 0),
		app:    app,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Pages returns the underlying tview.Pages
func (ps *PageStack) Pages() *tview.Pages {
	return ps.pages
}

// Push adds a new component to the stack
func (ps *PageStack) Push(component Component) error {
	ps.mx.Lock()
	defer ps.mx.Unlock()

	// Initialize if needed
	if err := component.Init(ps.ctx); err != nil {
		return err
	}

	// Stop current component if exists
	if len(ps.stack) > 0 {
		ps.stack[len(ps.stack)-1].Stop()
	}

	// Add to stack
	ps.stack = append(ps.stack, component)

	// Add to pages
	ps.pages.AddPage(component.Name(), component.Primitive(), true, true)
	ps.pages.SwitchToPage(component.Name())

	// Start the new component
	component.Start(ps.ctx)

	// Update crumbs
	ps.app.updateCrumbs()

	return nil
}

// Pop removes the top component from the stack
func (ps *PageStack) Pop() Component {
	ps.mx.Lock()
	defer ps.mx.Unlock()

	if len(ps.stack) == 0 {
		return nil
	}

	// Get and remove last component
	component := ps.stack[len(ps.stack)-1]
	ps.stack = ps.stack[:len(ps.stack)-1]

	// Stop it
	component.Stop()

	// Remove from pages
	ps.pages.RemovePage(component.Name())

	// Show previous component if exists
	if len(ps.stack) > 0 {
		prev := ps.stack[len(ps.stack)-1]
		ps.pages.SwitchToPage(prev.Name())
		prev.Start(ps.ctx)
	}

	// Update crumbs
	ps.app.updateCrumbs()

	return component
}

// Top returns the top component without removing it
func (ps *PageStack) Top() Component {
	ps.mx.RLock()
	defer ps.mx.RUnlock()

	if len(ps.stack) == 0 {
		return nil
	}
	return ps.stack[len(ps.stack)-1]
}

// Depth returns the current stack depth
func (ps *PageStack) Depth() int {
	ps.mx.RLock()
	defer ps.mx.RUnlock()
	return len(ps.stack)
}

// Clear removes all components from the stack
func (ps *PageStack) Clear() {
	ps.mx.Lock()
	defer ps.mx.Unlock()

	// Stop all components
	for _, component := range ps.stack {
		component.Stop()
	}

	// Clear pages
	for _, component := range ps.stack {
		ps.pages.RemovePage(component.Name())
	}

	ps.stack = make([]Component, 0)
	ps.app.updateCrumbs()
}

// GetCrumbs returns breadcrumb path
func (ps *PageStack) GetCrumbs() []string {
	ps.mx.RLock()
	defer ps.mx.RUnlock()

	crumbs := make([]string, len(ps.stack))
	for i, component := range ps.stack {
		crumbs[i] = component.Name()
	}
	return crumbs
}

// Stop stops the page stack and all components
func (ps *PageStack) Stop() {
	ps.cancel()
	ps.Clear()
}
