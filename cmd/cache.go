package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/logger"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the local cache database",
	Long:  "Commands for managing the local SQLite cache database, including TTL configuration and optimization.",
}

var cacheTTLCmd = &cobra.Command{
	Use:   "ttl",
	Short: "Configure cache TTL settings",
	Long: `Configure Time-To-Live (TTL) settings for cached data.

TTL can be set globally or per resource type. Resource types include:
  - global    : Default TTL for all types
  - instances : GCP instance location cache
  - zones     : Zone listings per project
  - projects  : Project entries for autocomplete
  - subnets   : Subnet information for IP lookup`,
}

var cacheTTLGetCmd = &cobra.Command{
	Use:   "get [type]",
	Short: "Get current TTL settings",
	Long:  "Display the current TTL configuration. If type is specified, shows that type's TTL.",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c, err := cache.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open cache: %v\n", err)
			os.Exit(1)
		}
		defer func() { _ = c.Close() }()

		if len(args) == 1 {
			// Show specific type
			ttlType := cache.TTLType(args[0])
			if !cache.IsValidTTLType(args[0]) {
				fmt.Fprintf(os.Stderr, "Invalid TTL type: %s. Valid types: %s\n", args[0], validTTLTypesString())
				os.Exit(1)
			}

			ttl := c.GetTTL(ttlType)
			fmt.Printf("%s: %v\n", args[0], formatDuration(ttl))

			return
		}

		// Show all TTLs
		fmt.Printf("Default TTL: %v\n\n", formatDuration(cache.DefaultCacheExpiry))

		ttls := c.GetAllTTLs()
		if len(ttls) == 0 {
			fmt.Println("No custom TTLs configured. Using defaults.")

			return
		}

		fmt.Println("Custom TTL settings:")

		for _, t := range cache.ValidTTLTypes() {
			if ttl, ok := ttls[t]; ok {
				fmt.Printf("  %s: %v\n", t, formatDuration(ttl))
			}
		}
	},
}

var cacheTTLSetCmd = &cobra.Command{
	Use:   "set <type> <duration>",
	Short: "Set TTL for a resource type",
	Long: `Set the Time-To-Live for a specific resource type.

Duration format: Use Go duration strings like "720h" (30 days), "168h" (7 days), "24h" (1 day).

Examples:
  compass cache ttl set global 720h      # Set global TTL to 30 days
  compass cache ttl set instances 168h   # Set instance TTL to 7 days
  compass cache ttl set subnets 48h      # Set subnet TTL to 2 days`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		ttlType := args[0]
		durationStr := args[1]

		if !cache.IsValidTTLType(ttlType) {
			fmt.Fprintf(os.Stderr, "Invalid TTL type: %s. Valid types: %s\n", ttlType, validTTLTypesString())
			os.Exit(1)
		}

		duration, err := time.ParseDuration(durationStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid duration: %v. Use format like '720h' or '24h'\n", err)
			os.Exit(1)
		}

		if duration < time.Hour {
			fmt.Fprintf(os.Stderr, "TTL must be at least 1 hour\n")
			os.Exit(1)
		}

		c, err := cache.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open cache: %v\n", err)
			os.Exit(1)
		}
		defer func() { _ = c.Close() }()

		if err := c.SetTTL(cache.TTLType(ttlType), duration); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to set TTL: %v\n", err)
			os.Exit(1)
		}

		logger.Log.Infof("Set %s TTL to %v", ttlType, formatDuration(duration))
	},
}

var cacheTTLClearCmd = &cobra.Command{
	Use:   "clear <type>",
	Short: "Clear custom TTL for a resource type",
	Long:  "Remove the custom TTL setting for a resource type, reverting to the default or global TTL.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ttlType := args[0]

		if !cache.IsValidTTLType(ttlType) {
			fmt.Fprintf(os.Stderr, "Invalid TTL type: %s. Valid types: %s\n", ttlType, validTTLTypesString())
			os.Exit(1)
		}

		c, err := cache.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open cache: %v\n", err)
			os.Exit(1)
		}
		defer func() { _ = c.Close() }()

		if err := c.ClearTTL(cache.TTLType(ttlType)); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to clear TTL: %v\n", err)
			os.Exit(1)
		}

		logger.Log.Infof("Cleared custom TTL for %s", ttlType)
	},
}

var cacheOptimizeCmd = &cobra.Command{
	Use:   "optimize",
	Short: "Optimize the cache database",
	Long: `Run database maintenance (VACUUM and ANALYZE) to reclaim disk space and update query statistics.

This is automatically run every 30 days, but can be run manually if needed.`,
	Run: func(cmd *cobra.Command, args []string) {
		c, err := cache.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open cache: %v\n", err)
			os.Exit(1)
		}
		defer func() { _ = c.Close() }()

		logger.Log.Info("Running database optimization...")

		result, err := c.Optimize()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Optimization failed: %v\n", err)
			os.Exit(1)
		}

		logger.Log.Info("Database optimization completed")
		logger.Log.Infof("  Size before: %s", formatBytes(result.SizeBefore))
		logger.Log.Infof("  Size after:  %s", formatBytes(result.SizeAfter))

		saved := result.SpaceSaved()
		if saved > 0 {
			logger.Log.Infof("  Space saved: %s", formatBytes(saved))
		} else if saved == 0 {
			logger.Log.Info("  Space saved: (no change)")
		} else {
			logger.Log.Infof("  Size increased: %s", formatBytes(-saved))
		}

		logger.Log.Infof("  Duration:    %v", result.Duration.Round(time.Millisecond))
	},
}

var cacheInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show cache information",
	Long:  "Display detailed information about the cache database, including size, entry counts, and statistics.",
	Run: func(cmd *cobra.Command, args []string) {
		c, err := cache.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open cache: %v\n", err)
			os.Exit(1)
		}
		defer func() { _ = c.Close() }()

		info, err := c.Info()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get cache info: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Cache Information")
		fmt.Println("=================")
		fmt.Printf("Path:           %s\n", info.Path)
		fmt.Printf("Size:           %s\n", formatBytes(info.SizeBytes))
		fmt.Printf("Schema Version: %d\n", info.SchemaVersion)
		fmt.Println()

		fmt.Println("Entry Counts")
		fmt.Println("------------")
		fmt.Printf("Instances:      %d\n", info.InstanceCount)
		fmt.Printf("Zones:          %d\n", info.ZoneCount)
		fmt.Printf("Projects:       %d\n", info.ProjectCount)
		fmt.Printf("Subnets:        %d\n", info.SubnetCount)
		fmt.Println()

		fmt.Println("TTL Configuration")
		fmt.Println("-----------------")
		fmt.Printf("Default:        %v\n", formatDuration(info.DefaultTTL))

		if len(info.TTLs) > 0 {
			fmt.Println("Custom:")

			for t, ttl := range info.TTLs {
				fmt.Printf("  %s: %v\n", t, formatDuration(ttl))
			}
		}

		fmt.Println()

		fmt.Println("Optimization")
		fmt.Println("------------")
		status := c.GetOptimizationStatus()

		if status.LastOptimized.IsZero() {
			fmt.Println("Last Optimized: Never")
		} else {
			fmt.Printf("Last Optimized: %s\n", status.LastOptimized.Format(time.RFC3339))
		}

		if !status.NextOptimization.IsZero() {
			fmt.Printf("Next Scheduled: %s\n", status.NextOptimization.Format(time.RFC3339))
		}

		if status.NeedsOptimization {
			fmt.Println("Status:         Optimization recommended")
		} else {
			fmt.Println("Status:         OK")
		}

		// Show stats if any operations occurred
		if info.Stats.Hits > 0 || info.Stats.Misses > 0 {
			fmt.Println()
			fmt.Println("Session Statistics")
			fmt.Println("------------------")
			fmt.Printf("Hits:           %d\n", info.Stats.Hits)
			fmt.Printf("Misses:         %d\n", info.Stats.Misses)
			fmt.Printf("Hit Rate:       %.1f%%\n", info.Stats.HitRate*100)
		}
	},
}

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear all cache entries",
	Long:  "Remove all cached data. This does not remove TTL settings.",
	Run: func(cmd *cobra.Command, args []string) {
		c, err := cache.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open cache: %v\n", err)
			os.Exit(1)
		}
		defer func() { _ = c.Close() }()

		if err := c.Clear(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to clear cache: %v\n", err)
			os.Exit(1)
		}

		logger.Log.Info("Cache cleared")
	},
}

var cacheSQLFile string

var cacheSQLCmd = &cobra.Command{
	Use:   "sql [query]",
	Short: "Execute a custom SQL query on the cache database",
	Long: `Execute a custom SQL query on the cache database and display results as a table.

The query can be provided as an argument or read from a file using --file/-f.

Examples:
  compass cache sql "SELECT * FROM instances LIMIT 10"
  compass cache sql "SELECT name, project FROM projects ORDER BY timestamp DESC"
  compass cache sql --file query.sql
  compass cache sql -f query.sql`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var query string

		if cacheSQLFile != "" {
			// Read query from file
			content, err := os.ReadFile(cacheSQLFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to read SQL file: %v\n", err)
				os.Exit(1)
			}

			query = string(content)
		} else if len(args) == 1 {
			query = args[0]
		} else {
			fmt.Fprintf(os.Stderr, "Error: either provide a SQL query as argument or use --file/-f\n")
			os.Exit(1)
		}

		c, err := cache.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open cache: %v\n", err)
			os.Exit(1)
		}
		defer func() { _ = c.Close() }()

		result, err := c.ExecuteSQL(query)
		if err != nil {
			fmt.Fprintf(os.Stderr, "SQL error: %v\n", err)
			os.Exit(1)
		}

		if result.IsQuery {
			renderSQLTable(result)
		} else {
			logger.Log.Infof("Statement executed successfully. Rows affected: %d", result.RowsAffected)
		}
	},
}

func init() {
	rootCmd.AddCommand(cacheCmd)

	cacheCmd.AddCommand(cacheTTLCmd)
	cacheCmd.AddCommand(cacheOptimizeCmd)
	cacheCmd.AddCommand(cacheInfoCmd)
	cacheCmd.AddCommand(cacheClearCmd)
	cacheCmd.AddCommand(cacheSQLCmd)

	cacheTTLCmd.AddCommand(cacheTTLGetCmd)
	cacheTTLCmd.AddCommand(cacheTTLSetCmd)
	cacheTTLCmd.AddCommand(cacheTTLClearCmd)

	cacheSQLCmd.Flags().StringVarP(&cacheSQLFile, "file", "f", "", "Read SQL query from file")
}

// validTTLTypesString returns a comma-separated list of valid TTL types.
func validTTLTypesString() string {
	types := cache.ValidTTLTypes()
	strs := make([]string, len(types))

	for i, t := range types {
		strs[i] = string(t)
	}

	return strings.Join(strs, ", ")
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	days := d / (24 * time.Hour)
	if days >= 1 {
		hours := (d % (24 * time.Hour)) / time.Hour

		if hours > 0 {
			return fmt.Sprintf("%dd %dh", days, hours)
		}

		return fmt.Sprintf("%dd", days)
	}

	return d.String()
}

// formatBytes formats bytes in a human-readable way.
func formatBytes(b int64) string {
	const unit = 1024

	if b < unit {
		return fmt.Sprintf("%d B", b)
	}

	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// renderSQLTable renders a SQL result as a table using pterm.
func renderSQLTable(result *cache.SQLResult) {
	if result == nil || len(result.Columns) == 0 {
		fmt.Println("No results")
		return
	}

	if len(result.Rows) == 0 {
		fmt.Println("Query returned 0 rows")
		return
	}

	// Build table data with header
	tableData := pterm.TableData{result.Columns}
	tableData = append(tableData, result.Rows...)

	// Render table
	if err := pterm.DefaultTable.WithHasHeader().WithData(tableData).Render(); err != nil {
		// Fall back to simple output if table rendering fails
		fmt.Println(strings.Join(result.Columns, "\t"))
		for _, row := range result.Rows {
			fmt.Println(strings.Join(row, "\t"))
		}
	}

	fmt.Printf("\n(%d row(s))\n", len(result.Rows))
}
