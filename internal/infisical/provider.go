package infisical

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/joaoseixas88/lazylock/internal/domain"
)

// Catalog reads one Infisical instance. Read-only: nothing here writes.
type Catalog struct {
	client *Client

	mu   sync.Mutex
	orgs map[string]string // project ID to organization ID, from the last Projects call
}

var _ domain.Catalog = (*Catalog)(nil)

func NewCatalog(client *Client) *Catalog { return &Catalog{client: client} }

func (c *Catalog) Projects(ctx context.Context) ([]domain.Project, error) {
	query := url.Values{
		"includeRoles": {strconv.FormatBool(false)},
		// Without this filter a KMS, PKI or secret-scanning project shows up in
		// the pane and every drill-down into it comes back empty.
		"type": {"secret-manager"},
	}
	var list wireProjectList
	if err := c.client.get(ctx, "/v1/projects", query, &list); err != nil {
		return nil, err
	}
	orgs := make(map[string]string, len(list.Projects))
	for _, p := range list.Projects {
		orgs[p.ID] = p.OrgID
	}
	c.mu.Lock()
	c.orgs = orgs
	c.mu.Unlock()
	return mapProjects(list), nil
}

func (c *Catalog) Scopes(ctx context.Context, projectID string) ([]domain.Scope, error) {
	var tree map[string]wireEnvTree
	path := "/v1/projects/" + url.PathEscape(projectID) + "/environment-folder-tree"
	if err := c.client.get(ctx, path, nil, &tree); err != nil {
		return nil, err
	}
	return mapScopes(projectID, tree), nil
}

func (c *Catalog) Secrets(ctx context.Context, at domain.Scope) ([]domain.Secret, error) {
	// Every boolean here is a z.enum(["true","false"]) server-side: "1" or
	// "TRUE" is a 422, not a default.
	query := url.Values{
		"projectId":                {at.ProjectID},
		"environment":              {at.EnvSlug},
		"secretPath":               {at.Path},
		"viewSecretValue":          {strconv.FormatBool(true)},
		"expandSecretReferences":   {strconv.FormatBool(true)},
		"recursive":                {strconv.FormatBool(false)},
		"includeImports":           {strconv.FormatBool(true)},
		"includePersonalOverrides": {strconv.FormatBool(false)},
	}
	var list wireSecretList
	if err := c.client.get(ctx, "/v4/secrets", query, &list); err != nil {
		return nil, err
	}
	return mapSecrets(at, list), nil
}

// VerifySession proves the token is both authenticated and scoped to an
// organization, which /v1/auth/checkAuth does not: it runs with requireOrg
// false, so a token can pass it and then fail every data call.
func (c *Catalog) VerifySession(ctx context.Context) error {
	_, err := c.Projects(ctx)
	return err
}

// WebURL is the page of the Infisical web app that shows the secrets at a
// scope. It needs the project's organization, which only Projects reports.
func (c *Catalog) WebURL(at domain.Scope) (string, error) {
	c.mu.Lock()
	org := c.orgs[at.ProjectID]
	c.mu.Unlock()
	if org == "" {
		return "", errors.New("the project's organization is not known yet")
	}
	site := strings.TrimSuffix(c.client.base, "/api")
	path := "/organizations/" + url.PathEscape(org) +
		"/projects/secret-management/" + url.PathEscape(at.ProjectID) +
		"/secrets/" + url.PathEscape(at.EnvSlug)
	return site + path + "?" + url.Values{"secretPath": {at.Path}}.Encode(), nil
}
