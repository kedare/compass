package tui

import (
	"context"
	"fmt"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/logger"
	"github.com/rivo/tview"
)

// ProjectRefreshResult holds the result of the project refresh operation
type ProjectRefreshResult struct {
	Cancelled        bool
	SelectedProjects []string
}

// ShowProjectRefreshPopup displays a popup to select projects for refresh
// It shows all cached projects with checkboxes for multi-selection
func ShowProjectRefreshPopup(app *App, onComplete func()) {
	c := app.config.Cache

	// Get projects sorted by usage
	projects := c.GetProjectsByUsage()

	if len(projects) == 0 {
		// No projects in cache, show error modal
		modal := tview.NewModal().
			SetText("No projects found in cache.\n\nRun 'compass gcp projects import' to import projects first.").
			AddButtons([]string{"OK"}).
			SetDoneFunc(func(buttonIndex int, buttonLabel string) {
				if onComplete != nil {
					onComplete()
				}
			})
		app.SetRoot(modal, true).SetFocus(modal)
		return
	}

	// Track selected projects
	selected := make(map[string]bool)
	allSelected := false

	// Create the list with checkboxes
	list := tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true)

	list.SetBorder(true).SetTitle(" Refresh Projects ")

	// Function to update the list display
	updateList := func() {
		list.Clear()

		// Add "All Projects" option first
		allCheckbox := "[ ]"
		if allSelected {
			allCheckbox = "[x]"
		}
		list.AddItem(fmt.Sprintf("%s All Projects (%d)", allCheckbox, len(projects)), "", 0, nil)

		// Add individual projects
		for _, project := range projects {
			checkbox := "[ ]"
			if selected[project] || allSelected {
				checkbox = "[x]"
			}
			list.AddItem(fmt.Sprintf("%s %s", checkbox, project), "", 0, nil)
		}
	}

	updateList()

	// Status bar
	status := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Space[-] toggle  [yellow]Enter[-] refresh  [yellow]Esc[-] cancel")

	// Button area
	refreshBtn := tview.NewButton("Refresh").SetSelectedFunc(func() {
		// Gather selected projects
		var toRefresh []string
		if allSelected {
			toRefresh = projects
		} else {
			for project := range selected {
				toRefresh = append(toRefresh, project)
			}
		}

		if len(toRefresh) == 0 {
			app.Flash("No projects selected", true)
			return
		}

		// Start the refresh process
		performProjectRefresh(app, toRefresh, onComplete)
	})

	cancelBtn := tview.NewButton("Cancel").SetSelectedFunc(func() {
		if onComplete != nil {
			onComplete()
		}
	})

	buttonFlex := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(refreshBtn, 12, 0, false).
		AddItem(nil, 2, 0, false).
		AddItem(cancelBtn, 10, 0, false).
		AddItem(nil, 0, 1, false)

	// Main layout
	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(list, 0, 1, true).
		AddItem(buttonFlex, 1, 0, false).
		AddItem(status, 1, 0, false)

	// Create centered modal container
	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(flex, 20, 0, true).
			AddItem(nil, 0, 1, false), 60, 0, true).
		AddItem(nil, 0, 1, false)

	// Handle keyboard input
	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			if onComplete != nil {
				onComplete()
			}
			return nil

		case tcell.KeyEnter:
			// Gather selected projects
			var toRefresh []string
			if allSelected {
				toRefresh = projects
			} else {
				for project := range selected {
					toRefresh = append(toRefresh, project)
				}
			}

			if len(toRefresh) == 0 {
				app.Flash("No projects selected", true)
				return nil
			}

			// Start the refresh process
			performProjectRefresh(app, toRefresh, onComplete)
			return nil

		case tcell.KeyTab:
			// Switch focus to buttons
			app.SetFocus(refreshBtn)
			return nil

		case tcell.KeyRune:
			if event.Rune() == ' ' {
				// Toggle selection
				idx := list.GetCurrentItem()
				if idx == 0 {
					// Toggle "All"
					allSelected = !allSelected
					if allSelected {
						// Clear individual selections when "All" is selected
						selected = make(map[string]bool)
					}
				} else if idx > 0 && idx <= len(projects) {
					project := projects[idx-1]
					if allSelected {
						// Deselect "All" when individual is toggled
						allSelected = false
						// Select all except the toggled one
						for _, p := range projects {
							if p != project {
								selected[p] = true
							}
						}
					} else {
						if selected[project] {
							delete(selected, project)
						} else {
							selected[project] = true
						}
					}
				}
				updateList()
				list.SetCurrentItem(idx)
				return nil
			}
		}
		return event
	})

	// Handle button navigation
	refreshBtn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			app.SetFocus(cancelBtn)
			return nil
		case tcell.KeyBacktab:
			app.SetFocus(list)
			return nil
		case tcell.KeyEscape:
			if onComplete != nil {
				onComplete()
			}
			return nil
		}
		return event
	})

	cancelBtn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			app.SetFocus(list)
			return nil
		case tcell.KeyBacktab:
			app.SetFocus(refreshBtn)
			return nil
		case tcell.KeyEscape:
			if onComplete != nil {
				onComplete()
			}
			return nil
		}
		return event
	})

	app.SetRoot(modal, true).SetFocus(list)
}

// performProjectRefresh executes the refresh operation for the selected projects
func performProjectRefresh(app *App, projects []string, onComplete func()) {
	ctx, cancel := context.WithCancel(app.ctx)

	// Show loading screen
	updateMsg, spinnerDone := showLoadingScreen(
		app.Application,
		" Refreshing Projects ",
		fmt.Sprintf("Refreshing %d project(s)...", len(projects)),
		func() {
			cancel()
			if onComplete != nil {
				onComplete()
			}
		},
	)

	// Run refresh in background
	go func() {
		// Track totals
		var totalStats gcp.ScanStats
		var statsMu sync.Mutex

		// Track errors
		var errors []string
		var errorsMu sync.Mutex

		// Scan each project concurrently with limited parallelism
		semaphore := make(chan struct{}, 10)
		var wg sync.WaitGroup

		for i, project := range projects {
			wg.Add(1)
			go func(proj string, idx int) {
				defer wg.Done()

				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				// Check for cancellation
				if ctx.Err() != nil {
					return
				}

				updateMsg(fmt.Sprintf("Refreshing %s (%d/%d)...", proj, idx+1, len(projects)))

				client, err := gcp.NewClient(ctx, proj)
				if err != nil {
					errorsMu.Lock()
					errors = append(errors, fmt.Sprintf("%s: %v", proj, err))
					errorsMu.Unlock()
					return
				}

				stats, err := client.ScanProjectResources(ctx, func(resource string, count int) {
					statsMu.Lock()
					switch resource {
					case "zones":
						totalStats.Zones += count
					case "instances":
						totalStats.Instances += count
					case "migs":
						totalStats.MIGs += count
					case "subnets":
						totalStats.Subnets += count
					}
					statsMu.Unlock()
				})

				if err != nil {
					errorsMu.Lock()
					errors = append(errors, fmt.Sprintf("%s: %v", proj, err))
					errorsMu.Unlock()
				}

				if stats != nil {
					logger.Log.Debugf("Refreshed project %s: %d zones, %d instances, %d MIGs, %d subnets",
						proj, stats.Zones, stats.Instances, stats.MIGs, stats.Subnets)
				}
			}(project, i)
		}

		// Wait for all scans to complete
		wg.Wait()

		// Update UI on completion
		app.QueueUpdateDraw(func() {
			// Stop spinner
			select {
			case spinnerDone <- true:
			default:
			}

			// Check for cancellation
			if ctx.Err() == context.Canceled {
				return
			}

			// Show result
			statsMu.Lock()
			resultMsg := fmt.Sprintf("Refreshed: %d zones, %d instances, %d MIGs, %d subnets",
				totalStats.Zones, totalStats.Instances, totalStats.MIGs, totalStats.Subnets)
			statsMu.Unlock()

			errorsMu.Lock()
			if len(errors) > 0 {
				resultMsg += fmt.Sprintf(" (%d errors)", len(errors))
				for _, e := range errors {
					logger.Log.Warnf("Refresh error: %s", e)
				}
			}
			errorsMu.Unlock()

			app.Flash(resultMsg, len(errors) > 0)

			if onComplete != nil {
				onComplete()
			}
		})
	}()
}
