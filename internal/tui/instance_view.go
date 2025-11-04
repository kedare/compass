package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/kedare/compass/internal/gcp"
	"github.com/rivo/tview"
)

// InstanceView displays the list of instances
type InstanceView struct {
	*BaseComponent
	app         *App
	table       *tview.Table
	filterInput *tview.InputField
	layout      *tview.Flex
	filterMode  bool
	filterText  string
	instances   []gcp.Instance
	mx          sync.RWMutex
	refreshTicker *time.Ticker
	cancelRefresh context.CancelFunc
}

// NewInstanceView creates a new instance view
func NewInstanceView(app *App) *InstanceView {
	view := &InstanceView{
		BaseComponent: NewBaseComponent("Instances"),
		app:           app,
		instances:     make([]gcp.Instance, 0),
	}

	// Create table
	view.table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0).
		SetSeparator(tview.Borders.Vertical)

	view.table.SetBorder(true).SetTitle(" Instances ")
	view.table.SetBackgroundColor(app.styles.BgColor)
	view.table.SetBordersColor(app.styles.BorderColor)
	view.table.SetSelectedStyle(tcell.StyleDefault.
		Background(app.styles.TableSelectedBg).
		Foreground(app.styles.TableSelectedFg).
		Bold(true))

	// Set headers
	headers := []string{"Name", "Project", "Zone", "Status", "Internal IP", "External IP"}
	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(app.styles.TableHeaderFg).
			SetBackgroundColor(app.styles.TableHeaderBg).
			SetAlign(tview.AlignLeft).
			SetSelectable(false).
			SetExpansion(1)
		view.table.SetCell(0, col, cell)
	}

	// Filter input
	view.filterInput = tview.NewInputField().
		SetLabel("Filter: ").
		SetFieldWidth(0).
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEnter {
				view.filterText = view.filterInput.GetText()
				view.updateTable()
				view.toggleFilterMode()
			} else if key == tcell.KeyEscape {
				view.filterInput.SetText("")
				view.filterText = ""
				view.toggleFilterMode()
			}
		})

	// Layout
	view.layout = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(view.table, 0, 1, true)

	// Setup actions
	view.setupActions()

	return view
}

// setupActions configures keyboard actions
func (v *InstanceView) setupActions() {
	v.actions.Add(tcell.KeyEnter, KeyAction{
		Description: "SSH",
		Action: func(evt *tcell.EventKey) *tcell.EventKey {
			v.sshToSelected()
			return nil
		},
		Visible: true,
	})

	v.actions.Add(tcell.KeyCtrlR, KeyAction{
		Description: "Refresh",
		Action: func(evt *tcell.EventKey) *tcell.EventKey {
			v.loadInstances()
			v.app.Flash("Refreshing...", false)
			return nil
		},
		Visible: true,
	})
}

// Primitive returns the tview primitive
func (v *InstanceView) Primitive() tview.Primitive {
	return v.layout
}

// Start starts the view
func (v *InstanceView) Start(ctx context.Context) {
	v.BaseComponent.Start(ctx)

	// Initial display with empty message
	v.app.QueueUpdateDraw(func() {
		v.table.SetTitle(" Instances (Loading...) ")
	})

	// Load instances in background
	go v.loadInstances()

	// Start background refresh
	refreshCtx, cancel := context.WithCancel(ctx)
	v.cancelRefresh = cancel
	v.refreshTicker = time.NewTicker(30 * time.Second)

	go func() {
		for {
			select {
			case <-refreshCtx.Done():
				return
			case <-v.refreshTicker.C:
				v.loadInstances()
			}
		}
	}()
}

// Stop stops the view
func (v *InstanceView) Stop() {
	v.BaseComponent.Stop()
	if v.cancelRefresh != nil {
		v.cancelRefresh()
	}
	if v.refreshTicker != nil {
		v.refreshTicker.Stop()
	}
}

// HandleKey handles keyboard events
func (v *InstanceView) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	// If in filter mode, let filter input handle it
	if v.filterMode {
		return event
	}

	// Handle slash for filter
	if event.Key() == tcell.KeyRune && event.Rune() == '/' {
		v.toggleFilterMode()
		return nil
	}

	return event
}

// loadInstances loads instances from cache
func (v *InstanceView) loadInstances() {
	instances := make([]gcp.Instance, 0)

	// Get all projects from cache
	projects := v.app.config.Cache.GetProjects()

	// Collect all instances from cache
	for _, project := range projects {
		locations, ok := v.app.config.Cache.GetLocationsByProject(project)
		if !ok {
			continue
		}

		for _, location := range locations {
			if location.Type == "instance" || location.Type == "mig" {
				// Create instance from cache
				instances = append(instances, gcp.Instance{
					Name:    location.Name,
					Project: project,
					Zone:    location.Location, // Location field contains zone info
					Status:  "CACHED",
				})
			}
		}
	}

	v.mx.Lock()
	v.instances = instances
	v.mx.Unlock()

	v.app.QueueUpdateDraw(func() {
		v.updateTable()
	})
}

// updateTable updates the table with instance data
func (v *InstanceView) updateTable() {
	// Clear existing rows (except header)
	rowCount := v.table.GetRowCount()
	for row := rowCount - 1; row > 0; row-- {
		v.table.RemoveRow(row)
	}

	v.mx.RLock()
	defer v.mx.RUnlock()

	count := 0
	for _, instance := range v.instances {
		// Apply filter
		if v.filterText != "" {
			match := strings.Contains(strings.ToLower(instance.Name), strings.ToLower(v.filterText)) ||
				strings.Contains(strings.ToLower(instance.Project), strings.ToLower(v.filterText)) ||
				strings.Contains(strings.ToLower(instance.Zone), strings.ToLower(v.filterText))
			if !match {
				continue
			}
		}

		count++
		row := v.table.GetRowCount()

		externalIP := instance.ExternalIP
		if externalIP == "" {
			externalIP = "-"
		}

		internalIP := instance.InternalIP
		if internalIP == "" {
			internalIP = "-"
		}

		// Format status with color
		statusText := v.app.styles.FormatStatus(instance.Status)

		cells := []string{
			instance.Name,
			instance.Project,
			instance.Zone,
			statusText,
			internalIP,
			externalIP,
		}

		for col, cellText := range cells {
			cell := tview.NewTableCell(cellText).
				SetTextColor(v.app.styles.FgColor).
				SetBackgroundColor(v.app.styles.BgColor).
				SetAlign(tview.AlignLeft).
				SetReference(instance).
				SetExpansion(1)
			v.table.SetCell(row, col, cell)
		}
	}

	// Update title with count
	v.table.SetTitle(fmt.Sprintf(" Instances (%d) ", count))
}

// toggleFilterMode toggles filter input mode
func (v *InstanceView) toggleFilterMode() {
	v.filterMode = !v.filterMode

	if v.filterMode {
		// Show filter input
		v.layout.Clear()
		v.layout.AddItem(v.filterInput, 1, 0, true).
			AddItem(v.table, 0, 1, false)
		v.app.SetFocus(v.filterInput)
	} else {
		// Hide filter input
		v.layout.Clear()
		v.layout.AddItem(v.table, 0, 1, true)
		v.app.SetFocus(v.table)
		v.updateTable()
	}
}

// sshToSelected initiates SSH to the selected instance
func (v *InstanceView) sshToSelected() {
	row, _ := v.table.GetSelection()
	if row <= 0 || row >= v.table.GetRowCount() {
		v.app.Flash("No instance selected", true)
		return
	}

	cell := v.table.GetCell(row, 0)
	if cell == nil {
		v.app.Flash("Invalid selection", true)
		return
	}

	ref := cell.GetReference()
	if ref == nil {
		v.app.Flash("No instance reference", true)
		return
	}

	instance, ok := ref.(gcp.Instance)
	if !ok {
		v.app.Flash("Invalid instance reference", true)
		return
	}

	// Suspend TUI and run SSH
	v.app.Suspend(func() {
		// Build gcloud SSH command
		args := []string{
			"compute",
			"ssh",
			instance.Name,
			"--project=" + instance.Project,
			"--zone=" + instance.Zone,
		}

		// Check if IAP is needed (assume yes if no external IP)
		if instance.ExternalIP == "" {
			args = append(args, "--tunnel-through-iap")
		}

		// Execute gcloud command
		cmd := exec.Command("gcloud", args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		fmt.Printf("Connecting to %s...\n", instance.Name)
		if err := cmd.Run(); err != nil {
			fmt.Printf("\nSSH connection failed: %v\n", err)
			fmt.Println("Press Enter to return...")
			fmt.Scanln()
		}
	})

	// After resume, flash a message
	v.app.Flash(fmt.Sprintf("Disconnected from %s", instance.Name), false)
}
