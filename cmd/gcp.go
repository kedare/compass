package cmd

import (
	"context"

	"codeberg.org/kedare/compass/internal/gcp"
	"codeberg.org/kedare/compass/internal/logger"
	"codeberg.org/kedare/compass/internal/ssh"
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
	Use:   "gcp [instance-name]",
	Short: "Connect to a GCP instance",
	Long: `Connect to a GCP instance or MIG using SSH with IAP tunneling

The tool caches project and location information after first use,
so subsequent connections don't need --project or --zone flags.

Examples:
  # First connection (requires project)
  compass gcp my-instance --project my-project --type instance

  # Subsequent connections (uses cached project and zone)
  compass gcp my-instance

  # Auto-detect resource type (tries MIG first, then instance)
  compass gcp my-resource --project my-project

  # Explicitly specify it's an instance for faster search
  compass gcp my-instance --project my-project --type instance

  # Explicitly specify it's a MIG for faster search
  compass gcp my-mig --project my-project --type mig

  # Specify zone to bypass cache and auto-detection
  compass gcp my-instance --zone us-central1-a --project my-project

  # Combine resource type with zone for fastest search
  compass gcp my-instance --type instance --zone us-central1-a --project my-project`,
	Args: cobra.ExactArgs(1),
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
			logger.Log.Debug("Resource type specified as MIG, searching MIG only")
			instance, err = gcpClient.FindInstanceInMIG(ctx, instanceName, zone)
			if err != nil {
				logger.Log.Fatalf("Failed to find MIG: %v", err)
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
			instance, err = gcpClient.FindInstanceInMIG(ctx, instanceName, zone)
			if err != nil {
				logger.Log.Debug("MIG not found or no instances available, trying standalone instance")
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
	},
}

func init() {
	// Use PersistentFlags for project so it's inherited by subcommands like connectivity-test
	gcpCmd.PersistentFlags().StringVarP(&project, "project", "p", "", "GCP project ID")
	gcpCmd.Flags().StringVarP(&zone, "zone", "z", "", "GCP zone (auto-discovered if not specified)")
	gcpCmd.Flags().StringVarP(&resourceType, "type", "t", "", "Resource type: 'instance' or 'mig' (auto-detected if not specified)")
	gcpCmd.Flags().StringSliceVar(&sshFlags, "ssh-flag", []string{}, "Additional SSH flags to pass to the SSH command (can be used multiple times)")
}
