package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
)

func TestImportedSecretShowsWhereItComesFrom(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	m = run(t, m, tea.WindowSizeMsg{Width: 120, Height: 36}, press("2"), press("down"))
	m = run(t, m, press("4"))

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "DATABASE_URL=••••••••  ⇠ Development /") {
		t.Fatalf("an imported secret must name its origin:\n%s", view)
	}
	if strings.Contains(view, "REDIS_URL=••••••••  ⇠") {
		t.Fatalf("a secret stored here must not carry a badge:\n%s", view)
	}
}

func TestBadgeFallsBackToTheSlugForAnUnknownEnvironment(t *testing.T) {
	m := onSecrets(t)
	at, _ := m.scopes.current()
	m = run(t, m, secretsLoadedMsg{gen: m.secrets.gen, at: at, items: []domain.Secret{
		{ID: "x", Key: "FROM_ELSEWHERE", Value: "v", ImportedFrom: domain.Scope{EnvSlug: "qa", Path: "/shared"}},
	}})
	if view := ansi.Strip(m.View()); !strings.Contains(view, "⇠ qa /shared") {
		t.Fatalf("expected the slug when the environment is not in the tree:\n%s", view)
	}
}
