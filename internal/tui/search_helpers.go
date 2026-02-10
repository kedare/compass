package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/rivo/tview"
)

// This file contains pure utility functions extracted from search_view.go
// for improved testability and reusability.

// ErrorHandler provides consistent error handling for the search view.
type ErrorHandler struct {
	Status          *tview.TextView
	App             *tview.Application
	OnRestoreStatus func()
}

// NewErrorHandler creates a new error handler.
func NewErrorHandler(status *tview.TextView, app *tview.Application, onRestore func()) *ErrorHandler {
	return &ErrorHandler{
		Status:          status,
		App:             app,
		OnRestoreStatus: onRestore,
	}
}

// ShowError displays an error message and restores status after a delay.
func (eh *ErrorHandler) ShowError(format string, args ...interface{}) {
	eh.Status.SetText(fmt.Sprintf(" [red]"+format+"[-]", args...))
	if eh.OnRestoreStatus != nil {
		time.AfterFunc(3*time.Second, func() {
			eh.App.QueueUpdateDraw(func() {
				eh.OnRestoreStatus()
			})
		})
	}
}

// ShowSuccess displays a success message and restores status after a delay.
func (eh *ErrorHandler) ShowSuccess(format string, args ...interface{}) {
	eh.Status.SetText(fmt.Sprintf(" [green]"+format+"[-]", args...))
	if eh.OnRestoreStatus != nil {
		time.AfterFunc(2*time.Second, func() {
			eh.App.QueueUpdateDraw(func() {
				eh.OnRestoreStatus()
			})
		})
	}
}

// ShowLoading displays a loading message.
func (eh *ErrorHandler) ShowLoading(format string, args ...interface{}) {
	eh.Status.SetText(fmt.Sprintf(" [yellow]"+format+"[-]", args...))
}

// getTypeColor returns the display color for a given resource type.
// This provides consistent color coding across the search interface.
func getTypeColor(resourceType string) string {
	switch {
	case strings.HasPrefix(resourceType, "compute."):
		return "blue"
	case strings.HasPrefix(resourceType, "storage."):
		return "green"
	case strings.HasPrefix(resourceType, "container."):
		return "cyan"
	case strings.HasPrefix(resourceType, "sqladmin."):
		return "magenta"
	case strings.HasPrefix(resourceType, "run."):
		return "yellow"
	case strings.HasPrefix(resourceType, "secretmanager."):
		return "red"
	case strings.HasPrefix(resourceType, "networkmanagement."):
		return "purple"
	default:
		return "white"
	}
}

// highlightMatch highlights the first occurrence of the search term in the text.
// Returns the text with tview color markup for highlighting.
func highlightMatch(text, term string) string {
	if term == "" || text == "" {
		return text
	}

	termLower := strings.ToLower(strings.TrimSpace(term))
	if termLower == "" {
		return text
	}

	idx := strings.Index(strings.ToLower(text), termLower)
	if idx < 0 {
		return text
	}

	before := text[:idx]
	matched := text[idx : idx+len(termLower)]
	after := text[idx+len(termLower):]

	return before + "[yellow::b]" + matched + "[-:-:-]" + after
}
