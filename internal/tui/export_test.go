package tui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/clipboard"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/export"
)

type gitSpy struct {
	exposure export.Exposure
	paths    []string
}

func (g *gitSpy) check(_ context.Context, path string) (export.Exposure, error) {
	g.paths = append(g.paths, path)
	return g.exposure, nil
}

func exporting(t *testing.T, exposure export.Exposure) (Model, *gitSpy) {
	t.Helper()
	git := &gitSpy{exposure: exposure}
	m := onSecrets(t)
	m.fx.Dir = t.TempDir()
	m.fx.GitExposure = git.check
	return m, git
}

func readFile(t *testing.T, path string) (string, os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data), info.Mode().Perm()
}

func TestExportWritesA0600FileInTheWorkingDirectory(t *testing.T) {
	m, _ := exporting(t, export.OutsideRepo)
	want := string(export.Render(export.Dotenv, m.secrets.items).Data)
	m = run(t, m, press("x"), press("enter"), press("enter"), press("enter"))

	data, perm := readFile(t, filepath.Join(m.fx.Dir, "dev.env"))
	if data != want || perm != 0o600 {
		t.Fatalf("wrote %q at %#o, want %q at 0600", data, perm, want)
	}
	if m.overlay != nil || m.toast.text != "Wrote 2 secrets to ./dev.env (0600)" {
		t.Fatalf("overlay=%v toast=%q", m.overlay, m.toast.text)
	}
}

func TestExampleExportIs0644AndSkipsTheGitCheck(t *testing.T) {
	m, git := exporting(t, export.Committable)
	m = run(t, m, press("x"), press("down"), press("down"), press("enter"), press("enter"), press("enter"))

	data, perm := readFile(t, filepath.Join(m.fx.Dir, ".env.example"))
	if perm != 0o644 || strings.Contains(data, "sk_test") {
		t.Fatalf("wrote %q at %#o", data, perm)
	}
	if len(git.paths) != 0 {
		t.Fatal("a file without values needs no git warning")
	}
}

func TestExportAsksBeforeOverwritingAndNoKeepsTheFile(t *testing.T) {
	m, _ := exporting(t, export.OutsideRepo)
	path := filepath.Join(m.fx.Dir, "dev.env")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	m = run(t, m, press("x"), press("enter"), press("enter"), press("enter"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "./dev.env exists and will be replaced") {
		t.Fatalf("expected an overwrite warning:\n%s", view)
	}
	m = run(t, m, press("n"))
	if data, _ := readFile(t, path); data != "old" {
		t.Fatalf("declining replaced the file: %q", data)
	}

	m = run(t, m, press("enter"))
	m = run(t, m, press("y"))
	if data, _ := readFile(t, path); data == "old" {
		t.Fatal("confirming must replace the file")
	}
	if m.overlay != nil {
		t.Fatal("the dialog must close after writing")
	}
}

func TestExportWarnsWhenGitWouldCommitTheFile(t *testing.T) {
	m, _ := exporting(t, export.Committable)
	m = run(t, m, press("x"), press("enter"), press("enter"), press("enter"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "is not in .gitignore") {
		t.Fatalf("expected a git warning:\n%s", view)
	}
	if _, err := os.Stat(filepath.Join(m.fx.Dir, "dev.env")); err == nil {
		t.Fatal("nothing may be written before the warning is answered")
	}
	m = run(t, m, press("y"))
	if _, err := os.Stat(filepath.Join(m.fx.Dir, "dev.env")); err != nil {
		t.Fatal("confirming must write the file")
	}
}

func TestExportWritesOnlyMarkedSecrets(t *testing.T) {
	m, _ := exporting(t, export.OutsideRepo)
	m = run(t, m, press("v"), press("x"))
	if title := m.overlay.title(m); !strings.Contains(title, "1 marked") {
		t.Fatalf("title = %q", title)
	}
	m = run(t, m, press("enter"), press("enter"), press("enter"))
	if data, _ := readFile(t, filepath.Join(m.fx.Dir, "dev.env")); data != "STRIPE_SECRET_KEY=sk_test_demo_123\n" {
		t.Fatalf("wrote %q", data)
	}
}

func TestExportToTheClipboardCopiesTheRenderedFile(t *testing.T) {
	m, _ := exporting(t, export.OutsideRepo)
	m, spy := withClipboard(m, clipboard.System, nil)
	want := string(export.Render(export.JSON, m.secrets.items).Data)
	m = run(t, m, press("x"), press("down"), press("enter"), press("down"), press("enter"))
	if !slices.Equal(spy.texts, []string{want}) {
		t.Fatalf("clipboard got %q, want %q", spy.texts, want)
	}
	if m.toast.text != "Copied 2 secrets as JSON" {
		t.Fatalf("toast = %q", m.toast.text)
	}
}

func TestExportSaysWhichSecretsWereLeftOut(t *testing.T) {
	m, _ := exporting(t, export.OutsideRepo)
	at, _ := m.scopes.current()
	m = run(t, m, secretsLoadedMsg{gen: m.secrets.gen, at: at, items: []domain.Secret{
		{ID: "a", Key: "OPEN", Value: "1"}, {ID: "b", Key: "SIGNING_KEY", Hidden: true},
	}})
	m = run(t, m, press("x"), press("enter"), press("enter"), press("enter"))
	if !strings.Contains(m.toast.text, "Wrote 1 secrets") || !strings.Contains(m.toast.text, "left out SIGNING_KEY") {
		t.Fatalf("toast = %q", m.toast.text)
	}
}

func TestTypingQInThePathNeitherClosesNorQuits(t *testing.T) {
	m, _ := exporting(t, export.OutsideRepo)
	m = run(t, m, press("x"), press("enter"), press("enter"), press("q"))
	e, ok := m.overlay.(exportOverlay)
	if !ok || !strings.HasSuffix(e.input.Value(), "q") {
		t.Fatalf("q must be typed into the path, overlay=%v", m.overlay)
	}
}

func TestStaleTargetCheckIsIgnored(t *testing.T) {
	m, _ := exporting(t, export.OutsideRepo)
	m = run(t, m, press("x"), press("enter"), press("enter"))
	e := m.overlay.(exportOverlay)
	e.step, e.gen, e.path = checkingPath, 7, filepath.Join(m.fx.Dir, "dev.env")
	m.overlay = e

	m = run(t, m, targetCheckedMsg{gen: 6, path: e.path})
	if m.overlay.(exportOverlay).step != checkingPath {
		t.Fatal("a check for an earlier submission must be ignored")
	}
}

func TestFileExportWithoutAWorkingDirectoryIsRefused(t *testing.T) {
	m, _ := exporting(t, export.OutsideRepo)
	m.fx.Dir = ""
	m = run(t, m, press("x"), press("enter"), press("enter"), press("enter"))
	e, ok := m.overlay.(exportOverlay)
	if !ok || e.err == nil || e.step != enterPath {
		t.Fatalf("a relative path with no working directory must be refused, overlay=%+v", m.overlay)
	}
}

func TestMissingDirectoryIsReportedInTheDialog(t *testing.T) {
	m, _ := exporting(t, export.OutsideRepo)
	m = run(t, m, press("x"), press("enter"), press("enter"))
	e := m.overlay.(exportOverlay)
	e.input.SetValue("./nope/dev.env")
	m.overlay = e
	m = run(t, m, press("enter"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "does not exist") {
		t.Fatalf("expected the missing directory to be named:\n%s", view)
	}
}
