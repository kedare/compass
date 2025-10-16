package cmd

import (
	"context"

	"cx/internal/gcp"
	"cx/internal/logger"
	"cx/internal/output"
	"github.com/spf13/cobra"
)

var runOutputFormat string

var runCmd = &cobra.Command{
	Use:   "run <test-name>",
	Short: "Run an existing connectivity test",
	Long: `Rerun an existing Google Cloud Network Connectivity Test and display the results.

This command reruns a previously created test with the same configuration and displays
the updated results.

Examples:
  # Run a test
  cx gcp connectivity-test run web-to-db --project my-project

  # Run and get detailed output
  cx gcp connectivity-test run web-to-db --project my-project --output detailed

  # Run and get JSON output
  cx gcp connectivity-test run web-to-db --project my-project --output json`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		testName := args[0]
		runRunTest(cmd.Context(), testName)
	},
}

func runRunTest(ctx context.Context, testName string) {
	logger.Log.Infof("Running connectivity test: %s", testName)

	// Create connectivity test client
	connClient, err := gcp.NewConnectivityClient(ctx, project)
	if err != nil {
		logger.Log.Fatalf("Failed to create connectivity client: %v", err)
	}

	// Run the test
	logger.Log.Info("Running connectivity test (this may take a minute)...")

	result, err := connClient.RunTest(ctx, testName)
	if err != nil {
		logger.Log.Fatalf("Failed to run connectivity test: %v", err)
	}

	logger.Log.Info("Connectivity test completed")

	// Display results
	if err := output.DisplayConnectivityTestResult(result, runOutputFormat); err != nil {
		logger.Log.Errorf("Failed to display results: %v", err)
	}
}

func init() {
	connectivityTestCmd.AddCommand(runCmd)

	runCmd.Flags().StringVarP(&runOutputFormat, "output", "o", "text", "Output format: text, json, detailed")
}
