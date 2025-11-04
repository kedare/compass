package tui

import (
	"github.com/gdamore/tcell/v2"
)

// Styles holds the color scheme for the TUI (k9s-inspired)
type Styles struct {
	// Base colors
	BgColor     tcell.Color
	FgColor     tcell.Color
	BorderColor tcell.Color

	// Status colors
	StatusOK      tcell.Color
	StatusWarning tcell.Color
	StatusError   tcell.Color
	StatusInfo    tcell.Color

	// Table colors
	TableHeaderBg   tcell.Color
	TableHeaderFg   tcell.Color
	TableSelectedBg tcell.Color
	TableSelectedFg tcell.Color

	// Title colors
	TitleFg tcell.Color
	TitleBg tcell.Color

	// Crumb/breadcrumb colors
	CrumbFg tcell.Color
	CrumbBg tcell.Color
}

// DefaultStyles returns the default k9s-inspired color scheme
func DefaultStyles() *Styles {
	return &Styles{
		// Base (dark theme)
		BgColor:     tcell.ColorBlack,
		FgColor:     tcell.ColorWhite,
		BorderColor: tcell.ColorDarkCyan,

		// Status
		StatusOK:      tcell.ColorGreen,
		StatusWarning: tcell.ColorYellow,
		StatusError:   tcell.ColorRed,
		StatusInfo:    tcell.ColorDodgerBlue,

		// Table
		TableHeaderBg:   tcell.ColorDarkCyan,
		TableHeaderFg:   tcell.ColorBlack,
		TableSelectedBg: tcell.ColorDarkCyan,
		TableSelectedFg: tcell.ColorWhite,

		// Title
		TitleFg: tcell.ColorAqua,
		TitleBg: tcell.ColorBlack,

		// Crumb
		CrumbFg: tcell.ColorGray,
		CrumbBg: tcell.ColorBlack,
	}
}

// ApplyTableStyle applies the style to a table (interface{} to avoid circular import)
func (s *Styles) ApplyTableStyle(table interface{}) {
	// Type assertion will be done by caller
}

// StatusColor returns the color for a status string
func (s *Styles) StatusColor(status string) tcell.Color {
	switch status {
	case "RUNNING", "UP", "ESTABLISHED", "REACHABLE":
		return s.StatusOK
	case "STOPPED", "DOWN", "UNREACHABLE":
		return s.StatusError
	case "PENDING", "PROVISIONING", "WARNING":
		return s.StatusWarning
	default:
		return s.StatusInfo
	}
}

// FormatStatus returns a styled status string
func (s *Styles) FormatStatus(status string) string {
	// tview uses color tags like [green]text[white]
	color := s.StatusColor(status)
	colorName := ColorName(color)
	return "[" + colorName + "]" + status + "[-]"
}

// ColorName converts tcell.Color to tview color name
func ColorName(color tcell.Color) string {
	switch color {
	case tcell.ColorGreen:
		return "green"
	case tcell.ColorRed:
		return "red"
	case tcell.ColorYellow:
		return "yellow"
	case tcell.ColorDodgerBlue:
		return "dodgerblue"
	case tcell.ColorWhite:
		return "white"
	case tcell.ColorGray:
		return "gray"
	case tcell.ColorAqua:
		return "aqua"
	case tcell.ColorDarkCyan:
		return "darkcyan"
	default:
		return "white"
	}
}
