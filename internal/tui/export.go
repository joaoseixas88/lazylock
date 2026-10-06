package tui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/atomicfile"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/export"
)

type exportStep uint8

const (
	pickFormat exportStep = iota
	pickDestination
	enterPath
	checkingPath
	confirmWrite
	writingFile
)

var destinations = []string{"File", "Clipboard"}

type exportOverlay struct {
	step     exportStep
	at       domain.Scope
	secrets  []domain.Secret
	marked   bool
	format   int
	dest     int
	input    textinput.Model
	path     string
	warnings []string
	err      error
	gen      int
}

type targetCheckedMsg struct {
	gen      int
	path     string
	exists   bool
	exposure export.Exposure
	gitErr   error
	err      error
}

type exportedMsg struct {
	path    string
	count   int
	leftOut []string
	perm    fs.FileMode
	err     error
}

func (m *Model) openExport() tea.Cmd {
	at, ok := m.scopes.current()
	if !ok || m.secrets.state != stateLoaded {
		return m.notify(toastError, "Nothing to export yet")
	}
	secrets := m.secrets.items
	if m.marks.count() > 0 {
		secrets = m.chosen()
	}
	m.overlay = exportOverlay{at: at, secrets: secrets, marked: m.marks.count() > 0}
	return nil
}

func (e exportOverlay) chosenFormat() export.Format { return export.Formats[e.format] }

func (e exportOverlay) title(Model) string {
	what := fmt.Sprintf("%d secrets", len(e.secrets))
	if e.marked {
		what = fmt.Sprintf("%d marked", len(e.secrets))
	}
	return fmt.Sprintf("[4] Export %s · %s", printable(e.at.EnvName+" "+e.at.Path), what)
}

func (e exportOverlay) body(m Model, _, _ int) []string {
	var lines []string
	choose := func(heading string, options []string, cursor int) {
		lines = append(lines, mutedStyle.Render(heading))
		for i, option := range options {
			if i == cursor {
				lines = append(lines, selectedStyle.Render("› ")+option)
			} else {
				lines = append(lines, "  "+option)
			}
		}
	}
	formats := make([]string, len(export.Formats))
	for i, f := range export.Formats {
		formats[i] = f.String()
	}

	if e.step == pickFormat {
		choose("Format", formats, e.format)
		return lines
	}
	lines = append(lines, mutedStyle.Render("Format       ")+e.chosenFormat().String())
	if e.step == pickDestination {
		choose("Destination", destinations, e.dest)
		return lines
	}
	lines = append(lines, mutedStyle.Render("Destination  ")+destinations[e.dest], "")

	switch e.step {
	case enterPath:
		lines = append(lines, mutedStyle.Render("Path"), e.input.View())
	case checkingPath:
		lines = append(lines, mutedStyle.Render("Checking "+m.fx.display(e.path)+"…"))
	case writingFile:
		lines = append(lines, mutedStyle.Render("Writing "+m.fx.display(e.path)+"…"))
	case confirmWrite:
		lines = append(lines, "Write "+m.fx.display(e.path)+"?")
		for _, warning := range e.warnings {
			lines = append(lines, errorStyle.Render("! ")+warning)
		}
	}
	if e.err != nil {
		lines = append(lines, "", errorStyle.Render(oneLine(e.err)))
	}
	return lines
}

func (e exportOverlay) hints(k keyMap) []key.Binding {
	switch e.step {
	case enterPath:
		return []key.Binding{key.NewBinding(key.WithHelp("enter", "write")), key.NewBinding(key.WithHelp("esc", "back"))}
	case confirmWrite:
		return []key.Binding{key.NewBinding(key.WithHelp("y", "write")), key.NewBinding(key.WithHelp("n", "back"))}
	case checkingPath, writingFile:
		return []key.Binding{key.NewBinding(key.WithHelp("esc", "cancel"))}
	}
	return []key.Binding{key.NewBinding(key.WithHelp("↑/↓", "choose")), key.NewBinding(key.WithHelp("enter", "next")), k.Close}
}

func (e exportOverlay) update(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch e.step {
	case pickFormat, pickDestination:
		return e.choose(m, k)
	case enterPath:
		switch k.Type {
		case tea.KeyEsc:
			e.step, e.err = pickDestination, nil
		case tea.KeyEnter:
			return e.submitPath(m)
		default:
			e.input, cmd = e.input.Update(k)
		}
	case confirmWrite:
		switch k.String() {
		case "y", "enter":
			return e.write(m)
		case "n", "esc":
			e.step = enterPath
		}
	case checkingPath, writingFile:
		if k.Type == tea.KeyEsc {
			m.overlay = nil
			return m, nil
		}
	}
	m.overlay = e
	return m, cmd
}

func (e exportOverlay) choose(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	cursor, count := &e.format, len(export.Formats)
	if e.step == pickDestination {
		cursor, count = &e.dest, len(destinations)
	}
	switch {
	case key.Matches(k, m.keys.Up):
		*cursor = max(0, *cursor-1)
	case key.Matches(k, m.keys.Down):
		*cursor = min(count-1, *cursor+1)
	case k.Type == tea.KeyEsc && e.step == pickDestination:
		e.step = pickFormat
	case key.Matches(k, m.keys.Close):
		m.overlay = nil
		return m, nil
	case k.Type == tea.KeyEnter && e.step == pickFormat:
		e.step = pickDestination
	case k.Type == tea.KeyEnter && destinations[e.dest] == "Clipboard":
		m.overlay = nil
		return m, m.exportToClipboard(e)
	case k.Type == tea.KeyEnter:
		e.step, e.err = enterPath, nil
		e.input = staticInput("› ", m.fx.display(m.fx.join(e.chosenFormat().FileName(e.at.EnvSlug))))
	}
	m.overlay = e
	return m, nil
}

func (e exportOverlay) submitPath(m Model) (Model, tea.Cmd) {
	path, err := m.fx.resolve(e.input.Value())
	if err != nil {
		e.err = err
		m.overlay = e
		return m, nil
	}
	m.seq++
	e.step, e.path, e.err, e.gen = checkingPath, path, nil, m.seq
	m.overlay = e
	return m, checkTarget(m.load.ctx, e.gen, path, e.chosenFormat(), m.fx.GitExposure, m.fx.display)
}

func checkTarget(ctx context.Context, gen int, path string, format export.Format, exposure func(context.Context, string) (export.Exposure, error), display func(string) string) tea.Cmd {
	return func() tea.Msg {
		msg := targetCheckedMsg{gen: gen, path: path}
		dir := filepath.Dir(path)
		if info, err := os.Stat(dir); err != nil {
			msg.err = fmt.Errorf("%s does not exist", display(dir))
			return msg
		} else if !info.IsDir() {
			msg.err = fmt.Errorf("%s is not a directory", display(dir))
			return msg
		}
		if info, err := os.Lstat(path); err == nil {
			if info.IsDir() {
				msg.err = fmt.Errorf("%s is a directory", display(path))
				return msg
			}
			msg.exists = true
		}
		if format.CarriesValues() && exposure != nil {
			ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			msg.exposure, msg.gitErr = exposure(ctx, path)
		}
		return msg
	}
}

func (m Model) targetChecked(msg targetCheckedMsg) (Model, tea.Cmd) {
	e, ok := m.overlay.(exportOverlay)
	if !ok || e.step != checkingPath || e.gen != msg.gen {
		return m, nil
	}
	if msg.err != nil {
		e.step, e.err = enterPath, msg.err
		m.overlay = e
		return m, nil
	}
	shown := m.fx.display(msg.path)
	e.warnings = nil
	if msg.exists {
		e.warnings = append(e.warnings, shown+" exists and will be replaced")
	}
	if msg.gitErr != nil {
		e.warnings = append(e.warnings, "could not ask git whether "+shown+" is ignored: "+oneLine(msg.gitErr))
	} else if msg.exposure == export.Committable {
		e.warnings = append(e.warnings, shown+" is not in .gitignore, so git would offer to commit it")
	}
	if len(e.warnings) > 0 {
		e.step = confirmWrite
		m.overlay = e
		return m, nil
	}
	return e.write(m)
}

func (e exportOverlay) write(m Model) (Model, tea.Cmd) {
	format := e.chosenFormat()
	rendered := export.Render(format, e.secrets)
	e.step = writingFile
	m.overlay = e
	path, perm := e.path, format.Perm()
	return m, func() tea.Msg {
		return exportedMsg{path: path, count: rendered.Count, leftOut: rendered.LeftOut, perm: perm,
			err: atomicfile.Write(path, rendered.Data, perm)}
	}
}

func (m Model) exported(msg exportedMsg) (Model, tea.Cmd) {
	if e, ok := m.overlay.(exportOverlay); ok && e.step == writingFile && e.path == msg.path {
		if msg.err != nil {
			e.step, e.err = enterPath, msg.err
			m.overlay = e
			return m, nil
		}
		m.overlay = nil
	}
	if msg.err != nil {
		return m, m.notify(toastError, "Could not write "+m.fx.display(msg.path)+": "+oneLine(msg.err))
	}
	text := fmt.Sprintf("Wrote %d secrets to %s (%#o)", msg.count, m.fx.display(msg.path), msg.perm)
	return m, m.notify(toastInfo, text+leftOutNote(msg.leftOut))
}

func (m *Model) exportToClipboard(e exportOverlay) tea.Cmd {
	format := e.chosenFormat()
	rendered := export.Render(format, e.secrets)
	if rendered.Count == 0 {
		return m.notify(toastError, "Nothing to copy"+leftOutNote(rendered.LeftOut))
	}
	what := fmt.Sprintf("%d secrets as %s", rendered.Count, format)
	return m.copyText(what+leftOutNote(rendered.LeftOut), string(rendered.Data))
}

func leftOutNote(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	return " (left out " + strings.Join(keys, ", ") + ")"
}

func (fx Effects) join(name string) string {
	if fx.Dir == "" {
		return name
	}
	return filepath.Join(fx.Dir, name)
}

func (fx Effects) resolve(input string) (string, error) {
	path := strings.TrimSpace(input)
	switch {
	case path == "":
		return "", errors.New("type a path to write to")
	case path == "~" || strings.HasPrefix(path, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	case !filepath.IsAbs(path) && fx.Dir == "":
		return "", fmt.Errorf("writing files is %w", errUnavailable)
	case !filepath.IsAbs(path):
		path = filepath.Join(fx.Dir, path)
	}
	return filepath.Clean(path), nil
}

func (fx Effects) display(path string) string {
	if fx.Dir != "" {
		if rel, err := filepath.Rel(fx.Dir, path); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return "./" + rel
		}
	}
	return path
}
