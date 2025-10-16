// Package ssh provides SSH connection capabilities with support for IAP tunneling
package ssh

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"cx/internal/gcp"
	"cx/internal/logger"
)

var ErrNoExternalIPAndNoIAP = errors.New("instance has no external IP and IAP is not available")

type commandRunner interface {
	Run(ctx context.Context, name string, args []string) error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args []string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

type Client struct {
	runner   commandRunner
	lookPath func(string) (string, error)
}

func NewClient() *Client {
	return &Client{
		runner:   execRunner{},
		lookPath: exec.LookPath,
	}
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
	gcloudPath, err := c.lookPath("gcloud")
	if err != nil {
		logger.Log.Errorf("gcloud binary not found in PATH: %v", err)

		return fmt.Errorf("gcloud binary not found in PATH: %w", err)
	}

	cmdArgs := []string{
		"compute", "ssh",
		instance.Name,
		"--zone", instance.Zone,
		"--project", project,
		"--tunnel-through-iap",
	}

	for _, flag := range sshFlags {
		cmdArgs = append(cmdArgs, "--ssh-flag="+flag)
	}

	logger.Log.Debugf("Executing gcloud command: %s %v", gcloudPath, cmdArgs)
	logger.Log.Debugf("Instance details - Name: %s, Zone: %s, Status: %s, Project: %s", instance.Name, instance.Zone, instance.Status, project)
	logger.Log.Debugf("Instance IPs - Internal: %s, External: %s", instance.InternalIP, instance.ExternalIP)

	logger.Log.Info("Establishing SSH connection via IAP tunnel...")

	if err := c.runner.Run(context.Background(), gcloudPath, cmdArgs); err != nil {
		return fmt.Errorf("gcloud ssh command failed: %w", err)
	}

	return nil
}

func (c *Client) connectDirect(instance *gcp.Instance, sshFlags []string) error {
	logger.Log.Debug("Attempting direct SSH connection")

	if instance.ExternalIP == "" {
		logger.Log.Error("Instance has no external IP and IAP is not available")

		return ErrNoExternalIPAndNoIAP
	}

	sshPath, err := c.lookPath("ssh")
	if err != nil {
		logger.Log.Errorf("ssh binary not found in PATH: %v", err)

		return fmt.Errorf("ssh binary not found in PATH: %w", err)
	}

	cmdArgs := []string{
		instance.ExternalIP,
	}

	cmdArgs = append(cmdArgs, sshFlags...)

	logger.Log.Debugf("Executing SSH command: %s %v", sshPath, cmdArgs)
	logger.Log.Debugf("Connecting to external IP: %s", instance.ExternalIP)

	logger.Log.Info("Establishing direct SSH connection...")

	if err := c.runner.Run(context.Background(), sshPath, cmdArgs); err != nil {
		return fmt.Errorf("ssh command failed: %w", err)
	}

	return nil
}
