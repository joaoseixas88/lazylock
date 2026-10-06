package tui

import (
	"errors"
	"slices"
	"testing"

	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
)

type linkedCatalog struct{ *fake.Catalog }

func (linkedCatalog) WebURL(at domain.Scope) (string, error) {
	return "https://infisical.example.test/" + at.EnvSlug + at.Path, nil
}

type browserSpy struct {
	urls []string
	err  error
}

func (b *browserSpy) open(url string) error {
	b.urls = append(b.urls, url)
	return b.err
}

func TestOpenSendsTheScopeURLToTheBrowser(t *testing.T) {
	spy := &browserSpy{}
	m := booted(t, linkedCatalog{fake.DemoCatalog()})
	m.fx.OpenURL = spy.open
	m = run(t, m, press("2"), press("down"))
	m = run(t, m, press("o"))
	if !slices.Equal(spy.urls, []string{"https://infisical.example.test/dev/services"}) {
		t.Fatalf("browser got %q", spy.urls)
	}
	if m.toast.text != "Opened Development /services in the browser" {
		t.Fatalf("toast = %q", m.toast.text)
	}
}

func TestOpenFailureIsReported(t *testing.T) {
	spy := &browserSpy{err: errors.New("xdg-open: not found")}
	m := booted(t, linkedCatalog{fake.DemoCatalog()})
	m.fx.OpenURL = spy.open
	m = run(t, m, press("o"))
	if m.toast.level != toastError {
		t.Fatalf("toast = %+v", m.toast)
	}
}

func TestOpenWithoutWebSupportSaysSo(t *testing.T) {
	spy := &browserSpy{}
	m := booted(t, fake.DemoCatalog())
	m.fx.OpenURL = spy.open
	m = run(t, m, press("o"))
	if len(spy.urls) != 0 || m.toast.level != toastError {
		t.Fatalf("a catalog with no web UI must not open anything: urls=%q toast=%+v", spy.urls, m.toast)
	}
}

func TestLoginBrowserGoesThroughTheEffects(t *testing.T) {
	spy := &browserSpy{}
	m := booted(t, fake.DemoCatalog())
	if msg := m.openBrowser("https://infisical.example.test/login")(); msg != nil {
		t.Fatalf("msg = %v", msg)
	}
	m.fx.OpenURL = spy.open
	m.openBrowser("https://infisical.example.test/login")()
	if !slices.Equal(spy.urls, []string{"https://infisical.example.test/login"}) {
		t.Fatalf("browser got %q", spy.urls)
	}
}
