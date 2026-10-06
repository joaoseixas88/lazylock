package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.width < 16 || m.height < 4 {
		return ansi.Truncate("Resize terminal · q quit", m.width, "")
	}
	if m.state != stateBrowsing {
		return m.authView()
	}
	left := lipgloss.JoinVertical(lipgloss.Left,
		m.panel("[1] Projects"+filterSuffix(m.projects.query), m.projects.query, m.projectItems(), m.projects.state, m.projects.err, projectsPane, m.leftWidth, m.projectHeight),
		m.panel(m.scopesTitle(), m.scopes.query, m.scopeItems(), m.scopes.state, m.scopes.err, contextPane, m.leftWidth, m.contextHeight),
		m.actionsPanel(),
	)
	right := m.rightPanel()
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	footer := ansi.Truncate(m.footer(), m.width, "")
	return body + "\n" + footer
}

func (m Model) rightPanel() string {
	if m.overlay == nil {
		title := "[4] Secrets"
		if n := m.marks.count(); n > 0 {
			title += fmt.Sprintf(" · %d marked", n)
		}
		title += filterSuffix(m.secrets.query)
		if at, ok := m.scopes.current(); ok && m.scopes.state == stateLoaded {
			title += " · " + printable(at.EnvName+" "+at.Path)
		}
		return m.panel(title, m.secrets.query, m.secretItems(), m.secrets.state, m.secrets.err, secretsPane, m.rightWidth, m.height-1)
	}
	lines := m.overlay.body(m, max(0, m.rightWidth-3), m.overlayHeight())
	padded := make([]string, len(lines))
	for i, line := range lines {
		padded[i] = " " + line
	}
	return frame(m.overlay.title(m), padded, m.rightWidth, m.height-1, true)
}

func (m Model) focused(p pane) bool { return m.overlay == nil && p == m.activePane }

// statusLines turns a pane's load state into what it should show, and reports
// whether those lines are real rows the cursor may point at.
func statusLines(state loadState, err error, items []string, query string) ([]string, bool) {
	switch {
	case state == stateIdle:
		return nil, false
	case state == stateLoading:
		return []string{mutedStyle.Render("Loading…")}, false
	case state == stateFailed:
		lines := []string{errorStyle.Render("Error: " + oneLine(err))}
		if errors.Is(err, domain.ErrUnauthorized) {
			lines = []string{errorStyle.Render("Session expired."), mutedStyle.Render("Log in again to continue.")}
		}
		return append(lines, mutedStyle.Render("r  retry")), false
	case len(items) == 0 && query != "":
		return []string{mutedStyle.Render("No matches for “" + query + "”")}, false
	case len(items) == 0:
		return []string{mutedStyle.Render("No items found.")}, false
	}
	return items, true
}

func oneLine(err error) string {
	if err == nil {
		return ""
	}
	return strings.Join(strings.Fields(err.Error()), " ")
}

func (m Model) panel(title, query string, items []string, state loadState, err error, target pane, width, height int) string {
	body, selectable := statusLines(state, err, items, query)
	lines := append([]string(nil), body...) // never alias the caller's slice
	if selectable {
		for i, item := range lines {
			if target == m.activePane && i == m.cursorFor(target) {
				lines[i] = selectedStyle.Render("› ") + item
			} else {
				lines[i] = "  " + item
			}
		}
		// Keep the selected row visible when the terminal becomes shorter.
		if capacity := max(0, height-2); capacity > 0 {
			start := max(0, m.cursorFor(target)-capacity+1)
			lines = lines[min(start, len(lines)):]
		}
	} else {
		for i, item := range lines {
			lines[i] = "  " + item
		}
	}
	return frame(title, lines, width, height, m.focused(target))
}

func (m Model) actionsPanel() string {
	return m.panel("[3] Actions", "", m.actionItems(), stateLoaded, nil, actionsPane, m.leftWidth, m.actionsHeight)
}

func frame(title string, lines []string, width, height int, focused bool) string {
	if width < 3 || height < 1 {
		return ""
	}
	border := mutedStyle
	if focused {
		border = selectedStyle
	}
	caption := ansi.Truncate(title, width-3, "")
	titleText := border.Render(caption)
	top := border.Render("╭─") + titleText + border.Render(strings.Repeat("─", max(0, width-lipgloss.Width(caption)-3))+"╮")
	if height == 1 {
		return top
	}
	innerWidth, innerHeight := width-2, height-2
	content := make([]string, 0, innerHeight)
	content = append(content, lines...)
	for len(content) < innerHeight {
		content = append(content, "")
	}
	content = content[:innerHeight]
	for i, line := range content {
		line = ansi.Truncate(strings.ReplaceAll(line, "\n", " "), innerWidth, "…")
		content[i] = border.Render("│") + line + strings.Repeat(" ", max(0, innerWidth-lipgloss.Width(line))) + border.Render("│")
	}
	bottom := border.Render("╰" + strings.Repeat("─", width-2) + "╯")
	return strings.Join(append([]string{top}, append(content, bottom)...), "\n")
}

func (m Model) cursorFor(target pane) int {
	switch target {
	case projectsPane:
		return m.projects.cursor
	case contextPane:
		return m.scopes.cursor
	case actionsPane:
		return m.menuCursor
	default:
		return m.secrets.cursor
	}
}

func (m Model) projectItems() (items []string) {
	for _, project := range m.projects.visible() {
		items = append(items, printable(project.Name))
	}
	return
}

// scopeItems draws the active environment's folders as a tree. A filtered
// tree loses the parents that give it meaning, so while filtering each row
// shows its whole path instead.
func (m Model) scopeItems() (items []string) {
	for _, item := range m.scopes.visible() {
		if m.scopes.query != "" {
			items = append(items, pathLabel(printable(item.Path)))
		} else {
			items = append(items, treeLabel(printable(item.Path)))
		}
	}
	return
}

func treeLabel(path string) string {
	if path == "/" {
		return "/"
	}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	return strings.Repeat("  ", len(segments)-1) + segments[len(segments)-1] + "/"
}

// scopesTitle shows the environments as tabs. The active one carries a marker
// as well as a style, so it still stands out where colours are off, and the
// tabs collapse to the active one when they do not fit.
func (m Model) scopesTitle() string {
	envs := m.environments()
	if m.scopes.state != stateLoaded || len(envs) == 0 {
		return "[2] Environments" + filterSuffix(m.scopes.query)
	}
	border, active := mutedStyle, lipgloss.NewStyle().Bold(true)
	if m.focused(contextPane) {
		border, active = selectedStyle, selectedStyle
	}
	tab := func(e environment) string {
		if e.slug == m.scopes.group {
			return active.Render("›" + printable(e.name))
		}
		return mutedStyle.Render(printable(e.name))
	}
	filter := border.Render(filterSuffix(m.scopes.query))

	tabs := make([]string, len(envs))
	for i, e := range envs {
		tabs[i] = tab(e)
	}
	full := border.Render("[2] ") + strings.Join(tabs, border.Render(" ─ ")) + filter
	if lipgloss.Width(full) <= m.leftWidth-3 {
		return full
	}
	at := max(0, slices.IndexFunc(envs, func(e environment) bool { return e.slug == m.scopes.group }))
	return border.Render("[2] ") + tab(envs[at]) + border.Render(fmt.Sprintf(" (%d/%d)", at+1, len(envs))) + filter
}

func filterSuffix(query string) string {
	if query == "" {
		return ""
	}
	return " · /" + printable(query)
}

// pathLabel dims all but the last segment, so nesting reads at a glance without
// the pane needing expand/collapse state.
func pathLabel(path string) string {
	if i := strings.LastIndex(strings.TrimSuffix(path, "/"), "/"); i >= 0 {
		return mutedStyle.Render(path[:i+1]) + path[i+1:]
	}
	return mutedStyle.Render(path)
}

func (m Model) secretItems() (items []string) {
	for _, secret := range m.secrets.visible() {
		row := fmt.Sprintf("%s=%s", printable(secret.Key), m.secretValue(secret))
		if from := secret.ImportedFrom; from != (domain.Scope{}) {
			row += mutedStyle.Render("  ⇠ " + printable(m.envName(from.EnvSlug)+" "+from.Path))
		}
		if m.marks.has(secret.ID) {
			row = selectedStyle.Render("● ") + row
		}
		items = append(items, row)
	}
	return
}

// secretValue renders the states a secret can be in. A hidden one says so
// whether or not it is revealed, so pressing space on it is never a silent
// no-op. A row has room for one line, so a multi-line value shows its first.
func (m Model) secretValue(s domain.Secret) string {
	switch {
	case s.Hidden:
		return mutedStyle.Render("(no read access)")
	case !m.reveal.shows(s.ID):
		return "••••••••"
	case s.Value == "":
		return mutedStyle.Render("(empty)")
	}
	if first, _, multiline := strings.Cut(s.Value, "\n"); multiline {
		return printable(first) + mutedStyle.Render(" …")
	}
	return printable(s.Value)
}

// footer names the account in play. Showing it is the one defence against a
// local process winning the login callback race: the Infisical flow has no
// nonce, so an unexpected email is what a user would notice. When the row is
// too short, the hints give way so the account stays on screen.
func (m Model) footer() string {
	left := mutedStyle.Render(m.hints())
	if m.filtering {
		left = m.filterInput.View() + mutedStyle.Render("  enter keep  •  esc clear  •  ↑/↓ move")
	} else if m.toast.text != "" {
		style := selectedStyle
		if m.toast.level == toastError {
			style = errorStyle
		}
		left = style.Render(printable(m.toast.text))
	}
	if m.account == "" {
		return left
	}
	who := m.account
	if m.store != nil {
		who += " (" + m.store.Backend() + ")"
	}
	who = mutedStyle.Render("  •  " + printable(who))
	if room := m.width - lipgloss.Width(who); room >= 10 {
		left = ansi.Truncate(left, room, "…")
	}
	return left + who
}

func (m Model) hints() string {
	bindings := m.keys.footer()
	if m.overlay != nil {
		bindings = m.overlay.hints(m.keys)
	}
	hints := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		help := binding.Help()
		hints = append(hints, help.Key+" "+help.Desc)
	}
	return strings.Join(hints, "  •  ")
}

func (m Model) envName(slug string) string {
	for _, s := range m.scopes.items {
		if s.EnvSlug == slug {
			return s.EnvName
		}
	}
	return slug
}
