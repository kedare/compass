package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/logger"
	"github.com/kedare/compass/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

var ipLookupOutputFormat string

// lookupResult represents the result of an IP lookup operation for a single GCP client.
type lookupResult struct {
	client       *gcp.Client
	associations []gcp.IPAssociation
	err          error
}

// lookupAttemptOutcome aggregates the results from multiple lookup attempts across projects.
type lookupAttemptOutcome struct {
	results        []gcp.IPAssociation
	successClients []*gcp.Client
	hadSuccess     bool
	canceled       bool
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

var loadCacheFunc = gcp.LoadCache

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
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		logger.Log.Fatalf("Invalid IP address: %s", rawIP)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	preferredProjects := preferredProjectsForIP(parsedIP)
	clients := lookupClients(ctx, preferredProjects)
	if len(clients) == 0 && len(preferredProjects) > 0 {
		logger.Log.Debug("Cached subnet matches did not yield any clients; falling back to full discovery")
		preferredProjects = nil
		clients = lookupClients(ctx, nil)
	}

	if len(clients) == 0 {
		logger.Log.Fatalf("No projects available for lookup. Use --project to specify one explicitly.")
	}

	spin := output.NewSpinner("Searching IP associations")
	spin.Start()

	combinedResults := make([]gcp.IPAssociation, 0)
	successClientSet := make(map[*gcp.Client]struct{})
	hadSuccess := false
	canceled := false

	outcome := executeLookupAcrossClients(ctx, ip, clients, spin)
	combinedResults = append(combinedResults, outcome.results...)
	for _, client := range outcome.successClients {
		successClientSet[client] = struct{}{}
	}
	hadSuccess = hadSuccess || outcome.hadSuccess
	canceled = canceled || outcome.canceled

	if !canceled && len(outcome.results) == 0 && len(preferredProjects) > 0 {
		spin.Update("No matches from cached subnets; scanning all configured projects…")
		fallbackClients := lookupClients(ctx, nil)
		outcome = executeLookupAcrossClients(ctx, ip, fallbackClients, spin)
		combinedResults = append(combinedResults, outcome.results...)
		for _, client := range outcome.successClients {
			successClientSet[client] = struct{}{}
		}
		hadSuccess = hadSuccess || outcome.hadSuccess
		canceled = canceled || outcome.canceled
	}

	if canceled {
		spin.Fail("Lookup canceled")

		return
	}

	if !hadSuccess {
		spin.Fail("Lookup failed for all projects")
		logger.Log.Fatalf("IP lookup failed for all checked projects")
	}

	deduped := dedupeAssociations(combinedResults)

	spin.Success("Lookup complete")

	if len(deduped) == 0 {
		fmt.Printf("No resources found for IP %s\n", ip)

		return
	}

	for client := range successClientSet {
		client.RememberProject()
	}

	if err := output.DisplayIPLookupResults(deduped, ipLookupOutputFormat); err != nil {
		logger.Log.Fatalf("Failed to render lookup results: %v", err)
	}
}

// preferredProjectsForIP returns projects whose cached subnets already contain the IP.
func preferredProjectsForIP(ip net.IP) []string {
	if ip == nil {
		return nil
	}

	cacheInst, err := loadCacheFunc()
	if err != nil || cacheInst == nil {
		return nil
	}

	entries := cacheInst.FindSubnetsForIP(ip)
	if len(entries) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(entries))
	ordered := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry == nil {
			continue
		}

		projectID := strings.TrimSpace(entry.Project)
		if projectID == "" {
			continue
		}

		key := strings.ToLower(projectID)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		ordered = append(ordered, projectID)
	}

	if len(ordered) > 0 {
		logger.Log.Debugf("Cached subnet data suggests %d project(s): %v", len(ordered), ordered)
	}

	return ordered
}

// executeLookupAcrossClients scans each client and aggregates the results while updating the spinner.
// The function uses the global concurrency setting to limit the number of concurrent operations
// and the number of concurrent progress spinners displayed.
func executeLookupAcrossClients(ctx context.Context, ip string, clients []*gcp.Client, spin *output.Spinner) lookupAttemptOutcome {
	outcome := lookupAttemptOutcome{}
	if len(clients) == 0 {
		return outcome
	}

	lookupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Use global concurrency setting (limited to reasonable max if needed)
	workerCount := concurrency
	if workerCount > len(clients) {
		workerCount = len(clients)
	}

	group, groupCtx := errgroup.WithContext(lookupCtx)
	group.SetLimit(workerCount)
	progressCh := make(chan string, len(clients))
	resultCh := make(chan lookupResult, len(clients))
	waitErrCh := make(chan error, 1)
	projectSpinners := make(map[string]*output.Spinner)
	var projectMu sync.Mutex

	// Create slot channel for worker pool (controls concurrent spinners)
	slotCh := make(chan int, workerCount)
	for i := 0; i < workerCount; i++ {
		slotCh <- i
	}

	stopAllProjectSpinners := func(finalize func(*output.Spinner)) {
		projectMu.Lock()
		spinners := make([]*output.Spinner, 0, len(projectSpinners))
		for key, sp := range projectSpinners {
			if sp != nil {
				spinners = append(spinners, sp)
			}
			delete(projectSpinners, key)
		}
		projectMu.Unlock()

		for _, sp := range spinners {
			finalize(sp)
		}
	}

	for _, client := range clients {
		client := client

		group.Go(func() error {
			// Acquire a worker slot
			var slotID int
			select {
			case slotID = <-slotCh:
				defer func() { slotCh <- slotID }() // Return slot when done
			case <-groupCtx.Done():
				return groupCtx.Err()
			}

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
	results := make([]gcp.IPAssociation, 0)
	ctxDone := ctx.Done()
	total := len(clients)
	started := 0
	processed := 0

loop:
	for progressCh != nil || resultCh != nil {
		select {
		case <-ctxDone:
			outcome.canceled = true
			cancel()
			stopAllProjectSpinners(func(sp *output.Spinner) {
				sp.Fail("Lookup canceled")
			})
			break loop
		case projectID, ok := <-progressCh:
			if !ok {
				progressCh = nil
				continue
			}
			started++
			projectMu.Lock()
			projSpinner, exists := projectSpinners[projectID]
			if !exists {
				projSpinner = output.NewSpinner(fmt.Sprintf("Scanning project %s", projectID))
				projSpinner.Start()
				projectSpinners[projectID] = projSpinner
			} else {
				projSpinner.Update(fmt.Sprintf("Scanning project %s", projectID))
			}
			projectMu.Unlock()

			if spin != nil {
				spin.Update(fmt.Sprintf("Scanning project %s (%d/%d started)", projectID, started, total))
			}
		case res, ok := <-resultCh:
			if !ok {
				resultCh = nil
				continue
			}

			processed++

			projectID := res.client.ProjectID()
			projectMu.Lock()
			projSpinner := projectSpinners[projectID]
			delete(projectSpinners, projectID)
			projectMu.Unlock()

			if res.err != nil {
				if isContextError(res.err) {
					outcome.canceled = true
					cancel()
					if projSpinner != nil {
						projSpinner.Fail("Lookup canceled")
					}
					break loop
				}

				logger.Log.Warnf("Skipping project %s: %v", res.client.ProjectID(), res.err)
				if spin != nil {
					spin.Update(fmt.Sprintf("Skipped project %s (%d/%d done)", res.client.ProjectID(), processed, total))
				}
				if projSpinner != nil {
					projSpinner.Fail(fmt.Sprintf("Failed: %v", res.err))
				}

				continue
			}

			if spin != nil {
				spin.Update(fmt.Sprintf("Completed project %s (%d/%d done)", res.client.ProjectID(), processed, total))
			}

			if projSpinner != nil {
				// Just stop without showing success message to keep output clean
				projSpinner.Stop()
			}

			outcome.hadSuccess = true
			if len(res.associations) > 0 {
				results = append(results, res.associations...)
			}

			if _, seen := successSeen[res.client]; !seen {
				successSeen[res.client] = struct{}{}
				successClients = append(successClients, res.client)
			}
		}
	}

	waitErr := <-waitErrCh
	if isContextError(waitErr) || isContextError(ctx.Err()) {
		outcome.canceled = true
	} else if waitErr != nil {
		logger.Log.Warnf("Lookup workers encountered an error: %v", waitErr)
	}

	stopAllProjectSpinners(func(sp *output.Spinner) {
		sp.Stop()
	})

	outcome.results = results
	outcome.successClients = successClients

	return outcome
}

// dedupeAssociations removes duplicate associations based on key fields.
func dedupeAssociations(input []gcp.IPAssociation) []gcp.IPAssociation {
	if len(input) <= 1 {
		return input
	}

	seen := make(map[string]struct{}, len(input))
	result := make([]gcp.IPAssociation, 0, len(input))

	for _, assoc := range input {
		key := fmt.Sprintf("%s|%s|%s|%s|%s|%s", assoc.Project, assoc.Kind, assoc.Resource, assoc.Location, assoc.IPAddress, assoc.Details)
		if _, exists := seen[key]; exists {
			continue
		}

		seen[key] = struct{}{}
		result = append(result, assoc)
	}

	return result
}

// lookupClients builds unique GCP clients that will participate in the lookup.
// If limitProjects is provided, only those projects are used. Otherwise, it falls back
// to the cache or creates a default client.
func lookupClients(ctx context.Context, limitProjects []string) []*gcp.Client {
	seen := make(map[string]struct{})
	clients := make([]*gcp.Client, 0)

	addClient := func(projectID string) {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return
			}
		}

		trimmed := strings.TrimSpace(projectID)
		if trimmed != "" {
			key := strings.ToLower(trimmed)
			if _, exists := seen[key]; exists {
				return
			}
		}

		client, err := gcp.NewClient(ctx, trimmed)
		if err != nil {
			logger.Log.Warnf("Failed to initialize GCP client for project %s: %v", trimmed, err)

			return
		}

		effectiveProject := strings.ToLower(strings.TrimSpace(client.ProjectID()))
		if effectiveProject != "" {
			if _, exists := seen[effectiveProject]; exists {
				return
			}
			seen[effectiveProject] = struct{}{}
		}

		clients = append(clients, client)
	}

	if project != "" {
		addClient(project)

		return clients
	}

	if len(limitProjects) > 0 {
		for _, pid := range limitProjects {
			addClient(pid)
		}

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
