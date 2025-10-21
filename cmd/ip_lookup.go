package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/logger"
	"github.com/kedare/compass/internal/output"
	"github.com/spf13/cobra"
)

var ipLookupOutputFormat string

// ipLookupResult captures the outcome of querying a single project for IP associations.
type ipLookupResult struct {
	associations []gcp.IPAssociation
	err          error
}

var ipLookupCmd = &cobra.Command{
	Use:   "lookup <ip-address>",
	Short: "Find the GCP resources referencing an IP address",
	Long: `Search Compute Engine instances, forwarding rules, and reserved addresses to identify
which resources own or reference a given IP address. When no project is specified, all cached
projects are scanned automatically.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runIPLookup(cmd.Context(), args[0])
	},
}

func init() {
	// init registers the IP lookup command on the parent IP command and configures flags.
	ipCmd.AddCommand(ipLookupCmd)

	ipLookupCmd.Flags().StringVarP(&ipLookupOutputFormat, "output", "o",
		output.DefaultFormat("table", []string{"table", "text", "json"}),
		"Output format: table, text, json")
}

// runIPLookup orchestrates the lookup by iterating over all candidate projects and gathering matches.
func runIPLookup(ctx context.Context, rawIP string) {
	ip := strings.TrimSpace(rawIP)
	if net.ParseIP(ip) == nil {
		logger.Log.Fatalf("Invalid IP address: %s", rawIP)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	clients := lookupClients(ctx)
	if len(clients) == 0 {
		logger.Log.Fatalf("No projects available for lookup. Use --project to specify one explicitly.")
	}

	spin := output.NewSpinner("Searching IP associations")
	spin.Start()

	var results []gcp.IPAssociation
	hadSuccess := false
	canceled := false

	for _, client := range clients {
		if err := ctx.Err(); err != nil {
			canceled = true
			break
		}

		projectID := client.ProjectID()
		spin.Update(fmt.Sprintf("Scanning project %s…", projectID))
		logger.Log.Debugf("Looking up IP %s in project %s", ip, projectID)

		resultCh := make(chan ipLookupResult, 1)
		go func(c *gcp.Client) {
			associations, err := c.LookupIPAddress(ctx, ip)
			resultCh <- ipLookupResult{associations: associations, err: err}
		}(client)

		select {
		case <-ctx.Done():
			canceled = true
		case res := <-resultCh:
			if isContextError(res.err) || isContextError(ctx.Err()) {
				canceled = true
				break
			}

			if res.err != nil {
				logger.Log.Warnf("Skipping project %s: %v", projectID, res.err)

				continue
			}

			hadSuccess = true
			results = append(results, res.associations...)
		}

		if canceled {
			break
		}
	}

	if canceled {
		spin.Fail("Lookup canceled")

		return
	}

	if !hadSuccess {
		spin.Fail("Lookup failed for all projects")
		logger.Log.Fatalf("IP lookup failed for all checked projects")
	}

	spin.Success("Lookup complete")

	if len(results) == 0 {
		fmt.Printf("No resources found for IP %s\n", ip)

		return
	}

	if err := output.DisplayIPLookupResults(results, ipLookupOutputFormat); err != nil {
		logger.Log.Fatalf("Failed to render lookup results: %v", err)
	}
}

// lookupClients builds unique GCP clients that will participate in the lookup.
func lookupClients(ctx context.Context) []*gcp.Client {
	seen := make(map[string]struct{})
	clients := make([]*gcp.Client, 0)

	addClient := func(projectID string) {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return
			}
		}

		if projectID != "" {
			if _, exists := seen[projectID]; exists {
				return
			}
		}

		client, err := gcp.NewClient(ctx, projectID)
		if err != nil {
			logger.Log.Warnf("Failed to initialize GCP client for project %s: %v", projectID, err)

			return
		}

		effectiveProject := client.ProjectID()
		if _, exists := seen[effectiveProject]; exists {
			return
		}

		seen[effectiveProject] = struct{}{}
		clients = append(clients, client)
	}

	if project != "" {
		addClient(project)

		return clients
	}

	cache, err := gcp.LoadCache()
	if err != nil {
		logger.Log.Debugf("Failed to load cache for project discovery: %v", err)
	} else if cache != nil {
		for _, cachedProject := range enumerateProjects(cache.GetProjects()) {
			if ctx != nil && ctx.Err() != nil {
				break
			}
			addClient(cachedProject)
		}
	}

	if len(clients) == 0 {
		if ctx == nil || ctx.Err() == nil {
			addClient("")
		}
	}

	return clients
}

// enumerateProjects trims and deduplicates cached project identifiers.
func enumerateProjects(projects []string) []string {
	if len(projects) == 0 {
		return nil
	}

	unique := make([]string, 0, len(projects))
	seen := make(map[string]struct{}, len(projects))

	for _, projectID := range projects {
		projectID = strings.TrimSpace(projectID)
		if projectID == "" {
			continue
		}

		if _, exists := seen[projectID]; exists {
			continue
		}

		seen[projectID] = struct{}{}
		unique = append(unique, projectID)
	}

	return unique
}

// isContextError reports whether the provided error is caused by context cancellation.
func isContextError(err error) bool {
	return err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}
