package cmd

import (
	"fmt"

	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/tui"
	"github.com/spf13/cobra"
)

var interactiveCmd = &cobra.Command{
	Use:   "interactive",
	Short: "Launch interactive TUI interface",
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
	fmt.Println("Starting interactive mode...")

	// Get cache
	fmt.Println("Initializing cache...")
	c, err := cache.New()
	if err != nil {
		return fmt.Errorf("failed to initialize cache: %w", err)
	}
	fmt.Println("Cache initialized")

	// Create GCP client (empty project string means use default)
	fmt.Println("Creating GCP client...")
	gcpClient, err := gcp.NewClient(cmd.Context(), "")
	if err != nil {
		return fmt.Errorf("failed to create GCP client: %w", err)
	}
	fmt.Println("GCP client created")

	// Create TUI app
	fmt.Println("Creating TUI app...")
	fmt.Printf("Cache has %d projects\n", len(c.GetProjects()))

	// Don't use NewApp, create a minimal app directly
	fmt.Println("TUI app created (skipping full initialization)")

	// Run the simplified version directly without NewApp
	fmt.Println("Starting TUI (press Ctrl+C to quit)...")
	if err := tui.RunDirect(c, gcpClient); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	fmt.Println("TUI exited")
	return nil
}
