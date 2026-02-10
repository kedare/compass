package tui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/kedare/compass/internal/cache"
	"github.com/rivo/tview"
)

// ProjectSelectorResult represents the result of project selection
type ProjectSelectorResult struct {
	Project   string
	Cancelled bool
}

// ShowProjectSelector displays a modal to select a project from the cache
// It shows projects sorted by usage (most recently used first)
// The modal supports filtering with '/' key like other tables in the TUI
func ShowProjectSelector(app *tview.Application, c *cache.Cache, title string, onSelect func(result ProjectSelectorResult)) {
	// Get projects sorted by usage
	projects := c.GetProjectsByUsage()

	if len(projects) == 0 {
		// No projects in cache, show error modal
		// Clear any app-level input capture first
		app.SetInputCapture(nil)
		modal := tview.NewModal().
			SetText("No projects found in cache.\n\nRun 'compass gcp projects import' to import projects first.").
			AddButtons([]string{"OK"}).
			SetDoneFunc(func(buttonIndex int, buttonLabel string) {
				onSelect(ProjectSelectorResult{Cancelled: true})
			})
		app.SetRoot(modal, true).SetFocus(modal)
		return
	}

	var filterMode bool
	var currentFilter string

	// Create the table
	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)

	table.SetBorder(true).SetTitle(title)

	// Add header
	headerCell := tview.NewTableCell("Project").
		SetTextColor(tcell.ColorBlack).
		SetBackgroundColor(tcell.ColorDarkCyan).
		SetSelectable(false).
		SetExpansion(1)
	table.SetCell(0, 0, headerCell)

	// Filter input
	filterInput := tview.NewInputField().
		SetLabel(" Filter: ").
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetLabelColor(tcell.ColorYellow)

	// Function to update table with filtered projects
	updateTable := func(filter string) {
		// Clear existing rows (keep header)
		for row := table.GetRowCount() - 1; row > 0; row-- {
			table.RemoveRow(row)
		}

		// Filter and add projects
		expr := parseFilter(filter)
		row := 1
		for _, project := range projects {
			if expr.matches(project) {
				table.SetCell(row, 0, tview.NewTableCell(project).SetExpansion(1))
				row++
			}
		}

		// Update title with count
		filteredCount := row - 1
		if filter != "" {
			table.SetTitle(fmt.Sprintf("%s[%d/%d] ", title, filteredCount, len(projects)))
		} else {
			table.SetTitle(fmt.Sprintf("%s[%d] ", title, len(projects)))
		}

		// Select first row if available
		if filteredCount > 0 {
			table.Select(1, 0)
		}
	}

	// Initial table population
	updateTable("")

	// Status bar
	status := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Enter[-] select  [yellow]/[-] filter  [yellow]Esc[-] cancel")

	// Main layout
	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(table, 0, 1, true).
		AddItem(status, 1, 0, false)

	// Create centered modal container
	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(flex, 20, 0, true).
			AddItem(nil, 0, 1, false), 60, 0, true).
		AddItem(nil, 0, 1, false)

	// Setup filter input handlers
	filterInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			// Apply filter and exit filter mode
			currentFilter = filterInput.GetText()
			updateTable(currentFilter)
			filterMode = false
			flex.Clear()
			flex.AddItem(table, 0, 1, true)
			flex.AddItem(status, 1, 0, false)
			app.SetFocus(table)
			if currentFilter != "" {
				status.SetText(fmt.Sprintf(" [green]Filter: %s[-]  [yellow]Enter[-] select  [yellow]/[-] edit  [yellow]Esc[-] clear/cancel", currentFilter))
			} else {
				status.SetText(" [yellow]Enter[-] select  [yellow]/[-] filter  [yellow]Esc[-] cancel")
			}
		case tcell.KeyEscape:
			// Cancel filter mode without applying
			filterInput.SetText(currentFilter)
			filterMode = false
			flex.Clear()
			flex.AddItem(table, 0, 1, true)
			flex.AddItem(status, 1, 0, false)
			app.SetFocus(table)
			if currentFilter != "" {
				status.SetText(fmt.Sprintf(" [green]Filter: %s[-]  [yellow]Enter[-] select  [yellow]/[-] edit  [yellow]Esc[-] clear/cancel", currentFilter))
			} else {
				status.SetText(" [yellow]Enter[-] select  [yellow]/[-] filter  [yellow]Esc[-] cancel")
			}
		}
	})

	// Live filter as user types
	filterInput.SetChangedFunc(func(text string) {
		updateTable(text)
	})

	// Handle selection on Enter
	table.SetSelectedFunc(func(row, column int) {
		if row > 0 {
			cell := table.GetCell(row, 0)
			if cell != nil {
				onSelect(ProjectSelectorResult{Project: cell.Text})
			}
		}
	})

	// Setup keyboard handling for table
	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if filterMode {
			return event
		}

		switch event.Key() {
		case tcell.KeyEscape:
			if currentFilter != "" {
				// Clear filter first
				currentFilter = ""
				filterInput.SetText("")
				updateTable("")
				status.SetText(" [yellow]Enter[-] select  [yellow]/[-] filter  [yellow]Esc[-] cancel")
				return nil
			}
			// Cancel selection
			onSelect(ProjectSelectorResult{Cancelled: true})
			return nil
		case tcell.KeyEnter:
			// Select current row
			row, _ := table.GetSelection()
			if row > 0 {
				cell := table.GetCell(row, 0)
				if cell != nil {
					onSelect(ProjectSelectorResult{Project: cell.Text})
				}
			}
			return nil
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
				status.SetText(" [yellow]Filter: spaces=AND  |=OR  -=NOT  (e.g. \"web|api -dev\")  Enter to apply, Esc to cancel[-]")
				return nil
			}
		}
		return event
	})

	// Clear any app-level input capture to prevent interference from parent views
	// The table's widget-level input capture will handle all events
	app.SetInputCapture(nil)
	app.SetRoot(modal, true).SetFocus(table)
}
