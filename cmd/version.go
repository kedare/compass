package cmd

import (
	"fmt"

	"codeberg.org/kedare/compass/internal/version"
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
		fmt.Printf("Built:        %s\n", info.BuildDate)
		fmt.Printf("Built By:     %s@%s\n", info.BuildUser, info.BuildHost)
		fmt.Printf("Architecture: %s\n", info.BuildArch)
		fmt.Printf("Go Version:   %s\n", info.GoVersion)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
