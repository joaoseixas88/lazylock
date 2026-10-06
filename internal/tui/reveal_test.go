package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
)

func onSecrets(t *testing.T) Model {
	t.Helper()
	m := booted(t, fake.DemoCatalog())
	return run(t, m, tea.WindowSizeMsg{Width: 120, Height: 36}, press("4"))
}

func TestRevealAllShowsEveryReadableValue(t *testing.T) {
	m := run(t, onSecrets(t), press("a"))
	view := ansi.Strip(m.View())
	for _, want := range []string{"STRIPE_SECRET_KEY=sk_test_demo_123", "DATABASE_URL=postgres://demo:demo@localhost/payments"} {
		if !strings.Contains(view, want) {
			t.Fatalf("%q missing after revealing all:\n%s", want, view)
		}
	}
}

func TestRevealAllNeverShowsAHiddenSecret(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	m = run(t, m, tea.WindowSizeMsg{Width: 120, Height: 36}, press("2"), press("down"), press("down"))
	m = run(t, m, press("4"), press("a"))
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "SIGNING_KEY=(no read access)") || !strings.Contains(view, "STRIPE_SECRET_KEY=sk_live_demo_456") {
		t.Fatalf("reveal all must show readable values and still say a hidden one has no read access:\n%s", view)
	}
}

func TestRevealedValuesHideThemselvesWhenTheTimerFires(t *testing.T) {
	m := run(t, onSecrets(t), press("a"))
	m = run(t, m, remaskMsg{gen: m.reveal.gen})
	if view := ansi.Strip(m.View()); !strings.Contains(view, "STRIPE_SECRET_KEY=••••••••") {
		t.Fatalf("the timer must mask the values again:\n%s", view)
	}
}

func TestAnOlderTimerNeverHidesAFreshReveal(t *testing.T) {
	m := run(t, onSecrets(t), press("a"))
	stale := m.reveal.gen
	m = run(t, m, press("a"), press("a"))
	m = run(t, m, remaskMsg{gen: stale})
	if !m.reveal.all {
		t.Fatal("a timer from an earlier reveal hid the current one")
	}
}

func TestRevealSchedulesARemaskWhenTimersAreOn(t *testing.T) {
	m := onSecrets(t)
	m.timers.remask = time.Hour
	if _, cmd := m.Update(press("a")); cmd == nil {
		t.Fatal("revealing must schedule the remask")
	}
	if _, cmd := m.Update(press(" ")); cmd == nil {
		t.Fatal("revealing one value must schedule the remask too")
	}
}

func TestMovingHidesASingleRevealButNotRevealAll(t *testing.T) {
	m := run(t, onSecrets(t), press(" "))
	first, _ := m.secrets.current()
	m = run(t, m, press("down"), press("up"))
	if m.reveal.shows(first.ID) {
		t.Fatal("moving away must hide a single revealed value")
	}

	m = run(t, m, press("a"), press("down"), press("up"))
	if !m.reveal.all {
		t.Fatal("moving inside the pane must keep reveal all")
	}
}

func TestLeavingTheSecretsPaneHidesEverything(t *testing.T) {
	m := run(t, onSecrets(t), press("a"), press("1"))
	if m.reveal.all {
		t.Fatal("leaving the secrets pane must hide every value")
	}
}

func TestRevealedMultilineValueShowsOnlyItsFirstLine(t *testing.T) {
	m := onSecrets(t)
	at, _ := m.scopes.current()
	cert := domain.Secret{ID: "cert", Key: "TLS_CERT", Value: "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----"}
	m = run(t, m, secretsLoadedMsg{gen: m.secrets.gen, at: at, items: []domain.Secret{cert}}, press("a"))

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "TLS_CERT=-----BEGIN CERTIFICATE----- …") || strings.Contains(view, "MIIB") {
		t.Fatalf("a multi-line value must show its first line only:\n%s", view)
	}
}
