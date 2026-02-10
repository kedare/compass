package tui

import (
	"fmt"
	"sort"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// This file contains modal dialog management extracted from search_view.go.

// ModalManager handles modal lifecycle for the search view.
type ModalManager struct {
	App       *tview.Application
	MainFlex  *tview.Flex
	MainFocus tview.Primitive
	ModalOpen *bool
	OnClose   func()
}

// NewModalManager creates a new modal manager.
func NewModalManager(app *tview.Application, mainFlex *tview.Flex, mainFocus tview.Primitive, modalOpen *bool, onClose func()) *ModalManager {
	return &ModalManager{
		App:       app,
		MainFlex:  mainFlex,
		MainFocus: mainFocus,
		ModalOpen: modalOpen,
		OnClose:   onClose,
	}
}

// showSearchResultDetail displays details for a search result
func showSearchResultDetail(app *tview.Application, table *tview.Table, mainFlex *tview.Flex, entry *searchEntry, modalOpen *bool, status *tview.TextView, currentFilter string, onRestoreStatus func()) {
	var detailText string

	detailText += fmt.Sprintf("[yellow::b]%s[-:-:-]\n\n", entry.Type)
	detailText += fmt.Sprintf("[white::b]Name:[-:-:-]     %s\n", entry.Name)
	detailText += fmt.Sprintf("[white::b]Project:[-:-:-]  %s\n", entry.Project)
	detailText += fmt.Sprintf("[white::b]Location:[-:-:-] %s\n", entry.Location)

	if len(entry.Details) > 0 {
		detailText += "\n[yellow::b]Details:[-:-:-]\n"

		// Sort keys for consistent display
		keys := make([]string, 0, len(entry.Details))
		for k := range entry.Details {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			v := entry.Details[k]
			if v != "" {
				detailText += fmt.Sprintf("  [white::b]%s:[-:-:-] %s\n", k, v)
			}
		}
	}

	detailText += "\n[darkgray]Press Esc to close[-]"

	detailView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(detailText).
		SetScrollable(true).
		SetWordWrap(true)
	detailView.SetBorder(true).SetTitle(fmt.Sprintf(" %s ", entry.Name))

	// Create status bar for detail view
	detailStatus := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Esc[-] back  [yellow]↑/↓[-] scroll")

	// Create fullscreen detail layout
	detailFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(detailView, 0, 1, true).
		AddItem(detailStatus, 1, 0, false)

	// Set up input handler
	detailView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			*modalOpen = false
			app.SetRoot(mainFlex, true)
			app.SetFocus(table)
			if onRestoreStatus != nil {
				onRestoreStatus()
			}
			return nil
		}
		return event
	})

	*modalOpen = true
	app.SetRoot(detailFlex, true).SetFocus(detailView)
}

// showInstanceDetailModal displays instance details fetched from GCP
func showInstanceDetailModal(app *tview.Application, table *tview.Table, mainFlex *tview.Flex, name string, details string, modalOpen *bool, status *tview.TextView, currentFilter string, onRestoreStatus func()) {
	detailView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(details).
		SetScrollable(true).
		SetWordWrap(true)
	detailView.SetBorder(true).SetTitle(fmt.Sprintf(" %s ", name))

	// Create status bar for detail view
	detailStatus := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Esc[-] back  [yellow]↑/↓[-] scroll")

	// Create fullscreen detail layout
	detailFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(detailView, 0, 1, true).
		AddItem(detailStatus, 1, 0, false)

	// Set up input handler
	detailView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			*modalOpen = false
			app.SetRoot(mainFlex, true)
			app.SetFocus(table)
			if onRestoreStatus != nil {
				onRestoreStatus()
			}
			return nil
		}
		return event
	})

	*modalOpen = true
	app.SetRoot(detailFlex, true).SetFocus(detailView)
}

// showSearchHelp displays help for the search view
func showSearchHelp(app *tview.Application, table *tview.Table, mainFlex *tview.Flex, modalOpen *bool, currentFilter string, status *tview.TextView, onRestoreStatus func()) {
	helpText := `[yellow::b]Search View - Keyboard Shortcuts[-:-:-]

[yellow]Search[-]
  [white]Enter[-]         Start search / Focus search input
  [white]↑/↓[-]           Navigate search history (in search box)
  [white]Tab[-]           Toggle fuzzy matching
  [white]Esc[-]           Cancel search (if running) / Clear filter / Go back

[yellow]Navigation[-]
  [white]↑/k[-]           Move selection up
  [white]↓/j[-]           Move selection down
  [white]Home/g[-]        Jump to first result
  [white]End/G[-]         Jump to last result

[yellow]Actions[-]
  [white]s[-]             SSH to selected instance
  [white]d[-]             Show details for selected result
  [white]b[-]             Open in Cloud Console (browser)
  [white]o[-]             Open in browser (for buckets)
  [white]/[-]             Filter displayed results

[yellow]Search Features[-]
  • Results appear progressively as they're found
  • High-priority projects (based on past searches) are searched first
  • Progress shows current project being searched
  • Result counts by resource type shown in title
  • Search history: Use ↑/↓ to recall previous searches
  • Search can be cancelled at any time with Esc
  • Cancelled searches keep existing results
  • Filter (/) narrows displayed results without new search
  • Filter supports: spaces (AND), | (OR), - (NOT)
    Example: "compute.instance prod" = instances in prod projects
    Example: "web|api -dev" = web or api resources, excluding dev
  • Tab toggles fuzzy mode (matches characters in order, e.g. "prd" matches "production")
  • Context-aware actions based on resource type

[darkgray]Press Esc or ? to close this help[-]`

	helpView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(helpText).
		SetScrollable(true)
	helpView.SetBorder(true).
		SetTitle(" Search Help ").
		SetTitleAlign(tview.AlignCenter)

	// Create status bar for help view
	helpStatus := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Esc[-] back  [yellow]?[-] close help")

	// Create fullscreen help layout
	helpFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(helpView, 0, 1, true).
		AddItem(helpStatus, 1, 0, false)

	// Set up input handler
	helpView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || (event.Key() == tcell.KeyRune && event.Rune() == '?') {
			*modalOpen = false
			app.SetRoot(mainFlex, true)
			app.SetFocus(table)
			if onRestoreStatus != nil {
				onRestoreStatus()
			}
			return nil
		}
		return event
	})

	*modalOpen = true
	app.SetRoot(helpFlex, true).SetFocus(helpView)
}
