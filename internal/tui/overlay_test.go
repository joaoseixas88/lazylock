package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
)

func TestQuestionMarkOpensHelpListingEveryBinding(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	m = run(t, m, tea.WindowSizeMsg{Width: 140, Height: 90}, press("?"))
	view := ansi.Strip(m.View())

	keys := reflect.ValueOf(m.keys)
	for i := range keys.NumField() {
		help := keys.Field(i).Interface().(key.Binding).Help()
		if !strings.Contains(view, help.Key) || !strings.Contains(view, help.Desc) {
			t.Errorf("help does not list %s (%s %s)", keys.Type().Field(i).Name, help.Key, help.Desc)
		}
	}
}

func TestKeysGoToTheOverlayWhileItIsOpen(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	m = run(t, m, press("?"), press("4"), press("j"))
	if m.overlay == nil {
		t.Fatal("a pane key closed the help")
	}
	if m.activePane != projectsPane || m.projects.cursor != 0 {
		t.Fatalf("a key reached the panes behind the help: pane=%v cursor=%d", m.activePane, m.projects.cursor)
	}
}

func TestEscQAndQuestionMarkCloseTheHelpWithoutQuitting(t *testing.T) {
	for _, k := range []string{"esc", "q", "?"} {
		t.Run(k, func(t *testing.T) {
			m := run(t, booted(t, fake.DemoCatalog()), press("?"))
			next, cmd := m.Update(press(k))
			if next.(Model).overlay != nil {
				t.Fatal("the help is still open")
			}
			if quits(cmd) {
				t.Fatal("closing the help quit the program")
			}
		})
	}
}

func TestCtrlCQuitsFromAnOverlay(t *testing.T) {
	m := run(t, booted(t, fake.DemoCatalog()), press("?"))
	if _, cmd := m.Update(press("ctrl+c")); !quits(cmd) {
		t.Fatal("ctrl+c must quit even with an overlay open")
	}
}

func TestOverlayStaysInsideTheTerminal(t *testing.T) {
	m := run(t, booted(t, fake.DemoCatalog()), press("?"))
	for _, size := range terminalSizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m = run(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			assertFits(t, m.View(), size[0], size[1])
		})
	}
}

func TestHelpScrollStopsAtTheLastScreenful(t *testing.T) {
	m := booted(t, fake.DemoCatalog())
	m = run(t, m, tea.WindowSizeMsg{Width: 80, Height: 8}, press("?"))
	for range 50 {
		m = run(t, m, press("j"))
	}
	bottom := m.View()
	m = run(t, m, press("k"))
	if m.View() == bottom {
		t.Fatal("one step up from the bottom must scroll")
	}
}
