package tui

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

func TestViewRendersPersistentPanelsWithPanelHotkeys(t *testing.T) {
	model := New(domain.DemoCatalog())
	model = run(t, model, tea.WindowSizeMsg{Width: 120, Height: 36})
	view := model.View()

	for _, label := range []string{"[1] Projects", "[2] Paths / Environments", "[3] Actions", "[4] Secrets"} {
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
	model := New(domain.DemoCatalog())
	model = run(t, model, tea.WindowSizeMsg{Width: 120, Height: 36})
	if model.leftWidth <= 0 || model.rightWidth <= model.leftWidth {
		t.Fatalf("unexpected layout widths: left=%d right=%d", model.leftWidth, model.rightWidth)
	}

	model = run(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if model.activePane != contextPane {
		t.Fatalf("activePane = %v, want contextPane", model.activePane)
	}

	model = run(t, model, tea.KeyMsg{Type: tea.KeyDown})
	if model.contextCursor != 1 {
		t.Fatalf("contextCursor = %d, want 1", model.contextCursor)
	}

	model = run(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	model = run(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	if !model.revealValue {
		t.Fatal("space should reveal the selected secret value")
	}

	model = run(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	model = run(t, model, tea.KeyMsg{Type: tea.KeyUp})
	if model.revealValue {
		t.Fatal("changing the selected context must hide its value")
	}
}

func TestResizeKeepsFramesAndFooterInsideTerminal(t *testing.T) {
	m := New(domain.DemoCatalog())
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
