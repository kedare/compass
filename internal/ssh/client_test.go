package ssh

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/kedare/compass/internal/gcp"
	"github.com/stretchr/testify/require"
)

type fakeRunner struct {
	ctx  context.Context
	err  error
	name string
	args []string
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string) error {
	f.ctx = ctx
	f.name = name

	f.args = append([]string(nil), args...)

	return f.err
}

func stubLookPath(mapping map[string]string) func(string) (string, error) {
	return func(bin string) (string, error) {
		path, ok := mapping[bin]
		if !ok {
			return "", fmt.Errorf("unexpected binary lookup for %q", bin)
		}

		return path, nil
	}
}

type ctxKey string

func TestNewClient(t *testing.T) {
	client := NewClient()
	require.NotNil(t, client)
	require.NotNil(t, client.runner)
	require.NotNil(t, client.lookPath)
}

func TestConnectWithIAP_NoExternalIP(t *testing.T) {
	client := NewClient()

	instance := &gcp.Instance{
		Name:       "internal-instance",
		Zone:       "us-central1-a",
		ExternalIP: "",
		CanUseIAP:  false,
	}

	err := client.ConnectWithIAP(t.Context(), instance, "test-project", nil, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoExternalIPAndNoIAP)
}

func TestConnectWithIAP_UsesDirectCommand(t *testing.T) {
	client := NewClient()
	runner := &fakeRunner{}
	client.runner = runner
	client.lookPath = stubLookPath(map[string]string{
		"ssh": "/usr/bin/ssh",
	})

	instance := &gcp.Instance{
		Name:       "test-instance",
		Zone:       "us-central1-a",
		ExternalIP: "203.0.113.1",
		CanUseIAP:  false,
	}

	sshFlags := []string{"-i", "~/.ssh/id_rsa"}
	ctx := context.WithValue(t.Context(), ctxKey("runner"), "direct")

	err := client.ConnectWithIAP(ctx, instance, "test-project", sshFlags, nil)
	require.NoError(t, err)
	require.Equal(t, "/usr/bin/ssh", runner.name)

	expectedArgs := append([]string{instance.ExternalIP}, sshFlags...)
	require.Equal(t, expectedArgs, runner.args)
	require.Same(t, ctx, runner.ctx)
}

func TestConnectWithIAP_UsesIAPCommand(t *testing.T) {
	client := NewClient()
	runner := &fakeRunner{}
	client.runner = runner
	client.lookPath = stubLookPath(map[string]string{
		"gcloud": "/opt/bin/gcloud",
	})

	instance := &gcp.Instance{
		Name:      "iap-instance",
		Zone:      "europe-west1-b",
		CanUseIAP: true,
	}

	flags := []string{"-L 8080:localhost:8080"}
	ctx := context.WithValue(t.Context(), ctxKey("runner"), "iap")

	err := client.ConnectWithIAP(ctx, instance, "demo-project", flags, nil)
	require.NoError(t, err)
	require.Equal(t, "/opt/bin/gcloud", runner.name)

	expectedArgs := []string{
		"compute", "ssh",
		"iap-instance",
		"--zone", "europe-west1-b",
		"--project", "demo-project",
		"--tunnel-through-iap",
		"--ssh-flag=-L 8080:localhost:8080",
	}

	require.Equal(t, expectedArgs, runner.args)
	require.Same(t, ctx, runner.ctx)
}

func TestConnectWithIAP_PropagatesRunnerError(t *testing.T) {
	client := NewClient()
	runner := &fakeRunner{err: errors.New("boom")}
	client.runner = runner
	client.lookPath = stubLookPath(map[string]string{
		"gcloud": "/opt/bin/gcloud",
	})

	instance := &gcp.Instance{
		Name:      "iap-instance",
		Zone:      "us-west1-a",
		CanUseIAP: true,
	}

	err := client.ConnectWithIAP(t.Context(), instance, "demo-project", nil, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, runner.err)
}

func TestConnectWithIAP_ForcesTunnelWhenRequested(t *testing.T) {
	client := NewClient()
	runner := &fakeRunner{}
	client.runner = runner
	client.lookPath = stubLookPath(map[string]string{
		"gcloud": "/opt/bin/gcloud",
	})

	instance := &gcp.Instance{
		Name:       "dual-mode",
		Zone:       "us-central1-a",
		ExternalIP: "34.0.0.1",
		CanUseIAP:  false,
	}

	force := true
	err := client.ConnectWithIAP(t.Context(), instance, "demo-project", nil, &force)
	require.NoError(t, err)
	require.Equal(t, "/opt/bin/gcloud", runner.name)
	require.Equal(t, []string{
		"compute", "ssh",
		"dual-mode",
		"--zone", "us-central1-a",
		"--project", "demo-project",
		"--tunnel-through-iap",
	}, runner.args)
}

func TestConnectWithIAP_ForcesDirectWhenRequested(t *testing.T) {
	client := NewClient()
	runner := &fakeRunner{}
	client.runner = runner
	client.lookPath = stubLookPath(map[string]string{
		"ssh": "/usr/bin/ssh",
	})

	instance := &gcp.Instance{
		Name:       "private-only",
		Zone:       "us-central1-a",
		ExternalIP: "203.0.113.2",
		CanUseIAP:  true,
	}

	force := false
	sshFlags := []string{"-i", "~/.ssh/id_ed25519"}
	err := client.ConnectWithIAP(t.Context(), instance, "demo-project", sshFlags, &force)
	require.NoError(t, err)
	require.Equal(t, "/usr/bin/ssh", runner.name)
	require.Equal(t, append([]string{instance.ExternalIP}, sshFlags...), runner.args)
}
