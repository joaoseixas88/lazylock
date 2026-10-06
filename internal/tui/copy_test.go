package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/clipboard"
)

type clipboardSpy struct {
	texts []string
	via   clipboard.Via
	err   error
}

func (s *clipboardSpy) copy(_ context.Context, text string) (clipboard.Via, error) {
	s.texts = append(s.texts, text)
	return s.via, s.err
}

func withClipboard(m Model, via clipboard.Via, err error) (Model, *clipboardSpy) {
	spy := &clipboardSpy{via: via, err: err}
	m.fx.Copy = spy.copy
	return m, spy
}

func lastLine(view string) string {
	lines := strings.Split(ansi.Strip(view), "\n")
	return lines[len(lines)-1]
}

func TestCopyPutsTheSelectedValueOnTheClipboard(t *testing.T) {
	m, spy := withClipboard(onSecrets(t), clipboard.System, nil)
	m = run(t, m, press("y"))
	if !slices.Equal(spy.texts, []string{"sk_test_demo_123"}) {
		t.Fatalf("clipboard got %q", spy.texts)
	}
	if got := lastLine(m.View()); !strings.HasPrefix(got, "Copied STRIPE_SECRET_KEY") {
		t.Fatalf("footer = %q, want the copy confirmed", got)
	}
}

func TestCopyingAHiddenSecretNeverReachesTheClipboard(t *testing.T) {
	m := onSecrets(t)
	m = toProduction(t, m)
	m, spy := withClipboard(m, clipboard.System, nil)
	m = run(t, m, press("4"), press("down"), press("y"))
	if len(spy.texts) != 0 {
		t.Fatalf("a secret without read access reached the clipboard: %q", spy.texts)
	}
	if m.toast.level != toastError || !strings.Contains(m.toast.text, "SIGNING_KEY") {
		t.Fatalf("toast = %+v, want an error naming the secret", m.toast)
	}
}

func TestToastNeverContainsTheValue(t *testing.T) {
	for _, err := range []error{nil, errors.New("wl-copy: exit status 1")} {
		m, _ := withClipboard(onSecrets(t), clipboard.System, err)
		m = run(t, m, press("y"))
		if m.toast.text == "" || strings.Contains(m.View(), "sk_test_demo_123") {
			t.Fatalf("the copied value leaked into the screen: %q", m.toast.text)
		}
	}
}

func TestTerminalClipboardIsReportedAsSentNotCopied(t *testing.T) {
	m, _ := withClipboard(onSecrets(t), clipboard.Terminal, nil)
	m = run(t, m, press("y"))
	if m.toast.text != "Sent STRIPE_SECRET_KEY to the terminal clipboard" {
		t.Fatalf("toast = %q", m.toast.text)
	}
}

func TestDefaultEffectsNeverTouchTheSystem(t *testing.T) {
	m := run(t, onSecrets(t), press("y"))
	if m.toast.level != toastError || !strings.Contains(m.toast.text, errUnavailable.Error()) {
		t.Fatalf("toast = %+v, want the copy reported as unavailable", m.toast)
	}
}

func TestToastExpiresOnlyForItsOwnGeneration(t *testing.T) {
	m := onSecrets(t)
	m.notify(toastInfo, "first")
	first := m.toast.gen
	m.notify(toastInfo, "second")

	m = run(t, m, toastExpiredMsg{gen: first})
	if m.toast.text != "second" {
		t.Fatalf("an older toast's timer cleared the newer one: %q", m.toast.text)
	}
	m = run(t, m, toastExpiredMsg{gen: m.toast.gen})
	if m.toast.text != "" {
		t.Fatal("the toast must clear when its own timer fires")
	}
	if got := lastLine(m.View()); !strings.HasPrefix(got, "↑/k up") {
		t.Fatalf("footer = %q, want the hints back", got)
	}
}
