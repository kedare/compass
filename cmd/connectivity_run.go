package cmd

import (
	"context"

	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/logger"
	"github.com/kedare/compass/internal/output"
	"github.com/spf13/cobra"
)

var runOutputFormat string

var runCmd = &cobra.Command{
	Use:     "run <test-name>",
	Aliases: []string{"r"},
	Short:   "Run an existing connectivity test",
	Long: `Rerun an existing Google Cloud Network Connectivity Test and display the results.

This command reruns a previously created test with the same configuration and displays
the updated results.

Examples:
  # Run a test
  compass gcp connectivity-test run web-to-db --project my-project

  # Run and get detailed output
  compass gcp connectivity-test run web-to-db --project my-project --output detailed

  # Run and get JSON output
  compass gcp connectivity-test run web-to-db --project my-project --output json`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		testName := args[0]
		runRunTest(cmd.Context(), testName)
	},
	ValidArgsFunction: connectivityTestNameCompletion,
}

func runRunTest(ctx context.Context, testName string) {
	// Set the output format to ensure JSON mode detection works correctly for spinners
	output.SetFormat(runOutputFormat)

	logger.Log.Infof("Running connectivity test: %s", testName)

	// Create connectivity test client
	connClient, err := gcp.NewConnectivityClient(ctx, project)
	if err != nil {
		logger.Log.Fatalf("Failed to create connectivity client: %v", err)
	}

	// Run the test
	logger.Log.Info("Running connectivity test (this may take a minute)...")

	spin := output.NewSpinner("Running connectivity test")
	spin.Start()

	result, err := connClient.RunTest(ctx, testName)
	if err != nil {
		spin.Fail("Failed to run connectivity test")
		logger.Log.Fatalf("Failed to run connectivity test: %v", err)
	}

	spin.Success("Connectivity test finished")

	logger.Log.Info("Connectivity test completed")

	// Display results
	if err := output.DisplayConnectivityTestResult(result, runOutputFormat); err != nil {
		logger.Log.Errorf("Failed to display results: %v", err)
	}
}

func init() {
	connectivityTestCmd.AddCommand(runCmd)

	runCmd.Flags().StringVarP(&runOutputFormat, "output", "o",
		output.DefaultFormat("text", []string{"text", "json", "detailed"}),
		"Output format: text, json, detailed")
}
