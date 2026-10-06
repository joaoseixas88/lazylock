package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/clipboard"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

func TestVMarksTheSelectedSecretAndTheTitleCountsIt(t *testing.T) {
	m := run(t, onSecrets(t), press("v"))
	first, _ := m.secrets.current()
	if !m.marks.has(first.ID) {
		t.Fatal("v must mark the selected secret")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "[4] Secrets · 1 marked") || !strings.Contains(view, "● STRIPE_SECRET_KEY") {
		t.Fatalf("the mark must show on the row and in the title:\n%s", view)
	}
	if m = run(t, m, press("v")); m.marks.count() != 0 {
		t.Fatal("v again must unmark")
	}
}

func TestShiftVMarksEverySecretThenNone(t *testing.T) {
	m := run(t, onSecrets(t), press("V"))
	if m.marks.count() != len(m.secrets.items) {
		t.Fatalf("marked %d of %d", m.marks.count(), len(m.secrets.items))
	}
	if m = run(t, m, press("V")); m.marks.count() != 0 {
		t.Fatal("V with everything marked must clear the marks")
	}
}

func TestEscClearsTheMarks(t *testing.T) {
	m := run(t, onSecrets(t), press("V"), press("esc"))
	if m.marks.count() != 0 {
		t.Fatal("esc must clear the marks")
	}
}

func TestMarksClearOnAScopeChangeEvenComingBack(t *testing.T) {
	m := run(t, onSecrets(t), press("V"))
	m = run(t, m, press("2"), press("down"))
	m = run(t, m, press("up"))
	if m = run(t, m, press("4")); m.marks.count() != 0 {
		t.Fatalf("marks survived a scope change: %d", m.marks.count())
	}
}

func TestReloadPrunesMarksOfVanishedSecrets(t *testing.T) {
	m := run(t, onSecrets(t), press("V"))
	at, _ := m.scopes.current()
	kept := m.secrets.items[:1]
	m = run(t, m, secretsLoadedMsg{gen: m.secrets.gen, at: at, items: kept})
	if m.marks.count() != 1 || !m.marks.has(kept[0].ID) {
		t.Fatalf("marks after reload = %v, want only %s", m.marks.ids, kept[0].ID)
	}
}

func TestMarkingOneModelNeverMarksItsCopy(t *testing.T) {
	one := run(t, onSecrets(t), press("v"))
	two := run(t, one, press("down"), press("v"))
	if one.marks.count() != 1 || two.marks.count() != 2 {
		t.Fatalf("copies share marks: one=%d two=%d", one.marks.count(), two.marks.count())
	}
}

func TestCopyLinesWithoutMarksUsesTheSelectedSecret(t *testing.T) {
	m, spy := withClipboard(onSecrets(t), clipboard.System, nil)
	m = run(t, m, press("Y"))
	if !slices.Equal(spy.texts, []string{"STRIPE_SECRET_KEY=sk_test_demo_123"}) {
		t.Fatalf("clipboard got %q", spy.texts)
	}
	if m.toast.text != "Copied STRIPE_SECRET_KEY as KEY=value" {
		t.Fatalf("toast = %q", m.toast.text)
	}
}

func TestCopyLinesUsesMarkedSecretsInListOrder(t *testing.T) {
	m, spy := withClipboard(onSecrets(t), clipboard.System, nil)
	m = run(t, m, press("down"), press("v"), press("up"), press("v"), press("Y"))
	want := "STRIPE_SECRET_KEY=sk_test_demo_123\nDATABASE_URL=postgres://demo:demo@localhost/payments"
	if !slices.Equal(spy.texts, []string{want}) {
		t.Fatalf("clipboard got %q, want %q", spy.texts, want)
	}
}

func TestCopyLinesLeavesOutUnreadableSecretsAndSaysSo(t *testing.T) {
	m := onSecrets(t)
	at, _ := m.scopes.current()
	m = run(t, m, secretsLoadedMsg{gen: m.secrets.gen, at: at, items: []domain.Secret{
		{ID: "a", Key: "OPEN", Value: "1"}, {ID: "b", Key: "SIGNING_KEY", Hidden: true},
	}})
	m, spy := withClipboard(m, clipboard.System, nil)
	m = run(t, m, press("V"), press("Y"))
	if !slices.Equal(spy.texts, []string{"OPEN=1"}) {
		t.Fatalf("clipboard got %q", spy.texts)
	}
	if !strings.Contains(m.toast.text, "left out SIGNING_KEY") {
		t.Fatalf("toast = %q, want the unreadable secret named", m.toast.text)
	}
}
