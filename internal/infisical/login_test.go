package infisical

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testOrigin = "https://infisical.example.com"

func newLogin(t *testing.T) *BrowserLogin {
	t.Helper()
	login, err := StartBrowserLogin(testOrigin, func(port int) string {
		return testOrigin + "/login?callback_port=" + strconv.Itoa(port)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { login.Close() })
	return login
}

func post(t *testing.T, login *BrowserLogin, origin, contentType, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	login.handle(rec, req)
	return rec.Result()
}

// The listener must never be reachable from the LAN.
func TestCallbackListensOnLoopbackOnly(t *testing.T) {
	login := newLogin(t)
	if addr := login.listener.Addr().String(); !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("listening on %q, want loopback", addr)
	}
	if login.Port == 0 {
		t.Fatal("no callback port was assigned")
	}
	if !strings.Contains(login.URL, "callback_port=") {
		t.Fatalf("login URL must carry the port: %q", login.URL)
	}
}

func TestCallbackAcceptsTheBrowserPayload(t *testing.T) {
	login := newLogin(t)
	resp := post(t, login, testOrigin, "application/json",
		`{"email":"someone@example.com","privateKey":"","JTWToken":"header.payload.sig"}`)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != testOrigin {
		t.Fatalf("the POST response needs valid CORS headers too, got %q", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	creds, err := login.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait() failed: %v", err)
	}
	if creds.Token != "header.payload.sig" || creds.Email != "someone@example.com" {
		t.Fatalf("credentials = %+v", creds)
	}
}

// A form POST from any site the user has open is a simple request, so an origin
// check that only omits a header is not a check at all.
func TestCallbackRejectsAnotherOrigin(t *testing.T) {
	login := newLogin(t)
	resp := post(t, login, "https://evil.example.com", "application/json", `{"JTWToken":"stolen"}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// Requiring JSON is what forces a preflight for anything cross-origin.
func TestCallbackRejectsFormContentType(t *testing.T) {
	login := newLogin(t)
	resp := post(t, login, testOrigin, "application/x-www-form-urlencoded", `JTWToken=stolen`)
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.StatusCode)
	}
}

func TestCallbackPreflight(t *testing.T) {
	login := newLogin(t)
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Access-Control-Request-Private-Network", "true")
	rec := httptest.NewRecorder()
	login.handle(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") {
		t.Fatalf("Allow-Methods = %q", got)
	}
	// Without this, Chrome blocks the public page from reaching loopback.
	if got := resp.Header.Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Fatalf("Allow-Private-Network = %q, want true", got)
	}
}

func TestCallbackRejectsEmptyToken(t *testing.T) {
	login := newLogin(t)
	resp := post(t, login, testOrigin, "application/json", `{"email":"a@b.c","JTWToken":""}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestSecondCallbackNeitherBlocksNorPanics(t *testing.T) {
	login := newLogin(t)
	body := `{"email":"a@b.c","JTWToken":"first"}`
	if resp := post(t, login, testOrigin, "application/json", body); resp.StatusCode != http.StatusOK {
		t.Fatalf("first post status = %d", resp.StatusCode)
	}
	done := make(chan struct{})
	go func() {
		post(t, login, testOrigin, "application/json", `{"email":"a@b.c","JTWToken":"second"}`)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a second callback blocked the handler")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	creds, err := login.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Token != "first" {
		t.Fatalf("token = %q, want the first payload to win", creds.Token)
	}
}

func TestWaitHonoursACancelledContext(t *testing.T) {
	login := newLogin(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	if _, err := login.Wait(ctx); err == nil {
		t.Fatal("Wait must fail on a cancelled context")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Wait took %s to notice cancellation", elapsed)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	login := newLogin(t)
	if err := login.Close(); err != nil {
		t.Fatalf("first Close() failed: %v", err)
	}
	if err := login.Close(); err != nil {
		t.Fatalf("second Close() failed: %v", err)
	}
}

// End to end over a real socket, which is what the browser actually does.
func TestCallbackOverTheRealSocket(t *testing.T) {
	login := newLogin(t)
	body := strings.NewReader(`{"email":"someone@example.com","privateKey":"","JTWToken":"real-socket"}`)
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+strconv.Itoa(login.Port)+"/", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	creds, err := login.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Token != "real-socket" {
		t.Fatalf("token = %q", creds.Token)
	}
}

func TestDecodePastedToken(t *testing.T) {
	payload := `{"email":"someone@example.com","privateKey":"","JTWToken":"pasted.token.value"}`
	encoded := base64.StdEncoding.EncodeToString([]byte(payload))

	creds, err := DecodePastedToken("  " + encoded + "\n")
	if err != nil {
		t.Fatalf("DecodePastedToken() failed: %v", err)
	}
	if creds.Token != "pasted.token.value" {
		t.Fatalf("token = %q", creds.Token)
	}

	for name, input := range map[string]string{
		"empty":      "   ",
		"not base64": "this is not base64 !!!",
		"not json":   base64.StdEncoding.EncodeToString([]byte("hello")),
		"no token":   base64.StdEncoding.EncodeToString([]byte(`{"email":"a@b.c"}`)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodePastedToken(input); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestCredentialsNeverFormatTheirToken(t *testing.T) {
	creds := Credentials{Email: "a@b.c", Token: "super-secret-token"}
	for _, rendered := range []string{creds.String(), sprint(creds), sprintPlus(creds)} {
		if strings.Contains(rendered, creds.Token) {
			t.Fatalf("formatting Credentials leaked the token: %q", rendered)
		}
	}
}

func TestOpenBrowserRefusesNonHTTPSchemes(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "javascript:alert(1)", "not a url at all"} {
		if err := OpenBrowser(raw); err == nil {
			t.Fatalf("OpenBrowser(%q) must refuse", raw)
		}
	}
}

func sprint(v any) string     { return fmt.Sprint(v) }
func sprintPlus(v any) string { return fmt.Sprintf("%+v", v) }
