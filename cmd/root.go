// Package cmd provides the command-line interface for the cx tool
package cmd

import (
	"context"
	"fmt"
	"os"

	"cx/internal/logger"
	"github.com/spf13/cobra"
)

var logLevel string

var rootCmd = &cobra.Command{
	Use:   "cx",
	Short: "Connect to cloud instances easily",
	Long:  "A CLI tool to connect to remote instances from cloud providers using SSH and IAP tunneling",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if err := logger.SetLevel(logLevel); err != nil {
			fmt.Fprintf(os.Stderr, "Invalid log level '%s': %v\n", logLevel, err)
			os.Exit(1)
		}
		logger.Log.Debugf("Log level set to: %s", logLevel)
	},
}

func Execute() error {
	return ExecuteContext(context.Background())
}

func ExecuteContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	return rootCmd.ExecuteContext(ctx)
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Set the logging level (trace, debug, info, warn, error, fatal, panic)")
	rootCmd.AddCommand(gcpCmd)
}
