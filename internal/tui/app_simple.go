package tui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// RunSimple runs a minimal TUI for testing
func (a *App) RunSimple() error {
	app := tview.NewApplication()

	// Create a simple table
	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false)

	table.SetBorder(true).SetTitle(" Test Instances ")

	// Add header
	headers := []string{"Name", "Project", "Zone", "Status"}
	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorWhite).
			SetBackgroundColor(tcell.ColorBlue).
			SetSelectable(false)
		table.SetCell(0, col, cell)
	}

	// Add test data
	testData := [][]string{
		{"instance-1", "my-project", "us-central1-a", "RUNNING"},
		{"instance-2", "my-project", "us-central1-b", "STOPPED"},
		{"instance-3", "other-project", "us-east1-a", "RUNNING"},
	}

	for row, data := range testData {
		for col, value := range data {
			cell := tview.NewTableCell(value).
				SetTextColor(tcell.ColorWhite).
				SetBackgroundColor(tcell.ColorBlack)
			table.SetCell(row+1, col, cell)
		}
	}

	// Load actual instances from cache
	projects := a.config.Cache.GetProjects()
	currentRow := len(testData) + 1

	for _, project := range projects {
		locations, ok := a.config.Cache.GetLocationsByProject(project)
		if !ok {
			continue
		}

		for _, location := range locations {
			if location.Type == "instance" || location.Type == "mig" {
				table.SetCell(currentRow, 0, tview.NewTableCell(location.Name))
				table.SetCell(currentRow, 1, tview.NewTableCell(project))
				table.SetCell(currentRow, 2, tview.NewTableCell(location.Location))
				table.SetCell(currentRow, 3, tview.NewTableCell("CACHED"))
				currentRow++
			}
		}
	}

	// Update title with count
	table.SetTitle(fmt.Sprintf(" Instances (%d) ", currentRow-1))

	// Status bar
	status := tview.NewTextView().
		SetDynamicColors(true).
		SetText("[yellow]Press Ctrl+C to quit, ? for help[-]")

	// Layout
	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(table, 0, 1, true).
		AddItem(status, 1, 0, false)

	// Setup keyboard
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC {
			app.Stop()
			return nil
		}
		if event.Key() == tcell.KeyRune && event.Rune() == '?' {
			// Show help
			status.SetText("[green]Help: ↑/↓ navigate, Ctrl+C quit[-]")
			return nil
		}
		return event
	})

	// Run
	return app.SetRoot(flex, true).EnableMouse(true).Run()
}
