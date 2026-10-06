package tui

import (
	"slices"
	"testing"
)

func words(items ...string) list[string] {
	l := list[string]{label: func(s string) string { return s }}
	l.accept(l.begin(), items, nil)
	return l
}

func TestFilterKeepsTheSelectedItemWhenItStillMatches(t *testing.T) {
	l := words("alpha", "beta", "gamma")
	l.move(1)
	l.setQuery("A")
	if got, _ := l.current(); got != "beta" {
		t.Fatalf("current = %q, want beta to stay selected", got)
	}
	l.setQuery("et")
	if got, _ := l.current(); got != "beta" || !slices.Equal(l.visible(), []string{"beta"}) {
		t.Fatalf("current = %q visible = %v", got, l.visible())
	}
}

func TestFilterFallsBackToTheFirstMatch(t *testing.T) {
	l := words("alpha", "beta", "gamma")
	l.setQuery("g")
	if got, _ := l.current(); got != "gamma" {
		t.Fatalf("current = %q, want the first match", got)
	}
}

func TestFilterWithoutMatchesSelectsNothing(t *testing.T) {
	l := words("alpha", "beta")
	l.setQuery("zz")
	if _, ok := l.current(); ok || len(l.visible()) != 0 {
		t.Fatal("nothing can be selected when nothing matches")
	}
	l.setQuery("")
	if got, _ := l.current(); got != "alpha" {
		t.Fatalf("clearing the filter must select again, got %q", got)
	}
}

func TestReloadReappliesTheFilter(t *testing.T) {
	l := words("alpha", "beta")
	l.setQuery("b")
	l.accept(l.begin(), []string{"bravo", "alpha", "bar"}, nil)
	if !slices.Equal(l.visible(), []string{"bravo", "bar"}) {
		t.Fatalf("visible = %v", l.visible())
	}
}

func TestResetKeepsTheFilter(t *testing.T) {
	l := words("alpha", "beta")
	l.setQuery("b")
	l.reset()
	l.accept(l.gen, []string{"bee", "ant"}, nil)
	if !slices.Equal(l.visible(), []string{"bee"}) {
		t.Fatalf("visible after reset = %v", l.visible())
	}
}
