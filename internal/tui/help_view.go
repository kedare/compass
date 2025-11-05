package tui

import (
	"context"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// HelpView displays keyboard shortcuts
type HelpView struct {
	*BaseComponent
	app      *App
	textView *tview.TextView
}

// NewHelpView creates a new help view
func NewHelpView(app *App) *HelpView {
	view := &HelpView{
		BaseComponent: NewBaseComponent("Help"),
		app:           app,
	}

	// Create text view
	view.textView = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true)

	view.textView.SetBorder(true).
		SetTitle(" Keyboard Shortcuts ").
		SetBackgroundColor(app.styles.BgColor)

	// Setup actions
	view.setupActions()

	return view
}

// setupActions configures keyboard actions
func (v *HelpView) setupActions() {
	v.actions.Add(tcell.KeyEscape, KeyAction{
		Description: "Close",
		Action: func(evt *tcell.EventKey) *tcell.EventKey {
			// Pop will be handled by global escape handler
			return evt
		},
		Visible: true,
	})
}

// Primitive returns the tview primitive
func (v *HelpView) Primitive() tview.Primitive {
	return v.textView
}

// Start starts the view
func (v *HelpView) Start(ctx context.Context) {
	v.BaseComponent.Start(ctx)
	v.renderHelp()
}

// renderHelp renders the help content
func (v *HelpView) renderHelp() {
	help := `[aqua::b]Compass TUI - Keyboard Shortcuts[-::-]

[yellow]Global Shortcuts[-]
  [white]?[-]         Show this help
  [white]Ctrl+C[-]    Quit application
  [white]Ctrl+R[-]    Refresh current view
  [white]Esc[-]       Go back / Close dialog

[yellow]Instance List[-]
  [white]↑/↓[-]       Navigate list
  [white]j/k[-]       Navigate list (vim style)
  [white]Enter[-]     SSH to selected instance
  [white]/[-]         Filter instances
  [white]Esc[-]       Clear filter / Go back

[yellow]Filter Mode[-]
  [white]Type[-]      Filter text
  [white]Enter[-]     Apply filter
  [white]Esc[-]       Cancel filter

[yellow]SSH Session[-]
  When you press Enter on an instance, the TUI will suspend
  and open an SSH session. When you exit SSH, you'll return
  to the TUI.

[gray]Press Esc to close this help screen[-]
`

	v.textView.SetText(help)
}
