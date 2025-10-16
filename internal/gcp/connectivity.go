// Package gcp provides Google Cloud Platform integration for connectivity testing
package gcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cx/internal/logger"
	"google.golang.org/api/networkmanagement/v1"
	"google.golang.org/api/option"
)

// ConnectivityClient handles GCP Network Connectivity Test operations.
type ConnectivityClient struct {
	service *networkmanagement.Service
	project string
}

// ConnectivityTestConfig holds configuration for creating a connectivity test.
type ConnectivityTestConfig struct {
	Labels              map[string]string
	SourceProject       string
	DestinationZone     string
	SourceZone          string
	SourceIP            string
	SourceNetwork       string
	Name                string
	DestinationInstance string
	SourceInstance      string
	DestinationIP       string
	Description         string
	DestinationNetwork  string
	DestinationProject  string
	Protocol            string
	DestinationPort     int64
}

// ConnectivityTestResult represents the result of a connectivity test.
type ConnectivityTestResult struct {
	CreateTime                time.Time
	UpdateTime                time.Time
	Source                    *EndpointInfo
	Destination               *EndpointInfo
	ReachabilityDetails       *ReachabilityDetails
	ReturnReachabilityDetails *ReachabilityDetails
	Name                      string
	DisplayName               string
	Description               string
	Protocol                  string
	State                     string
	Result                    string
}

// EndpointInfo represents source or destination endpoint information.
type EndpointInfo struct {
	Instance    string
	IPAddress   string
	Network     string
	ProjectID   string
	CloudRegion string
	Port        int64
}

// ReachabilityDetails contains detailed information about reachability.
type ReachabilityDetails struct {
	Result     string
	VerifyTime time.Time
	Error      string
	Traces     []*Trace
}

// Trace represents a network trace.
type Trace struct {
	EndpointInfo   *EndpointInfo
	Steps          []*TraceStep
	ForwardTraceID int64
}

// TraceStep represents a single step in the network trace.
type TraceStep struct {
	Description  string
	State        string
	ProjectID    string
	Instance     string
	Firewall     string
	Route        string
	VPC          string
	LoadBalancer string
	CausesDrop   bool
}

// NewConnectivityClient creates a new connectivity test client.
func NewConnectivityClient(ctx context.Context, project string) (*ConnectivityClient, error) {
	logger.Log.Debug("Creating new GCP connectivity test client")

	if project == "" {
		project = getDefaultProject()
		if project == "" {
			return nil, ErrProjectRequired
		}

		logger.Log.Debugf("Using default project: %s", project)
	}

	service, err := networkmanagement.NewService(ctx, option.WithScopes(
		"https://www.googleapis.com/auth/cloud-platform",
	))
	if err != nil {
		logger.Log.Errorf("Failed to create network management service: %v", err)

		return nil, fmt.Errorf("failed to create network management service: %w", err)
	}

	logger.Log.Debug("Connectivity test client created successfully")

	return &ConnectivityClient{
		service: service,
		project: project,
	}, nil
}

// CreateTest creates a new connectivity test.
func (c *ConnectivityClient) CreateTest(ctx context.Context, config *ConnectivityTestConfig) (*ConnectivityTestResult, error) {
	logger.Log.Debugf("Creating connectivity test: %s", config.Name)

	test := &networkmanagement.ConnectivityTest{
		DisplayName: config.Name,
		Description: config.Description,
		Source:      &networkmanagement.Endpoint{},
		Destination: &networkmanagement.Endpoint{},
		Protocol:    config.Protocol,
		Labels:      config.Labels,
	}

	// Configure source
	if config.SourceInstance != "" {
		test.Source.Instance = fmt.Sprintf("projects/%s/zones/%s/instances/%s",
			c.getProject(config.SourceProject), config.SourceZone, config.SourceInstance)
	}

	if config.SourceIP != "" {
		test.Source.IpAddress = config.SourceIP
	}

	if config.SourceNetwork != "" {
		test.Source.Network = fmt.Sprintf("projects/%s/global/networks/%s",
			c.getProject(config.SourceProject), config.SourceNetwork)
	}

	// Configure destination
	if config.DestinationInstance != "" {
		test.Destination.Instance = fmt.Sprintf("projects/%s/zones/%s/instances/%s",
			c.getProject(config.DestinationProject), config.DestinationZone, config.DestinationInstance)
	}

	if config.DestinationIP != "" {
		test.Destination.IpAddress = config.DestinationIP
	}

	if config.DestinationPort > 0 {
		test.Destination.Port = config.DestinationPort
	}

	if config.DestinationNetwork != "" {
		test.Destination.Network = fmt.Sprintf("projects/%s/global/networks/%s",
			c.getProject(config.DestinationProject), config.DestinationNetwork)
	}

	parent := fmt.Sprintf("projects/%s/locations/global", c.project)

	op, err := c.service.Projects.Locations.Global.ConnectivityTests.Create(parent, test).
		TestId(config.Name).
		Context(ctx).
		Do()
	if err != nil {
		logger.Log.Errorf("Failed to create connectivity test: %v", err)

		return nil, fmt.Errorf("failed to create connectivity test: %w", err)
	}

	logger.Log.Debugf("Connectivity test creation initiated: %s", op.Name)

	// Wait for the operation to complete
	if err := c.waitForOperation(ctx, op.Name); err != nil {
		return nil, fmt.Errorf("failed to wait for test creation: %w", err)
	}

	// Get the created test
	return c.GetTest(ctx, config.Name)
}

// GetTest retrieves a connectivity test and its results.
func (c *ConnectivityClient) GetTest(ctx context.Context, testName string) (*ConnectivityTestResult, error) {
	logger.Log.Debugf("Getting connectivity test: %s", testName)

	name := fmt.Sprintf("projects/%s/locations/global/connectivityTests/%s", c.project, testName)

	test, err := c.service.Projects.Locations.Global.ConnectivityTests.Get(name).Context(ctx).Do()
	if err != nil {
		logger.Log.Errorf("Failed to get connectivity test: %v", err)

		return nil, fmt.Errorf("failed to get connectivity test: %w", err)
	}

	return c.convertTestResult(test), nil
}

// RunTest reruns an existing connectivity test.
func (c *ConnectivityClient) RunTest(ctx context.Context, testName string) (*ConnectivityTestResult, error) {
	logger.Log.Debugf("Running connectivity test: %s", testName)

	name := fmt.Sprintf("projects/%s/locations/global/connectivityTests/%s", c.project, testName)

	op, err := c.service.Projects.Locations.Global.ConnectivityTests.Rerun(name, &networkmanagement.RerunConnectivityTestRequest{}).
		Context(ctx).
		Do()
	if err != nil {
		logger.Log.Errorf("Failed to run connectivity test: %v", err)

		return nil, fmt.Errorf("failed to run connectivity test: %w", err)
	}

	logger.Log.Debugf("Connectivity test run initiated: %s", op.Name)

	// Wait for the operation to complete
	if err := c.waitForOperation(ctx, op.Name); err != nil {
		return nil, fmt.Errorf("failed to wait for test run: %w", err)
	}

	// Get the updated test results
	return c.GetTest(ctx, testName)
}

// ListTests lists all connectivity tests in the project.
func (c *ConnectivityClient) ListTests(ctx context.Context, filter string) ([]*ConnectivityTestResult, error) {
	logger.Log.Debug("Listing connectivity tests")

	parent := fmt.Sprintf("projects/%s/locations/global", c.project)
	call := c.service.Projects.Locations.Global.ConnectivityTests.List(parent).Context(ctx)

	if filter != "" {
		call = call.Filter(filter)
	}

	var results []*ConnectivityTestResult

	err := call.Pages(ctx, func(page *networkmanagement.ListConnectivityTestsResponse) error {
		for _, test := range page.Resources {
			results = append(results, c.convertTestResult(test))
		}

		return nil
	})
	if err != nil {
		logger.Log.Errorf("Failed to list connectivity tests: %v", err)

		return nil, fmt.Errorf("failed to list connectivity tests: %w", err)
	}

	logger.Log.Debugf("Found %d connectivity tests", len(results))

	return results, nil
}

// DeleteTest deletes a connectivity test.
func (c *ConnectivityClient) DeleteTest(ctx context.Context, testName string) error {
	logger.Log.Debugf("Deleting connectivity test: %s", testName)

	name := fmt.Sprintf("projects/%s/locations/global/connectivityTests/%s", c.project, testName)

	op, err := c.service.Projects.Locations.Global.ConnectivityTests.Delete(name).Context(ctx).Do()
	if err != nil {
		logger.Log.Errorf("Failed to delete connectivity test: %v", err)

		return fmt.Errorf("failed to delete connectivity test: %w", err)
	}

	logger.Log.Debugf("Connectivity test deletion initiated: %s", op.Name)

	// Wait for the operation to complete
	return c.waitForOperation(ctx, op.Name)
}

// waitForOperation waits for a long-running operation to complete.
func (c *ConnectivityClient) waitForOperation(ctx context.Context, opName string) error {
	logger.Log.Debugf("Waiting for operation: %s", opName)

	maxAttempts := 60 // 5 minutes with 5 second intervals
	for i := range maxAttempts {
		op, err := c.service.Projects.Locations.Global.Operations.Get(opName).Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("failed to get operation status: %w", err)
		}

		if op.Done {
			if op.Error != nil {
				return fmt.Errorf("operation failed: %s", op.Error.Message)
			}

			logger.Log.Debug("Operation completed successfully")

			return nil
		}

		logger.Log.Tracef("Operation still running (attempt %d/%d)", i+1, maxAttempts)
		time.Sleep(5 * time.Second)
	}

	return errors.New("operation timed out after 5 minutes")
}

// convertTestResult converts API test result to our internal format.
func (c *ConnectivityClient) convertTestResult(test *networkmanagement.ConnectivityTest) *ConnectivityTestResult {
	result := &ConnectivityTestResult{
		Name:        extractTestName(test.Name),
		DisplayName: test.DisplayName,
		Description: test.Description,
		Protocol:    test.Protocol,
		Source:      c.convertEndpoint(test.Source),
		Destination: c.convertEndpoint(test.Destination),
	}

	if test.ReachabilityDetails != nil {
		result.ReachabilityDetails = c.convertReachabilityDetails(test.ReachabilityDetails)
	}

	if test.ReturnReachabilityDetails != nil {
		result.ReturnReachabilityDetails = c.convertReachabilityDetails(test.ReturnReachabilityDetails)
	}

	if test.CreateTime != "" {
		if t, err := time.Parse(time.RFC3339, test.CreateTime); err == nil {
			result.CreateTime = t
		}
	}

	if test.UpdateTime != "" {
		if t, err := time.Parse(time.RFC3339, test.UpdateTime); err == nil {
			result.UpdateTime = t
		}
	}

	return result
}

// convertReachabilityDetails converts API reachability details to our internal format.
func (c *ConnectivityClient) convertReachabilityDetails(details *networkmanagement.ReachabilityDetails) *ReachabilityDetails {
	if details == nil {
		return nil
	}

	errMsg := ""
	if details.Error != nil {
		errMsg = details.Error.Message
	}

	result := &ReachabilityDetails{
		Result: details.Result,
		Error:  errMsg,
	}

	if details.VerifyTime != "" {
		if t, err := time.Parse(time.RFC3339, details.VerifyTime); err == nil {
			result.VerifyTime = t
		}
	}

	// Convert traces
	for _, trace := range details.Traces {
		t := &Trace{
			Steps:          make([]*TraceStep, 0),
			ForwardTraceID: trace.ForwardTraceId,
		}
		if trace.EndpointInfo != nil {
			t.EndpointInfo = c.convertEndpointInfo(trace.EndpointInfo)
		}

		for _, step := range trace.Steps {
			t.Steps = append(t.Steps, c.convertTraceStep(step))
		}

		result.Traces = append(result.Traces, t)
	}

	return result
}

// convertEndpoint converts API endpoint to our internal format.
func (c *ConnectivityClient) convertEndpoint(endpoint *networkmanagement.Endpoint) *EndpointInfo {
	if endpoint == nil {
		return nil
	}

	return &EndpointInfo{
		Instance:  endpoint.Instance,
		IPAddress: endpoint.IpAddress,
		Port:      endpoint.Port,
		Network:   endpoint.Network,
		ProjectID: endpoint.ProjectId,
	}
}

// convertEndpointInfo converts API EndpointInfo to our internal format.
func (c *ConnectivityClient) convertEndpointInfo(endpointInfo *networkmanagement.EndpointInfo) *EndpointInfo {
	if endpointInfo == nil {
		return nil
	}

	return &EndpointInfo{
		IPAddress: endpointInfo.DestinationIp,
		Port:      endpointInfo.DestinationPort,
		Network:   endpointInfo.DestinationNetworkUri,
	}
}

// convertTraceStep converts API trace step to our internal format.
func (c *ConnectivityClient) convertTraceStep(step *networkmanagement.Step) *TraceStep {
	if step == nil {
		return nil
	}

	traceStep := &TraceStep{
		Description: step.Description,
		State:       step.State,
		CausesDrop:  step.CausesDrop,
		ProjectID:   step.ProjectId,
	}

	// Extract resource information
	if step.Instance != nil {
		traceStep.Instance = step.Instance.DisplayName
	}

	if step.Firewall != nil {
		traceStep.Firewall = step.Firewall.DisplayName
	}

	if step.Route != nil {
		traceStep.Route = step.Route.DisplayName
	}

	if step.Network != nil {
		traceStep.VPC = step.Network.DisplayName
	}

	if step.LoadBalancer != nil {
		traceStep.LoadBalancer = step.LoadBalancer.LoadBalancerType
	}

	return traceStep
}

// getProject returns the project to use (config or default).
func (c *ConnectivityClient) getProject(configProject string) string {
	if configProject != "" {
		return configProject
	}

	return c.project
}

// extractTestName extracts the test name from the full resource name.
func extractTestName(fullName string) string {
	// Full name format: projects/{project}/locations/global/connectivityTests/{test}
	parts := splitResourceName(fullName)
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return fullName
}

// splitResourceName splits a resource name by '/'.
func splitResourceName(name string) []string {
	return strings.Split(name, "/")
}
