package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/clipboard"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
)

func onPaths(t *testing.T, width int) Model {
	t.Helper()
	m := booted(t, fake.DemoCatalog())
	return run(t, m, tea.WindowSizeMsg{Width: width, Height: 36}, press("2"))
}

func scopeAt(t *testing.T, m Model) domain.Scope {
	t.Helper()
	at, ok := m.scopes.current()
	if !ok {
		t.Fatal("no path selected")
	}
	return at
}

func TestPathsPaneListsOnlyTheActiveEnvironmentAsATree(t *testing.T) {
	m := onPaths(t, 160)
	for _, at := range m.scopes.visible() {
		if at.EnvSlug != "dev" {
			t.Fatalf("a row of %s leaked into Development: %+v", at.EnvName, at)
		}
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"[2] ›Development ─ Staging ─ Production", "│› /", "│  services/", "│    api/"} {
		if !strings.Contains(view, want) {
			t.Fatalf("%q missing from the paths pane:\n%s", want, view)
		}
	}
}

func TestBracketsSwitchEnvironmentKeepingThePath(t *testing.T) {
	m := run(t, onPaths(t, 160), press("down"))
	m = run(t, m, press("]"))
	if at := scopeAt(t, m); at.EnvSlug != "stg" || at.Path != "/services" {
		t.Fatalf("] from dev /services landed on %s %s", at.EnvSlug, at.Path)
	}
	m = run(t, m, press("]"))
	if at := scopeAt(t, m); at.EnvSlug != "prod" || at.Path != "/" {
		t.Fatalf("a path missing in prod must fall back to the root, got %s %s", at.EnvSlug, at.Path)
	}
	m = run(t, m, press("]"))
	if at := scopeAt(t, m); at.EnvSlug != "dev" {
		t.Fatalf("] on the last environment must wrap around, got %s", at.EnvSlug)
	}
	m = run(t, m, press("["))
	if at := scopeAt(t, m); at.EnvSlug != "prod" {
		t.Fatalf("[ on the first environment must wrap around, got %s", at.EnvSlug)
	}
}

func TestSwitchingFallsBackToTheNearestParentFolder(t *testing.T) {
	m := run(t, onPaths(t, 160), press("down"), press("down"))
	if at := scopeAt(t, m); at.Path != "/services/api" {
		t.Fatalf("setup: at %s", at.Path)
	}
	m = run(t, m, press("]"))
	if at := scopeAt(t, m); at.EnvSlug != "stg" || at.Path != "/services" {
		t.Fatalf("expected the closest existing parent, got %s %s", at.EnvSlug, at.Path)
	}
}

func TestBracketsFromTheSecretsPaneReloadTheSecretsAndKeepFocus(t *testing.T) {
	m := run(t, booted(t, fake.DemoCatalog()), press("4"))
	m = run(t, m, press("]"))
	if m.activePane != secretsPane {
		t.Fatalf("focus moved to %v", m.activePane)
	}
	if !strings.Contains(ansi.Strip(m.View()), "FEATURE_FLAGS") {
		t.Fatalf("the secrets must be Staging's after ]:\n%s", ansi.Strip(m.View()))
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "[4] Secrets · Staging /") {
		t.Fatalf("the secrets title must name the scope:\n%s", view)
	}
}

func TestActionsRightAfterASwitchNeverReachThePreviousEnvironment(t *testing.T) {
	m, spy := withClipboard(onSecrets(t), clipboard.System, nil)
	m = run(t, m, press("]"), press("y"))
	if len(spy.texts) != 0 {
		t.Fatalf("copied %q from the environment the user had left", spy.texts)
	}
	m = run(t, onSecrets(t), press("]"), press("x"))
	if m.overlay != nil {
		t.Fatalf("export opened over the previous environment's secrets: %T", m.overlay)
	}
}

type secretsSpy struct {
	*fake.Catalog
	asked []domain.Scope
}

func (s *secretsSpy) Secrets(ctx context.Context, at domain.Scope) ([]domain.Secret, error) {
	s.asked = append(s.asked, at)
	return s.Catalog.Secrets(ctx, at)
}

func TestBracketsDuringAProjectChangeNeverLoadThePreviousProject(t *testing.T) {
	spy := &secretsSpy{Catalog: fake.DemoCatalog()}
	m := booted(t, spy)
	spy.asked = nil
	run(t, m, press("1"), press("down"), press("]"))
	for _, at := range spy.asked {
		if at.ProjectID == "payments" {
			t.Fatalf("] switched environment on the project being left: %+v", spy.asked)
		}
	}
}

func TestTheChosenEnvironmentSurvivesAProjectWithoutIt(t *testing.T) {
	m := onPaths(t, 160)
	m = run(t, m, press("]"))
	m = run(t, m, press("1"), press("down"))
	if at := scopeAt(t, m); at.EnvSlug != "prod" {
		t.Fatalf("website only has Production, got %s", at.EnvSlug)
	}
	m = run(t, m, press("up"))
	if at := scopeAt(t, m); at.EnvSlug != "stg" {
		t.Fatalf("back on payments the chosen Staging must return, got %s", at.EnvSlug)
	}
}

func TestPassingThroughAnotherProjectKeepsDevelopment(t *testing.T) {
	m := run(t, booted(t, fake.DemoCatalog()), press("1"), press("down"))
	m = run(t, m, press("up"))
	if at := scopeAt(t, m); at.EnvSlug != "dev" {
		t.Fatalf("a project without Development must not move the user to Production, got %s", at.EnvSlug)
	}
}

func TestProjectWithoutPathsShowsThePlainTitle(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	m = run(t, m, tea.WindowSizeMsg{Width: 120, Height: 36}, press("1"), press("down"), press("down"))
	if p, _ := m.projects.current(); p.ID != "homelab" {
		t.Fatalf("setup: at %s", p.ID)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "[2] Environments") {
		t.Fatalf("a project without paths must not draw tabs:\n%s", view)
	}
	if m = run(t, m, press("]")); m.scopes.state != stateLoaded {
		t.Fatal("] on a project without paths must do nothing")
	}
}

func TestFilteringThePathsShowsWholePaths(t *testing.T) {
	m := filtered(t, onPaths(t, 160), "2", "api")
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "/services/api") || strings.Contains(view, "│    api/") {
		t.Fatalf("a filtered row must show its whole path:\n%s", view)
	}
}

func TestFilterWithoutMatchesInTheNextEnvironmentBlanksTheSecrets(t *testing.T) {
	m := run(t, filtered(t, onPaths(t, 160), "2", "api"), press("enter"))
	m = run(t, m, press("]"))
	if _, ok := m.scopes.current(); ok {
		t.Fatal("Staging has no folder matching api")
	}
	if m.secrets.state != stateIdle {
		t.Fatalf("secrets state = %v, want blank", m.secrets.state)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "[4] Secrets─") {
		t.Fatalf("with no path selected the secrets title must not name one:\n%s", view)
	}
}

func TestTabsCollapseWhenTheyDoNotFit(t *testing.T) {
	view := ansi.Strip(onPaths(t, 90).View())
	if !strings.Contains(view, "[2] ›Development (1/3)") {
		t.Fatalf("expected the collapsed tab:\n%s", view)
	}
}

func TestOneEnvironmentProjectIgnoresTheBrackets(t *testing.T) {
	m := run(t, booted(t, fake.DemoCatalog()), press("1"), press("down"))
	before := scopeAt(t, m)
	if m = run(t, m, press("]")); scopeAt(t, m) != before {
		t.Fatal("] must do nothing with a single environment")
	}
}

func TestEndingASessionForgetsTheEnvironment(t *testing.T) {
	m := run(t, onPaths(t, 160), press("]"))
	m.endSession()
	if m.scopes.group != "" || m.scopes.preferred != "" {
		t.Fatalf("group=%q preferred=%q", m.scopes.group, m.scopes.preferred)
	}
}

func TestThePathSurvivesEnvironmentsThatLackIt(t *testing.T) {
	m := run(t, onPaths(t, 160), press("down"), press("down"))
	for range 3 {
		m = run(t, m, press("]"))
	}
	if at := scopeAt(t, m); at.EnvSlug != "dev" || at.Path != "/services/api" {
		t.Fatalf("back in Development the chosen folder must return, got %s %s", at.EnvSlug, at.Path)
	}
}

func TestAProjectChangeForgetsTheChosenPath(t *testing.T) {
	m := run(t, onPaths(t, 160), press("down"))
	m = run(t, m, press("1"), press("down"))
	if m.chosenPath != "" {
		t.Fatalf("chosenPath = %q after changing project", m.chosenPath)
	}
}
