package infisical

import (
	"cmp"
	"slices"

	"github.com/joaoseixas88/lazylock/internal/domain"
)

// The response shapes below mirror the Infisical server's zod schemas. Only the
// fields lazylock reads are declared; the rest are ignored on purpose.

type wireProjectList struct {
	Projects []wireProject `json:"projects"`
}

type wireProject struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Slug         string    `json:"slug"`
	Environments []wireEnv `json:"environments"`
}

type wireEnv struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// wireEnvTree is one value of the environment-folder-tree response, which is a
// JSON object keyed by environment slug. The key is ignored: each value already
// carries its own slug.
type wireEnvTree struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Slug      string       `json:"slug"`
	Position  int          `json:"position"`
	ProjectID string       `json:"projectId"`
	Folders   []wireFolder `json:"folders"`
}

type wireFolder struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	EnvID    string  `json:"envId"`
	ParentID *string `json:"parentId"`
	Path     string  `json:"path"` // built server-side: "/" at the root
}

type wireSecretList struct {
	Secrets []wireSecret `json:"secrets"`
}

type wireSecret struct {
	ID                string `json:"id"`
	SecretKey         string `json:"secretKey"`
	SecretValue       string `json:"secretValue"`
	SecretValueHidden bool   `json:"secretValueHidden"`
	SecretPath        string `json:"secretPath"`
	SecretComment     string `json:"secretComment"`
}

func mapProjects(list wireProjectList) []domain.Project {
	projects := make([]domain.Project, 0, len(list.Projects))
	for _, p := range list.Projects {
		name := p.Name
		if name == "" {
			name = p.Slug
		}
		projects = append(projects, domain.Project{ID: p.ID, Name: name})
	}
	return projects
}

// mapScopes flattens the environment x folder tree into the pane's rows.
//
// Two guarantees the Catalog contract requires: every environment gets its root
// "/" row even when the server sent no folders for it, and the order is stable
// across calls, since the cursor rides on it.
func mapScopes(projectID string, tree map[string]wireEnvTree) []domain.Scope {
	envs := make([]wireEnvTree, 0, len(tree))
	for slug, env := range tree {
		if env.Slug == "" {
			env.Slug = slug // tolerate a server that only keys by slug
		}
		envs = append(envs, env)
	}
	slices.SortFunc(envs, func(a, b wireEnvTree) int {
		if n := cmp.Compare(a.Position, b.Position); n != 0 {
			return n
		}
		return cmp.Compare(a.Slug, b.Slug)
	})

	var scopes []domain.Scope
	for _, env := range envs {
		paths := make([]string, 0, len(env.Folders)+1)
		seen := map[string]bool{}
		for _, folder := range env.Folders {
			if folder.Path == "" || seen[folder.Path] {
				continue
			}
			seen[folder.Path] = true
			paths = append(paths, folder.Path)
		}
		if !seen["/"] {
			paths = append(paths, "/")
		}
		slices.Sort(paths)

		name := env.Name
		if name == "" {
			name = env.Slug
		}
		for _, path := range paths {
			scopes = append(scopes, domain.Scope{
				ProjectID: projectID,
				EnvSlug:   env.Slug,
				EnvName:   name,
				Path:      path,
			})
		}
	}
	return scopes
}

func mapSecrets(list wireSecretList) []domain.Secret {
	secrets := make([]domain.Secret, 0, len(list.Secrets))
	for _, s := range list.Secrets {
		secret := domain.Secret{ID: s.ID, Key: s.SecretKey, Hidden: s.SecretValueHidden, Comment: s.SecretComment}
		// Defence in depth: a hidden secret must never carry a value, whatever
		// the server chose to send.
		if !s.SecretValueHidden {
			secret.Value = s.SecretValue
		}
		secrets = append(secrets, secret)
	}
	return secrets
}
