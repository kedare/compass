package widgets

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Table is an enhanced table widget
type Table struct {
	*tview.Table
	headers      []string
	sortColumn   int
	sortAsc      bool
	selectedRow  int
	filterActive bool
}

// NewTable creates a new table widget
func NewTable() *Table {
	table := &Table{
		Table:      tview.NewTable(),
		sortColumn: 0,
		sortAsc:    true,
	}

	table.SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0). // Fix header row
		SetSeparator(tview.Borders.Vertical)

	return table
}

// SetHeaders sets the table headers
func (t *Table) SetHeaders(headers []string) *Table {
	t.headers = headers
	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorBlack).
			SetBackgroundColor(tcell.ColorDarkCyan).
			SetAlign(tview.AlignLeft).
			SetSelectable(false).
			SetExpansion(1)
		t.SetCell(0, col, cell)
	}
	return t
}

// AddRow adds a data row to the table
func (t *Table) AddRow(cells []string, reference interface{}) {
	row := t.GetRowCount()
	for col, cellText := range cells {
		cell := tview.NewTableCell(cellText).
			SetTextColor(tcell.ColorWhite).
			SetBackgroundColor(tcell.ColorBlack).
			SetAlign(tview.AlignLeft).
			SetReference(reference).
			SetExpansion(1)
		t.SetCell(row, col, cell)
	}
}

// ClearRows clears all rows except headers
func (t *Table) ClearRows() {
	rowCount := t.GetRowCount()
	for row := rowCount - 1; row > 0; row-- {
		t.RemoveRow(row)
	}
}

// GetSelectedReference returns the reference of the selected row
func (t *Table) GetSelectedReference() interface{} {
	row, _ := t.GetSelection()
	if row <= 0 || row >= t.GetRowCount() {
		return nil
	}
	cell := t.GetCell(row, 0)
	if cell == nil {
		return nil
	}
	return cell.GetReference()
}

// SetSortColumn sets the sort column
func (t *Table) SetSortColumn(col int, ascending bool) {
	t.sortColumn = col
	t.sortAsc = ascending
	// Update header to show sort indicator
	if col >= 0 && col < len(t.headers) {
		indicator := " ▼"
		if ascending {
			indicator = " ▲"
		}
		cell := t.GetCell(0, col)
		cell.SetText(t.headers[col] + indicator)
	}
}

// SetCellColor sets the color of a specific cell
func (t *Table) SetCellColor(row, col int, fg, bg tcell.Color) {
	if cell := t.GetCell(row, col); cell != nil {
		cell.SetTextColor(fg).SetBackgroundColor(bg)
	}
}

// HighlightRow highlights a row with a specific color
func (t *Table) HighlightRow(row int, fg, bg tcell.Color) {
	colCount := t.GetColumnCount()
	for col := 0; col < colCount; col++ {
		t.SetCellColor(row, col, fg, bg)
	}
}
