package tui

import (
	"github.com/gdamore/tcell/v2"
)

// ViewMode represents the current state of the TUI
type ViewMode int

const (
	ModeNormal ViewMode = iota
	ModeFilter
	ModeModal
	ModeHelp
)

// KeyBinding defines a keyboard shortcut
type KeyBinding struct {
	Key         tcell.Key   // For special keys (Enter, Escape, etc.)
	Rune        rune        // For character keys ('s', 'R', etc.)
	Description string      // For help display
	Modes       []ViewMode  // Which modes this binding is active in (empty = all modes)
	Handler     func() bool // Returns true if event was handled
}

// KeyBindings manages all keyboard shortcuts
type KeyBindings struct {
	bindings []KeyBinding
	mode     ViewMode
}

// NewKeyBindings creates a new keybinding manager
func NewKeyBindings() *KeyBindings {
	return &KeyBindings{
		bindings: make([]KeyBinding, 0),
		mode:     ModeNormal,
	}
}

// SetMode changes the current input mode
func (kb *KeyBindings) SetMode(mode ViewMode) {
	kb.mode = mode
}

// GetMode returns the current input mode
func (kb *KeyBindings) GetMode() ViewMode {
	return kb.mode
}

// Register adds a new keybinding
func (kb *KeyBindings) Register(binding KeyBinding) {
	kb.bindings = append(kb.bindings, binding)
}

// RegisterKey is a convenience method for registering a character key
func (kb *KeyBindings) RegisterKey(r rune, description string, modes []ViewMode, handler func() bool) {
	kb.Register(KeyBinding{
		Key:         tcell.KeyRune,
		Rune:        r,
		Description: description,
		Modes:       modes,
		Handler:     handler,
	})
}

// RegisterSpecial is a convenience method for registering a special key
func (kb *KeyBindings) RegisterSpecial(key tcell.Key, description string, modes []ViewMode, handler func() bool) {
	kb.Register(KeyBinding{
		Key:         key,
		Description: description,
		Modes:       modes,
		Handler:     handler,
	})
}

// Handle processes a key event and returns true if it was handled
func (kb *KeyBindings) Handle(event *tcell.EventKey) bool {
	for _, binding := range kb.bindings {
		// Check if binding applies to current mode
		if len(binding.Modes) > 0 {
			modeMatch := false
			for _, m := range binding.Modes {
				if m == kb.mode {
					modeMatch = true
					break
				}
			}
			if !modeMatch {
				continue
			}
		}

		// Check if key matches
		if event.Key() == tcell.KeyRune {
			if binding.Key == tcell.KeyRune && binding.Rune == event.Rune() {
				if binding.Handler() {
					return true
				}
			}
		} else {
			if binding.Key == event.Key() {
				if binding.Handler() {
					return true
				}
			}
		}
	}
	return false
}

// GetHelpText returns formatted help text for all bindings
func (kb *KeyBindings) GetHelpText() []HelpEntry {
	entries := make([]HelpEntry, 0, len(kb.bindings))
	for _, b := range kb.bindings {
		if b.Description == "" {
			continue
		}
		entries = append(entries, HelpEntry{
			Key:         kb.formatKey(b),
			Description: b.Description,
		})
	}
	return entries
}

// HelpEntry represents a single help item
type HelpEntry struct {
	Key         string
	Description string
}

func (kb *KeyBindings) formatKey(b KeyBinding) string {
	if b.Key == tcell.KeyRune {
		if b.Rune >= 'A' && b.Rune <= 'Z' {
			return "Shift+" + string(b.Rune)
		}
		return string(b.Rune)
	}
	switch b.Key {
	case tcell.KeyEnter:
		return "Enter"
	case tcell.KeyEscape:
		return "Esc"
	case tcell.KeyTab:
		return "Tab"
	case tcell.KeyCtrlC:
		return "Ctrl+C"
	case tcell.KeyCtrlR:
		return "Ctrl+R"
	case tcell.KeyUp:
		return "↑"
	case tcell.KeyDown:
		return "↓"
	default:
		return "?"
	}
}
