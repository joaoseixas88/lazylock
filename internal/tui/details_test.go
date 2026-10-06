package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/clipboard"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
)

func onServices(t *testing.T) Model {
	t.Helper()
	m := booted(t, fake.DemoCatalog())
	m = run(t, m, tea.WindowSizeMsg{Width: 120, Height: 36}, press("2"), press("down"))
	return run(t, m, press("4"))
}

func TestEnterShowsTheWholeMultilineValueOnlyWhenRevealed(t *testing.T) {
	m := run(t, onServices(t), press("down"), press("enter"))
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "[4] TLS_CERT") || strings.Contains(view, "MIIB") {
		t.Fatalf("details must open masked:\n%s", view)
	}

	m = run(t, m, press(" "))
	view = ansi.Strip(m.View())
	for _, line := range []string{"-----BEGIN CERTIFICATE-----", "MIIBszCCAVmgAwIBAgIUDEMO", "-----END CERTIFICATE-----"} {
		if !strings.Contains(view, line) {
			t.Fatalf("%q missing from the revealed value:\n%s", line, view)
		}
	}
}

func TestDetailsShowCommentTagsVersionAndOrigin(t *testing.T) {
	m := run(t, onSecrets(t), press("enter"))
	view := ansi.Strip(m.View())
	for _, want := range []string{"Test-mode key from the Stripe dashboard", "payments, stripe", "Version  3"} {
		if !strings.Contains(view, want) {
			t.Fatalf("%q missing from the details:\n%s", want, view)
		}
	}

	m = run(t, onServices(t), press("down"), press("down"), press("enter"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "From     Development /") {
		t.Fatalf("an imported secret must say where it comes from:\n%s", view)
	}
}

func TestDetailsOfAHiddenSecretNeverShowAValue(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	m = toProduction(t, run(t, m, tea.WindowSizeMsg{Width: 120, Height: 36}))
	m = run(t, m, press("4"), press("down"), press("enter"), press(" "), press("a"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "(no read access)") {
		t.Fatalf("expected the hidden secret to say so:\n%s", view)
	}
}

func TestDetailsOfAVanishedSecretSaySo(t *testing.T) {
	m := run(t, onSecrets(t), press("enter"))
	at, _ := m.scopes.current()
	m = run(t, m, secretsLoadedMsg{gen: m.secrets.gen, at: at, items: []domain.Secret{{ID: "other", Key: "OTHER", Value: "v"}}})
	if view := ansi.Strip(m.View()); !strings.Contains(view, "This secret no longer exists.") {
		t.Fatalf("details of a secret that left the list must say so:\n%s", view)
	}
}

func TestCopyFromDetailsCopiesThatSecret(t *testing.T) {
	m, spy := withClipboard(onSecrets(t), clipboard.System, nil)
	m = run(t, m, press("down"), press("enter"), press("y"), press("Y"))
	want := []string{"postgres://demo:demo@localhost/payments", "DATABASE_URL=postgres://demo:demo@localhost/payments"}
	if !slices.Equal(spy.texts, want) {
		t.Fatalf("clipboard got %q, want %q", spy.texts, want)
	}
	if m.overlay == nil {
		t.Fatal("copying must keep the details open")
	}
}

func TestDetailsStayInsideTheTerminal(t *testing.T) {
	m := run(t, onServices(t), press("down"), press("enter"), press(" "))
	for _, size := range terminalSizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m = run(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			assertFits(t, m.View(), size[0], size[1])
		})
	}
}

func TestEnterMovesFromProjectsToPathsToSecrets(t *testing.T) {
	m := run(t, booted(t, fake.DemoCatalog()), press("1"), press("enter"))
	if m.activePane != contextPane {
		t.Fatalf("enter on projects focused %v", m.activePane)
	}
	if m = run(t, m, press("enter")); m.activePane != secretsPane {
		t.Fatalf("enter on paths focused %v", m.activePane)
	}
}

func TestControlCharactersInAValueNeverReachTheTerminal(t *testing.T) {
	m := onSecrets(t)
	at, _ := m.scopes.current()
	evil := "x\x1b]52;c;ZXZpbA==\x07\x1b[2Jy"
	m = run(t, m, secretsLoadedMsg{gen: m.secrets.gen, at: at, items: []domain.Secret{{ID: "e", Key: "EVIL", Value: evil, Comment: evil}}})
	m = run(t, m, press("a"))
	for _, view := range []string{m.View(), run(t, m, press("enter")).View()} {
		if strings.Contains(view, "\x1b]52") || strings.Contains(view, "\x1b[2J") || !strings.Contains(view, "␛") {
			t.Fatalf("a value reached the terminal as control codes: %q", view)
		}
	}
}
