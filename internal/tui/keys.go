package tui

import (
	"sync"

	"github.com/gdamore/tcell/v2"
)

// ActionHandler is a function that handles a key action
type ActionHandler func(evt *tcell.EventKey) *tcell.EventKey

// KeyAction represents a keyboard action
type KeyAction struct {
	Description string
	Action      ActionHandler
	Visible     bool
	Dangerous   bool
}

// KeyActions manages keyboard bindings
type KeyActions struct {
	actions map[tcell.Key]KeyAction
	mx      sync.RWMutex
}

// NewKeyActions creates a new key actions manager
func NewKeyActions() KeyActions {
	return KeyActions{
		actions: make(map[tcell.Key]KeyAction),
	}
}

// Add adds a key action
func (k *KeyActions) Add(key tcell.Key, action KeyAction) {
	k.mx.Lock()
	defer k.mx.Unlock()
	k.actions[key] = action
}

// Delete removes a key action
func (k *KeyActions) Delete(key tcell.Key) {
	k.mx.Lock()
	defer k.mx.Unlock()
	delete(k.actions, key)
}

// Get retrieves a key action
func (k *KeyActions) Get(key tcell.Key) (KeyAction, bool) {
	k.mx.RLock()
	defer k.mx.RUnlock()
	action, ok := k.actions[key]
	return action, ok
}

// Clear removes all actions
func (k *KeyActions) Clear() {
	k.mx.Lock()
	defer k.mx.Unlock()
	k.actions = make(map[tcell.Key]KeyAction)
}

// List returns all key bindings
func (k *KeyActions) List() map[tcell.Key]KeyAction {
	k.mx.RLock()
	defer k.mx.RUnlock()
	result := make(map[tcell.Key]KeyAction, len(k.actions))
	for key, action := range k.actions {
		result[key] = action
	}
	return result
}

// Hints returns visible action hints for status bar
func (k *KeyActions) Hints() []string {
	k.mx.RLock()
	defer k.mx.RUnlock()

	var hints []string
	// Common keys in order
	keyOrder := []struct {
		key  tcell.Key
		name string
	}{
		{tcell.KeyEnter, "Enter"},
		{tcell.KeyEscape, "Esc"},
		{tcell.KeyCtrlR, "^R"},
		{tcell.KeyCtrlC, "^C"},
		{tcell.KeyRune, "/"}, // Special handling needed
	}

	for _, ko := range keyOrder {
		if action, ok := k.actions[ko.key]; ok && action.Visible {
			hints = append(hints, "<"+ko.name+"> "+action.Description)
		}
	}

	return hints
}
