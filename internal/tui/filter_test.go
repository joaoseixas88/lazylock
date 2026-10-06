package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
)

func typing(text string) []tea.Msg {
	var keys []tea.Msg
	for _, r := range text {
		keys = append(keys, press(string(r)))
	}
	return keys
}

func filtered(t *testing.T, m Model, pane, query string) Model {
	t.Helper()
	m = run(t, m, press(pane), press("/"))
	return run(t, m, typing(query)...)
}

func TestFilteringProjectsLoadsTheFirstMatch(t *testing.T) {
	m := filtered(t, booted(t, fake.DemoCatalog()), "1", "market")
	if p, _ := m.projects.current(); p.ID != "website" {
		t.Fatalf("current project = %q, want website", p.ID)
	}
	if at, _ := m.scopes.current(); at.ProjectID != "website" {
		t.Fatalf("scopes still belong to %q", at.ProjectID)
	}
}

func TestFilterThatMatchesNothingBlanksTheDependentPanes(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	m = run(t, m, tea.WindowSizeMsg{Width: 120, Height: 36})
	m = filtered(t, m, "1", "zzz")
	if m.scopes.state != stateIdle || m.secrets.state != stateIdle {
		t.Fatalf("scopes=%v secrets=%v, want both blank", m.scopes.state, m.secrets.state)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "No matches for “zzz”") {
		t.Fatalf("expected the empty filter to say so:\n%s", view)
	}

	m = run(t, m, press("esc"))
	if at, ok := m.scopes.current(); !ok || at.ProjectID != "payments" {
		t.Fatal("clearing the filter must bring the cascade back")
	}
}

func TestSecretsFilterMatchesKeysNeverValues(t *testing.T) {
	m := filtered(t, booted(t, fake.DemoCatalog()), "4", "sk_test")
	if len(m.secrets.visible()) != 0 {
		t.Fatal("a filter must never match on a value")
	}
	m = run(t, m, press("esc"))
	m = filtered(t, m, "4", "stripe")
	if s, _ := m.secrets.current(); s.Key != "STRIPE_SECRET_KEY" || len(m.secrets.visible()) != 1 {
		t.Fatalf("visible = %v", m.secrets.visible())
	}
}

func TestTypingInTheFilterNeverTriggersHotkeys(t *testing.T) {
	m := run(t, booted(t, fake.DemoCatalog()), press("4"), press("/"))
	for _, k := range []string{"q", "a", "y", "x", "v", "?", "1"} {
		next, cmd := m.Update(press(k))
		if quits(cmd) {
			t.Fatalf("%q quit the program while filtering", k)
		}
		m = next.(Model)
	}
	if !m.filtering || m.reveal.all || m.marks.count() != 0 || m.overlay != nil || m.activePane != secretsPane {
		t.Fatalf("a key typed into the filter reached the panes: %+v", m.reveal)
	}
	if got := m.filterInput.Value(); got != "qayxv?1" {
		t.Fatalf("filter = %q", got)
	}
}

func TestEnterKeepsTheFilterAndEscClearsIt(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	m = run(t, m, tea.WindowSizeMsg{Width: 120, Height: 36})
	m = filtered(t, m, "4", "str")
	m = run(t, m, press("enter"))
	if m.filtering || m.secrets.query != "str" {
		t.Fatalf("filtering=%v query=%q", m.filtering, m.secrets.query)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "[4] Secrets /str") {
		t.Fatalf("the title must show the filter:\n%s", view)
	}
	m = run(t, m, press("esc"))
	if m.secrets.query != "" || len(m.secrets.visible()) != 2 {
		t.Fatalf("esc must clear the filter, query=%q", m.secrets.query)
	}
}

func TestFilterPersistsAcrossScopes(t *testing.T) {
	m := filtered(t, booted(t, fake.DemoCatalog()), "4", "stripe")
	m = run(t, m, press("enter"), press("2"), press("down"), press("down"))
	m = run(t, m, press("4"))
	if s, _ := m.secrets.current(); s.Key != "STRIPE_SECRET_KEY" || len(m.secrets.visible()) != 1 {
		t.Fatalf("the filter must apply to the next scope too: %v", m.secrets.visible())
	}
}

func TestMarksSurviveAFilterAndShiftVMarksOnlyWhatIsVisible(t *testing.T) {
	m := run(t, booted(t, fake.DemoCatalog()), press("4"), press("V"))
	m = run(t, filtered(t, m, "4", "stripe"), press("enter"))
	if m.marks.count() != 2 {
		t.Fatalf("filtering dropped marks: %d", m.marks.count())
	}
	m = run(t, m, press("V"))
	if m.marks.count() != 1 {
		t.Fatalf("V must unmark only the visible secrets, left %d", m.marks.count())
	}
}

func TestStaleScopesReplyIsDroppedAfterFilteringAway(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	stale := m.scopes.gen
	m = filtered(t, m, "1", "market")
	ghost := []domain.Scope{{ProjectID: "payments", EnvSlug: "dev", EnvName: "Ghost", Path: "/"}}
	m = run(t, m, scopesLoadedMsg{gen: stale, projectID: "payments", items: ghost})
	if at, _ := m.scopes.current(); at.EnvName == "Ghost" {
		t.Fatal("a reply for the project filtered away must be dropped")
	}
}
