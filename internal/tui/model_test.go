package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

func TestViewRendersPersistentProjectContextAndSecretPanels(t *testing.T) {
	view := New(domain.DemoCatalog()).View()

	for _, label := range []string{"Projects", "Paths / Environments", "Actions", "Secrets"} {
		if !strings.Contains(view, label) {
			t.Fatalf("view does not contain %q", label)
		}
	}
	if !strings.Contains(view, "STRIPE_SECRET_KEY=••••••••") {
		t.Fatal("secrets must be visible and masked from the first render")
	}
}

func TestPanelSelectionChangesSecretContextAndHidesRevealedValue(t *testing.T) {
	model := New(domain.DemoCatalog())
	model = send(model, tea.KeyMsg{Type: tea.KeyTab})
	if model.activePane != contextPane {
		t.Fatalf("activePane = %v, want contextPane", model.activePane)
	}

	model = send(model, tea.KeyMsg{Type: tea.KeyDown})
	if model.contextCursor != 1 {
		t.Fatalf("contextCursor = %d, want 1", model.contextCursor)
	}

	model = send(model, tea.KeyMsg{Type: tea.KeyTab})
	model = send(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	if !model.revealValue {
		t.Fatal("space should reveal the selected secret value")
	}

	model = send(model, tea.KeyMsg{Type: tea.KeyTab})
	model = send(model, tea.KeyMsg{Type: tea.KeyUp})
	if model.revealValue {
		t.Fatal("changing the selected context must hide its value")
	}
}

func send(model Model, msg tea.Msg) Model {
	next, _ := model.Update(msg)
	return next.(Model)
}
