package tui

import (
	"context"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Component represents a TUI view component with lifecycle management
type Component interface {
	// Name returns the component name for identification
	Name() string

	// Init initializes the component (called once)
	Init(ctx context.Context) error

	// Start is called when the component becomes active
	Start(ctx context.Context)

	// Stop is called when the component becomes inactive
	Stop()

	// Primitive returns the tview primitive for rendering
	Primitive() tview.Primitive

	// Actions returns the keyboard actions for this component
	Actions() KeyActions

	// HandleKey handles keyboard events not covered by actions
	HandleKey(event *tcell.EventKey) *tcell.EventKey
}

// BaseComponent provides default implementations for Component interface
type BaseComponent struct {
	name    string
	actions KeyActions
}

// NewBaseComponent creates a new base component
func NewBaseComponent(name string) *BaseComponent {
	return &BaseComponent{
		name:    name,
		actions: NewKeyActions(),
	}
}

// Name returns the component name
func (b *BaseComponent) Name() string {
	return b.name
}

// Init provides default initialization
func (b *BaseComponent) Init(ctx context.Context) error {
	return nil
}

// Start provides default start behavior
func (b *BaseComponent) Start(ctx context.Context) {
}

// Stop provides default stop behavior
func (b *BaseComponent) Stop() {
}

// Actions returns the keyboard actions
func (b *BaseComponent) Actions() KeyActions {
	return b.actions
}

// HandleKey provides default key handling (pass-through)
func (b *BaseComponent) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	return event
}
