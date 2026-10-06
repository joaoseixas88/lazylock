package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
)

var (
	stgRoot     = domain.Scope{ProjectID: "payments", EnvSlug: "stg", EnvName: "Staging", Path: "/"}
	stgServices = domain.Scope{ProjectID: "payments", EnvSlug: "stg", EnvName: "Staging", Path: "/services"}
)

func copying(t *testing.T, m Model) copyToOverlay {
	t.Helper()
	c, ok := m.overlay.(copyToOverlay)
	if !ok {
		t.Fatalf("overlay = %T, want the copy dialog", m.overlay)
	}
	return c
}

func TestPlanCopyClassifiesEachKey(t *testing.T) {
	source := []domain.Secret{{Key: "SAME", Value: "1"}, {Key: "DIFF", Value: "new"}, {Key: "NEW", Value: "n"}, {Key: "UNREAD", Value: "u"}, {Key: "IMPORTED", Value: "i"}}
	stored := []domain.Secret{{Key: "SAME", Value: "1\n "}, {Key: "DIFF", Value: "old"}, {Key: "UNREAD", Hidden: true}}
	visible := append(slices.Clone(stored), domain.Secret{Key: "IMPORTED", Value: "x", ImportedFrom: devRoot})
	normalize := (&fake.Catalog{}).Normalize

	got := map[string]copyKind{}
	for _, r := range planCopy([]string{"SAME", "DIFF", "NEW", "UNREAD", "IMPORTED"}, source, stored, visible, normalize) {
		got[r.key] = r.kind
	}
	want := map[string]copyKind{"SAME": copySame, "DIFF": copyOverwrite, "NEW": copyCreate, "UNREAD": copyOverwriteUnread, "IMPORTED": copyHideImport}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("plan = %v, want %v", got, want)
	}
}

func TestCopyOverwritesOnlyWhatDiffers(t *testing.T) {
	cat := fake.DemoCatalog()
	m := run(t, writable(t, cat), press("V"), press("C"))
	if names := copying(t, m).candidates; len(names) != 2 || names[0].EnvSlug != "stg" {
		t.Fatalf("candidates = %+v", names)
	}
	m = run(t, m, press("enter"))
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "~ DATABASE_URL") || !strings.Contains(view, "= STRIPE_SECRET_KEY  same, skipped") || strings.Contains(view, "postgres://") {
		t.Fatalf("the plan must show what changes, masked:\n%s", view)
	}
	m = run(t, m, press("y"))
	if s := rawValue(t, cat, stgRoot, "DATABASE_URL"); s.Value != "postgres://demo:demo@localhost/payments" {
		t.Fatalf("staging DATABASE_URL = %q", s.Value)
	}
	if m.toast.text != "Copied 1 secret to Staging / (0 new, 1 overwritten)" || m.marks.count() != 0 {
		t.Fatalf("toast = %q marks = %d", m.toast.text, m.marks.count())
	}
}

func TestCopyingAFolderCreatesWithCommentsAndLeavesImportsOut(t *testing.T) {
	cat := fake.DemoCatalog()
	m := run(t, writable(t, cat), press("2"), press("down"))
	m = run(t, m, press("4"), press("V"), press("C"))
	c := copying(t, m)
	if c.target != stgServices || !slices.Contains(c.leftOut, "STRIPE_SECRET_KEY (imported)") {
		t.Fatalf("one candidate must skip the picker and imports stay out: %+v", c)
	}
	m = run(t, m, press("y"))
	if s := rawValue(t, cat, stgServices, "TLS_CERT"); s.Comment != "Self-signed, for local TLS only" || !strings.Contains(s.Value, "MIIB") {
		t.Fatalf("a new key must come with its comment: %+v", s)
	}
	if s := rawValue(t, cat, stgServices, "REDIS_URL"); s.Value != "redis://localhost:6379/0" {
		t.Fatalf("REDIS_URL = %q", s.Value)
	}
}

func TestCopyToProductionNeedsItsName(t *testing.T) {
	m := run(t, writable(t, fake.DemoCatalog()), press("C"), press("down"), press("enter"))
	m = run(t, m, press("y"))
	if c := copying(t, m); c.target.EnvSlug != "prod" || c.gate.step != gateTyping {
		t.Fatalf("target = %s gate = %v, want the name prompt for the target", c.target.EnvSlug, c.gate.step)
	}
}

func TestCopyPlansAgainWhenTheTargetMoved(t *testing.T) {
	cat := fake.DemoCatalog()
	m := run(t, writable(t, cat), press("down"), press("C"), press("enter"))
	cat.Edit(stgRoot, "DATABASE_URL", "changed-meanwhile")
	m = run(t, m, press("y"))
	c := copying(t, m)
	if !c.moved || c.gate.step != gateReview || c.rows[0].before != "changed-meanwhile" {
		t.Fatalf("overlay = %+v", c)
	}
	if s := rawValue(t, cat, stgRoot, "DATABASE_URL"); s.Value != "changed-meanwhile" {
		t.Fatal("nothing may be written over a change the user never saw")
	}
	m = run(t, m, press("y"))
	if s := rawValue(t, cat, stgRoot, "DATABASE_URL"); s.Value != "postgres://demo:demo@localhost/payments" {
		t.Fatalf("confirming the new plan must copy, got %q", s.Value)
	}
}

func TestCopyLeavesOutWhatCannotBeRead(t *testing.T) {
	m := toProduction(t, writable(t, fake.DemoCatalog()))
	m = run(t, m, press("4"), press("V"), press("C"))
	if c := copying(t, m); !slices.Contains(c.leftOut, "SIGNING_KEY (no read access)") {
		t.Fatalf("left out = %v", c.leftOut)
	}
}

func TestNothingToCopyWritesNothing(t *testing.T) {
	m := run(t, writable(t, fake.DemoCatalog()), press("C"), press("enter"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Nothing to copy") {
		t.Fatalf("expected the plan to say so:\n%s", view)
	}
	if m = run(t, m, press("y")); m.overlay != nil || m.toast.text != "" {
		t.Fatalf("overlay = %T toast = %q", m.overlay, m.toast.text)
	}
}

func TestCopyDialogFitsTheTerminal(t *testing.T) {
	m := run(t, writable(t, fake.DemoCatalog()), press("V"), press("C"), press("enter"), press(" "))
	for _, size := range terminalSizes {
		m = run(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		assertFits(t, m.View(), size[0], size[1])
	}
}
