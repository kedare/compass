package cmd

import (
	"context"

	"cx/internal/gcp"
	"cx/internal/logger"
	"cx/internal/output"
	"github.com/spf13/cobra"
)

var (
	listFilter       string
	listOutputFormat string
	listLimit        int
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List connectivity tests",
	Long: `List all Google Cloud Network Connectivity Tests in the project.

You can filter results using the --filter flag with a filter expression.

Examples:
  # List all tests
  cx gcp connectivity-test list --project my-project

  # List with table output
  cx gcp connectivity-test list --project my-project --output table

  # List with filter
  cx gcp connectivity-test list --project my-project --filter "labels.env=prod"

  # List with JSON output
  cx gcp connectivity-test list --project my-project --output json`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runListTests(cmd.Context())
	},
}

func runListTests(ctx context.Context) {
	logger.Log.Debug("Listing connectivity tests")

	// Create connectivity test client
	connClient, err := gcp.NewConnectivityClient(ctx, project)
	if err != nil {
		logger.Log.Fatalf("Failed to create connectivity client: %v", err)
	}

	// List tests
	results, err := connClient.ListTests(ctx, listFilter)
	if err != nil {
		logger.Log.Fatalf("Failed to list connectivity tests: %v", err)
	}

	// Apply limit if specified
	if listLimit > 0 && len(results) > listLimit {
		results = results[:listLimit]
	}

	// Display results
	if err := output.DisplayConnectivityTestList(results, listOutputFormat); err != nil {
		logger.Log.Errorf("Failed to display results: %v", err)
	}
}

func init() {
	connectivityTestCmd.AddCommand(listCmd)

	listCmd.Flags().StringVar(&listFilter, "filter", "", "Filter expression")
	listCmd.Flags().StringVarP(&listOutputFormat, "output", "o", "text", "Output format: text, table, json")
	listCmd.Flags().IntVar(&listLimit, "limit", 0, "Maximum number of results (0 = no limit)")
}
