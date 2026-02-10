package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rivo/tview"
)

// This file contains table rendering and update logic extracted from search_view.go.
// The TableUpdater provides a clean separation of table rendering concerns from
// the main search orchestration logic.

// TableUpdater handles all table-related operations for the search view.
// It manages row population, filtering, selection preservation, and title updates.
type TableUpdater struct {
	Table             *tview.Table // The tview table widget
	CurrentSearchTerm string       // Current search term for highlighting matches
}

// NewTableUpdater creates a new table updater for the given table.
func NewTableUpdater(table *tview.Table) *TableUpdater {
	return &TableUpdater{
		Table: table,
	}
}

// UpdateWithData updates the table with the given results and filter.
// This is the main entry point for updating the search results table.
func (tu *TableUpdater) UpdateWithData(filter string, results []searchEntry) {
	// Capture current selection before clearing
	selectedKey := tu.captureSelection()

	// Clear all rows except header
	tu.clearRows()

	// Apply filter and populate rows
	filterExpr := parseFilter(filter)
	matchCount := tu.populateRows(results, filterExpr, selectedKey)

	// Update title with counts and type summary
	tu.updateTitle(len(results), matchCount, filter, results)

	// Restore or set selection
	tu.ensureSelection(matchCount)
}

// captureSelection captures the currently selected row as a unique key.
// Returns empty string if no valid selection exists.
func (tu *TableUpdater) captureSelection() string {
	currentSelectedRow, _ := tu.Table.GetSelection()
	if currentSelectedRow <= 0 || currentSelectedRow >= tu.Table.GetRowCount() {
		return ""
	}

	nameCell := tu.Table.GetCell(currentSelectedRow, 1)
	projectCell := tu.Table.GetCell(currentSelectedRow, 2)
	locationCell := tu.Table.GetCell(currentSelectedRow, 3)
	if nameCell != nil && projectCell != nil && locationCell != nil {
		return nameCell.Text + "|" + projectCell.Text + "|" + locationCell.Text
	}
	return ""
}

// clearRows removes all data rows from the table, keeping only the header.
func (tu *TableUpdater) clearRows() {
	for row := tu.Table.GetRowCount() - 1; row > 0; row-- {
		tu.Table.RemoveRow(row)
	}
}

// populateRows adds filtered entries to the table and attempts to restore selection.
// Returns the number of matched entries and the row index of the restored selection (-1 if not found).
func (tu *TableUpdater) populateRows(results []searchEntry, filterExpr filterExpr, selectedKey string) int {
	currentRow := 1
	matchCount := 0
	newSelectedRow := -1

	for _, entry := range results {
		// Apply filter
		if !filterExpr.matches(entry.Name, entry.Project, entry.Location, entry.Type) {
			continue
		}

		// Add row with colored type and highlighted matches
		typeColor := getTypeColor(entry.Type)
		tu.Table.SetCell(currentRow, 0, tview.NewTableCell(fmt.Sprintf("[%s]%s[-]", typeColor, entry.Type)).SetExpansion(1))
		tu.Table.SetCell(currentRow, 1, tview.NewTableCell(highlightMatch(entry.Name, tu.CurrentSearchTerm)).SetExpansion(1))
		tu.Table.SetCell(currentRow, 2, tview.NewTableCell(highlightMatch(entry.Project, tu.CurrentSearchTerm)).SetExpansion(1))
		tu.Table.SetCell(currentRow, 3, tview.NewTableCell(highlightMatch(entry.Location, tu.CurrentSearchTerm)).SetExpansion(1))

		// Check if this row matches the previously selected key
		if selectedKey != "" && newSelectedRow == -1 {
			rowKey := entry.Name + "|" + entry.Project + "|" + entry.Location
			if rowKey == selectedKey {
				newSelectedRow = currentRow
			}
		}

		currentRow++
		matchCount++
	}

	// Restore selection if we found the previously selected item
	if newSelectedRow > 0 {
		tu.Table.Select(newSelectedRow, 0)
	}

	return matchCount
}

// updateTitle updates the table title with result counts and type summary.
func (tu *TableUpdater) updateTitle(totalCount, matchCount int, filter string, results []searchEntry) {
	typeSummary := tu.buildTypeSummary(results)

	if filter != "" {
		tu.Table.SetTitle(fmt.Sprintf(" Search Results (%d/%d matched)%s ", matchCount, totalCount, typeSummary))
	} else {
		tu.Table.SetTitle(fmt.Sprintf(" Search Results (%d)%s ", totalCount, typeSummary))
	}
}

// buildTypeSummary creates a summary string showing the top 3 resource types by count.
func (tu *TableUpdater) buildTypeSummary(results []searchEntry) string {
	typeCounts := make(map[string]int)
	for _, entry := range results {
		typeCounts[entry.Type]++
	}

	if len(typeCounts) == 0 {
		return ""
	}

	// Sort types by count (descending)
	type typeCount struct {
		name  string
		count int
	}
	var sortedTypes []typeCount
	for typeName, count := range typeCounts {
		sortedTypes = append(sortedTypes, typeCount{name: typeName, count: count})
	}
	sort.Slice(sortedTypes, func(i, j int) bool {
		return sortedTypes[i].count > sortedTypes[j].count
	})

	// Build summary string with top 3 types
	var parts []string
	for i, tc := range sortedTypes {
		if i >= 3 {
			break
		}
		// Shorten type name for display
		shortType := tc.name
		if idx := strings.LastIndex(tc.name, "."); idx >= 0 {
			shortType = tc.name[idx+1:]
		}
		parts = append(parts, fmt.Sprintf("%d %s", tc.count, shortType))
	}
	if len(sortedTypes) > 3 {
		parts = append(parts, fmt.Sprintf("%d other", len(sortedTypes)-3))
	}

	return " [" + strings.Join(parts, ", ") + "]"
}

// ensureSelection ensures that a row is selected if any rows exist.
// This handles cases where the selection was lost or needs to be restored.
func (tu *TableUpdater) ensureSelection(matchCount int) {
	if matchCount <= 0 || tu.Table.GetRowCount() <= 1 {
		return
	}

	currentSelectedRow, _ := tu.Table.GetSelection()

	// If already have a valid selection, keep it
	if currentSelectedRow > 0 && currentSelectedRow < tu.Table.GetRowCount() {
		return
	}

	// If selection is beyond the end, select the last row
	if currentSelectedRow >= tu.Table.GetRowCount() && tu.Table.GetRowCount() > 1 {
		tu.Table.Select(tu.Table.GetRowCount()-1, 0)
		return
	}

	// Default to selecting the first data row
	tu.Table.Select(1, 0)
}
