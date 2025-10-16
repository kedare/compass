// Package ssh provides SSH connection capabilities with support for IAP tunneling
package ssh

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"cx/internal/gcp"
	"cx/internal/logger"
)

var ErrNoExternalIPAndNoIAP = errors.New("instance has no external IP and IAP is not available")

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) ConnectWithIAP(instance *gcp.Instance, project string, sshFlags []string) error {
	if !instance.CanUseIAP {
		logger.Log.Debug("IAP not available, using direct connection")

		return c.connectDirect(instance, sshFlags)
	}

	logger.Log.Debug("Using IAP tunnel for connection")

	return c.connectViaIAP(instance, project, sshFlags)
}

func (c *Client) connectViaIAP(instance *gcp.Instance, project string, sshFlags []string) error {
	// Find gcloud binary path
	gcloudPath, err := exec.LookPath("gcloud")
	if err != nil {
		logger.Log.Errorf("gcloud binary not found in PATH: %v", err)

		return fmt.Errorf("gcloud binary not found in PATH: %w", err)
	}

	logger.Log.Debugf("Found gcloud binary at: %s", gcloudPath)

	args := []string{
		"compute", "ssh",
		instance.Name,
		"--zone", instance.Zone,
		"--project", project,
		"--tunnel-through-iap",
	}

	// Add SSH flags if provided
	for _, flag := range sshFlags {
		args = append(args, "--ssh-flag="+flag)
	}

	logger.Log.Debugf("Executing gcloud command: %s %v", gcloudPath, args)
	logger.Log.Debugf("Instance details - Name: %s, Zone: %s, Status: %s, Project: %s", instance.Name, instance.Zone, instance.Status, project)
	logger.Log.Debugf("Instance IPs - Internal: %s, External: %s", instance.InternalIP, instance.ExternalIP)

	logger.Log.Info("Establishing SSH connection via IAP tunnel...")
	// Replace current process with gcloud ssh command
	return syscall.Exec(gcloudPath, append([]string{"gcloud"}, args...), os.Environ()) //nolint:gosec // G204: Required for executing gcloud binary
}

func (c *Client) connectDirect(instance *gcp.Instance, sshFlags []string) error {
	logger.Log.Debug("Attempting direct SSH connection")

	if instance.ExternalIP == "" {
		logger.Log.Error("Instance has no external IP and IAP is not available")

		return ErrNoExternalIPAndNoIAP
	}

	// Find ssh binary path
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		logger.Log.Errorf("ssh binary not found in PATH: %v", err)

		return fmt.Errorf("ssh binary not found in PATH: %w", err)
	}

	logger.Log.Debugf("Found ssh binary at: %s", sshPath)

	args := []string{
		instance.ExternalIP,
	}

	// Add SSH flags if provided
	args = append(args, sshFlags...)

	logger.Log.Debugf("Executing SSH command: %s %v", sshPath, args)
	logger.Log.Debugf("Connecting to external IP: %s", instance.ExternalIP)

	logger.Log.Info("Establishing direct SSH connection...")

	return syscall.Exec(sshPath, append([]string{"ssh"}, args...), os.Environ()) //nolint:gosec // G204: Required for executing ssh binary
}
