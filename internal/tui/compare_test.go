package tui

import (
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

func kinds(rows []diffRow) map[string]diffKind {
	out := map[string]diffKind{}
	for _, row := range rows {
		out[row.key] = row.kind
	}
	return out
}

func TestDiffClassifiesEveryKey(t *testing.T) {
	left := []domain.Secret{{Key: "SAME", Value: "1"}, {Key: "CHANGED", Value: "a"}, {Key: "ONLY_LEFT", Value: "x"}}
	right := []domain.Secret{{Key: "SAME", Value: "1"}, {Key: "CHANGED", Value: "b"}, {Key: "ONLY_RIGHT", Value: "y"}}
	want := map[string]diffKind{"SAME": diffSame, "CHANGED": diffChanged, "ONLY_LEFT": diffOnlyLeft, "ONLY_RIGHT": diffOnlyRight}
	if got := kinds(diffSecrets(left, right)); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestHiddenValuesAreNeverReportedEqual(t *testing.T) {
	for _, pair := range [][2]domain.Secret{
		{{Key: "K", Hidden: true}, {Key: "K", Hidden: true}},
		{{Key: "K", Hidden: true}, {Key: "K", Value: ""}},
		{{Key: "K", Value: "v"}, {Key: "K", Hidden: true}},
	} {
		if got := kinds(diffSecrets(pair[:1], pair[1:]))["K"]; got != diffUnknown {
			t.Fatalf("%+v: kind %v, want unknown", pair, got)
		}
	}
}

func TestDiffSortsKeysIgnoringCase(t *testing.T) {
	rows := diffSecrets([]domain.Secret{{Key: "b"}, {Key: "C"}}, []domain.Secret{{Key: "A"}})
	var keys []string
	for _, row := range rows {
		keys = append(keys, row.key)
	}
	if !slices.Equal(keys, []string{"A", "b", "C"}) {
		t.Fatalf("keys = %v", keys)
	}
}

func onScope(t *testing.T, downs int) Model {
	t.Helper()
	m := booted(t, fake.DemoCatalog())
	m = run(t, m, tea.WindowSizeMsg{Width: 200, Height: 40}, press("2"))
	for range downs {
		m = run(t, m, press("down"))
	}
	return m
}

func TestCompareOffersOnlyEnvironmentsWithThePath(t *testing.T) {
	m := run(t, onScope(t, 0), press("c"))
	c, ok := m.overlay.(compareOverlay)
	if !ok {
		t.Fatalf("overlay = %T", m.overlay)
	}
	var names []string
	for _, s := range c.candidates {
		names = append(names, s.EnvName)
	}
	if !slices.Equal(names, []string{"Production", "Staging"}) {
		t.Fatalf("candidates = %v", names)
	}
}

func TestCompareWithOneCandidateSkipsThePicker(t *testing.T) {
	m := run(t, onScope(t, 1), press("c"))
	c, ok := m.overlay.(compareOverlay)
	if !ok || c.state != stateLoaded || c.b.EnvSlug != "stg" {
		t.Fatalf("overlay = %+v", m.overlay)
	}
	got := kinds(c.rows)
	if got["REDIS_URL"] != diffChanged || got["TLS_CERT"] != diffOnlyLeft || got["DATABASE_URL"] != diffOnlyLeft {
		t.Fatalf("rows = %v", got)
	}
}

func TestCompareWithNoOtherEnvironmentSaysSo(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	m = run(t, m, press("1"), press("down"))
	m = run(t, m, press("c"))
	if m.overlay != nil || !strings.Contains(m.toast.text, "No other environment") {
		t.Fatalf("overlay=%T toast=%q", m.overlay, m.toast.text)
	}
}

func TestCompareKeepsValuesMaskedUntilRevealed(t *testing.T) {
	m := run(t, onScope(t, 0), press("c"), press("down"), press("enter"))
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "~ DATABASE_URL  ••••••••  →  ••••••••") || strings.Contains(view, "postgres://") {
		t.Fatalf("values must stay masked:\n%s", view)
	}
	if !strings.Contains(view, "+ FEATURE_FLAGS") || !strings.Contains(view, "= STRIPE_SECRET_KEY") {
		t.Fatalf("expected an added and an equal row:\n%s", view)
	}

	m = run(t, m, press(" "))
	view = ansi.Strip(m.View())
	if !strings.Contains(view, "postgres://demo:demo@localhost/payments  →  postgres://demo:demo@staging/payments") {
		t.Fatalf("space must reveal the selected row:\n%s", view)
	}
	if strings.Contains(view, "checkout-v2") {
		t.Fatal("only the selected row may be revealed")
	}

	if m = run(t, m, press("esc")); m.reveal.one != "" || m.reveal.all {
		t.Fatal("closing the comparison must mask everything")
	}
}

func TestHideSameRowsToggles(t *testing.T) {
	m := run(t, onScope(t, 0), press("c"), press("down"), press("enter"))
	m = run(t, m, press("s"))
	if view := ansi.Strip(m.View()); strings.Contains(view, "= STRIPE_SECRET_KEY") {
		t.Fatalf("s must hide equal rows:\n%s", view)
	}
	if view := ansi.Strip(run(t, m, press("s")).View()); !strings.Contains(view, "= STRIPE_SECRET_KEY") {
		t.Fatalf("s again must show them:\n%s", view)
	}
}

func TestStaleComparisonIsIgnored(t *testing.T) {
	m := run(t, onScope(t, 1), press("c"))
	c := m.overlay.(compareOverlay)
	c.state = stateLoading
	m.overlay = c
	m = run(t, m, comparedMsg{gen: c.gen - 1, left: []domain.Secret{{Key: "GHOST", Value: "x"}}})
	if m.overlay.(compareOverlay).state != stateLoading {
		t.Fatal("a reply for an earlier comparison must be ignored")
	}
}

func TestExpiredSessionDuringCompareSendsBackToLogin(t *testing.T) {
	m := app(t, config.Config{SiteURL: "https://infisical.example.com"})
	m.state = stateBrowsing
	m.overlay = compareOverlay{state: stateLoading, gen: 3, b: domain.Scope{EnvSlug: "stg"}}
	next, _ := m.Update(comparedMsg{gen: 3, err: fmt.Errorf("list secrets: %w", domain.ErrUnauthorized)})
	if got := next.(Model); got.state != stateLogin || got.overlay != nil {
		t.Fatalf("state=%v overlay=%T", got.state, got.overlay)
	}
}

func TestCompareStaysInsideTheTerminal(t *testing.T) {
	m := run(t, onScope(t, 0), press("c"), press("down"), press("enter"))
	m = run(t, m, press("a"))
	for _, size := range terminalSizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m = run(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			assertFits(t, m.View(), size[0], size[1])
		})
	}
}
