package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
)

func secretNamed(m Model, key string) (domain.Secret, bool) {
	for _, s := range m.secrets.items {
		if s.Key == key {
			return s, true
		}
	}
	return domain.Secret{}, false
}

func drafting(t *testing.T, m Model, key string, value []tea.Msg, comment string) Model {
	t.Helper()
	m = run(t, m, press("n"))
	m = run(t, m, typing(key)...)
	m = run(t, m, press("tab"))
	m = run(t, m, value...)
	m = run(t, m, press("tab"))
	return run(t, m, typing(comment)...)
}

func TestCreateWritesTheSecretAfterTheReview(t *testing.T) {
	m := drafting(t, writable(t, fake.DemoCatalog()), "NEW_KEY", typing("abc"), "a note")
	m = run(t, m, press("ctrl+s"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Create NEW_KEY in Development /?") || strings.Contains(view, "abc") {
		t.Fatalf("the review must ask first and keep the value masked:\n%s", view)
	}
	m = run(t, m, press("y"))
	s, ok := secretNamed(m, "NEW_KEY")
	if !ok || s.Value != "abc" || s.Comment != "a note" || m.overlay != nil {
		t.Fatalf("secret = %+v overlay = %T", s, m.overlay)
	}
	if m.toast.text != "Created NEW_KEY in Development /" {
		t.Fatalf("toast = %q", m.toast.text)
	}
}

func TestAMultilineValueKeepsItsLines(t *testing.T) {
	value := append(typing("line1"), press("enter"))
	value = append(value, typing("line2")...)
	m := drafting(t, writable(t, fake.DemoCatalog()), "CERT", value, "")
	m = run(t, m, press("ctrl+s"), press("y"))
	if s, _ := secretNamed(m, "CERT"); s.Value != "line1\nline2" {
		t.Fatalf("value = %q", s.Value)
	}
}

func TestCreateRefusesBadAndExistingKeys(t *testing.T) {
	for key, want := range map[string]string{
		"bad key":           "use letters, digits",
		"STRIPE_SECRET_KEY": "already exists here",
		"":                  "the key is empty",
	} {
		m := drafting(t, writable(t, fake.DemoCatalog()), key, typing("v"), "")
		m = run(t, m, press("ctrl+s"))
		e, ok := m.overlay.(editorOverlay)
		if !ok || e.step != editing || e.err == nil || !strings.Contains(e.err.Error(), want) {
			t.Fatalf("key %q: overlay = %+v", key, m.overlay)
		}
	}
}

func TestCreatingAnImportedKeySaysItHidesTheImport(t *testing.T) {
	m := run(t, writable(t, fake.DemoCatalog()), press("2"), press("down"))
	m = drafting(t, run(t, m, press("4")), "DATABASE_URL", typing("local"), "")
	m = run(t, m, press("ctrl+s"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Hides DATABASE_URL imported from Development /") {
		t.Fatalf("expected the shadowing warning:\n%s", view)
	}
}

func TestDiscardingAsksAndNoKeepsTheDraft(t *testing.T) {
	m := run(t, writable(t, fake.DemoCatalog()), press("n"))
	m = run(t, m, typing("DRAFT")...)
	m = run(t, m, press("esc"))
	if e, ok := m.overlay.(editorOverlay); !ok || e.step != discarding {
		t.Fatalf("esc with changes must ask, overlay = %+v", m.overlay)
	}
	m = run(t, m, press("n"))
	if e, ok := m.overlay.(editorOverlay); !ok || e.step != editing || e.key.Value() != "DRAFT" {
		t.Fatalf("n must keep the draft, overlay = %+v", m.overlay)
	}
	if m = run(t, m, press("esc"), press("y")); m.overlay != nil {
		t.Fatal("y must discard")
	}
}

func TestEscWithoutChangesClosesTheEditor(t *testing.T) {
	if m := run(t, writable(t, fake.DemoCatalog()), press("n"), press("esc")); m.overlay != nil {
		t.Fatal("an untouched editor must close on esc")
	}
}

func TestNoInTheReviewGoesBackToTheDraft(t *testing.T) {
	m := drafting(t, writable(t, fake.DemoCatalog()), "KEEP_ME", typing("v"), "")
	m = run(t, m, press("ctrl+s"), press("n"))
	if e, ok := m.overlay.(editorOverlay); !ok || e.step != editing || e.key.Value() != "KEEP_ME" {
		t.Fatalf("overlay = %+v", m.overlay)
	}
}

func TestCreateInProductionNeedsItsName(t *testing.T) {
	m := toProduction(t, writable(t, fake.DemoCatalog()))
	m = drafting(t, run(t, m, press("4")), "PROD_ONLY", typing("v"), "")
	m = run(t, m, press("ctrl+s"), press("y"))
	if e, ok := m.overlay.(editorOverlay); !ok || e.gate.step != gateTyping {
		t.Fatalf("overlay = %+v, want the name prompt", m.overlay)
	}
	m = run(t, m, typing("prod")...)
	m = run(t, m, press("enter"))
	if _, ok := secretNamed(m, "PROD_ONLY"); !ok {
		t.Fatal("typing the slug must create the secret")
	}
}

func TestARejectedCreateReturnsToTheDraftWithTheReason(t *testing.T) {
	cat := fake.DemoCatalog()
	cat.WriteErr = fmt.Errorf("%w: Secret key must be uppercase", domain.ErrRejected)
	m := drafting(t, writable(t, cat), "lower", typing("v"), "")
	m = run(t, m, press("ctrl+s"), press("y"))
	e, ok := m.overlay.(editorOverlay)
	if !ok || e.step != editing || e.key.Value() != "lower" {
		t.Fatalf("overlay = %+v", m.overlay)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Secret key must be uppercase") {
		t.Fatalf("the reason must be on screen:\n%s", view)
	}
}

func TestKeysTypedInTheEditorNeverReachThePanes(t *testing.T) {
	m := run(t, writable(t, fake.DemoCatalog()), press("n"))
	m = run(t, m, typing("qa1?x")...)
	e, ok := m.overlay.(editorOverlay)
	if !ok || e.key.Value() != "qa1?x" || m.activePane != secretsPane || m.reveal.all {
		t.Fatalf("overlay = %+v pane = %v", m.overlay, m.activePane)
	}
}

func TestEditorFitsTheTerminal(t *testing.T) {
	m := drafting(t, writable(t, fake.DemoCatalog()), "SIZED", typing(strings.Repeat("x", 100)), "c")
	for _, size := range terminalSizes {
		m = run(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		assertFits(t, m.View(), size[0], size[1])
	}
}

func TestCreateAppearsInTheMenuOnlyWhenWritable(t *testing.T) {
	titles := func(m Model) []string {
		var out []string
		for _, a := range m.actions() {
			out = append(out, a.title())
		}
		return out
	}
	if slices.Contains(titles(booted(t, fake.DemoCatalog())), "new secret") || !slices.Contains(titles(writable(t, fake.DemoCatalog())), "new secret") {
		t.Fatal("new secret must be offered exactly when writable")
	}
}
