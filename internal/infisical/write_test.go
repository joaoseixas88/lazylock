package infisical

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joaoseixas88/lazylock/internal/domain"
)

type writeCall struct {
	method, path, contentType string
	body                      map[string]any
}

func writeServer(t *testing.T, status int, reply string) (*Catalog, *writeCall) {
	t.Helper()
	got := &writeCall{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.contentType = r.Method, r.URL.EscapedPath(), r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got.body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return NewCatalog(NewClient(srv.URL+"/api", WithTransport(srv.Client().Transport))), got
}

var devApp = domain.Scope{ProjectID: "proj", EnvSlug: "dev", EnvName: "Development", Path: "/app"}

func TestDeletingOneSecretSendsItsScopeAsJSON(t *testing.T) {
	catalog, got := writeServer(t, http.StatusOK, `{"secret":{"id":"1","secretKey":"API KEY"}}`)
	outcome, err := catalog.Delete(context.Background(), devApp, []string{"API KEY"})
	if err != nil || outcome.Pending {
		t.Fatalf("Delete = %+v, %v", outcome, err)
	}
	if got.method != http.MethodDelete || got.path != "/api/v4/secrets/API%20KEY" || got.contentType != "application/json" {
		t.Fatalf("request = %s %s (%s)", got.method, got.path, got.contentType)
	}
	want := map[string]any{"projectId": "proj", "environment": "dev", "secretPath": "/app", "type": "shared"}
	for k, v := range want {
		if got.body[k] != v {
			t.Fatalf("body[%s] = %v, want %v (body %v)", k, got.body[k], v, got.body)
		}
	}
}

func TestDeletingSeveralSecretsUsesTheAtomicBatch(t *testing.T) {
	catalog, got := writeServer(t, http.StatusOK, `{"secrets":[{"id":"1"},{"id":"2"}]}`)
	if _, err := catalog.Delete(context.Background(), devApp, []string{"A", "B"}); err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodDelete || got.path != "/api/v4/secrets/batch" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	items, _ := got.body["secrets"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["secretKey"] != "A" || got.body["secretPath"] != "/app" {
		t.Fatalf("body = %v", got.body)
	}
}

func TestASecretNamedBatchGoesThroughTheBatchRoute(t *testing.T) {
	catalog, got := writeServer(t, http.StatusOK, `{"secrets":[{"id":"1"}]}`)
	if _, err := catalog.Delete(context.Background(), devApp, []string{"batch"}); err != nil {
		t.Fatal(err)
	}
	if items, _ := got.body["secrets"].([]any); got.path != "/api/v4/secrets/batch" || len(items) != 1 {
		t.Fatalf("request = %s, body %v", got.path, got.body)
	}
}

func TestAnApprovalReplyMeansNothingChangedYet(t *testing.T) {
	for _, keys := range [][]string{{"A"}, {"A", "B"}} {
		catalog, _ := writeServer(t, http.StatusOK, `{"approval":{"id":"req-1","status":"open","hasMerged":false}}`)
		outcome, err := catalog.Delete(context.Background(), devApp, keys)
		if err != nil || !outcome.Pending {
			t.Fatalf("%d keys: Delete = %+v, %v; want a pending change request", len(keys), outcome, err)
		}
	}
}

func TestAReplyWithoutChangeOrApprovalIsAnError(t *testing.T) {
	catalog, _ := writeServer(t, http.StatusOK, `{}`)
	if _, err := catalog.Delete(context.Background(), devApp, []string{"A"}); err == nil {
		t.Fatal("an empty reply must not pass for success")
	}
}

func TestWriteErrorsSayWhetherTheServerChangedNothing(t *testing.T) {
	cases := []struct {
		name                   string
		status                 int
		body                   string
		rejected, unauthorized bool
	}{
		{"permission denied", 403, `{"statusCode":403,"error":"PermissionDenied","message":"You are not allowed to delete on secrets"}`, true, false},
		{"expired token", 403, `{"statusCode":403,"error":"TokenError","message":"Your token has expired. Please re-authenticate."}`, true, true},
		{"not found", 404, `{"statusCode":404,"error":"NotFound","message":"Secret not found"}`, true, false},
		{"server error", 500, `{"statusCode":500,"error":"InternalServerError","message":"boom"}`, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			catalog, _ := writeServer(t, c.status, c.body)
			_, err := catalog.Delete(context.Background(), devApp, []string{"A"})
			if errors.Is(err, domain.ErrRejected) != c.rejected || errors.Is(err, domain.ErrUnauthorized) != c.unauthorized {
				t.Fatalf("err = %v: rejected=%v unauthorized=%v", err, errors.Is(err, domain.ErrRejected), errors.Is(err, domain.ErrUnauthorized))
			}
		})
	}
}

func TestNormalizeTrimsLikeTheServer(t *testing.T) {
	for in, want := range map[string]string{
		"  value  ":        "value",
		"line1\nline2\n\n": "line1\nline2\n",
		"\tcert\r\n":       "cert\n",
		"":                 "",
	} {
		if got := (*Catalog)(nil).Normalize(in); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCreatingASecretPostsItsValueAndComment(t *testing.T) {
	catalog, got := writeServer(t, http.StatusOK, `{"secret":{"id":"9","secretKey":"NEW"}}`)
	draft := domain.Draft{Key: "NEW", Value: "line1\nline2", Comment: "why"}
	if _, err := catalog.Create(context.Background(), devApp, draft); err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.path != "/api/v4/secrets/NEW" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	want := map[string]any{"projectId": "proj", "environment": "dev", "secretPath": "/app", "secretValue": "line1\nline2", "secretComment": "why", "type": "shared"}
	for k, v := range want {
		if got.body[k] != v {
			t.Fatalf("body[%s] = %v, want %v", k, got.body[k], v)
		}
	}
}

func TestCreatingAKeyNamedBatchUsesTheBatchBody(t *testing.T) {
	catalog, got := writeServer(t, http.StatusOK, `{"secrets":[{"id":"9"}]}`)
	if _, err := catalog.Create(context.Background(), devApp, domain.Draft{Key: "batch", Value: "v"}); err != nil {
		t.Fatal(err)
	}
	items, _ := got.body["secrets"].([]any)
	if got.path != "/api/v4/secrets/batch" || len(items) != 1 || items[0].(map[string]any)["secretKey"] != "batch" {
		t.Fatalf("request = %s body = %v", got.path, got.body)
	}
}
