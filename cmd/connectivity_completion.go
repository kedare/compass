package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"compass/internal/gcp"
	"github.com/spf13/cobra"
)

func connectivityTestNameCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// Respect any project flag value provided in the current completion request.
	projectID, err := cmd.Flags().GetString("project")
	if err != nil {
		projectID = ""
	}

	if projectID == "" {
		projectID = project
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := gcp.NewConnectivityClient(ctx, projectID)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	tests, err := client.ListTests(ctx, "")
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	completions := make([]string, 0, len(tests))

	for _, test := range tests {
		if toComplete != "" && !strings.HasPrefix(test.Name, toComplete) {
			continue
		}

		entry := test.Name
		if test.DisplayName != "" && test.DisplayName != test.Name {
			entry = fmt.Sprintf("%s\t%s", test.Name, test.DisplayName)
		}

		completions = append(completions, entry)
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}
