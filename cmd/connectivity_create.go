package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"codeberg.org/kedare/compass/internal/gcp"
	"codeberg.org/kedare/compass/internal/logger"
	"codeberg.org/kedare/compass/internal/output"
	"github.com/spf13/cobra"
)

var (
	createTestName            string
	createDescription         string
	createSourceInstance      string
	createSourceZone          string
	createSourceType          string
	createSourceIP            string
	createSourceNetwork       string
	createSourceProject       string
	createDestinationInstance string
	createDestinationZone     string
	createDestinationType     string
	createDestinationIP       string
	createDestinationPort     int64
	createDestinationNetwork  string
	createDestinationProject  string
	createProtocol            string
	createLabels              string
	createOutputFormat        string
)

var createCmd = &cobra.Command{
	Use:     "create <test-name>",
	Aliases: []string{"c"},
	Short:   "Create a new connectivity test",
	Long: `Create a new Google Cloud Network Connectivity Test to validate network paths.

You can specify source and destination by instance name (with automatic discovery) or by IP address.
When using instance names, the tool will automatically resolve their internal IP addresses.

Examples:
  # Instance-to-instance test
  compass gcp connectivity-test create web-to-db \
    --project my-project \
    --source-instance web-server-1 \
    --destination-instance db-server-1 \
    --destination-port 5432

  # IP-based test
  compass gcp connectivity-test create ip-test \
    --project my-project \
    --source-ip 10.128.0.5 \
    --destination-ip 10.138.0.10 \
    --destination-port 443 \
    --protocol TCP

  # MIG to instance test
  compass gcp connectivity-test create mig-test \
    --project my-project \
    --source-instance api-mig \
    --source-type mig \
    --destination-instance backend \
    --destination-port 8080`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		createTestName = args[0]
		runCreateTest(cmd.Context())
	},
}

func runCreateTest(ctx context.Context) {
	logger.Log.Infof("Creating connectivity test: %s", createTestName)

	// Validate inputs
	if err := validateCreateInputs(); err != nil {
		logger.Log.Fatalf("Invalid configuration: %v", err)
	}

	// Create GCP client
	gcpClient, err := gcp.NewClient(ctx, project)
	if err != nil {
		logger.Log.Fatalf("Failed to create GCP client: %v", err)
	}

	// Build config
	config := &gcp.ConnectivityTestConfig{
		Name:               createTestName,
		Description:        createDescription,
		Protocol:           createProtocol,
		SourceProject:      createSourceProject,
		DestinationProject: createDestinationProject,
		Labels:             parseLabels(createLabels),
	}

	// Resolve source
	if err := resolveSource(ctx, gcpClient, config); err != nil {
		logger.Log.Fatalf("Failed to resolve source: %v", err)
	}

	// Resolve destination
	if err := resolveDestination(ctx, gcpClient, config); err != nil {
		logger.Log.Fatalf("Failed to resolve destination: %v", err)
	}

	config.DestinationPort = createDestinationPort

	// Create connectivity test client
	connClient, err := gcp.NewConnectivityClient(ctx, project)
	if err != nil {
		logger.Log.Fatalf("Failed to create connectivity client: %v", err)
	}

	// Create the test
	logger.Log.Info("Creating connectivity test (this may take a minute)...")

	spin := output.NewSpinner("Creating connectivity test")
	spin.Start()

	result, err := connClient.CreateTest(ctx, config)
	if err != nil {
		spin.Fail("Failed to create connectivity test")
		logger.Log.Fatalf("Failed to create connectivity test: %v", err)
	}

	spin.Success("Connectivity test created")

	logger.Log.Info("Connectivity test created successfully")

	// Display results
	if err := output.DisplayConnectivityTestResult(result, createOutputFormat); err != nil {
		logger.Log.Errorf("Failed to display results: %v", err)
	}
}

func validateCreateInputs() error {
	// Must have either source instance or source IP
	if createSourceInstance == "" && createSourceIP == "" {
		return errors.New("either --source-instance or --source-ip must be specified")
	}

	// Must have either destination instance or destination IP
	if createDestinationInstance == "" && createDestinationIP == "" {
		return errors.New("either --destination-instance or --destination-ip must be specified")
	}

	// If source instance is specified but no zone, we'll auto-discover
	// If source IP is specified, we may need a network
	if createSourceIP != "" && createSourceNetwork == "" && createSourceInstance == "" {
		logger.Log.Warn("Source IP specified without network or instance - may need --source-network")
	}

	// Validate protocol
	validProtocols := []string{"TCP", "UDP", "ICMP", "ESP", "AH", "SCTP", "GRE"}

	if createProtocol != "" {
		createProtocol = strings.ToUpper(createProtocol)
		valid := false

		for _, p := range validProtocols {
			if createProtocol == p {
				valid = true

				break
			}
		}

		if !valid {
			return fmt.Errorf("invalid protocol '%s', must be one of: %s", createProtocol, strings.Join(validProtocols, ", "))
		}
	} else {
		createProtocol = "TCP" // Default
	}

	return nil
}

func resolveSource(ctx context.Context, gcpClient *gcp.Client, config *gcp.ConnectivityTestConfig) error {
	if createSourceInstance != "" {
		logger.Log.Debugf("Resolving source instance: %s", createSourceInstance)

		var instance *gcp.Instance
		var err error

		// Check if it's a MIG or instance
		switch createSourceType {
		case resourceTypeMIG:
			instance, err = gcpClient.FindInstanceInMIG(ctx, createSourceInstance, createSourceZone)
		case resourceTypeInstance:
			instance, err = gcpClient.FindInstance(ctx, createSourceInstance, createSourceZone)
		default:
			// Auto-detect: try MIG first, then instance
			instance, err = gcpClient.FindInstanceInMIG(ctx, createSourceInstance, createSourceZone)
			if err != nil {
				instance, err = gcpClient.FindInstance(ctx, createSourceInstance, createSourceZone)
			}
		}

		if err != nil {
			return fmt.Errorf("failed to find source instance: %w", err)
		}

		logger.Log.Infof("Found source instance: %s in zone %s (IP: %s)", instance.Name, instance.Zone, instance.InternalIP)

		config.SourceInstance = instance.Name
		config.SourceZone = instance.Zone

		if createSourceIP == "" {
			config.SourceIP = instance.InternalIP
		} else {
			config.SourceIP = createSourceIP
		}
	} else {
		// Using source IP directly
		config.SourceIP = createSourceIP
		config.SourceNetwork = createSourceNetwork
	}

	return nil
}

func resolveDestination(ctx context.Context, gcpClient *gcp.Client, config *gcp.ConnectivityTestConfig) error {
	if createDestinationInstance != "" {
		logger.Log.Debugf("Resolving destination instance: %s", createDestinationInstance)

		var instance *gcp.Instance
		var err error

		// Check if it's a MIG or instance
		switch createDestinationType {
		case resourceTypeMIG:
			instance, err = gcpClient.FindInstanceInMIG(ctx, createDestinationInstance, createDestinationZone)
		case resourceTypeInstance:
			instance, err = gcpClient.FindInstance(ctx, createDestinationInstance, createDestinationZone)
		default:
			// Auto-detect: try MIG first, then instance
			instance, err = gcpClient.FindInstanceInMIG(ctx, createDestinationInstance, createDestinationZone)
			if err != nil {
				instance, err = gcpClient.FindInstance(ctx, createDestinationInstance, createDestinationZone)
			}
		}

		if err != nil {
			return fmt.Errorf("failed to find destination instance: %w", err)
		}

		logger.Log.Infof("Found destination instance: %s in zone %s (IP: %s)", instance.Name, instance.Zone, instance.InternalIP)

		config.DestinationInstance = instance.Name
		config.DestinationZone = instance.Zone

		if createDestinationIP == "" {
			config.DestinationIP = instance.InternalIP
		} else {
			config.DestinationIP = createDestinationIP
		}
	} else {
		// Using destination IP directly
		config.DestinationIP = createDestinationIP
		config.DestinationNetwork = createDestinationNetwork
	}

	return nil
}

func parseLabels(labelStr string) map[string]string {
	if labelStr == "" {
		return nil
	}

	labels := make(map[string]string)

	pairs := strings.Split(labelStr, ",")
	for _, pair := range pairs {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) == 2 {
			labels[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}

	return labels
}

func init() {
	connectivityTestCmd.AddCommand(createCmd)

	// Source flags
	createCmd.Flags().StringVar(&createSourceInstance, "source-instance", "", "Source instance name")
	createCmd.Flags().StringVar(&createSourceZone, "source-zone", "", "Source zone (auto-discovered if not specified)")
	createCmd.Flags().StringVarP(&createSourceType, "source-type", "s", "", "Source type: 'instance' or 'mig' (auto-detected)")
	createCmd.Flags().StringVar(&createSourceIP, "source-ip", "", "Source IP address")
	createCmd.Flags().StringVar(&createSourceNetwork, "source-network", "", "Source VPC network name")
	createCmd.Flags().StringVar(&createSourceProject, "source-project", "", "Source project (if different from main project)")

	// Destination flags
	createCmd.Flags().StringVar(&createDestinationInstance, "destination-instance", "", "Destination instance name")
	createCmd.Flags().StringVar(&createDestinationZone, "destination-zone", "", "Destination zone (auto-discovered if not specified)")
	createCmd.Flags().StringVarP(&createDestinationType, "destination-type", "d", "", "Destination type: 'instance' or 'mig'")
	createCmd.Flags().StringVar(&createDestinationIP, "destination-ip", "", "Destination IP address")
	createCmd.Flags().Int64Var(&createDestinationPort, "destination-port", 0, "Destination port number")
	createCmd.Flags().StringVar(&createDestinationNetwork, "destination-network", "", "Destination VPC network")
	createCmd.Flags().StringVar(&createDestinationProject, "destination-project", "", "Destination project (if different)")

	// Test configuration flags
	createCmd.Flags().StringVar(&createProtocol, "protocol", "TCP", "Protocol (TCP, UDP, ICMP, ESP, AH, SCTP, GRE)")
	createCmd.Flags().StringVar(&createDescription, "description", "", "Test description")
	createCmd.Flags().StringVar(&createLabels, "labels", "", "Labels in format 'key1=value1,key2=value2'")

	// Output flags
	createCmd.Flags().StringVarP(&createOutputFormat, "output", "o",
		output.DefaultFormat("text", []string{"text", "json", "detailed"}),
		"Output format: text, json, detailed")
}
