package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/config"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
)

func projectForm(t *testing.T, m Model, name, description, slug string) Model {
	t.Helper()
	m = run(t, m, press("N"))
	m = run(t, m, typing(name)...)
	m = run(t, m, press("tab"))
	m = run(t, m, typing(description)...)
	m = run(t, m, press("tab"))
	return run(t, m, typing(slug)...)
}

func projectCount(t *testing.T, cat domain.Catalog) int {
	t.Helper()
	projects, err := cat.Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return len(projects)
}

func TestNewProjectIsCreatedAfterTheReviewAndSelected(t *testing.T) {
	m := run(t, filtered(t, writable(t, fake.DemoCatalog()), "1", "pay"), press("enter"))
	m = projectForm(t, m, "Billing", "Invoices and refunds", "billing")
	m = run(t, m, press("ctrl+s"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Create project Billing?") || !strings.Contains(view, "Invoices and refunds") {
		t.Fatalf("the review must come first:\n%s", view)
	}
	m = run(t, m, press("y"))
	if p, ok := m.projects.current(); !ok || p.ID != "billing" || m.overlay != nil || m.activePane != projectsPane {
		t.Fatalf("selected = %+v overlay = %T pane = %v", p, m.overlay, m.activePane)
	}
	if m.projects.query != "" || m.toast.text != "Created project Billing" {
		t.Fatalf("query = %q toast = %q", m.projects.query, m.toast.text)
	}
	if envs := m.environments(); len(envs) != 3 || m.secrets.state != stateLoaded {
		t.Fatalf("the new project must load down to its secrets: envs = %+v secrets = %v", envs, m.secrets.state)
	}
}

func TestNewProjectRefusesAnEmptyNameAndABadSlug(t *testing.T) {
	for _, c := range []struct{ name, slug, want string }{
		{"", "", "the name is empty"},
		{"   ", "", "the name is empty"},
		{"Billing", "Bad Slug", "in the slug"},
		{"Billing", "bill", "in the slug"},
		{"Billing", "bill--ing", "in the slug"},
		{"Billing", "-billing", "in the slug"},
	} {
		m := projectForm(t, writable(t, fake.DemoCatalog()), c.name, "", c.slug)
		m = run(t, m, press("ctrl+s"))
		p, ok := m.overlay.(projectOverlay)
		if !ok || p.step != editing || p.err == nil || !strings.Contains(p.err.Error(), c.want) {
			t.Fatalf("%+v: overlay = %+v", c, m.overlay)
		}
	}
}

func TestEnterWalksTheFieldsAndThenReviews(t *testing.T) {
	m := run(t, writable(t, fake.DemoCatalog()), press("N"))
	m = run(t, m, typing("Billing")...)
	m = run(t, m, press("enter"), press("enter"))
	if p := m.overlay.(projectOverlay); p.focus != slugField || p.step != editing {
		t.Fatalf("focus = %v step = %v", p.focus, p.step)
	}
	if m = run(t, m, press("enter")); m.overlay.(projectOverlay).step != reviewing {
		t.Fatal("enter on the last field must open the review")
	}
}

func TestOnlyYCreatesTheProject(t *testing.T) {
	for _, k := range []string{"n", "esc", "q", "enter", "N"} {
		t.Run(k, func(t *testing.T) {
			cat := fake.DemoCatalog()
			before := projectCount(t, cat)
			run(t, projectForm(t, writable(t, cat), "Billing", "", ""), press("ctrl+s"), press(k))
			if projectCount(t, cat) != before {
				t.Fatalf("%s created a project", k)
			}
		})
	}
}

func TestNoInTheProjectReviewGoesBackToTheForm(t *testing.T) {
	m := projectForm(t, writable(t, fake.DemoCatalog()), "Billing", "", "")
	m = run(t, m, press("ctrl+s"), press("n"))
	if p, ok := m.overlay.(projectOverlay); !ok || p.step != editing || p.draft().Name != "Billing" {
		t.Fatalf("overlay = %+v", m.overlay)
	}
}

func TestTheReviewWarnsAboutAProjectWithTheSameName(t *testing.T) {
	m := projectForm(t, writable(t, fake.DemoCatalog()), "payments api", "", "")
	m = run(t, m, press("ctrl+s"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Another project is already called payments api") {
		t.Fatalf("expected the warning:\n%s", view)
	}
}

func TestARejectedProjectReturnsToTheFormWithTheReason(t *testing.T) {
	m := projectForm(t, writable(t, fake.DemoCatalog()), "Payments v2", "", "payments")
	m = run(t, m, press("ctrl+s"), press("y"))
	if p, ok := m.overlay.(projectOverlay); !ok || p.step != editing || p.draft().Slug != "payments" {
		t.Fatalf("overlay = %+v, want the form back with the draft", m.overlay)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, `A project with the slug "payments" already exists`) {
		t.Fatalf("the reason must be on screen:\n%s", view)
	}
}

func TestAnUnknownProjectOutcomeSaysSoAndReloads(t *testing.T) {
	cat := fake.DemoCatalog()
	cat.WriteErr = context.DeadlineExceeded
	m := run(t, projectForm(t, writable(t, cat), "Billing", "", ""), press("ctrl+s"))
	gen := m.projects.gen
	m = run(t, m, press("y"))
	if m.overlay != nil || !strings.Contains(m.toast.text, "Could not tell whether project Billing was created") {
		t.Fatalf("overlay = %T toast = %q", m.overlay, m.toast.text)
	}
	if m.projects.gen == gen || m.projects.state != stateLoaded {
		t.Fatalf("the projects must be reloaded: gen %d → %d, state %v", gen, m.projects.gen, m.projects.state)
	}
}

type countingCreator struct {
	*fake.Catalog
	creates int
}

func (c *countingCreator) CreateProject(ctx context.Context, p domain.NewProject) (domain.Project, error) {
	c.creates++
	return c.Catalog.CreateProject(ctx, p)
}

func TestASecondYCreatesOneProject(t *testing.T) {
	counter := &countingCreator{Catalog: fake.DemoCatalog()}
	run(t, projectForm(t, writable(t, counter), "Billing", "", ""), press("ctrl+s"), press("y"), press("y"))
	if counter.creates != 1 {
		t.Fatalf("creates = %d, want 1", counter.creates)
	}
}

func TestReadOnlyNeverCreatesProjects(t *testing.T) {
	m := run(t, booted(t, fake.DemoCatalog()), press("N"))
	if m.overlay != nil || !strings.Contains(m.toast.text, "-readonly") {
		t.Fatalf("overlay = %T toast = %q", m.overlay, m.toast.text)
	}
	m = run(t, writable(t, readOnlyCatalog{fake.DemoCatalog()}), press("N"))
	if m.overlay != nil || !strings.Contains(m.toast.text, "cannot write") {
		t.Fatalf("overlay = %T toast = %q", m.overlay, m.toast.text)
	}
}

func TestNewProjectIsInTheMenuOnlyWhenItCanBeCreated(t *testing.T) {
	offers := func(m Model) bool {
		return slices.ContainsFunc(m.actions(), func(a action) bool { return a.title() == "new project" })
	}
	if offers(booted(t, fake.DemoCatalog())) || offers(writable(t, readOnlyCatalog{fake.DemoCatalog()})) {
		t.Fatal("new project is offered without a way to create one")
	}
	m := writable(t, fake.DemoCatalog())
	if !offers(m) {
		t.Fatal("a writable model must offer new project")
	}
	if m = choose(t, run(t, m, press("3")), "new project"); m.overlay == nil {
		t.Fatal("the menu entry must open the form")
	}
	if _, ok := m.overlay.(projectOverlay); !ok {
		t.Fatalf("overlay = %T", m.overlay)
	}
}

func TestKeysTypedInTheProjectFormNeverReachThePanes(t *testing.T) {
	m := run(t, writable(t, fake.DemoCatalog()), press("N"))
	m = run(t, m, typing("q1/[a")...)
	if p, ok := m.overlay.(projectOverlay); !ok || p.draft().Name != "q1/[a" || m.activePane != secretsPane {
		t.Fatalf("overlay = %+v pane = %v", m.overlay, m.activePane)
	}
}

func TestDiscardingTheProjectFormAsksAndNoKeepsTheDraft(t *testing.T) {
	m := projectForm(t, writable(t, fake.DemoCatalog()), "Billing", "", "")
	if m = run(t, m, press("esc")); m.overlay.(projectOverlay).step != discarding {
		t.Fatal("esc on a filled form must ask first")
	}
	m = run(t, m, press("n"))
	if p := m.overlay.(projectOverlay); p.step != editing || p.draft().Name != "Billing" {
		t.Fatalf("n must keep the draft: %+v", p)
	}
	if m = run(t, m, press("esc"), press("y")); m.overlay != nil {
		t.Fatalf("overlay = %T", m.overlay)
	}
	if m = run(t, m, press("N"), press("esc")); m.overlay != nil {
		t.Fatal("esc on an empty form must close it")
	}
}

func TestProjectFormFitsTheTerminal(t *testing.T) {
	m := projectForm(t, writable(t, fake.DemoCatalog()), "Payments API", strings.Repeat("long text ", 10), "")
	for _, step := range []tea.Msg{press("tab"), press("ctrl+s")} {
		m = run(t, m, step)
		for _, size := range terminalSizes {
			m = run(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			assertFits(t, m.View(), size[0], size[1])
		}
	}
}

func TestExpiredSessionWhileCreatingAProjectGoesToLogin(t *testing.T) {
	cat := fake.DemoCatalog()
	m := app(t, config.Config{SiteURL: "https://infisical.example.com"})
	m.state, m.writes, m.load.catalog, m.load.debounce = stateBrowsing, true, cat, 0
	m = run(t, m, exec(t, m.load.projects(m.projects.begin()))...)
	cat.WriteErr = fmt.Errorf("%w: expired", domain.ErrUnauthorized)

	m = run(t, projectForm(t, m, "Billing", "", ""), press("ctrl+s"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Create project Billing on infisical.example.com?") {
		t.Fatalf("the review must name the instance:\n%s", view)
	}
	next, create := m.Update(press("y"))
	next, _ = next.(Model).Update(create())
	if got := next.(Model); got.state != stateLogin {
		t.Fatalf("state = %v, want the login screen", got.state)
	}
}
