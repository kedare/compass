package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strings"

	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/logger"
	"github.com/kedare/compass/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

var ipLookupOutputFormat string

type lookupResult struct {
	client       *gcp.Client
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
		output.DefaultFormat("text", []string{"table", "text", "json"}),
		"Output format: table, text, json")
}

// runIPLookup orchestrates the lookup by querying all candidate projects in parallel and gathering matches.
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

	results := make([]gcp.IPAssociation, 0)
	hadSuccess := false
	canceled := false

	lookupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	group, groupCtx := errgroup.WithContext(lookupCtx)
	group.SetLimit(lookupConcurrency(len(clients)))
	progressCh := make(chan string, len(clients))
	resultCh := make(chan lookupResult, len(clients))
	waitErrCh := make(chan error, 1)

	for _, client := range clients {
		client := client

		group.Go(func() error {
			select {
			case progressCh <- client.ProjectID():
			case <-groupCtx.Done():
				return groupCtx.Err()
			}

			associations, err := client.LookupIPAddress(groupCtx, ip)

			res := lookupResult{
				client:       client,
				associations: associations,
				err:          err,
			}

			select {
			case resultCh <- res:
			case <-groupCtx.Done():
				return groupCtx.Err()
			}

			if isContextError(err) {
				return err
			}

			return nil
		})
	}

	go func() {
		err := group.Wait()
		cancel()
		close(progressCh)
		close(resultCh)
		waitErrCh <- err
		close(waitErrCh)
	}()

	successSeen := make(map[*gcp.Client]struct{}, len(clients))
	successClients := make([]*gcp.Client, 0, len(clients))
	ctxDone := ctx.Done()
	started := 0
	processed := 0
	total := len(clients)

loop:
	for progressCh != nil || resultCh != nil {
		select {
		case <-ctxDone:
			canceled = true
			cancel()
			break loop
		case _, ok := <-progressCh:
			if !ok {
				progressCh = nil
				continue
			}
			started++
		case res, ok := <-resultCh:
			if !ok {
				resultCh = nil
				continue
			}

			processed++

			if res.err != nil {
				if isContextError(res.err) {
					canceled = true
					cancel()
					break loop
				}

				logger.Log.Warnf("Skipping project %s: %v", res.client.ProjectID(), res.err)
				spin.Update(fmt.Sprintf("Skipped project %s (%d/%d done)", res.client.ProjectID(), processed, total))

				continue
			}

			spin.Update(fmt.Sprintf("Completed project %s (%d/%d done)", res.client.ProjectID(), processed, total))

			hadSuccess = true
			if len(res.associations) > 0 {
				results = append(results, res.associations...)
			}

			if _, seen := successSeen[res.client]; !seen {
				successSeen[res.client] = struct{}{}
				successClients = append(successClients, res.client)
			}
		}
	}

	if canceled {
		spin.Fail("Lookup canceled")

		return
	}

	waitErr := <-waitErrCh
	if isContextError(waitErr) || isContextError(ctx.Err()) {
		canceled = true
	} else if waitErr != nil {
		logger.Log.Warnf("Lookup workers encountered an error: %v", waitErr)
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

	for _, client := range successClients {
		client.RememberProject()
	}

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

// lookupConcurrency determines the number of concurrent lookups to run.
func lookupConcurrency(total int) int {
	if total <= 1 {
		return 1
	}

	max := runtime.NumCPU() * 4
	if max < 4 {
		max = 4
	}

	if total < max {
		return total
	}

	return max
}

// isContextError reports whether the provided error is caused by context cancellation.
func isContextError(err error) bool {
	return err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}
