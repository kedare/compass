package ssh

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"cx/internal/gcp"
)

type fakeRunner struct {
	ctx  context.Context
	name string
	args []string
	err  error
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
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}

	if client.runner == nil {
		t.Fatal("expected default runner to be set")
	}

	if client.lookPath == nil {
		t.Fatal("expected default lookPath to be set")
	}
}

func TestConnectWithIAP_NoExternalIP(t *testing.T) {
	client := NewClient()

	instance := &gcp.Instance{
		Name:       "internal-instance",
		Zone:       "us-central1-a",
		ExternalIP: "",
		CanUseIAP:  false,
	}

	err := client.ConnectWithIAP(context.Background(), instance, "test-project", nil)
	if err == nil {
		t.Fatal("expected error for instance without external IP and IAP disabled")
	}

	if !errors.Is(err, ErrNoExternalIPAndNoIAP) {
		t.Fatalf("expected ErrNoExternalIPAndNoIAP, got %v", err)
	}
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
	ctx := context.WithValue(context.Background(), ctxKey("runner"), "direct")

	if err := client.ConnectWithIAP(ctx, instance, "test-project", sshFlags); err != nil {
		t.Fatalf("ConnectWithIAP returned error: %v", err)
	}

	if runner.name != "/usr/bin/ssh" {
		t.Fatalf("expected ssh to be invoked, got %q", runner.name)
	}

	expectedArgs := append([]string{instance.ExternalIP}, sshFlags...)
	if !reflect.DeepEqual(runner.args, expectedArgs) {
		t.Fatalf("unexpected args: got %v want %v", runner.args, expectedArgs)
	}

	if runner.ctx != ctx {
		t.Fatal("expected provided context to be forwarded to runner")
	}
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
	ctx := context.WithValue(context.Background(), ctxKey("runner"), "iap")

	if err := client.ConnectWithIAP(ctx, instance, "demo-project", flags); err != nil {
		t.Fatalf("ConnectWithIAP returned error: %v", err)
	}

	if runner.name != "/opt/bin/gcloud" {
		t.Fatalf("expected gcloud to be invoked, got %q", runner.name)
	}

	expectedArgs := []string{
		"compute", "ssh",
		"iap-instance",
		"--zone", "europe-west1-b",
		"--project", "demo-project",
		"--tunnel-through-iap",
		"--ssh-flag=-L 8080:localhost:8080",
	}

	if !reflect.DeepEqual(runner.args, expectedArgs) {
		t.Fatalf("unexpected args: got %v want %v", runner.args, expectedArgs)
	}

	if runner.ctx != ctx {
		t.Fatal("expected provided context to be forwarded to runner")
	}
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

	err := client.ConnectWithIAP(context.Background(), instance, "demo-project", nil)
	if err == nil {
		t.Fatal("expected error when runner fails")
	}

	if !errors.Is(err, runner.err) {
		t.Fatalf("expected runner error to propagate, got %v", err)
	}
}
