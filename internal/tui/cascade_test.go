package tui

import (
	"testing"

	"github.com/joaoseixas88/lazylock/internal/clipboard"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
)

// The harness queues a command after the keys still waiting in the same run,
// so the keys below land before the debounced reload, as they would in real
// time inside the debounce window.

func TestActionsNeverReachThePreviousScopeDuringTheDebounce(t *testing.T) {
	m, spy := withClipboard(booted(t, fake.DemoCatalog()), clipboard.System, nil)
	m = run(t, m, press("2"))
	next, _ := m.Update(press("down"))
	if next.(Model).secrets.state != stateLoading {
		t.Fatal("the secrets of the previous scope must go away as soon as the selection moves")
	}
	run(t, m, press("down"), press("4"), press("y"))
	if len(spy.texts) != 0 {
		t.Fatalf("copied %q from the scope the user had left", spy.texts)
	}
}

func TestActionsNeverReachThePreviousProjectDuringTheDebounce(t *testing.T) {
	m, spy := withClipboard(booted(t, fake.DemoCatalog()), clipboard.System, nil)
	m = run(t, m, press("1"), press("down"), press("4"), press("y"))
	if len(spy.texts) != 0 {
		t.Fatalf("copied %q from the project the user had left", spy.texts)
	}
	if p, _ := m.projects.current(); p.ID != "website" {
		t.Fatalf("project = %q", p.ID)
	}
	if at, _ := m.scopes.current(); at.ProjectID != "website" {
		t.Fatalf("scopes still belong to %q", at.ProjectID)
	}
}
