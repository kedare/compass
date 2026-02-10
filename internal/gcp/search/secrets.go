package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// SecretClientFactory creates a Secret Manager client scoped to a project.
type SecretClientFactory func(ctx context.Context, project string) (SecretClient, error)

// SecretClient exposes the subset of gcp.Client used by the secret searcher.
type SecretClient interface {
	ListSecrets(ctx context.Context) ([]*gcp.Secret, error)
}

// SecretProvider searches Secret Manager secrets for query matches.
type SecretProvider struct {
	NewClient SecretClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *SecretProvider) Kind() ResourceKind {
	return KindSecret
}

// Search implements the Provider interface.
func (p *SecretProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	secrets, err := client.ListSecrets(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(secrets))
	for _, secret := range secrets {
		if secret == nil || !query.MatchesAny(secret.Name, secret.Replication) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindSecret,
			Name:     secret.Name,
			Project:  project,
			Location: "global",
			Details:  secretDetails(secret),
		})
	}

	return matches, nil
}

// secretDetails extracts display metadata for a secret.
func secretDetails(secret *gcp.Secret) map[string]string {
	details := make(map[string]string)

	if secret.Replication != "" {
		details["replication"] = secret.Replication
	}

	if secret.VersionCount > 0 {
		details["versions"] = fmt.Sprintf("%d", secret.VersionCount)
	}

	return details
}
