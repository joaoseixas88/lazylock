// Package clipboard puts text on the system clipboard, or on the terminal's
// through OSC 52 when there is no system clipboard within reach.
package clipboard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

type Via int

const (
	Terminal Via = iota
	System
)

const maxTerminalPayload = 100 << 10

var ErrTooLarge = errors.New("too large for the terminal clipboard")

type Clipboard struct {
	terminal io.Writer
	getenv   func(string) string
	lookPath func(string) (string, error)
	goos     string
	pipe     func(ctx context.Context, stdin, name string, args ...string) error
	probe    func(ctx context.Context, name string, args ...string) (string, error)

	sensitiveOnce sync.Once
	sensitive     bool
}

func New(terminal io.Writer) *Clipboard {
	return &Clipboard{
		terminal: terminal,
		getenv:   os.Getenv,
		lookPath: exec.LookPath,
		goos:     runtime.GOOS,
		pipe:     runPipe,
		probe:    runProbe,
	}
}

type command struct {
	name string
	args []string
}

func (c *Clipboard) Copy(ctx context.Context, text string) (Via, error) {
	cmd, ok := c.command(ctx)
	if !ok {
		return Terminal, c.osc52(text)
	}
	if err := c.pipe(ctx, text, cmd.name, cmd.args...); err != nil {
		return System, fmt.Errorf("%s: %w", cmd.name, err)
	}
	return System, nil
}

func (c *Clipboard) command(ctx context.Context) (command, bool) {
	if c.remote() {
		return command{}, false
	}
	switch {
	case c.goos == "darwin" && c.has("pbcopy"):
		return command{name: "pbcopy"}, true
	case c.getenv("WAYLAND_DISPLAY") != "" && c.has("wl-copy"):
		if c.supportsSensitive(ctx) {
			return command{name: "wl-copy", args: []string{"--sensitive"}}, true
		}
		return command{name: "wl-copy"}, true
	case c.getenv("DISPLAY") != "" && c.has("xclip"):
		return command{name: "xclip", args: []string{"-selection", "clipboard"}}, true
	case c.getenv("DISPLAY") != "" && c.has("xsel"):
		return command{name: "xsel", args: []string{"--clipboard", "--input"}}, true
	}
	return command{}, false
}

func (c *Clipboard) remote() bool {
	return c.getenv("SSH_CONNECTION") != "" || c.getenv("SSH_TTY") != "" || c.getenv("SSH_CLIENT") != ""
}

func (c *Clipboard) has(name string) bool {
	_, err := c.lookPath(name)
	return err == nil
}

func (c *Clipboard) supportsSensitive(ctx context.Context) bool {
	c.sensitiveOnce.Do(func() {
		help, err := c.probe(ctx, "wl-copy", "--help")
		c.sensitive = err == nil && strings.Contains(help, "--sensitive")
	})
	return c.sensitive
}

func (c *Clipboard) osc52(text string) error {
	if len(text) > maxTerminalPayload {
		return ErrTooLarge
	}
	_, err := io.WriteString(c.terminal, ansi.SetSystemClipboard(text))
	return err
}

// runPipe leaves stdout and stderr unset on purpose: wl-copy and xclip fork a
// process that keeps serving the clipboard, and an inherited pipe would make
// Run wait until the clipboard changes hands.
func runPipe(ctx context.Context, stdin, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	return cmd.Run()
}

func runProbe(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}
