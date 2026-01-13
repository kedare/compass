// Package gcp provides Google Cloud Platform integration for instance discovery and connection
package gcp

import (
	"context"
	"fmt"
	"sort"

	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/logger"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

// Client provides access to GCP compute resources.
type Client struct {
	service *compute.Service
	cache   *cache.Cache
	project string
}

// NewClient creates a new GCP client for the specified project.
func NewClient(ctx context.Context, project string) (*Client, error) {
	logger.Log.Debug("Creating new GCP client")

	if project == "" {
		logger.Log.Debug("No project specified, attempting to get default project")
		// Try to get default project from gcloud config
		project = getDefaultProject()
		if project == "" {
			logger.Log.Error("No project specified and no default project found")

			return nil, ErrProjectRequired
		}

		logger.Log.Debugf("Using default project: %s", project)
	} else {
		logger.Log.Debugf("Using specified project: %s", project)
	}

	logger.Log.Debug("Creating compute service")

	httpClient, err := newHTTPClientWithLogging(ctx, compute.ComputeScope)
	if err != nil {
		logger.Log.Errorf("Failed to create HTTP client: %v", err)

		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	service, err := compute.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		logger.Log.Errorf("Failed to create compute service: %v", err)

		return nil, fmt.Errorf("failed to create compute service: %w", err)
	}

	logger.Log.Debug("GCP client created successfully")

	client := &Client{
		service: service,
		project: project,
	}

	c, err := getSharedCache()
	if err != nil {
		logger.Log.Warnf("Failed to initialize cache: %v", err)
	} else {
		client.cache = c
	}

	return client, nil
}

// ProjectID returns the project associated with the client.
func (c *Client) ProjectID() string {
	if c == nil {
		return ""
	}

	return c.project
}

// ListAllProjects lists all GCP projects the user has access to using Cloud Resource Manager API
func ListAllProjects(ctx context.Context) ([]string, error) {
	logger.Log.Debug("Listing all accessible GCP projects")

	httpClient, err := newHTTPClientWithLogging(ctx, cloudresourcemanager.CloudPlatformReadOnlyScope)
	if err != nil {
		logger.Log.Errorf("Failed to create HTTP client for Resource Manager: %v", err)
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	service, err := cloudresourcemanager.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		logger.Log.Errorf("Failed to create Resource Manager service: %v", err)
		return nil, fmt.Errorf("failed to create resource manager service: %w", err)
	}

	var projects []string
	pageToken := ""

	for {
		call := service.Projects.List()
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Context(ctx).Do()
		if err != nil {
			logger.Log.Errorf("Failed to list projects: %v", err)
			return nil, fmt.Errorf("failed to list projects: %w", err)
		}

		for _, project := range resp.Projects {
			if project.LifecycleState == "ACTIVE" && project.ProjectId != "" {
				projects = append(projects, project.ProjectId)
			}
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	logger.Log.Debugf("Found %d active projects", len(projects))

	// Sort projects by name for better UX
	sort.Strings(projects)

	return projects, nil
}

// RememberProject persists the client's project in the local cache when available.
func (c *Client) RememberProject() {
	if c == nil {
		return
	}

	if c.cache == nil || !cache.Enabled() {
		return
	}

	if err := c.cache.AddProject(c.project); err != nil {
		logger.Log.Warnf("Failed to remember project %s: %v", c.project, err)
	}
}
