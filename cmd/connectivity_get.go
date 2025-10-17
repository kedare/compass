package cmd

import (
	"context"
	"fmt"
	"time"

	"cx/internal/gcp"
	"cx/internal/logger"
	"cx/internal/output"
	"github.com/spf13/cobra"
)

var (
	getWatch        bool
	getOutputFormat string
	getTimeout      int
)

var getCmd = &cobra.Command{
	Use:   "get <test-name>",
	Short: "Get connectivity test results",
	Long: `Get the results of a Google Cloud Network Connectivity Test.

Use --watch to poll for results until the test completes. This is useful when
a test is still running.

Examples:
  # Get test results once
  cx gcp connectivity-test get web-to-db --project my-project

  # Watch for results (poll until complete)
  cx gcp connectivity-test get web-to-db --project my-project --watch

  # Get detailed results
  cx gcp connectivity-test get web-to-db --project my-project --output detailed

  # Get JSON results for automation
  cx gcp connectivity-test get web-to-db --project my-project --output json`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		testName := args[0]
		runGetTest(cmd.Context(), testName)
	},
	ValidArgsFunction: connectivityTestNameCompletion,
}

func runGetTest(ctx context.Context, testName string) {
	logger.Log.Debugf("Getting connectivity test: %s", testName)

	// Create connectivity test client
	connClient, err := gcp.NewConnectivityClient(ctx, project)
	if err != nil {
		logger.Log.Fatalf("Failed to create connectivity client: %v", err)
	}

	var result *gcp.ConnectivityTestResult

	if getWatch {
		// Watch mode: poll until complete or timeout
		result, err = watchTest(ctx, connClient, testName)
		if err != nil {
			logger.Log.Fatalf("Failed to watch connectivity test: %v", err)
		}
	} else {
		spin := output.NewSpinner("Fetching connectivity test details")
		spin.Start()

		// Single retrieval
		result, err = connClient.GetTest(ctx, testName)
		if err != nil {
			spin.Fail("Failed to fetch connectivity test")
			logger.Log.Fatalf("Failed to get connectivity test: %v", err)
		}
		spin.Success("Connectivity test details received")
	}

	// Display results
	if err := output.DisplayConnectivityTestResult(result, getOutputFormat); err != nil {
		logger.Log.Errorf("Failed to display results: %v", err)
	}
}

func watchTest(ctx context.Context, client *gcp.ConnectivityClient, testName string) (*gcp.ConnectivityTestResult, error) {
	logger.Log.Info("Watching connectivity test (polling every 5 seconds)...")

	timeout := time.Duration(getTimeout) * time.Second
	pollInterval := 5 * time.Second
	startTime := time.Now()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	spin := output.NewSpinner("Checking connectivity test status")
	spin.Start()
	defer spin.Stop()

	// First immediate check
	result, err := client.GetTest(ctx, testName)
	if err != nil {
		spin.Fail("Failed to fetch connectivity test status")
		return nil, err
	}

	if isTestComplete(result) {
		spin.Success("Connectivity test completed")
		logger.Log.Info("Test already completed")

		return result, nil
	}

	// Poll for completion
	for {
		select {
		case <-ctx.Done():
			spin.Fail("Context cancelled while waiting for connectivity test")
			return nil, ctx.Err()
		case <-ticker.C:
			elapsed := time.Since(startTime)
			spin.Update(fmt.Sprintf("Waiting for connectivity test completion (elapsed %s)", elapsed.Round(time.Second)))

			result, err := client.GetTest(ctx, testName)
			if err != nil {
				logger.Log.Warnf("Error checking test status: %v", err)
				spin.Update(fmt.Sprintf("Retrying after error (elapsed %s)", elapsed.Round(time.Second)))

				continue
			}

			if isTestComplete(result) {
				spin.Success("Connectivity test completed")
				logger.Log.Info("Test completed")

				return result, nil
			}

			if elapsed >= timeout {
				spin.Fail("Connectivity test watch timed out")
				logger.Log.Warn("Watch timeout reached")

				return result, fmt.Errorf("watch timeout after %v", elapsed)
			}

			logger.Log.Debugf("Test still running (elapsed: %v)", elapsed.Round(time.Second))
		}
	}
}

func isTestComplete(result *gcp.ConnectivityTestResult) bool {
	if result.ReachabilityDetails == nil {
		return false
	}
	// Test is complete if there's a result
	return result.ReachabilityDetails.Result != ""
}

func init() {
	connectivityTestCmd.AddCommand(getCmd)

	getCmd.Flags().BoolVarP(&getWatch, "watch", "w", false, "Watch test progress until completion")
	getCmd.Flags().StringVarP(&getOutputFormat, "output", "o", "text", "Output format: text, json, detailed")
	getCmd.Flags().IntVar(&getTimeout, "timeout", 300, "Watch timeout in seconds")
}
