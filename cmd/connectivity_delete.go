package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"cx/internal/gcp"
	"cx/internal/logger"
	"cx/internal/output"
	"github.com/spf13/cobra"
)

var (
	deleteForce   bool
	deleteConfirm bool
)

var deleteCmd = &cobra.Command{
	Use:   "delete <test-name>",
	Short: "Delete a connectivity test",
	Long: `Delete a Google Cloud Network Connectivity Test.

By default, you will be prompted for confirmation unless --force is used.

Examples:
  # Delete with confirmation prompt
  cx gcp connectivity-test delete web-to-db --project my-project

  # Delete without confirmation
  cx gcp connectivity-test delete web-to-db --project my-project --force

  # Delete with explicit confirmation flag
  cx gcp connectivity-test delete web-to-db --project my-project --confirm`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		testName := args[0]
		runDeleteTest(cmd.Context(), testName)
	},
	ValidArgsFunction: connectivityTestNameCompletion,
}

func runDeleteTest(ctx context.Context, testName string) {
	logger.Log.Debugf("Deleting connectivity test: %s", testName)

	// Confirm deletion unless --force is used
	if !deleteForce {
		confirmed := deleteConfirm
		if !confirmed {
			confirmed = confirmDeletion(testName)
		}

		if !confirmed {
			logger.Log.Info("Deletion canceled")

			return
		}
	}

	// Create connectivity test client
	connClient, err := gcp.NewConnectivityClient(ctx, project)
	if err != nil {
		logger.Log.Fatalf("Failed to create connectivity client: %v", err)
	}

	// Delete the test
	logger.Log.Infof("Deleting connectivity test: %s", testName)

	spin := output.NewSpinner("Deleting connectivity test")
	spin.Start()

	if err := connClient.DeleteTest(ctx, testName); err != nil {
		spin.Fail("Failed to delete connectivity test")
		logger.Log.Fatalf("Failed to delete connectivity test: %v", err)
	}
	spin.Success("Connectivity test deleted")

	logger.Log.Infof("Connectivity test '%s' deleted successfully", testName)
}

func confirmDeletion(testName string) bool {
	reader := bufio.NewReader(os.Stdin)

	fmt.Printf("Are you sure you want to delete connectivity test '%s'? (y/N): ", testName)

	response, err := reader.ReadString('\n')
	if err != nil {
		logger.Log.Errorf("Failed to read input: %v", err)

		return false
	}

	response = strings.ToLower(strings.TrimSpace(response))

	return response == "y" || response == "yes"
}

func init() {
	connectivityTestCmd.AddCommand(deleteCmd)

	deleteCmd.Flags().BoolVarP(&deleteForce, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&deleteConfirm, "confirm", false, "Prompt for confirmation (default behavior)")
}
