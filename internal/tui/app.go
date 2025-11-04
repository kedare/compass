package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/logger"
	"github.com/rivo/tview"
)

// Config holds the TUI configuration
type Config struct {
	Cache       *cache.Cache
	GCPClient   *gcp.Client
	Logger      *logger.Logger
	Concurrency int
}

// App is the main TUI application
type App struct {
	*tview.Application
	config      *Config
	styles      *Styles
	pageStack   *PageStack
	header      *tview.TextView
	crumbs      *tview.TextView
	statusBar   *tview.TextView
	flash       *tview.TextView
	content     *tview.Flex
	globalKeys  KeyActions
	ctx         context.Context
	cancel      context.CancelFunc
	flashMx     sync.Mutex
}

// NewApp creates a new TUI application
func NewApp(config *Config) *App {
	ctx, cancel := context.WithCancel(context.Background())

	tviewApp := tview.NewApplication()

	// Enable mouse support as per k9s style
	tviewApp.EnableMouse(true)

	app := &App{
		Application: tviewApp,
		config:      config,
		styles:      DefaultStyles(),
		globalKeys:  NewKeyActions(),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Initialize page stack
	app.pageStack = NewPageStack(app)

	// Setup global key bindings
	app.setupGlobalKeys()

	// Build UI
	app.buildUI()

	return app
}

// setupGlobalKeys configures global keyboard shortcuts
func (a *App) setupGlobalKeys() {
	// Quit
	a.globalKeys.Add(tcell.KeyCtrlC, KeyAction{
		Description: "Quit",
		Action: func(evt *tcell.EventKey) *tcell.EventKey {
			a.Stop()
			return nil
		},
		Visible: true,
	})

	// Help
	a.globalKeys.Add(tcell.KeyRune, KeyAction{
		Description: "Help",
		Action: func(evt *tcell.EventKey) *tcell.EventKey {
			if evt.Rune() == '?' {
				a.ShowHelp()
				return nil
			}
			return evt
		},
		Visible: true,
	})

	// Back/Escape
	a.globalKeys.Add(tcell.KeyEscape, KeyAction{
		Description: "Back",
		Action: func(evt *tcell.EventKey) *tcell.EventKey {
			if a.pageStack.Depth() > 1 {
				a.pageStack.Pop()
			}
			return nil
		},
		Visible: true,
	})

	// Refresh
	a.globalKeys.Add(tcell.KeyCtrlR, KeyAction{
		Description: "Refresh",
		Action: func(evt *tcell.EventKey) *tcell.EventKey {
			if comp := a.pageStack.Top(); comp != nil {
				// Stop and restart to trigger refresh
				comp.Stop()
				comp.Start(a.ctx)
				a.Flash("Refreshed", false)
			}
			return nil
		},
		Visible: true,
	})
}

// buildUI constructs the UI layout
func (a *App) buildUI() {
	// Header
	a.header = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	a.header.SetBackgroundColor(a.styles.BgColor)
	a.header.SetText("[aqua::b]compass[-::-] ")

	// Crumbs (breadcrumbs)
	a.crumbs = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	a.crumbs.SetBackgroundColor(a.styles.CrumbBg)
	a.crumbs.SetTextColor(a.styles.CrumbFg)

	// Status bar
	a.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	a.statusBar.SetBackgroundColor(a.styles.BgColor)
	a.updateStatusBar()

	// Flash messages
	a.flash = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	a.flash.SetBackgroundColor(a.styles.BgColor)

	// Main layout
	mainFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(a.header, 1, 0, false).
		AddItem(a.crumbs, 1, 0, false).
		AddItem(a.pageStack.Pages(), 0, 1, true).
		AddItem(a.statusBar, 1, 0, false).
		AddItem(a.flash, 1, 0, false)

	// Set root and capture input
	a.SetRoot(mainFlex, true)
	a.SetInputCapture(a.handleGlobalKeys)
}

// handleGlobalKeys handles global keyboard events
func (a *App) handleGlobalKeys(event *tcell.EventKey) *tcell.EventKey {
	// Try global keys first
	if event.Key() == tcell.KeyRune {
		if action, ok := a.globalKeys.Get(tcell.KeyRune); ok {
			if result := action.Action(event); result == nil {
				return nil
			}
		}
	} else {
		if action, ok := a.globalKeys.Get(event.Key()); ok {
			return action.Action(event)
		}
	}

	// Forward to active component
	if comp := a.pageStack.Top(); comp != nil {
		// Try component's actions
		if event.Key() == tcell.KeyRune {
			// For rune keys, we need to check the specific rune
			// This is a simplification - full implementation would be more complex
			return comp.HandleKey(event)
		}

		if action, ok := comp.Actions().Get(event.Key()); ok {
			return action.Action(event)
		}
		return comp.HandleKey(event)
	}

	return event
}

// Run starts the TUI application
func (a *App) Run() error {
	// Initialize default view (instance list)
	instanceView := NewInstanceView(a)

	if err := a.pageStack.Push(instanceView); err != nil {
		return fmt.Errorf("failed to initialize instance view: %w", err)
	}

	// Run the application
	return a.Application.Run()
}

// Stop stops the TUI application
func (a *App) Stop() {
	a.cancel()
	a.pageStack.Stop()
	a.Application.Stop()
}

// updateCrumbs updates the breadcrumb display
func (a *App) updateCrumbs() {
	crumbs := a.pageStack.GetCrumbs()
	if len(crumbs) == 0 {
		a.crumbs.SetText("")
		return
	}

	breadcrumb := "[gray]compass > " + strings.Join(crumbs, " > ") + "[-]"
	a.crumbs.SetText(breadcrumb)
}

// updateStatusBar updates the status bar with hints
func (a *App) updateStatusBar() {
	var hints []string

	// Get component hints
	if comp := a.pageStack.Top(); comp != nil {
		hints = comp.Actions().Hints()
	}

	// Add global hints
	hints = append(hints, "<Esc> Back", "<^C> Quit", "<?> Help")

	// Add cache info
	projects := a.config.Cache.GetProjects()
	hints = append(hints, fmt.Sprintf("[gray]%d projects[-]", len(projects)))

	statusText := strings.Join(hints, " ")
	a.QueueUpdateDraw(func() {
		a.statusBar.SetText(" " + statusText)
	})
}

// Flash displays a temporary message
func (a *App) Flash(message string, isError bool) {
	a.flashMx.Lock()
	defer a.flashMx.Unlock()

	color := "green"
	if isError {
		color = "red"
	}

	a.QueueUpdateDraw(func() {
		a.flash.SetText(fmt.Sprintf("[%s::b] %s ", color, message))
	})

	// Clear after 3 seconds
	go func() {
		select {
		case <-a.ctx.Done():
			// Context cancelled, don't clear
			return
		case <-time.After(3 * time.Second):
			a.ClearFlash()
		}
	}()
}

// ClearFlash clears the flash message
func (a *App) ClearFlash() {
	a.flashMx.Lock()
	defer a.flashMx.Unlock()

	a.QueueUpdateDraw(func() {
		a.flash.SetText("")
	})
}

// ShowHelp displays the help view
func (a *App) ShowHelp() {
	helpView := NewHelpView(a)
	if err := a.pageStack.Push(helpView); err != nil {
		a.Flash("Failed to show help", true)
	}
}

// GetConfig returns the app configuration
func (a *App) GetConfig() *Config {
	return a.config
}

// GetStyles returns the app styles
func (a *App) GetStyles() *Styles {
	return a.styles
}

// GetContext returns the app context
func (a *App) GetContext() context.Context {
	return a.ctx
}

// Suspend suspends the TUI (for SSH)
func (a *App) Suspend(f func()) {
	a.Application.Suspend(f)
}
