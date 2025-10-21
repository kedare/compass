package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/logger"
	"github.com/kedare/compass/internal/ssh"
	"github.com/spf13/cobra"
)

const (
	resourceTypeInstance = "instance"
	resourceTypeMIG      = "mig"
)

var (
	project      string
	zone         string
	sshFlags     []string
	resourceType string
)

var gcpCmd = &cobra.Command{
	Use:   "gcp",
	Short: "Manage GCP resources",
	Long: `Access GCP tooling including SSH connections, connectivity tests, and VPN helpers.

Use subcommands like "compass gcp ssh" to open IAP-backed SSH sessions or
"compass gcp connectivity-test" to manage Network Connectivity Tests.`,
}

var gcpSshCmd = &cobra.Command{
	Use:   "ssh [instance-name]",
	Short: "Connect to a GCP instance",
	Long: `Connect to a GCP instance or MIG using SSH with IAP tunneling.

The tool caches project and location information after first use, so subsequent
connections don't need --project or --zone flags.

Examples:
  # First connection (requires project)
  compass gcp ssh my-instance --project my-project --type instance

  # Subsequent connections (uses cached project and zone)
  compass gcp ssh my-instance

  # Auto-detect resource type (tries MIG first, then instance)
  compass gcp ssh my-resource --project my-project

  # Explicitly specify it's an instance for faster search
  compass gcp ssh my-instance --project my-project --type instance

  # Explicitly specify it's a MIG for faster search
  compass gcp ssh my-mig --project my-project --type mig

  # Specify zone to bypass cache and auto-detection
  compass gcp ssh my-instance --zone us-central1-a --project my-project

  # Combine resource type with zone for fastest search
  compass gcp ssh my-instance --type instance --zone us-central1-a --project my-project`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: gcpSSHCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		instanceName := args[0]
		logger.Log.Infof("Starting connection process for: %s", instanceName)

		// Validate resource type if specified
		if resourceType != "" && resourceType != resourceTypeInstance && resourceType != resourceTypeMIG {
			logger.Log.Fatalf("Invalid resource type '%s'. Must be 'instance' or 'mig'", resourceType)
		}

		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		// If project or resource type is not specified, try to get it from cache
		var cachedResourceType string
		if project == "" || resourceType == "" {
			logger.Log.Debug("Project or resource type not specified, checking cache")
			cache, err := gcp.LoadCache()
			if err == nil && cache != nil {
				if cachedInfo, found := cache.Get(instanceName); found {
					if project == "" {
						logger.Log.Debugf("Found cached project for resource: %s", cachedInfo.Project)
						project = cachedInfo.Project
					}
					if resourceType == "" {
						cachedResourceType = string(cachedInfo.Type)
						logger.Log.Debugf("Found cached resource type for resource: %s", cachedResourceType)
					}
				}
			}
		}

		gcpClient, err := gcp.NewClient(ctx, project)
		if err != nil {
			logger.Log.Fatalf("Failed to create GCP client: %v", err)
		}

		var instance *gcp.Instance

		// Use cached resource type if available and not explicitly specified
		effectiveResourceType := resourceType
		if effectiveResourceType == "" && cachedResourceType != "" {
			effectiveResourceType = cachedResourceType
			logger.Log.Debugf("Using cached resource type: %s", effectiveResourceType)
		}

		// Check if resource type is specified or cached
		switch effectiveResourceType {
		case resourceTypeMIG:
			logger.Log.Debug("Resource type specified as MIG, presenting instances for selection")
			instance, err = resolveInstanceFromMIG(ctx, cmd, gcpClient, instanceName, zone)
			if err != nil {
				logger.Log.Fatalf("Failed to resolve MIG instances: %v", err)
			}
		case resourceTypeInstance:
			logger.Log.Debug("Resource type specified as instance, searching instance only")
			instance, err = gcpClient.FindInstance(ctx, instanceName, zone)
			if err != nil {
				logger.Log.Fatalf("Failed to find instance: %v", err)
			}
		default:
			// Auto-detect: Try MIG first, then instance
			logger.Log.Debug("Attempting to find instance in MIG first")
			instance, err = resolveInstanceFromMIG(ctx, cmd, gcpClient, instanceName, zone)
			if err != nil {
				if errors.Is(err, gcp.ErrMIGNotFound) ||
					errors.Is(err, gcp.ErrNoInstancesInMIG) ||
					errors.Is(err, gcp.ErrNoInstancesInRegionalMIG) {
					logger.Log.Debugf("MIG lookup failed (%v), trying standalone instance", err)
					instance, err = gcpClient.FindInstance(ctx, instanceName, zone)
					if err != nil {
						logger.Log.Fatalf("Failed to find instance: %v", err)
					}

					break
				}

				logger.Log.Fatalf("Failed to list MIG instances: %v", err)

				return
			}

			if instance == nil {
				logger.Log.Debug("No MIG instance selected, attempting standalone instance")
				// If MIG not found, try to find standalone instance
				instance, err = gcpClient.FindInstance(ctx, instanceName, zone)
				if err != nil {
					logger.Log.Fatalf("Failed to find instance: %v", err)
				}
			}
		}

		logger.Log.Infof("Connecting to instance: %s of project %s in zone: %s\n", instance.Name, project, instance.Zone)

		// Connect via SSH with IAP tunnel
		sshClient := ssh.NewClient()
		if err := sshClient.ConnectWithIAP(ctx, instance, project, sshFlags); err != nil {
			logger.Log.Fatalf("Failed to connect via SSH: %v", err)
		}

		gcpClient.RememberProject()
	},
}

func resolveInstanceFromMIG(ctx context.Context, cmd *cobra.Command, client *gcp.Client, migName, location string) (*gcp.Instance, error) {
	refs, _, err := client.ListMIGInstances(ctx, migName, location)
	if err != nil {
		return nil, err
	}

	if len(refs) == 0 {
		return nil, fmt.Errorf("MIG '%s': %w", migName, gcp.ErrNoInstancesInMIG)
	}

	if len(refs) == 1 {
		selected := refs[0]
		logger.Log.Infof("MIG %s has a single instance: %s (zone %s)", migName, selected.Name, selected.Zone)

		return client.FindInstance(ctx, selected.Name, selected.Zone)
	}

	selectedRef, err := promptMIGInstanceSelection(cmd, migName, refs)
	if err != nil {
		return nil, err
	}

	logger.Log.Infof("Selected MIG instance: %s (zone %s)", selectedRef.Name, selectedRef.Zone)

	return client.FindInstance(ctx, selectedRef.Name, selectedRef.Zone)
}

func promptMIGInstanceSelection(cmd *cobra.Command, migName string, refs []gcp.ManagedInstanceRef) (gcp.ManagedInstanceRef, error) {
	reader := bufio.NewReader(cmd.InOrStdin())

	return promptMIGInstanceSelectionFromReader(reader, cmd.OutOrStdout(), migName, refs)
}

func promptMIGInstanceSelectionFromReader(reader *bufio.Reader, out io.Writer, migName string, refs []gcp.ManagedInstanceRef) (gcp.ManagedInstanceRef, error) {
	defaultIdx := defaultMIGSelectionIndex(refs)

	if _, err := fmt.Fprintf(out, "Multiple instances available in MIG %s:\n", migName); err != nil {
		return gcp.ManagedInstanceRef{}, err
	}
	for i, ref := range refs {
		marker := " "
		if i == defaultIdx {
			marker = "*"
		}

		if _, err := fmt.Fprintf(out, "  [%d]%s %s (zone: %s, status: %s)\n", i+1, marker, ref.Name, ref.Zone, ref.Status); err != nil {
			return gcp.ManagedInstanceRef{}, err
		}
	}
	if _, err := fmt.Fprintf(out, "Select instance [default %d]: ", defaultIdx+1); err != nil {
		return gcp.ManagedInstanceRef{}, err
	}

	selected := defaultIdx

	for {
		input, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				if _, writeErr := fmt.Fprintln(out); writeErr != nil {
					return gcp.ManagedInstanceRef{}, writeErr
				}

				break
			}

			return gcp.ManagedInstanceRef{}, err
		}

		input = strings.TrimSpace(input)
		if input == "" {
			if _, writeErr := fmt.Fprintln(out); writeErr != nil {
				return gcp.ManagedInstanceRef{}, writeErr
			}

			break
		}

		value, err := strconv.Atoi(input)
		if err != nil || value < 1 || value > len(refs) {
			if _, writeErr := fmt.Fprintf(out, "Invalid selection. Enter a value between 1 and %d: ", len(refs)); writeErr != nil {
				return gcp.ManagedInstanceRef{}, writeErr
			}

			continue
		}

		if _, writeErr := fmt.Fprintln(out); writeErr != nil {
			return gcp.ManagedInstanceRef{}, writeErr
		}
		selected = value - 1

		break
	}

	return refs[selected], nil
}

func defaultMIGSelectionIndex(refs []gcp.ManagedInstanceRef) int {
	for idx, ref := range refs {
		if ref.IsRunning() {
			return idx
		}
	}

	return 0
}

func init() {
	// Use PersistentFlags for project so it's inherited by subcommands like connectivity-test
	gcpCmd.PersistentFlags().StringVarP(&project, "project", "p", "", "GCP project ID")
	if err := gcpCmd.RegisterFlagCompletionFunc("project", gcpProjectCompletion); err != nil {
		logger.Log.Fatalf("Failed to register project completion: %v", err)
	}
	gcpSshCmd.Flags().StringVarP(&zone, "zone", "z", "", "GCP zone (auto-discovered if not specified)")
	gcpSshCmd.Flags().StringVarP(&resourceType, "type", "t", "", "Resource type: 'instance' or 'mig' (auto-detected if not specified)")
	gcpSshCmd.Flags().StringSliceVar(&sshFlags, "ssh-flag", []string{}, "Additional SSH flags to pass to the SSH command (can be used multiple times)")
	if err := gcpSshCmd.RegisterFlagCompletionFunc("zone", gcpSSHZoneCompletion); err != nil {
		logger.Log.Fatalf("Failed to register zone completion: %v", err)
	}

	gcpCmd.AddCommand(gcpSshCmd)
}
