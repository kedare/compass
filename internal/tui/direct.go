package tui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/gcp"
	"github.com/rivo/tview"
)

// instanceData holds information about an instance for filtering
type instanceData struct {
	Name     string
	Project  string
	Zone     string
	Status   string
	SearchText string // Combined text for fuzzy search
}

// RunDirect runs a minimal TUI without the full app structure
func RunDirect(c *cache.Cache, gcpClient *gcp.Client) error {
	app := tview.NewApplication()

	// Store all instances for filtering
	var allInstances []instanceData

	// Load instances from cache
	projects := c.GetProjects()
	for _, project := range projects {
		locations, ok := c.GetLocationsByProject(project)
		if !ok {
			continue
		}

		for _, location := range locations {
			if location.Type == "instance" || location.Type == "mig" {
				inst := instanceData{
					Name:    location.Name,
					Project: project,
					Zone:    location.Location,
					Status:  "[green]CACHED[-]",
				}
				// Create combined search text for fuzzy matching
				inst.SearchText = strings.ToLower(inst.Name + " " + inst.Project + " " + inst.Zone)
				allInstances = append(allInstances, inst)
			}
		}
	}

	// Create a simple table
	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)

	table.SetBorder(true).SetTitle(fmt.Sprintf(" Compass Instances (%d) ", len(allInstances)))

	// Add header
	headers := []string{"Name", "Project", "Zone", "Status"}
	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorBlack).
			SetBackgroundColor(tcell.ColorDarkCyan).
			SetSelectable(false).
			SetExpansion(1)
		table.SetCell(0, col, cell)
	}

	// Filter input field (hidden by default)
	filterInput := tview.NewInputField().
		SetLabel(" Filter: ").
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetLabelColor(tcell.ColorYellow)

	var filterMode bool
	var currentFilter string

	// Function to update table with current filter
	updateTable := func(filter string) {
		// Clear existing rows (keep header)
		for row := table.GetRowCount() - 1; row > 0; row-- {
			table.RemoveRow(row)
		}

		currentRow := 1
		matchCount := 0

		for _, inst := range allInstances {
			// Apply filter if active
			if filter != "" {
				filterLower := strings.ToLower(filter)
				nameLower := strings.ToLower(inst.Name)
				projectLower := strings.ToLower(inst.Project)
				zoneLower := strings.ToLower(inst.Zone)

				// Use substring matching (contains) for exact matches
				nameMatch := strings.Contains(nameLower, filterLower)
				projectMatch := strings.Contains(projectLower, filterLower)
				zoneMatch := strings.Contains(zoneLower, filterLower)

				// Match if any field contains the filter
				if !nameMatch && !projectMatch && !zoneMatch {
					continue
				}
			}

			table.SetCell(currentRow, 0, tview.NewTableCell(inst.Name).SetExpansion(1))
			table.SetCell(currentRow, 1, tview.NewTableCell(inst.Project).SetExpansion(1))
			table.SetCell(currentRow, 2, tview.NewTableCell(inst.Zone).SetExpansion(1))
			table.SetCell(currentRow, 3, tview.NewTableCell(inst.Status).SetExpansion(1))
			currentRow++
			matchCount++
		}

		// Update title with match count
		if filter != "" {
			table.SetTitle(fmt.Sprintf(" Instances (%d/%d matched) ", matchCount, len(allInstances)))
		} else {
			table.SetTitle(fmt.Sprintf(" Compass Instances (%d) ", len(allInstances)))
		}

		// Select first data row if available
		if matchCount > 0 {
			table.Select(1, 0)
		}
	}

	// Initial table population
	updateTable("")

	// Status bar
	status := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]↑/↓[-] navigate  [yellow]/[-] filter  [yellow]Ctrl+C[-] quit  [yellow]?[-] help")

	// Layout
	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(table, 0, 1, true).
		AddItem(status, 1, 0, false)

	// Setup filter input handlers
	filterInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			// Apply filter and exit filter mode
			currentFilter = filterInput.GetText()
			updateTable(currentFilter)
			filterMode = false
			flex.RemoveItem(filterInput)
			app.SetFocus(table)
			if currentFilter != "" {
				status.SetText(fmt.Sprintf(" [green]Filter active: '%s'[-]  [yellow]Esc[-] clear  [yellow]/[-] edit", currentFilter))
			} else {
				status.SetText(" [yellow]↑/↓[-] navigate  [yellow]/[-] filter  [yellow]Ctrl+C[-] quit  [yellow]?[-] help")
			}
		} else if key == tcell.KeyEscape {
			// Cancel filter mode without applying
			filterInput.SetText(currentFilter)
			filterMode = false
			flex.RemoveItem(filterInput)
			app.SetFocus(table)
			if currentFilter != "" {
				status.SetText(fmt.Sprintf(" [green]Filter active: '%s'[-]  [yellow]Esc[-] clear  [yellow]/[-] edit", currentFilter))
			} else {
				status.SetText(" [yellow]↑/↓[-] navigate  [yellow]/[-] filter  [yellow]Ctrl+C[-] quit  [yellow]?[-] help")
			}
		}
	})

	// Update table as user types (live fuzzy search)
	filterInput.SetChangedFunc(func(text string) {
		updateTable(text)
	})

	// Setup keyboard
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// If in filter mode, let the input field handle it
		if filterMode {
			return event
		}

		switch event.Key() {
		case tcell.KeyCtrlC:
			app.Stop()
			return nil
		case tcell.KeyEscape:
			// Clear filter if active
			if currentFilter != "" {
				currentFilter = ""
				filterInput.SetText("")
				updateTable("")
				status.SetText(" [yellow]↑/↓[-] navigate  [yellow]/[-] filter  [yellow]Ctrl+C[-] quit  [yellow]?[-] help")
				return nil
			}
		case tcell.KeyRune:
			if event.Rune() == '/' {
				// Enter filter mode
				filterMode = true
				filterInput.SetText(currentFilter)
				flex.Clear()
				flex.AddItem(filterInput, 1, 0, true)
				flex.AddItem(table, 0, 1, false)
				flex.AddItem(status, 1, 0, false)
				app.SetFocus(filterInput)
				status.SetText(" [yellow]Type to filter, Enter to apply, Esc to cancel[-]")
				return nil
			}
			if event.Rune() == '?' {
				// Show help
				status.SetText(" [green]↑/↓[-] navigate  [green]/[-] fuzzy filter  [green]Esc[-] clear filter  [green]q/Ctrl+C[-] quit")
				return nil
			}
			if event.Rune() == 'q' {
				app.Stop()
				return nil
			}
		}
		return event
	})

	// Run
	if err := app.SetRoot(flex, true).EnableMouse(true).Run(); err != nil {
		return fmt.Errorf("tview run error: %w", err)
	}

	return nil
}
