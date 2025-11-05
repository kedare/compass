package cmd

import (
	"fmt"

	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/logger"
	"github.com/kedare/compass/internal/tui"
	"github.com/spf13/cobra"
)

var interactiveCmd = &cobra.Command{
	Use:     "interactive",
	Aliases: []string{"i", "tui"},
	Short:   "Launch interactive TUI interface",
	Long: `Start the terminal UI for interactive exploration of GCP resources.

The TUI provides keyboard-driven navigation similar to k9s, allowing you to:
- Browse and SSH to GCP instances
- Inspect VPN gateways and tunnels
- Manage connectivity tests
- Lookup IP addresses
- And more...

Press '?' at any time to see keyboard shortcuts.`,
	RunE: runInteractive,
}

func init() {
	rootCmd.AddCommand(interactiveCmd)
}

func runInteractive(cmd *cobra.Command, args []string) error {
	logger.Log.Info("Starting interactive mode...")

	// Get cache
	logger.Log.Debug("Initializing cache...")
	c, err := cache.New()
	if err != nil {
		return fmt.Errorf("failed to initialize cache: %w", err)
	}
	logger.Log.Debugf("Cache initialized with %d projects", len(c.GetProjects()))

	// Create GCP client (empty project string means use default)
	logger.Log.Debug("Creating GCP client...")
	gcpClient, err := gcp.NewClient(cmd.Context(), "")
	if err != nil {
		return fmt.Errorf("failed to create GCP client: %w", err)
	}
	logger.Log.Debug("GCP client created")

	// Run the simplified version directly without NewApp
	logger.Log.Info("Starting TUI (press Ctrl+C to quit)...")
	if err := tui.RunDirect(c, gcpClient); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	logger.Log.Info("TUI exited")
	return nil
}
