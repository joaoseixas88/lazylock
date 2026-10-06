package infisical

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/joaoseixas88/lazylock/internal/domain"
)

var _ domain.Writer = (*Catalog)(nil)

// Normalize mirrors the server's own trim of every secret value: whitespace
// goes from both ends, but a value that ended in a newline keeps one.
func (*Catalog) Normalize(value string) string {
	if strings.HasSuffix(value, "\n") {
		return strings.TrimSpace(value) + "\n"
	}
	return strings.TrimSpace(value)
}

type wireScope struct {
	ProjectID   string `json:"projectId"`
	Environment string `json:"environment"`
	SecretPath  string `json:"secretPath"`
}

func scopeOf(at domain.Scope) wireScope {
	return wireScope{ProjectID: at.ProjectID, Environment: at.EnvSlug, SecretPath: at.Path}
}

type wireKey struct {
	SecretKey string `json:"secretKey"`
	Type      string `json:"type"`
}

// wireWriteReply is the envelope every write answers with: the secret or
// secrets it changed, or, under an approval policy, the change request it
// opened instead.
type wireWriteReply struct {
	Secret   json.RawMessage `json:"secret"`
	Secrets  json.RawMessage `json:"secrets"`
	Approval json.RawMessage `json:"approval"`
}

func present(raw json.RawMessage) bool { return len(raw) > 0 && string(raw) != "null" }

func (r wireWriteReply) outcome() (domain.Outcome, error) {
	switch {
	case present(r.Approval):
		return domain.Outcome{Pending: true}, nil
	case present(r.Secret), present(r.Secrets):
		return domain.Outcome{}, nil
	}
	return domain.Outcome{}, errors.New("infisical answered without the change or a change request")
}

func secretPath(key string) string { return "/v4/secrets/" + url.PathEscape(key) }

// single reports whether keys can go to the one-secret routes. A key named
// "batch" cannot: the server matches /v4/secrets/batch to its batch route
// first, so that one always goes through the batch body.
func single(keys []string) bool { return len(keys) == 1 && keys[0] != "batch" }

type wireDraft struct {
	SecretKey     string `json:"secretKey"`
	SecretValue   string `json:"secretValue"`
	SecretComment string `json:"secretComment"`
}

func (c *Catalog) Create(ctx context.Context, at domain.Scope, s domain.Draft) (domain.Outcome, error) {
	var reply wireWriteReply
	var err error
	if single([]string{s.Key}) {
		body := struct {
			wireScope
			SecretValue   string `json:"secretValue"`
			SecretComment string `json:"secretComment"`
			Type          string `json:"type"`
		}{scopeOf(at), s.Value, s.Comment, "shared"}
		err = c.client.do(ctx, http.MethodPost, secretPath(s.Key), nil, body, &reply)
	} else {
		body := struct {
			wireScope
			Secrets []wireDraft `json:"secrets"`
		}{scopeOf(at), []wireDraft{{SecretKey: s.Key, SecretValue: s.Value, SecretComment: s.Comment}}}
		err = c.client.do(ctx, http.MethodPost, "/v4/secrets/batch", nil, body, &reply)
	}
	if err != nil {
		return domain.Outcome{}, err
	}
	return reply.outcome()
}

func (c *Catalog) Delete(ctx context.Context, at domain.Scope, keys []string) (domain.Outcome, error) {
	if len(keys) == 0 {
		return domain.Outcome{}, nil
	}
	var reply wireWriteReply
	var err error
	if single(keys) {
		body := struct {
			wireScope
			Type string `json:"type"`
		}{scopeOf(at), "shared"}
		err = c.client.do(ctx, http.MethodDelete, secretPath(keys[0]), nil, body, &reply)
	} else {
		body := struct {
			wireScope
			Secrets []wireKey `json:"secrets"`
		}{wireScope: scopeOf(at)}
		for _, key := range keys {
			body.Secrets = append(body.Secrets, wireKey{SecretKey: key, Type: "shared"})
		}
		err = c.client.do(ctx, http.MethodDelete, "/v4/secrets/batch", nil, body, &reply)
	}
	if err != nil {
		return domain.Outcome{}, err
	}
	return reply.outcome()
}
