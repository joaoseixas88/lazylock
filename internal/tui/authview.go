package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// authView renders the screens that come before the panes. It is a plain
// centred card rather than the four-pane frame, because there is nothing to
// navigate yet.
func (m Model) authView() string {
	var title string
	var lines []string

	switch m.state {
	case stateSetup:
		title = "Connect to Infisical"
		lines = []string{
			mutedStyle.Render("Which instance should lazylock read from?"),
			"",
			m.input.View(),
			"",
			mutedStyle.Render("enter  connect     esc  quit"),
		}
	case stateRestoring:
		title = "lazylock"
		lines = []string{mutedStyle.Render("Unlocking the stored session…")}
	case stateLogin:
		title = "Log in to " + hostOf(m.cfg.SiteURL)
		lines = []string{mutedStyle.Render("Your browser should have opened. If not, open this:"), ""}
		if m.login != nil {
			lines = append(lines, selectedStyle.Render(m.login.URL), "")
		}
		lines = append(lines,
			mutedStyle.Render("If the browser cannot reach lazylock, paste the token it shows:"),
			m.input.View(),
			"",
			mutedStyle.Render("enter  submit     esc  quit"),
		)
	}

	if m.authErr != nil {
		lines = append(lines, "", errorStyle.Render(oneLine(m.authErr)))
	}

	card := make([]string, 0, len(lines)+2)
	card = append(card, selectedStyle.Render(title), "")
	card = append(card, lines...)

	width := min(max(m.width-4, 20), 78)
	for i, line := range card {
		card[i] = ansi.Truncate(strings.ReplaceAll(line, "\n", " "), width, "…")
	}
	body := lipgloss.JoinVertical(lipgloss.Left, card...)
	return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, body)
}

func hostOf(siteURL string) string {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(siteURL, "https://"), "http://")
	if i := strings.IndexByte(trimmed, '/'); i >= 0 {
		return trimmed[:i]
	}
	if trimmed == "" {
		return "Infisical"
	}
	return trimmed
}
