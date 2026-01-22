// Package cmd provides the command-line interface for the compass tool
package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/logger"
	"github.com/spf13/cobra"
)

var (
	logLevel    string
	logFile     string
	useCache    bool
	concurrency int
)

var rootCmd = &cobra.Command{
	Use:     "compass",
	Aliases: []string{"cps"},
	Short:   "Connect and troubleshoot GCP workloads from your terminal",
	Long: `compass speeds up day-to-day GCP operations with zero-config SSH access (including IAP tunnels),
automatic zone and MIG discovery, reusable connection caching, and rich logging. It also manages Google
Cloud Network Connectivity Tests so you can validate network paths, stream results, and view structured
diagnostics without leaving the CLI.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if err := logger.SetLevel(logLevel); err != nil {
			fmt.Fprintf(os.Stderr, "Invalid log level '%s': %v\n", logLevel, err)
			os.Exit(1)
		}

		if logFile != "" {
			if err := logger.SetLogFile(logFile); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to set log file '%s': %v\n", logFile, err)
				os.Exit(1)
			}
		}

		logger.Log.Debugf("Log level set to: %s", logLevel)

		cache.SetEnabled(useCache)
		if !useCache {
			logger.Log.Debug("Global cache disabled via --cache=false")
		}

		if concurrency < 1 {
			fmt.Fprintf(os.Stderr, "Invalid concurrency value '%d': must be at least 1\n", concurrency)
			os.Exit(1)
		}
		logger.Log.Debugf("Concurrency set to: %d", concurrency)
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
	rootCmd.PersistentFlags().StringVar(&logFile, "log-file", "", "Write logs to a file (in addition to stderr)")
	rootCmd.PersistentFlags().BoolVar(&useCache, "cache", true, "Enable cache usage across commands")
	rootCmd.PersistentFlags().IntVar(&concurrency, "concurrency", 10, "Maximum number of concurrent operations (worker pool size)")
	rootCmd.AddCommand(gcpCmd)
}
