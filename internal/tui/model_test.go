package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

func TestNavigationDrillsIntoContextAndMasksSecretValues(t *testing.T) {
	model := New(domain.DemoCatalog())

	for range 4 {
		model = send(model, tea.KeyMsg{Type: tea.KeyEnter})
	}

	if model.level != secretsLevel {
		t.Fatalf("level = %v, want secretsLevel", model.level)
	}
	if model.selectedFolderID == "" {
		t.Fatal("expected a selected folder")
	}
	if model.revealValue {
		t.Fatal("secret values must start hidden")
	}

	model = send(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	if !model.revealValue {
		t.Fatal("space should reveal the selected secret value")
	}

	model = send(model, tea.KeyMsg{Type: tea.KeyDown})
	if model.revealValue {
		t.Fatal("changing the selected secret must hide its value")
	}
}

func TestNavigationBackClearsDependentSelections(t *testing.T) {
	model := New(domain.DemoCatalog())
	for range 4 {
		model = send(model, tea.KeyMsg{Type: tea.KeyEnter})
	}

	model = send(model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.level != foldersLevel {
		t.Fatalf("level = %v, want foldersLevel", model.level)
	}
	if model.selectedFolderID != "" {
		t.Fatalf("selectedFolderID = %q, want empty", model.selectedFolderID)
	}
}

func send(model Model, msg tea.Msg) Model {
	next, _ := model.Update(msg)
	return next.(Model)
}
