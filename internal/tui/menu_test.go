package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
)

func onMenu(t *testing.T) Model {
	t.Helper()
	m := booted(t, fake.DemoCatalog())
	return run(t, m, tea.WindowSizeMsg{Width: 120, Height: 50}, press("3"))
}

func menuIndex(t *testing.T, m Model, title string) int {
	t.Helper()
	for i, a := range m.actions() {
		if a.title() == title {
			return i
		}
	}
	t.Fatalf("no %q action", title)
	return -1
}

func choose(t *testing.T, m Model, title string) Model {
	t.Helper()
	for range menuIndex(t, m, title) {
		m = run(t, m, press("down"))
	}
	return run(t, m, press("enter"))
}

func TestActionsMenuListsEachActionWithItsHotkey(t *testing.T) {
	m := onMenu(t)
	view := ansi.Strip(m.View())
	for _, a := range m.actions() {
		row := a.binding.Help().Key + strings.Repeat(" ", 4-len([]rune(a.binding.Help().Key))) + a.title()
		if !strings.Contains(view, row) {
			t.Fatalf("menu row %q missing:\n%s", row, view)
		}
	}
}

func TestEnterRunsTheSelectedActionOnTheSecrets(t *testing.T) {
	m := choose(t, onMenu(t), "reveal all")
	if !m.reveal.all || m.activePane != secretsPane {
		t.Fatalf("reveal all from the menu: all=%v pane=%v", m.reveal.all, m.activePane)
	}
}

func TestMenuOpensTheExportDialog(t *testing.T) {
	m := choose(t, onMenu(t), "export")
	if _, ok := m.overlay.(exportOverlay); !ok {
		t.Fatalf("overlay = %T, want the export dialog", m.overlay)
	}
}

func TestMenuStartsAFilterOnTheSecrets(t *testing.T) {
	m := choose(t, onMenu(t), "filter secrets")
	if !m.filtering || m.activePane != secretsPane {
		t.Fatalf("filtering=%v pane=%v", m.filtering, m.activePane)
	}
}

func TestMenuCursorStaysInsideTheMenu(t *testing.T) {
	m := onMenu(t)
	for range 50 {
		m = run(t, m, press("down"))
	}
	if m.menuCursor != len(m.actions())-1 {
		t.Fatalf("cursor = %d of %d", m.menuCursor, len(m.actions()))
	}
}
