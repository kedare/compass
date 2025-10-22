// Package logger provides structured logging configuration for the compass application
package logger

import (
	"os"

	"github.com/pterm/pterm"
)

// InitPterm configures pterm to write all diagnostic output to stderr,
// leaving stdout clean for JSON and structured output.
func InitPterm() {
	// Configure all pterm prefix printers to write to stderr
	// This includes Info, Success, Warning, Error, Debug printers
	pterm.Info.Writer = os.Stderr
	pterm.Success.Writer = os.Stderr
	pterm.Warning.Writer = os.Stderr
	pterm.Error.Writer = os.Stderr
	pterm.Debug.Writer = os.Stderr

	// Note: We deliberately do NOT change pterm.DefaultTable.Writer or pterm.DefaultPanel.Writer
	// Tables and panels are part of the structured output and should go to stdout
	// (or they use Srender() to return strings that are then printed via fmt.Print)
	//
	// Interactive components (InteractiveSelect, InteractiveMultiselect) already write to
	// the appropriate streams by default, so no configuration is needed for them.
}
