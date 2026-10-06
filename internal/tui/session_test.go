package tui

import (
	"errors"
	"testing"

	"github.com/joaoseixas88/lazylock/internal/config"
	"github.com/joaoseixas88/lazylock/internal/credstore"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
)

func browsingApp(t *testing.T) Model {
	t.Helper()
	m := app(t, config.Config{SiteURL: "https://infisical.example.com"})
	if err := m.store.Save(credstore.Session{SiteURL: m.cfg.SiteURL, Email: "a@b.c", Token: "live"}); err != nil {
		t.Fatal(err)
	}
	m.state, m.account = stateBrowsing, "a@b.c"
	return m
}

func stored(t *testing.T, m Model) bool {
	t.Helper()
	_, err := m.store.Load(m.cfg.SiteURL)
	if err != nil && !errors.Is(err, credstore.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

func TestLogoutAsksFirstAndNoKeepsTheSession(t *testing.T) {
	m := run(t, browsingApp(t), press("L"))
	if _, ok := m.overlay.(confirmOverlay); !ok {
		t.Fatalf("overlay = %T, want a confirmation", m.overlay)
	}
	m = run(t, m, press("n"))
	if m.overlay != nil || m.state != stateBrowsing || !stored(t, m) {
		t.Fatal("declining must change nothing")
	}
}

func TestLogoutForgetsTheSessionAndShowsLogin(t *testing.T) {
	m := run(t, browsingApp(t), press("L"))
	next, forget := m.Update(press("y"))
	if forget == nil {
		t.Fatal("confirming must delete the session in a command")
	}
	next, _ = next.(Model).Update(forget())
	m = next.(Model)
	if m.state != stateLogin || m.account != "" || stored(t, m) {
		t.Fatalf("state=%v account=%q stored=%v", m.state, m.account, stored(t, m))
	}
}

func TestLogoutIsRefusedWhileLAZYLOCK_TOKENIsSet(t *testing.T) {
	m := browsingApp(t)
	t.Setenv(config.EnvToken, "from.the.environment")
	m = run(t, m, press("L"))
	if m.overlay != nil || m.toast.level != toastError || !stored(t, m) {
		t.Fatalf("overlay=%T toast=%+v", m.overlay, m.toast)
	}
}

func TestLogoutIsUnavailableInTheDemo(t *testing.T) {
	m := run(t, booted(t, fake.DemoCatalog()), press("L"))
	if m.overlay != nil || m.toast.level != toastError {
		t.Fatalf("overlay=%T toast=%+v", m.overlay, m.toast)
	}
}

func TestSwitchInstanceKeepsTheOldSessionAndAsksForAURL(t *testing.T) {
	m := run(t, browsingApp(t), press("3"))
	m = choose(t, m, "switch instance")
	m = run(t, m, press("y"))
	if m.state != stateSetup || m.input.Value() != "https://infisical.example.com" {
		t.Fatalf("state=%v input=%q", m.state, m.input.Value())
	}
	if !stored(t, m) {
		t.Fatal("switching must not delete the current instance's session")
	}
}

func TestEscDuringASwitchGoesBackToTheCurrentInstance(t *testing.T) {
	m := run(t, browsingApp(t), press("3"))
	m = run(t, choose(t, m, "switch instance"), press("y"))
	next, cmd := m.Update(press("esc"))
	m = next.(Model)
	if quits(cmd) || cmd == nil {
		t.Fatal("esc during a switch must restore the session, not quit")
	}
	if m.state != stateRestoring || m.cfg.SiteURL != "https://infisical.example.com" || m.previous != nil {
		t.Fatalf("state=%v site=%q", m.state, m.cfg.SiteURL)
	}
}

func TestSwitchingToAnotherSiteForgetsTheAccountHint(t *testing.T) {
	m := run(t, browsingApp(t), press("3"))
	m = run(t, choose(t, m, "switch instance"), press("y"))
	m.cfg.Account = "a@b.c"
	next, _, _ := m.handleAuth(siteCheckedMsg{siteURL: "https://other.example.com"})
	if next.cfg.Account != "" || next.account != "" || next.previous != nil {
		t.Fatalf("the old account must not follow to another instance: %+v", next.cfg)
	}
}

func TestEndingASessionClearsMarksFiltersAndOverlays(t *testing.T) {
	m := run(t, booted(t, fake.DemoCatalog()), press("4"), press("V"), press("a"))
	m = run(t, filtered(t, m, "4", "str"), press("enter"), press("?"))
	m.endSession()
	if m.marks.count() != 0 || m.secrets.query != "" || m.overlay != nil || m.reveal.all || m.filtering {
		t.Fatalf("leftovers: marks=%d query=%q overlay=%T reveal=%+v", m.marks.count(), m.secrets.query, m.overlay, m.reveal)
	}
}
