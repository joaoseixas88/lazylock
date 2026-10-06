package clipboard

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const secret = "sk_live_do_not_leak"

type call struct {
	name  string
	args  []string
	stdin string
}

type rig struct {
	clip     *Clipboard
	terminal *bytes.Buffer
	calls    []call
}

func newRig(env map[string]string, tools []string, help string) *rig {
	r := &rig{terminal: &bytes.Buffer{}}
	r.clip = &Clipboard{
		terminal: r.terminal,
		getenv:   func(k string) string { return env[k] },
		lookPath: func(name string) (string, error) {
			if slices.Contains(tools, name) {
				return "/usr/bin/" + name, nil
			}
			return "", exec.ErrNotFound
		},
		goos: "linux",
		pipe: func(_ context.Context, stdin, name string, args ...string) error {
			r.calls = append(r.calls, call{name: name, args: args, stdin: stdin})
			return nil
		},
		probe: func(context.Context, string, ...string) (string, error) { return help, nil },
	}
	return r
}

const wlCopyHelp = "  -c, --clear  Instead of copying, clear the clipboard.\n      --sensitive  Hint that the content is sensitive.\n"

func TestWaylandUsesWlCopySensitive(t *testing.T) {
	r := newRig(map[string]string{"WAYLAND_DISPLAY": "wayland-1"}, []string{"wl-copy"}, wlCopyHelp)
	via, err := r.clip.Copy(context.Background(), secret)
	if err != nil || via != System {
		t.Fatalf("Copy = %v, %v; want System", via, err)
	}
	want := call{name: "wl-copy", args: []string{"--sensitive"}, stdin: secret}
	if len(r.calls) != 1 || r.calls[0].name != want.name || !slices.Equal(r.calls[0].args, want.args) || r.calls[0].stdin != secret {
		t.Fatalf("calls = %+v, want %+v", r.calls, want)
	}
}

func TestOldWlCopyCopiesWithoutTheFlag(t *testing.T) {
	r := newRig(map[string]string{"WAYLAND_DISPLAY": "wayland-1"}, []string{"wl-copy"}, "  -c, --clear\n")
	if _, err := r.clip.Copy(context.Background(), secret); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || len(r.calls[0].args) != 0 {
		t.Fatalf("calls = %+v, want plain wl-copy", r.calls)
	}
}

func TestSSHSessionsUseOSC52EvenWithWlCopyInstalled(t *testing.T) {
	r := newRig(map[string]string{"WAYLAND_DISPLAY": "wayland-1", "SSH_CONNECTION": "10.0.0.1 22 10.0.0.2 51000"}, []string{"wl-copy"}, wlCopyHelp)
	via, err := r.clip.Copy(context.Background(), secret)
	if err != nil || via != Terminal {
		t.Fatalf("Copy = %v, %v; want Terminal", via, err)
	}
	if len(r.calls) != 0 {
		t.Fatalf("a remote session must not touch the remote machine's clipboard: %+v", r.calls)
	}
}

func TestX11PrefersXclipThenXsel(t *testing.T) {
	for tools, want := range map[string]string{"xclip xsel": "xclip", "xsel": "xsel"} {
		r := newRig(map[string]string{"DISPLAY": ":0"}, strings.Fields(tools), "")
		if _, err := r.clip.Copy(context.Background(), secret); err != nil {
			t.Fatal(err)
		}
		if len(r.calls) != 1 || r.calls[0].name != want {
			t.Fatalf("with %s installed: calls = %+v, want %s", tools, r.calls, want)
		}
	}
}

func TestMacUsesPbcopy(t *testing.T) {
	r := newRig(nil, []string{"pbcopy"}, "")
	r.clip.goos = "darwin"
	if _, err := r.clip.Copy(context.Background(), secret); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || r.calls[0].name != "pbcopy" {
		t.Fatalf("calls = %+v, want pbcopy", r.calls)
	}
}

func TestNothingInstalledFallsBackToOSC52(t *testing.T) {
	r := newRig(map[string]string{"WAYLAND_DISPLAY": "wayland-1", "DISPLAY": ":0"}, nil, "")
	via, err := r.clip.Copy(context.Background(), secret)
	if err != nil || via != Terminal {
		t.Fatalf("Copy = %v, %v; want Terminal", via, err)
	}
	if got := r.terminal.String(); got != ansi.SetSystemClipboard(secret) {
		t.Fatalf("terminal got %q, want exactly one OSC 52 sequence", got)
	}
}

func TestSecretNeverAppearsInArgv(t *testing.T) {
	setups := []struct {
		env   map[string]string
		tools []string
		goos  string
	}{
		{map[string]string{"WAYLAND_DISPLAY": "wayland-1"}, []string{"wl-copy"}, "linux"},
		{map[string]string{"DISPLAY": ":0"}, []string{"xclip"}, "linux"},
		{map[string]string{"DISPLAY": ":0"}, []string{"xsel"}, "linux"},
		{nil, []string{"pbcopy"}, "darwin"},
	}
	for _, setup := range setups {
		r := newRig(setup.env, setup.tools, wlCopyHelp)
		r.clip.goos = setup.goos
		if _, err := r.clip.Copy(context.Background(), secret); err != nil {
			t.Fatal(err)
		}
		for _, c := range r.calls {
			if slices.ContainsFunc(c.args, func(arg string) bool { return strings.Contains(arg, secret) }) {
				t.Fatalf("%s got the secret on its command line: %v", c.name, c.args)
			}
		}
	}
}

func TestOSC52RefusesTextTheTerminalCannotTake(t *testing.T) {
	r := newRig(nil, nil, "")
	if _, err := r.clip.Copy(context.Background(), strings.Repeat("x", maxTerminalPayload+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if r.terminal.Len() != 0 {
		t.Fatal("nothing may reach the terminal when the copy is refused")
	}
}

func TestFailingClipboardToolIsReported(t *testing.T) {
	r := newRig(map[string]string{"DISPLAY": ":0"}, []string{"xclip"}, "")
	boom := errors.New("exit status 1")
	r.clip.pipe = func(context.Context, string, string, ...string) error { return boom }
	if _, err := r.clip.Copy(context.Background(), secret); !errors.Is(err, boom) || !strings.Contains(err.Error(), "xclip") {
		t.Fatalf("err = %v, want the tool's failure naming the tool", err)
	}
}
