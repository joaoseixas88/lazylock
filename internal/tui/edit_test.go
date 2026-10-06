package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
)

type changeSpy struct {
	*fake.Catalog
	changes []domain.Change
}

func (c *changeSpy) Update(ctx context.Context, at domain.Scope, key string, ch domain.Change) (domain.Outcome, error) {
	c.changes = append(c.changes, ch)
	return c.Catalog.Update(ctx, at, key, ch)
}

var (
	devRoot = domain.Scope{ProjectID: "payments", EnvSlug: "dev", EnvName: "Development", Path: "/"}
	devAPI  = domain.Scope{ProjectID: "payments", EnvSlug: "dev", EnvName: "Development", Path: "/services/api"}
)

func onAPI(t *testing.T, cat domain.Catalog) Model {
	t.Helper()
	m := run(t, writable(t, cat), press("2"), press("down"), press("down"))
	return run(t, m, press("4"))
}

func editor(t *testing.T, m Model) editorOverlay {
	t.Helper()
	e, ok := m.overlay.(editorOverlay)
	if !ok {
		t.Fatalf("overlay = %T, want the editor", m.overlay)
	}
	return e
}

func rawValue(t *testing.T, cat *fake.Catalog, at domain.Scope, key string) domain.Secret {
	t.Helper()
	secrets, err := cat.Raw(context.Background(), at)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range secrets {
		if s.Key == key {
			return s
		}
	}
	t.Fatalf("%s is not stored at %s", key, at.Path)
	return domain.Secret{}
}

func TestEditStartsFromTheRawValue(t *testing.T) {
	m := run(t, onAPI(t, fake.DemoCatalog()), press("down"), press("a"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "API_AUTH_HEADER=Bearer tok_demo_789") {
		t.Fatalf("the list must show the reference expanded:\n%s", view)
	}
	m = run(t, m, press("e"))
	if got := editor(t, m).value.Value(); got != "Bearer ${API_TOKEN}" {
		t.Fatalf("editor value = %q, want the raw reference", got)
	}
}

func TestSavingACommentAloneSendsNoValue(t *testing.T) {
	spy := &changeSpy{Catalog: fake.DemoCatalog()}
	m := run(t, onAPI(t, spy), press("down"), press("e"))
	m = run(t, m, press("tab"))
	m = run(t, m, typing(" for the gateway")...)
	m = run(t, m, press("ctrl+s"), press("y"))
	if len(spy.changes) != 1 || spy.changes[0].Value != nil || spy.changes[0].NewKey != nil || *spy.changes[0].Comment != "Built from API_TOKEN for the gateway" {
		t.Fatalf("changes = %+v", spy.changes)
	}
	if s := rawValue(t, spy.Catalog, devAPI, "API_AUTH_HEADER"); s.Value != "Bearer ${API_TOKEN}" {
		t.Fatalf("the reference was overwritten: %q", s.Value)
	}
}

func TestEditingTheValueSavesIt(t *testing.T) {
	cat := fake.DemoCatalog()
	m := run(t, writable(t, cat), press("e"))
	m = run(t, m, typing("_v2")...)
	m = run(t, m, press("ctrl+s"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Save STRIPE_SECRET_KEY in Development /?") || strings.Contains(view, "sk_test") {
		t.Fatalf("the review must ask first with values masked:\n%s", view)
	}
	m = run(t, m, press("y"))
	if s := rawValue(t, cat, devRoot, "STRIPE_SECRET_KEY"); s.Value != "sk_test_demo_123_v2" || m.overlay != nil {
		t.Fatalf("value = %q overlay = %T", s.Value, m.overlay)
	}
	if m.toast.text != "Saved STRIPE_SECRET_KEY in Development /" {
		t.Fatalf("toast = %q", m.toast.text)
	}
}

func TestRenamingSendsTheNewKeyOnlyWhenItReallyChanges(t *testing.T) {
	spy := &changeSpy{Catalog: fake.DemoCatalog()}
	m := run(t, writable(t, spy), press("e"))
	m = run(t, m, press("shift+tab"), press(" "), press("ctrl+s"))
	if e := editor(t, m); e.err == nil || !strings.Contains(e.err.Error(), "nothing to save") {
		t.Fatalf("a key changed only by spaces is not a rename, err = %v", e.err)
	}
	m = run(t, m, press("backspace"))
	m = run(t, m, typing("_OLD")...)
	m = run(t, m, press("ctrl+s"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "STRIPE_SECRET_KEY → STRIPE_SECRET_KEY_OLD") {
		t.Fatalf("the review must show the rename:\n%s", view)
	}
	m = run(t, m, press("y"))
	if len(spy.changes) != 1 || *spy.changes[0].NewKey != "STRIPE_SECRET_KEY_OLD" || spy.changes[0].Value != nil {
		t.Fatalf("changes = %+v", spy.changes)
	}
	if _, ok := secretNamed(m, "STRIPE_SECRET_KEY_OLD"); !ok || m.toast.text != "Renamed STRIPE_SECRET_KEY to STRIPE_SECRET_KEY_OLD in Development /" {
		t.Fatalf("keys = %v toast = %q", keysIn(m), m.toast.text)
	}
}

func TestAValueTheEditorWouldChangeIsLockedUntilReplaced(t *testing.T) {
	spy := &changeSpy{Catalog: fake.DemoCatalog()}
	spy.Edit(devRoot, "STRIPE_SECRET_KEY", "col1\tcol2\r\nend")
	m := run(t, writable(t, spy), press("e"))
	e := editor(t, m)
	if !e.locked[valueField] || !strings.Contains(ansi.Strip(m.View()), "ctrl+r replaces it") {
		t.Fatalf("the value must be locked:\n%s", ansi.Strip(m.View()))
	}
	m = run(t, m, typing("typed")...)
	m = run(t, m, press("tab"))
	m = run(t, m, typing("!")...)
	m = run(t, m, press("ctrl+s"), press("y"))
	if len(spy.changes) != 1 || spy.changes[0].Value != nil {
		t.Fatalf("a locked value must not be sent: %+v", spy.changes)
	}

	m = run(t, m, press("e"))
	m = run(t, m, press("ctrl+r"))
	m = run(t, m, typing("clean")...)
	m = run(t, m, press("ctrl+s"), press("y"))
	if len(spy.changes) != 2 || spy.changes[1].Value == nil || *spy.changes[1].Value != "clean" {
		t.Fatalf("a replaced value must be sent: %+v", spy.changes)
	}
}

func TestAnUnreadableValueIsLockedToo(t *testing.T) {
	m := toProduction(t, writable(t, fake.DemoCatalog()))
	m = run(t, m, press("4"), press("down"), press("e"))
	if e := editor(t, m); !e.locked[valueField] || !strings.Contains(ansi.Strip(m.View()), "no read access") {
		t.Fatalf("an unreadable value must be locked:\n%s", ansi.Strip(m.View()))
	}
	m = run(t, m, press("ctrl+s"))
	if e := editor(t, m); e.err == nil || !strings.Contains(e.err.Error(), "nothing to save") {
		t.Fatalf("err = %v", e.err)
	}
}

func TestAConflictRefreshesTheBeforeSideAndAsksAgain(t *testing.T) {
	spy := &changeSpy{Catalog: fake.DemoCatalog()}
	m := run(t, writable(t, spy), press("e"))
	m = run(t, m, press("tab"))
	m = run(t, m, typing(" (rotated)")...)
	spy.Edit(devRoot, "STRIPE_SECRET_KEY", "sk_changed_elsewhere")

	m = run(t, m, press("ctrl+s"), press("y"))
	e := editor(t, m)
	if len(spy.changes) != 0 || !e.conflict || e.step != reviewing || e.before.Value != "sk_changed_elsewhere" {
		t.Fatalf("changes = %+v overlay = %+v", spy.changes, e)
	}
	if view := ansi.Strip(run(t, m, press(" ")).View()); !strings.Contains(view, "Someone else changed this secret") {
		t.Fatalf("the conflict must be on screen:\n%s", view)
	}

	m = run(t, m, press("y"))
	if len(spy.changes) != 1 || spy.changes[0].Value != nil {
		t.Fatalf("only the comment may be saved over someone else's value: %+v", spy.changes)
	}
	if s := rawValue(t, spy.Catalog, devRoot, "STRIPE_SECRET_KEY"); s.Value != "sk_changed_elsewhere" || s.Comment != "Test-mode key from the Stripe dashboard (rotated)" {
		t.Fatalf("stored = %+v", s)
	}
}

func TestEditingAnImportedSecretNamesItsSource(t *testing.T) {
	m := run(t, writable(t, fake.DemoCatalog()), press("2"), press("down"))
	m = run(t, m, press("4"), press("down"), press("down"), press("e"))
	if m.overlay != nil || !strings.Contains(m.toast.text, "edit it in Development /") {
		t.Fatalf("overlay = %T toast = %q", m.overlay, m.toast.text)
	}
}

func TestEditFromTheDetails(t *testing.T) {
	m := run(t, writable(t, fake.DemoCatalog()), press("enter"), press("e"))
	if e := editor(t, m); e.loading || e.before.Key != "STRIPE_SECRET_KEY" {
		t.Fatalf("editor = %+v", e)
	}
}

func TestEscWhileLoadingCancelsTheEdit(t *testing.T) {
	m := writable(t, fake.DemoCatalog())
	next, load := m.Update(press("e"))
	if !editor(t, next.(Model)).loading {
		t.Fatal("the editor must open loading, so keys cannot reach the panes")
	}
	next, _ = next.(Model).Update(press("esc"))
	next, _ = next.(Model).Update(load())
	if next.(Model).overlay != nil {
		t.Fatal("a load that lands after esc must not reopen the editor")
	}
}

func TestASecretGoneBeforeSavingSaysSo(t *testing.T) {
	cat := fake.DemoCatalog()
	m := run(t, writable(t, cat), press("e"))
	m = run(t, m, typing("x")...)
	if _, err := cat.Delete(context.Background(), devRoot, []string{"STRIPE_SECRET_KEY"}); err != nil {
		t.Fatal(err)
	}
	m = run(t, m, press("ctrl+s"), press("y"))
	if e := editor(t, m); e.step != editing || e.err == nil || !strings.Contains(e.err.Error(), "no longer exists") {
		t.Fatalf("overlay = %+v", e)
	}
}

func TestEditInProductionNeedsItsName(t *testing.T) {
	m := toProduction(t, writable(t, fake.DemoCatalog()))
	m = run(t, m, press("4"), press("e"))
	m = run(t, m, typing("x")...)
	m = run(t, m, press("ctrl+s"), press("y"))
	if e := editor(t, m); e.gate.step != gateTyping {
		t.Fatalf("gate = %v, want the name prompt", e.gate.step)
	}
}
