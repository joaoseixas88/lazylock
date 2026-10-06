// Package fake serves the deterministic catalog the TUI tests run against, so
// they never need a network or a live Infisical. It is never linked into the
// binary except behind the -demo flag.
package fake

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joaoseixas88/lazylock/internal/domain"
)

// Catalog is an in-memory domain.Catalog and domain.Writer with knobs for the
// failure and latency cases a live provider produces but a fixture otherwise
// never would. Imports are relations between folders, as in Infisical, so a
// secret stored in one folder shows up in every folder that imports it.
type Catalog struct {
	projects []domain.Project
	scopes   map[string][]domain.Scope

	mu      sync.Mutex
	stored  map[domain.Scope][]domain.Secret
	imports map[domain.Scope][]domain.Scope

	ProjectsErr, ScopesErr, SecretsErr error
	// WriteErr fails every write. Approval turns every write into a change
	// request that applies nothing, as an Infisical approval policy does.
	WriteErr error
	Approval bool
	Delay    time.Duration

	calls atomic.Int64
}

var (
	_ domain.Catalog = (*Catalog)(nil)
	_ domain.Writer  = (*Catalog)(nil)
)

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
		imports: map[domain.Scope][]domain.Scope{
			devServices: {devRoot},
		},
		stored: map[domain.Scope][]domain.Secret{
			devRoot: {
				{ID: "stripe-key", Key: "STRIPE_SECRET_KEY", Value: "sk_test_demo_123", Comment: "Test-mode key from the Stripe dashboard", Tags: []string{"payments", "stripe"}, Version: 3},
				{ID: "db-url", Key: "DATABASE_URL", Value: "postgres://demo:demo@localhost/payments", Comment: "Primary database", Version: 1},
			},
			devServices: {
				{ID: "redis-url", Key: "REDIS_URL", Value: "redis://localhost:6379/0", Version: 2},
				{ID: "tls-cert", Key: "TLS_CERT", Value: "-----BEGIN CERTIFICATE-----\nMIIBszCCAVmgAwIBAgIUDEMO\n-----END CERTIFICATE-----", Comment: "Self-signed, for local TLS only", Version: 1},
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
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.visible(at), nil
}

// visible merges a folder's own secrets with the ones it imports, with the
// precedence Infisical uses: the folder's own first, then a later import over
// an earlier one.
func (c *Catalog) visible(at domain.Scope) []domain.Secret {
	secrets := slices.Clone(c.stored[at])
	seen := map[string]bool{}
	for _, s := range secrets {
		seen[s.Key] = true
	}
	sources := c.imports[at]
	imported := make([][]domain.Secret, len(sources))
	for i := len(sources) - 1; i >= 0; i-- {
		for _, s := range c.stored[sources[i]] {
			if seen[s.Key] {
				continue
			}
			seen[s.Key] = true
			s.ImportedFrom = sources[i]
			imported[i] = append(imported[i], s)
		}
	}
	for _, group := range imported {
		secrets = append(secrets, group...)
	}
	for i := range secrets {
		if secrets[i].Hidden {
			secrets[i].Value = ""
		}
	}
	return secrets
}

func (*Catalog) Normalize(value string) string {
	if strings.HasSuffix(value, "\n") {
		return strings.TrimSpace(value) + "\n"
	}
	return strings.TrimSpace(value)
}

// write runs change under the lock unless a knob says the write fails or
// becomes a change request.
func (c *Catalog) write(ctx context.Context, change func() error) (domain.Outcome, error) {
	if err := c.enter(ctx); err != nil {
		return domain.Outcome{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.WriteErr != nil {
		return domain.Outcome{}, c.WriteErr
	}
	if c.Approval {
		return domain.Outcome{Pending: true}, nil
	}
	return domain.Outcome{}, change()
}

func (c *Catalog) Delete(ctx context.Context, at domain.Scope, keys []string) (domain.Outcome, error) {
	return c.write(ctx, func() error {
		for _, key := range keys {
			if !slices.ContainsFunc(c.stored[at], func(s domain.Secret) bool { return s.Key == key }) {
				return fmt.Errorf("%w: secret %s not found in %s %s", domain.ErrRejected, key, at.EnvName, at.Path)
			}
		}
		c.stored[at] = slices.DeleteFunc(slices.Clone(c.stored[at]), func(s domain.Secret) bool {
			return slices.Contains(keys, s.Key)
		})
		return nil
	})
}
