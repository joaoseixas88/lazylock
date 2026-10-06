// Package fake serves the deterministic catalog the TUI tests run against, so
// they never need a network or a live Infisical. It is never linked into the
// binary except behind the -demo flag.
package fake

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/joaoseixas88/lazylock/internal/domain"
)

// Catalog is an in-memory domain.Catalog with knobs for the failure and
// latency cases a live provider produces but a fixture otherwise never would.
type Catalog struct {
	projects []domain.Project
	scopes   map[string][]domain.Scope
	secrets  map[domain.Scope][]domain.Secret

	ProjectsErr, ScopesErr, SecretsErr error
	Delay                              time.Duration

	calls atomic.Int64
}

var _ domain.Catalog = (*Catalog)(nil)

func scope(project, slug, name, path string) domain.Scope {
	return domain.Scope{ProjectID: project, EnvSlug: slug, EnvName: name, Path: path}
}

// DemoCatalog is the catalog the prototype shipped with, kept as a fixture.
func DemoCatalog() *Catalog {
	var (
		devRoot        = scope("payments", "dev", "Development", "/")
		devServices    = scope("payments", "dev", "Development", "/services")
		devServicesAPI = scope("payments", "dev", "Development", "/services/api")
		stgRoot        = scope("payments", "stg", "Staging", "/")
		stgServices    = scope("payments", "stg", "Staging", "/services")
		prodRoot       = scope("payments", "prod", "Production", "/")
		siteProd       = scope("website", "prod", "Production", "/")
	)
	return &Catalog{
		projects: []domain.Project{
			{ID: "payments", Name: "Payments API"},
			{ID: "website", Name: "Marketing Website"},
			{ID: "homelab", Name: "Homelab"},
		},
		scopes: map[string][]domain.Scope{
			"payments": {devRoot, devServices, devServicesAPI, stgRoot, stgServices, prodRoot},
			"website":  {siteProd},
			"homelab":  nil,
		},
		secrets: map[domain.Scope][]domain.Secret{
			devRoot: {
				{ID: "stripe-key", Key: "STRIPE_SECRET_KEY", Value: "sk_test_demo_123", Comment: "Test-mode key from the Stripe dashboard", Tags: []string{"payments", "stripe"}, Version: 3},
				{ID: "db-url", Key: "DATABASE_URL", Value: "postgres://demo:demo@localhost/payments", Comment: "Primary database", Version: 1},
			},
			devServices: {
				{ID: "redis-url", Key: "REDIS_URL", Value: "redis://localhost:6379/0", Version: 2},
				{ID: "tls-cert", Key: "TLS_CERT", Value: "-----BEGIN CERTIFICATE-----\nMIIBszCCAVmgAwIBAgIUDEMO\n-----END CERTIFICATE-----", Comment: "Self-signed, for local TLS only", Version: 1},
				{ID: "db-url", Key: "DATABASE_URL", Value: "postgres://demo:demo@localhost/payments", Comment: "Primary database", Version: 1, ImportedFrom: devRoot},
			},
			devServicesAPI: {
				{ID: "api-token", Key: "API_TOKEN", Value: "tok_demo_789", Version: 1},
			},
			stgRoot: {
				{ID: "stg-stripe-key", Key: "STRIPE_SECRET_KEY", Value: "sk_test_demo_123", Comment: "Test-mode key from the Stripe dashboard"},
				{ID: "stg-db-url", Key: "DATABASE_URL", Value: "postgres://demo:demo@staging/payments"},
				{ID: "stg-flags", Key: "FEATURE_FLAGS", Value: "checkout-v2"},
			},
			stgServices: {
				{ID: "stg-redis-url", Key: "REDIS_URL", Value: "redis://staging:6379/0"},
			},
			prodRoot: {
				{ID: "stripe-live-key", Key: "STRIPE_SECRET_KEY", Value: "sk_live_demo_456"},
				{ID: "signing-key", Key: "SIGNING_KEY", Hidden: true},
				{ID: "legacy-flag", Key: "LEGACY_FLAG", Value: ""},
			},
		},
	}
}

// Calls counts every method entered, so a test can assert that rendering does
// no I/O.
func (c *Catalog) Calls() int64 { return c.calls.Load() }

func (c *Catalog) enter(ctx context.Context) error {
	c.calls.Add(1)
	if c.Delay == 0 {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(c.Delay):
		return nil
	}
}

func (c *Catalog) Projects(ctx context.Context) ([]domain.Project, error) {
	if err := c.enter(ctx); err != nil {
		return nil, err
	}
	if c.ProjectsErr != nil {
		return nil, c.ProjectsErr
	}
	return append([]domain.Project(nil), c.projects...), nil
}

func (c *Catalog) Scopes(ctx context.Context, projectID string) ([]domain.Scope, error) {
	if err := c.enter(ctx); err != nil {
		return nil, err
	}
	if c.ScopesErr != nil {
		return nil, c.ScopesErr
	}
	return append([]domain.Scope(nil), c.scopes[projectID]...), nil
}

func (c *Catalog) Secrets(ctx context.Context, at domain.Scope) ([]domain.Secret, error) {
	if err := c.enter(ctx); err != nil {
		return nil, err
	}
	if c.SecretsErr != nil {
		return nil, c.SecretsErr
	}
	return append([]domain.Secret(nil), c.secrets[at]...), nil
}
