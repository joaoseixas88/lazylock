package infisical

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joaoseixas88/lazylock/internal/domain"
)

type recorded struct {
	path  string
	query url.Values
	auth  string
}

// serve answers every request with the given fixture and records what was asked.
func serve(t *testing.T, status int, fixture string) (*Catalog, *recorded) {
	t.Helper()
	got := &recorded{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.query, got.auth = r.URL.Path, r.URL.Query(), r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write(load(t, fixture))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL+"/api", WithTransport(srv.Client().Transport))
	client.SetToken("test-token")
	return NewCatalog(client), got
}

func load(t *testing.T, name string) []byte {
	t.Helper()
	if name == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestProjectsFiltersToSecretManager(t *testing.T) {
	catalog, got := serve(t, http.StatusOK, "projects.json")

	projects, err := catalog.Projects(context.Background())
	if err != nil {
		t.Fatalf("Projects() failed: %v", err)
	}
	if got.path != "/api/v1/projects" {
		t.Fatalf("requested %q", got.path)
	}
	if got.query.Get("type") != "secret-manager" {
		t.Fatalf("type filter = %q; without it, KMS and PKI projects show up empty", got.query.Get("type"))
	}
	if got.auth != "Bearer test-token" {
		t.Fatalf("Authorization = %q", got.auth)
	}
	if len(projects) != 2 || projects[0].Name != "Payments API" {
		t.Fatalf("Projects() = %+v", projects)
	}
}

func TestScopesFlattenTheFolderTree(t *testing.T) {
	catalog, got := serve(t, http.StatusOK, "folder-tree.json")

	scopes, err := catalog.Scopes(context.Background(), "3f1c7a10-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatalf("Scopes() failed: %v", err)
	}
	if !strings.HasSuffix(got.path, "/environment-folder-tree") {
		t.Fatalf("requested %q", got.path)
	}

	want := []domain.Scope{
		{EnvSlug: "dev", EnvName: "Development", Path: "/"},
		{EnvSlug: "dev", EnvName: "Development", Path: "/services"},
		{EnvSlug: "dev", EnvName: "Development", Path: "/services/api"},
		{EnvSlug: "prod", EnvName: "Production", Path: "/"},
		{EnvSlug: "stg", EnvName: "Staging", Path: "/"},
	}
	if len(scopes) != len(want) {
		t.Fatalf("Scopes() returned %d rows, want %d: %+v", len(scopes), len(want), scopes)
	}
	for i, w := range want {
		got := scopes[i]
		if got.EnvSlug != w.EnvSlug || got.EnvName != w.EnvName || got.Path != w.Path {
			t.Fatalf("row %d = %+v, want %+v", i, got, w)
		}
		if got.ProjectID != "3f1c7a10-0000-4000-8000-000000000001" {
			t.Fatalf("row %d lost its project id", i)
		}
	}
}

// An environment the server sent no folders for must still be reachable: the
// root is the most common place for a secret to live.
func TestScopesAlwaysOfferTheRoot(t *testing.T) {
	catalog, _ := serve(t, http.StatusOK, "folder-tree.json")
	scopes, err := catalog.Scopes(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"dev", "prod", "stg"} {
		found := false
		for _, s := range scopes {
			found = found || (s.EnvSlug == slug && s.Path == "/")
		}
		if !found {
			t.Fatalf("environment %q has no root scope", slug)
		}
	}
}

func TestScopesOrderIsStable(t *testing.T) {
	catalog, _ := serve(t, http.StatusOK, "folder-tree.json")
	first, err := catalog.Scopes(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		again, err := catalog.Scopes(context.Background(), "p")
		if err != nil {
			t.Fatal(err)
		}
		if len(again) != len(first) {
			t.Fatal("scope count changed between calls")
		}
		for i := range first {
			if again[i] != first[i] {
				t.Fatalf("row %d moved: %+v then %+v (map iteration is not an order)", i, first[i], again[i])
			}
		}
	}
}

func TestSecretsSendExactBooleansAndHonourHidden(t *testing.T) {
	catalog, got := serve(t, http.StatusOK, "secrets.json")

	at := domain.Scope{ProjectID: "proj", EnvSlug: "dev", Path: "/"}
	secrets, err := catalog.Secrets(context.Background(), at)
	if err != nil {
		t.Fatalf("Secrets() failed: %v", err)
	}
	// The server declares these as z.enum(["true","false"]); anything else 422s.
	for key, want := range map[string]string{
		"projectId": "proj", "environment": "dev", "secretPath": "/",
		"viewSecretValue": "true", "expandSecretReferences": "true",
		"recursive": "false", "includeImports": "false",
	} {
		if got.query.Get(key) != want {
			t.Fatalf("query %s = %q, want %q", key, got.query.Get(key), want)
		}
	}

	if len(secrets) != 3 {
		t.Fatalf("Secrets() = %+v", secrets)
	}
	if secrets[0].Key != "DATABASE_URL" || secrets[0].Value == "" || secrets[0].Hidden || secrets[0].Comment != "primary database" {
		t.Fatalf("readable secret mapped wrong: %+v", secrets[0])
	}
	if !secrets[1].Hidden {
		t.Fatalf("SIGNING_KEY must come back hidden: %+v", secrets[1])
	}
	if secrets[1].Value != "" {
		t.Fatalf("a hidden secret must never carry a value, got %q", secrets[1].Value)
	}
	if secrets[2].Hidden || secrets[2].Value != "" {
		t.Fatalf("an empty secret is not a hidden one: %+v", secrets[2])
	}
}

func TestUnauthorizedMapsToTheDomainSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"reqId":"req-123","statusCode":401,"message":"Unauthorized","error":"UnauthorizedError"}`))
	}))
	defer srv.Close()

	catalog := NewCatalog(NewClient(srv.URL+"/api", WithTransport(srv.Client().Transport)))
	_, err := catalog.Projects(context.Background())
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("error = %v, want domain.ErrUnauthorized", err)
	}
	if !strings.Contains(err.Error(), "req-123") {
		t.Fatalf("reqId is what makes a self-hosted failure traceable, got %q", err)
	}
}

func TestForbiddenIsUnauthorizedOnlyForATokenError(t *testing.T) {
	cases := []struct {
		name, body       string
		wantUnauthorized bool
	}{
		{"expired token", `{"reqId":"req-123","statusCode":403,"message":"Your token has expired. Please re-authenticate.","error":"TokenError"}`, true},
		{"permission denied", `{"reqId":"req-123","statusCode":403,"message":"You are not allowed to read on secrets","error":"PermissionDenied"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			catalog := NewCatalog(NewClient(srv.URL+"/api", WithTransport(srv.Client().Transport)))
			_, err := catalog.Projects(context.Background())
			if got := errors.Is(err, domain.ErrUnauthorized); got != tc.wantUnauthorized {
				t.Fatalf("errors.Is(%v, ErrUnauthorized) = %v, want %v", err, got, tc.wantUnauthorized)
			}
		})
	}
}

// A zod validation failure returns message as an ARRAY of issues. Decoding it
// as a string makes every 422 surface as an unmarshal complaint instead.
func TestValidationErrorWithArrayMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"reqId":"req-9","statusCode":422,"error":"ValidationFailure","message":[{"code":"invalid_enum_value","path":["query","recursive"],"message":"Invalid enum value. Expected 'true' | 'false', received '1'"}]}`))
	}))
	defer srv.Close()

	catalog := NewCatalog(NewClient(srv.URL+"/api", WithTransport(srv.Client().Transport)))
	_, err := catalog.Projects(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "cannot unmarshal") {
		t.Fatalf("the array message was not handled: %v", err)
	}
	if !strings.Contains(err.Error(), "Invalid enum value") {
		t.Fatalf("the real complaint should survive, got %q", err)
	}
}

// A self-hosted instance behind a proxy answers with HTML on a bad gateway.
func TestHTMLErrorBodyNeverReachesTheMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("<html><head><title>502 Bad Gateway</title></head><body><h1>502</h1></body></html>"))
	}))
	defer srv.Close()

	catalog := NewCatalog(NewClient(srv.URL+"/api", WithTransport(srv.Client().Transport)))
	_, err := catalog.Projects(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "<html") || strings.Contains(err.Error(), "<h1>") {
		t.Fatalf("raw HTML must not reach a pane: %q", err)
	}
	if !strings.Contains(err.Error(), "502") {
		t.Fatalf("the status should still be legible, got %q", err)
	}
}

// Query values carry project ids and secret paths; an error pane is the wrong
// place for them, and a transport failure stringifies the whole URL by default.
func TestTransportErrorDoesNotLeakTheQuery(t *testing.T) {
	catalog := NewCatalog(NewClient("http://127.0.0.1:1/api"))
	_, err := catalog.Secrets(context.Background(), domain.Scope{
		ProjectID: "super-secret-project", EnvSlug: "prod", Path: "/very/telling/path",
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, leaked := range []string{"super-secret-project", "very/telling/path"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("error leaked %q: %v", leaked, err)
		}
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	var reached string
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = r.Header.Get("Authorization")
		w.Write([]byte(`{"projects":[]}`))
	}))
	defer elsewhere.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/api/v1/projects", http.StatusMovedPermanently)
	}))
	defer srv.Close()

	client := NewClient(srv.URL+"/api", WithTransport(srv.Client().Transport))
	client.SetToken("test-token")
	if _, err := NewCatalog(client).Projects(context.Background()); err == nil {
		t.Fatal("a redirect must be an error, not a silently followed hop")
	}
	if reached != "" {
		t.Fatalf("the bearer token was replayed to another host: %q", reached)
	}
}

func TestContextCancellationPropagates(t *testing.T) {
	catalog, _ := serve(t, http.StatusOK, "projects.json")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := catalog.Projects(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

// Golden files are copied from a live instance, so this guards the one mistake
// that would matter: committing a real secret value.
func TestFixturesAreScrubbed(t *testing.T) {
	entries, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("no fixtures found: %v", err)
	}
	for _, path := range entries {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var list wireSecretList
		if err := json.Unmarshal(data, &list); err != nil {
			continue
		}
		for _, secret := range list.Secrets {
			if v := secret.SecretValue; v != "" && !strings.HasPrefix(v, "PLACEHOLDER") {
				t.Fatalf("%s: %s looks like a real value; scrub fixtures before committing", path, secret.SecretKey)
			}
		}
	}
}
