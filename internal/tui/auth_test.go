package tui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/config"
	"github.com/joaoseixas88/lazylock/internal/credstore"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
	"github.com/joaoseixas88/lazylock/internal/infisical"
)

func sized(m Model, width, height int) Model {
	m.resize(width, height)
	return m
}

// app builds a real-flow model with the stores pointed at a temp dir, so no
// test can touch the user's own config or session.
func app(t *testing.T, cfg config.Config) Model {
	t.Helper()
	t.Setenv(config.EnvPath, t.TempDir()+"/config.json")
	t.Setenv(credstore.EnvPath, t.TempDir()+"/session.json")
	t.Setenv(config.EnvToken, "")
	return NewApp(context.Background(), cfg, &credstore.Store{})
}

func TestNewAppAsksForTheInstanceWhenUnconfigured(t *testing.T) {
	m := sized(app(t, config.Config{}), 80, 24)
	if m.state != stateSetup {
		t.Fatalf("state = %v, want stateSetup", m.state)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Connect to Infisical") {
		t.Fatalf("setup screen missing:\n%s", view)
	}
}

func TestNewAppRestoresWhenConfigured(t *testing.T) {
	m := app(t, config.Config{SiteURL: "https://infisical.example.com"})
	if m.state != stateRestoring {
		t.Fatalf("state = %v, want stateRestoring", m.state)
	}
	if m.client == nil {
		t.Fatal("a configured app must have a client")
	}
}

func TestNoStoredSessionLeadsToLogin(t *testing.T) {
	m := app(t, config.Config{SiteURL: "https://infisical.example.com"})
	next, _, handled := m.handleAuth(sessionRestoredMsg{err: credstore.ErrNotFound})
	if !handled {
		t.Fatal("sessionRestoredMsg must be handled")
	}
	if next.state != stateLogin {
		t.Fatalf("state = %v, want stateLogin", next.state)
	}
}

func TestExpiredSessionIsDiscardedAndSendsBackToLogin(t *testing.T) {
	m := app(t, config.Config{SiteURL: "https://infisical.example.com"})
	session := credstore.Session{SiteURL: m.cfg.SiteURL, Email: "a@b.c", Token: "stale"}
	if err := m.store.Save(session); err != nil {
		t.Fatal(err)
	}

	next, _, _ := m.handleAuth(sessionReadyMsg{err: fmt.Errorf("list projects: %w", domain.ErrUnauthorized)})
	if next.state != stateLogin {
		t.Fatalf("state = %v, want stateLogin", next.state)
	}
	if !errors.Is(next.authErr, errSessionExpired) {
		t.Fatalf("authErr = %v, the login screen should say why it is back", next.authErr)
	}
	if _, err := m.store.Load(m.cfg.SiteURL); !errors.Is(err, credstore.ErrNotFound) {
		t.Fatal("a token the server rejected must not be kept")
	}
}

func TestSessionExpiringWhileBrowsingSendsBackToLogin(t *testing.T) {
	m := app(t, config.Config{SiteURL: "https://infisical.example.com"})
	session := credstore.Session{SiteURL: m.cfg.SiteURL, Email: "a@b.c", Token: "stale"}
	if err := m.store.Save(session); err != nil {
		t.Fatal(err)
	}
	m.state = stateBrowsing
	expired := fmt.Errorf("list projects: %w", domain.ErrUnauthorized)
	inFlight := m.projects.gen

	next, _ := m.Update(projectsLoadedMsg{gen: inFlight, err: expired})
	m = next.(Model)
	if m.state != stateLogin {
		t.Fatalf("state = %v, want stateLogin", m.state)
	}
	if _, err := m.store.Load(m.cfg.SiteURL); !errors.Is(err, credstore.ErrNotFound) {
		t.Fatal("a token the server rejected must not be kept")
	}
	if _, cmd := m.Update(projectsLoadedMsg{gen: inFlight, err: expired}); cmd != nil {
		t.Fatal("a reply still in flight for the old session must not restart the login")
	}
}

func TestRejectedEnvironmentTokenLeavesTheStoredSessionAlone(t *testing.T) {
	m := app(t, config.Config{SiteURL: "https://infisical.example.com"})
	t.Setenv(config.EnvToken, "from.the.environment")
	session := credstore.Session{SiteURL: m.cfg.SiteURL, Email: "a@b.c", Token: "stored"}
	if err := m.store.Save(session); err != nil {
		t.Fatal(err)
	}

	next, _, _ := m.handleAuth(sessionReadyMsg{err: fmt.Errorf("list projects: %w", domain.ErrUnauthorized)})
	if next.state != stateBrowsing {
		t.Fatalf("state = %v, want the failure on the panes", next.state)
	}
	if _, err := m.store.Load(m.cfg.SiteURL); err != nil {
		t.Fatalf("the stored session must survive a rejected LAZYLOCK_TOKEN: %v", err)
	}
}

// A reachable instance that fails for some other reason is a pane error, not a
// reason to throw the session away and make the user log in again.
func TestNonAuthFailureKeepsTheSession(t *testing.T) {
	m := app(t, config.Config{SiteURL: "https://infisical.example.com"})
	next, _, _ := m.handleAuth(sessionReadyMsg{err: errors.New("refused")})
	if next.state != stateBrowsing {
		t.Fatalf("state = %v, want stateBrowsing", next.state)
	}
	if next.projects.state != stateFailed {
		t.Fatalf("projects state = %v, want stateFailed", next.projects.state)
	}
	// The pane is narrow, so assert on a fragment that survives truncation.
	view := ansi.Strip(sized(next, 120, 36).View())
	if !strings.Contains(view, "refused") {
		t.Fatalf("the real failure should be on screen:\n%s", view)
	}
}

func TestRetryAfterANonAuthFailureReachesTheServer(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	m := app(t, config.Config{SiteURL: srv.URL})
	m, _, _ = m.handleAuth(sessionReadyMsg{err: errors.New("refused")})
	run(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if hits.Load() != 1 {
		t.Fatalf("retry reached the server %d times, want 1", hits.Load())
	}
}

func TestSuccessfulLoginStoresTheSessionAndShowsTheAccount(t *testing.T) {
	m := app(t, config.Config{SiteURL: "https://infisical.example.com"})
	m.client = infisical.NewClient(m.cfg.APIBase())

	next, _, handled := m.handleAuth(loginDoneMsg{creds: infisical.Credentials{
		Email: "someone@example.com", Token: "fresh.token.value",
	}})
	if !handled {
		t.Fatal("loginDoneMsg must be handled")
	}
	if next.account != "someone@example.com" {
		t.Fatalf("account = %q", next.account)
	}
	if next.client.Token() != "fresh.token.value" {
		t.Fatal("the client must carry the new token")
	}

	session, err := next.store.Load(next.cfg.SiteURL)
	if err != nil {
		t.Fatal(err)
	}
	if session.Token != "fresh.token.value" {
		t.Fatalf("stored token = %q", session.Token)
	}
}

// The Infisical callback flow has no nonce, so a local process could win the
// race. Naming the account is what lets a user notice.
func TestFooterNamesTheAccount(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	m.account = "someone@example.com"
	m.store = &credstore.Store{}
	if got := m.footer(); !strings.Contains(got, "someone@example.com") {
		t.Fatalf("footer = %q", got)
	}
}

func TestAuthScreensStayInsideTheTerminal(t *testing.T) {
	for _, state := range []appState{stateSetup, stateRestoring, stateLogin} {
		for _, size := range [][2]int{{80, 24}, {40, 10}, {120, 40}} {
			m := app(t, config.Config{SiteURL: "https://infisical.example.com"})
			m.state = state
			m.authErr = errors.New("a rather long failure message that should still be truncated to the terminal width")
			m = sized(m, size[0], size[1])

			view := ansi.Strip(m.View())
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > size[0] {
					t.Fatalf("state %v at %v overflowed: %q", state, size, line)
				}
			}
		}
	}
}

func TestSetupSubmitNormalizesBeforeAnythingElse(t *testing.T) {
	m := app(t, config.Config{})
	next, _, _ := m.handleAuth(siteCheckedMsg{err: errors.New("https://typo.example did not answer")})
	if next.state != stateSetup {
		t.Fatalf("a bad URL must keep the user on setup, got %v", next.state)
	}
	if next.authErr == nil {
		t.Fatal("the failure must be shown")
	}
}
