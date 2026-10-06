package tui

import (
	"errors"
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

func grouped(items ...string) list[string] {
	l := list[string]{label: func(s string) string { return s }, groupOf: func(s string) string { return s[:1] }}
	l.accept(l.begin(), items, nil)
	return l
}

func TestGroupShowsOnlyItsItemsAndMoveStaysInside(t *testing.T) {
	l := grouped("a1", "a2", "b1")
	if !slices.Equal(l.visible(), []string{"a1", "a2"}) {
		t.Fatalf("visible = %v, want the first group", l.visible())
	}
	l.move(5)
	if got, _ := l.current(); got != "a2" {
		t.Fatalf("move left the group: %q", got)
	}
	l.setGroup("b")
	if !slices.Equal(l.visible(), []string{"b1"}) {
		t.Fatalf("visible = %v", l.visible())
	}
}

func TestReloadPrefersTheChosenGroupWithoutForgettingIt(t *testing.T) {
	l := grouped("a1", "b1")
	l.setGroup("b")
	l.accept(l.begin(), []string{"a2", "c1"}, nil)
	if l.group != "a" || l.preferred != "b" {
		t.Fatalf("group=%q preferred=%q, want a shown and b remembered", l.group, l.preferred)
	}
	l.accept(l.begin(), []string{"a3", "b3"}, nil)
	if l.group != "b" {
		t.Fatalf("group = %q, want the chosen b back", l.group)
	}
}

func TestEmptyOrFailedReloadKeepsTheGroup(t *testing.T) {
	l := grouped("a1", "b1")
	l.setGroup("b")
	l.accept(l.begin(), nil, nil)
	l.accept(l.begin(), nil, errors.New("boom"))
	if l.group != "b" || len(l.visible()) != 0 {
		t.Fatalf("group=%q visible=%v", l.group, l.visible())
	}
}

func TestResetKeepsTheGroup(t *testing.T) {
	l := grouped("a1", "b1")
	l.setGroup("b")
	l.reset()
	l.accept(l.gen, []string{"a9", "b9"}, nil)
	if !slices.Equal(l.visible(), []string{"b9"}) {
		t.Fatalf("visible after reset = %v", l.visible())
	}
}

func TestSetGroupAppliesTheQuery(t *testing.T) {
	l := grouped("a1", "b1", "b2")
	l.setQuery("2")
	l.setGroup("b")
	if !slices.Equal(l.visible(), []string{"b2"}) {
		t.Fatalf("visible = %v", l.visible())
	}
}
