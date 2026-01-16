package cmd

import (
	"fmt"

	"github.com/kedare/compass/internal/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show build metadata for this binary",
	Long:  "Display build time, commit, builder information, and target architecture embedded in the binary.",
	Run: func(cmd *cobra.Command, args []string) {
		info := version.Get()

		fmt.Printf("Version:      %s\n", info.Version)
		fmt.Printf("Commit:       %s\n", info.Commit)

		// Display build date with relative time if available
		if relTime := info.RelativeTime(); relTime != "" {
			fmt.Printf("Built:        %s (%s)\n", info.BuildDate, relTime)
		} else {
			fmt.Printf("Built:        %s\n", info.BuildDate)
		}

		fmt.Printf("Built By:     %s@%s\n", info.BuildUser, info.BuildHost)
		fmt.Printf("Architecture: %s\n", info.BuildArch)
		fmt.Printf("Go Version:   %s\n", info.GoVersion)

		// Check for updates
		if latestVersion, url, err := version.CheckForUpdate(); err == nil && latestVersion != "" {
			fmt.Printf("\nNew version available: %s\n", latestVersion)
			fmt.Printf("Download: %s\n", url)
			fmt.Println("Or use `compass update`")
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
