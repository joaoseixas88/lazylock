package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/config"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
)

func writable(t *testing.T, cat domain.Catalog) Model {
	t.Helper()
	m := booted(t, cat)
	m.writes = true
	return run(t, m, tea.WindowSizeMsg{Width: 140, Height: 40}, press("4"))
}

func keysIn(m Model) []string {
	var keys []string
	for _, s := range m.secrets.items {
		keys = append(keys, s.Key)
	}
	return keys
}

func TestDeleteAsksFirstAndYRemovesTheSecret(t *testing.T) {
	m := run(t, writable(t, fake.DemoCatalog()), press("d"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Delete STRIPE_SECRET_KEY from Development /?") {
		t.Fatalf("expected the review first:\n%s", view)
	}
	m = run(t, m, press("y"))
	if slices.Contains(keysIn(m), "STRIPE_SECRET_KEY") || m.overlay != nil {
		t.Fatalf("keys = %v overlay = %T", keysIn(m), m.overlay)
	}
	if m.toast.text != "Deleted STRIPE_SECRET_KEY from Development /" {
		t.Fatalf("toast = %q", m.toast.text)
	}
}

func TestOnlyYConfirmsADelete(t *testing.T) {
	for _, k := range []string{"n", "esc", "q", "enter"} {
		t.Run(k, func(t *testing.T) {
			m := run(t, writable(t, fake.DemoCatalog()), press("d"), press(k))
			if !slices.Contains(keysIn(m), "STRIPE_SECRET_KEY") {
				t.Fatalf("%s deleted the secret", k)
			}
		})
	}
}

func TestDeleteInProductionNeedsItsNameTyped(t *testing.T) {
	m := toProduction(t, writable(t, fake.DemoCatalog()))
	m = run(t, m, press("4"), press("d"), press("y"))
	if d, ok := m.overlay.(deleteOverlay); !ok || d.gate.step != gateTyping {
		t.Fatalf("overlay = %+v, want the name prompt", m.overlay)
	}
	m = run(t, m, typing("q a ny")...)
	if d, ok := m.overlay.(deleteOverlay); !ok || d.gate.input.Value() != "q a ny" {
		t.Fatalf("keys typed at the prompt must stay text, overlay = %+v", m.overlay)
	}
	m = run(t, m, press("enter"))
	if !slices.Contains(keysIn(m), "STRIPE_SECRET_KEY") {
		t.Fatal("a wrong name deleted the secret")
	}
	for range 6 {
		m = run(t, m, press("backspace"))
	}
	m = run(t, m, typing("production")...)
	m = run(t, m, press("enter"))
	if slices.Contains(keysIn(m), "STRIPE_SECRET_KEY") {
		t.Fatal("typing the environment's name must delete")
	}
}

func TestDeletingMarkedSecretsLeavesImportedOnesOut(t *testing.T) {
	m := run(t, writable(t, fake.DemoCatalog()), press("2"), press("down"))
	m = run(t, m, press("4"), press("V"), press("d"))
	d, ok := m.overlay.(deleteOverlay)
	if !ok || !slices.Equal(d.keys, []string{"REDIS_URL", "TLS_CERT"}) || !slices.Equal(d.leftOut, []string{"STRIPE_SECRET_KEY", "DATABASE_URL"}) {
		t.Fatalf("overlay = %+v", m.overlay)
	}
	m = run(t, m, press("y"))
	if got := keysIn(m); !slices.Equal(got, []string{"STRIPE_SECRET_KEY", "DATABASE_URL"}) || m.marks.count() != 0 {
		t.Fatalf("keys = %v marks = %d", got, m.marks.count())
	}
}

func TestDeletingOnlyImportedSecretsNamesTheirSource(t *testing.T) {
	m := run(t, writable(t, fake.DemoCatalog()), press("2"), press("down"))
	m = run(t, m, press("4"), press("down"), press("down"), press("d"))
	if m.overlay != nil || !strings.Contains(m.toast.text, "delete it in Development /") {
		t.Fatalf("overlay = %T toast = %q", m.overlay, m.toast.text)
	}
}

type countingWriter struct {
	*fake.Catalog
	deletes int
}

func (c *countingWriter) Delete(ctx context.Context, at domain.Scope, keys []string) (domain.Outcome, error) {
	c.deletes++
	return c.Catalog.Delete(ctx, at, keys)
}

func TestASecondYWritesOnce(t *testing.T) {
	counter := &countingWriter{Catalog: fake.DemoCatalog()}
	run(t, writable(t, counter), press("d"), press("y"), press("y"))
	if counter.deletes != 1 {
		t.Fatalf("deletes = %d, want 1", counter.deletes)
	}
}

func TestAChangeRequestChangesNothingAndSaysSo(t *testing.T) {
	cat := fake.DemoCatalog()
	cat.Approval = true
	m := run(t, writable(t, cat), press("d"), press("y"))
	if !slices.Contains(keysIn(m), "STRIPE_SECRET_KEY") || !strings.Contains(m.toast.text, "change request") {
		t.Fatalf("keys = %v toast = %q", keysIn(m), m.toast.text)
	}
}

func TestARejectedDeleteKeepsTheDialogWithTheReason(t *testing.T) {
	cat := fake.DemoCatalog()
	cat.WriteErr = fmt.Errorf("%w: You are not allowed to delete on secrets", domain.ErrRejected)
	m := run(t, writable(t, cat), press("d"), press("y"))
	if d, ok := m.overlay.(deleteOverlay); !ok || d.gate.step != gateReview {
		t.Fatalf("overlay = %+v, want the review back", m.overlay)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "You are not allowed to delete on secrets") {
		t.Fatalf("the reason must be on screen:\n%s", view)
	}
}

func TestAnUnknownOutcomeSaysSoAndReloads(t *testing.T) {
	cat := fake.DemoCatalog()
	cat.WriteErr = context.DeadlineExceeded
	m := run(t, writable(t, cat), press("d"), press("y"))
	if m.overlay != nil || !strings.Contains(m.toast.text, "Could not tell whether") {
		t.Fatalf("overlay = %T toast = %q", m.overlay, m.toast.text)
	}
}

type readOnlyCatalog struct{ domain.Catalog }

func TestReadOnlyNeverDeletes(t *testing.T) {
	m := run(t, booted(t, fake.DemoCatalog()), press("4"), press("d"))
	if m.overlay != nil || !strings.Contains(m.toast.text, "-readonly") {
		t.Fatalf("overlay = %T toast = %q", m.overlay, m.toast.text)
	}
	m = run(t, writable(t, readOnlyCatalog{fake.DemoCatalog()}), press("d"))
	if m.overlay != nil || !strings.Contains(m.toast.text, "cannot write") {
		t.Fatalf("overlay = %T toast = %q", m.overlay, m.toast.text)
	}
}

func TestWriteActionsAppearInTheMenuOnlyWhenWritable(t *testing.T) {
	titles := func(m Model) []string {
		var out []string
		for _, a := range m.actions() {
			out = append(out, a.title())
		}
		return out
	}
	if slices.Contains(titles(booted(t, fake.DemoCatalog())), "delete") {
		t.Fatal("a read-only model offers delete")
	}
	if !slices.Contains(titles(writable(t, fake.DemoCatalog())), "delete") {
		t.Fatal("a writable model must offer delete")
	}
}

func TestDeleteDialogFitsTheTerminal(t *testing.T) {
	m := toProduction(t, writable(t, fake.DemoCatalog()))
	m = run(t, m, press("4"), press("V"), press("d"), press("y"))
	for _, size := range terminalSizes {
		m = run(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		assertFits(t, m.View(), size[0], size[1])
	}
}

func TestAResultForAScopeNoLongerShownDoesNotReloadIt(t *testing.T) {
	m := writable(t, fake.DemoCatalog())
	elsewhere := domain.Scope{ProjectID: "payments", EnvSlug: "prod", EnvName: "Production", Path: "/"}
	if _, cmd := m.written(writtenMsg{at: elsewhere, done: "Deleted X from Production /"}); cmd != nil {
		t.Fatal("a write elsewhere must not reload the scope on screen")
	}
	here, _ := m.scopes.current()
	if _, cmd := m.written(writtenMsg{at: here, done: "Deleted X from Development /"}); cmd == nil {
		t.Fatal("a write here must reload it")
	}
}

func TestExpiredSessionDuringADeleteGoesToLogin(t *testing.T) {
	cat := fake.DemoCatalog()
	m := app(t, config.Config{SiteURL: "https://infisical.example.com"})
	m.state, m.writes, m.load.catalog, m.load.debounce = stateBrowsing, true, cat, 0
	m = run(t, m, exec(t, m.load.projects(m.projects.begin()))...)
	cat.WriteErr = fmt.Errorf("%w: expired", domain.ErrUnauthorized)

	m = run(t, m, press("4"), press("d"))
	next, write := m.Update(press("y"))
	next, _ = next.(Model).Update(write())
	if got := next.(Model); got.state != stateLogin {
		t.Fatalf("state = %v, want the login screen", got.state)
	}
}
