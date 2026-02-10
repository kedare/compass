package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestSecretProviderReturnsMatches(t *testing.T) {
	client := &fakeSecretClient{secrets: []*gcp.Secret{
		{Name: "prod-api-key", Replication: "AUTOMATIC", VersionCount: 3},
		{Name: "dev-secret", Replication: "USER_MANAGED", VersionCount: 1},
	}}

	provider := &SecretProvider{NewClient: func(ctx context.Context, project string) (SecretClient, error) {
		if project != "proj-a" {
			t.Fatalf("unexpected project %s", project)
		}
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "prod"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Name != "prod-api-key" {
		t.Fatalf("expected prod-api-key, got %s", results[0].Name)
	}

	if results[0].Type != KindSecret {
		t.Fatalf("expected type %s, got %s", KindSecret, results[0].Type)
	}

	if results[0].Location != "global" {
		t.Fatalf("expected location global, got %s", results[0].Location)
	}

	if results[0].Details["replication"] != "AUTOMATIC" {
		t.Fatalf("expected replication AUTOMATIC, got %s", results[0].Details["replication"])
	}

	if results[0].Details["versions"] != "3" {
		t.Fatalf("expected versions 3, got %s", results[0].Details["versions"])
	}
}

func TestSecretProviderMatchesByReplication(t *testing.T) {
	client := &fakeSecretClient{secrets: []*gcp.Secret{
		{Name: "auto-secret", Replication: "AUTOMATIC", VersionCount: 1},
		{Name: "managed-secret", Replication: "USER_MANAGED", VersionCount: 2},
	}}

	provider := &SecretProvider{NewClient: func(ctx context.Context, project string) (SecretClient, error) {
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "USER_MANAGED"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "managed-secret" {
		t.Fatalf("expected 1 result for replication search, got %d", len(results))
	}
}

func TestSecretProviderPropagatesErrors(t *testing.T) {
	provider := &SecretProvider{NewClient: func(context.Context, string) (SecretClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &SecretProvider{NewClient: func(context.Context, string) (SecretClient, error) {
		return &fakeSecretClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestSecretProviderNilProvider(t *testing.T) {
	var provider *SecretProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

type fakeSecretClient struct {
	secrets []*gcp.Secret
	err     error
}

func (f *fakeSecretClient) ListSecrets(context.Context) ([]*gcp.Secret, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.secrets, nil
}
