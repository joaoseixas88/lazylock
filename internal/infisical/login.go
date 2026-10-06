package infisical

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// LoginTimeout matches the Infisical CLI: the callback listener does not stay
// open longer than this.
const LoginTimeout = 10 * time.Minute

// Credentials is what the Infisical web app POSTs back once the user has logged
// in. The field name JTWToken is the real wire spelling, typo and all.
//
// PrivateKey is always empty on current Infisical: end-to-end encryption is
// gone, so there is no client-side crypto and nothing here to carry further.
type Credentials struct {
	Email      string `json:"email"`
	PrivateKey string `json:"privateKey"`
	Token      string `json:"JTWToken"`
}

// String keeps the token out of anything that formats the struct.
func (c Credentials) String() string { return "infisical.Credentials{redacted}" }

// BrowserLogin is a one-shot listener that catches the token the Infisical web
// app hands back.
type BrowserLogin struct {
	Port   int
	URL    string
	origin string

	listener net.Listener
	server   *http.Server
	result   chan Credentials
	closeOne sync.Once
}

// StartBrowserLogin binds the callback port and starts serving. It binds
// 127.0.0.1 explicitly: ":0" would be reachable from the LAN, and "localhost"
// can resolve to ::1 while the Infisical frontend hardcodes 127.0.0.1.
func StartBrowserLogin(origin string, loginURL func(port int) string) (*BrowserLogin, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("open callback listener: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	login := &BrowserLogin{
		Port:     port,
		URL:      loginURL(port),
		origin:   origin,
		listener: listener,
		result:   make(chan Credentials, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", login.handle)
	login.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	go login.server.Serve(listener)
	return login, nil
}

// handle is deliberately stricter than the Infisical CLI's, which only omits
// the CORS header on an origin mismatch and lets the handler run anyway. A
// plain form POST from any site the user has open is a simple request and would
// reach that. Requiring application/json forces a preflight for anything
// cross-origin, and the preflight is only answered for our own origin.
func (b *BrowserLogin) handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != b.origin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", b.origin)
	h.Set("Access-Control-Allow-Credentials", "true")
	h.Set("Vary", "Origin")

	if r.Method == http.MethodOptions {
		h.Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type")
		h.Set("Access-Control-Max-Age", "600")
		// Chrome gates a public page reaching loopback behind this.
		if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
			h.Set("Access-Control-Allow-Private-Network", "true")
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct != "application/json" {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}

	var creds Credentials
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&creds); err != nil || creds.Token == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	// First payload wins; a later one is dropped rather than blocking.
	select {
	case b.result <- creds:
	default:
	}
}

// Wait blocks until the browser posts credentials or ctx is done.
func (b *BrowserLogin) Wait(ctx context.Context) (Credentials, error) {
	select {
	case creds := <-b.result:
		return creds, nil
	case <-ctx.Done():
		return Credentials{}, ctx.Err()
	}
}

// Close shuts the listener down and is safe to call more than once, which
// matters because the browser callback and the pasted token race each other.
func (b *BrowserLogin) Close() error {
	var err error
	b.closeOne.Do(func() { err = b.server.Close() })
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// DecodePastedToken reads the blob the Infisical fallback page shows when the
// browser cannot reach the listener. It is base64 of the same JSON payload.
func DecodePastedToken(pasted string) (Credentials, error) {
	trimmed := strings.TrimSpace(pasted)
	if trimmed == "" {
		return Credentials{}, errors.New("nothing pasted")
	}
	raw, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		if raw, err = base64.RawStdEncoding.DecodeString(trimmed); err != nil {
			return Credentials{}, errors.New("that does not look like a login token")
		}
	}
	var creds Credentials
	if err := json.Unmarshal(raw, &creds); err != nil {
		return Credentials{}, errors.New("that token is not in the expected format")
	}
	if creds.Token == "" {
		return Credentials{}, errors.New("that token carries no session")
	}
	return creds, nil
}
