package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
)

func TestViewRendersPersistentPanelsWithPanelHotkeys(t *testing.T) {
	model := booted(t, fake.DemoCatalog())
	model = run(t, model, tea.WindowSizeMsg{Width: 120, Height: 36})
	view := model.View()

	for _, label := range []string{"[1] Projects", "[2] ›Development", "[3] Actions", "[4] Secrets"} {
		if !strings.Contains(view, label) {
			t.Fatalf("view does not contain %q", label)
		}
	}
	if !strings.Contains(view, "STRIPE_SECRET_KEY=••••••••") {
		t.Fatal("secrets must be visible and masked from the first render")
	}
	if strings.Contains(view, "1 projects • 2 context") {
		t.Fatal("panel focus shortcuts belong in panel titles, not the footer")
	}
}

func TestPanelHotkeysFocusPanelsAndResizeUsesTerminalDimensions(t *testing.T) {
	model := booted(t, fake.DemoCatalog())
	model = run(t, model, tea.WindowSizeMsg{Width: 120, Height: 36})
	if model.leftWidth <= 0 || model.rightWidth <= model.leftWidth {
		t.Fatalf("unexpected layout widths: left=%d right=%d", model.leftWidth, model.rightWidth)
	}

	model = run(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if model.activePane != contextPane {
		t.Fatalf("activePane = %v, want contextPane", model.activePane)
	}

	model = run(t, model, tea.KeyMsg{Type: tea.KeyDown})
	if model.scopes.cursor != 1 {
		t.Fatalf("scopes cursor = %d, want 1", model.scopes.cursor)
	}

	model = run(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	model = run(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	selected, _ := model.secrets.current()
	if !model.reveal.shows(selected.ID) {
		t.Fatal("space should reveal the selected secret value")
	}

	model = run(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	model = run(t, model, tea.KeyMsg{Type: tea.KeyUp})
	if model.reveal.shows(selected.ID) {
		t.Fatal("changing the selected context must hide its value")
	}
}

func TestResizeKeepsFramesAndFooterInsideTerminal(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	for _, size := range [][2]int{{200, 50}, {80, 24}, {60, 10}, {40, 7}, {80, 4}, {120, 36}, {10, 2}, {1, 1}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m = run(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			view := ansi.Strip(m.View())
			if lipgloss.Height(view) > size[1] {
				t.Fatalf("height overflow: %d > %d", lipgloss.Height(view), size[1])
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > size[0] {
					t.Fatalf("width overflow: %q", line)
				}
			}
			if size[0] >= 40 && size[1] >= 4 {
				for _, key := range []string{"[1]", "[2]", "[3]", "[4]"} {
					if !strings.Contains(view, key) {
						t.Fatalf("missing title %s", key)
					}
				}
				rows := strings.Split(view, "\n")
				if !strings.HasPrefix(rows[len(rows)-1], "↑/k") {
					t.Fatal("footer must be left aligned")
				}
				if lipgloss.Width(rows[0]) != size[0] {
					t.Fatal("panels must fill available width")
				}
			}
		})
	}
}

func TestFrameClipsLongContentWithoutWrapping(t *testing.T) {
	view := frame("An extremely long title", []string{strings.Repeat("界", 100)}, 12, 3, true)
	if lipgloss.Width(view) != 12 || lipgloss.Height(view) != 3 {
		t.Fatalf("frame escaped bounds: %q", view)
	}
}

// Rendering must be pure: the whole point of the async rework is that a frame
// costs nothing. This fails loudly if anything under View reaches the catalog.
func TestViewDoesNotTouchTheCatalog(t *testing.T) {
	cat := fake.DemoCatalog()
	m := booted(t, cat)
	m = run(t, m, tea.WindowSizeMsg{Width: 120, Height: 36})

	before := cat.Calls()
	for range 50 {
		_ = m.View()
	}
	if after := cat.Calls(); after != before {
		t.Fatalf("View made %d catalog calls, want 0", after-before)
	}
}

func TestInitCascadesProjectsToScopesToSecrets(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	for _, pane := range []struct {
		name  string
		state loadState
		count int
	}{
		{"projects", m.projects.state, len(m.projects.items)},
		{"scopes", m.scopes.state, len(m.scopes.items)},
		{"secrets", m.secrets.state, len(m.secrets.items)},
	} {
		if pane.state != stateLoaded {
			t.Fatalf("%s state = %v, want stateLoaded", pane.name, pane.state)
		}
		if pane.count == 0 {
			t.Fatalf("%s loaded nothing", pane.name)
		}
	}
}

func TestStaleSecretsReplyIsIgnored(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	at, _ := m.scopes.current()
	ghost := []domain.Secret{{ID: "ghost", Key: "GHOST", Value: "boo"}}

	t.Run("older generation", func(t *testing.T) {
		out := run(t, m, secretsLoadedMsg{gen: m.secrets.gen - 1, at: at, items: ghost})
		if strings.Contains(out.View(), "GHOST") {
			t.Fatal("a reply from a superseded request must be dropped")
		}
	})

	t.Run("different scope", func(t *testing.T) {
		other := at
		other.Path = "/somewhere-else"
		out := run(t, m, secretsLoadedMsg{gen: m.secrets.gen, at: other, items: ghost})
		if strings.Contains(out.View(), "GHOST") {
			t.Fatal("a reply addressed to another scope must be dropped")
		}
	})
}

func TestSwitchingProjectsDiscardsTheOldSecrets(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	stale := m.secrets.gen
	at, _ := m.scopes.current()

	m = run(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")}, tea.KeyMsg{Type: tea.KeyDown})
	if p, _ := m.projects.current(); p.ID != "website" {
		t.Fatalf("expected to have moved to the second project, got %q", p.ID)
	}

	m = run(t, m, secretsLoadedMsg{gen: stale, at: at, items: []domain.Secret{{Key: "GHOST", Value: "boo"}}})
	if strings.Contains(m.View(), "GHOST") {
		t.Fatal("secrets in flight for the previous project must not land")
	}
}

func TestCatalogErrorRendersInsideTheFrame(t *testing.T) {
	cat := fake.DemoCatalog()
	cat.SecretsErr = errors.New("boom: upstream refused the connection")
	m := booted(t, cat)
	m = run(t, m, tea.WindowSizeMsg{Width: 40, Height: 20})

	if m.secrets.state != stateFailed {
		t.Fatalf("secrets state = %v, want stateFailed", m.secrets.state)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "boom") {
		t.Fatalf("error text missing from view:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatalf("error text escaped the frame: %q", line)
		}
	}
}

func TestExpiredSessionGetsItsOwnMessage(t *testing.T) {
	cat := fake.DemoCatalog()
	cat.ProjectsErr = fmt.Errorf("list projects: %w", domain.ErrUnauthorized)
	m := booted(t, cat)
	m = run(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	if view := ansi.Strip(m.View()); !strings.Contains(view, "Session expired.") {
		t.Fatalf("expected the re-login hint, got:\n%s", view)
	}
}

func TestHiddenSecretNeverRevealsAValue(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	m = run(t, m, tea.WindowSizeMsg{Width: 120, Height: 36})

	// Production root is the scope holding the hidden secret.
	m = toProduction(t, m)
	if at, _ := m.scopes.current(); at.Path != "/" || at.EnvSlug != "prod" {
		t.Fatalf("expected to be on prod /, got %+v", at)
	}

	m = run(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")},
		tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "SIGNING_KEY=(no read access)") {
		t.Fatalf("hidden secret must say so:\n%s", view)
	}
}

func TestRevealedEmptySecretIsNotMistakenForAValue(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	m = run(t, m, tea.WindowSizeMsg{Width: 120, Height: 36})
	m = toProduction(t, m)
	m = run(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")},
		tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyDown},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})

	if view := ansi.Strip(m.View()); !strings.Contains(view, "LEGACY_FLAG=(empty)") {
		t.Fatalf("an empty value must read as empty:\n%s", view)
	}
}

func TestRetryReloadsTheFocusedPane(t *testing.T) {
	cat := fake.DemoCatalog()
	cat.SecretsErr = errors.New("transient")
	m := booted(t, cat)
	if m.secrets.state != stateFailed {
		t.Fatalf("secrets state = %v, want stateFailed", m.secrets.state)
	}

	cat.SecretsErr = nil
	m = run(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if m.secrets.state != stateLoaded || len(m.secrets.items) == 0 {
		t.Fatalf("retry did not reload: state=%v items=%d", m.secrets.state, len(m.secrets.items))
	}
}
