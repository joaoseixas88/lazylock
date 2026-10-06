package infisical

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
)

// OpenBrowser launches the user's browser without ever blocking the caller.
//
// Three things matter here and each has bitten someone: the child's standard
// streams are nil, or it writes into the alt-screen and corrupts the render;
// the process is started and never waited on, or the UI freezes until the
// browser exits; and the scheme is validated, because xdg-open will happily act
// on file:// or hand a .desktop handler whatever it is given.
func OpenBrowser(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("refusing to open %q", raw)
	}

	var cmd *exec.Cmd
	switch {
	case os.Getenv("BROWSER") != "":
		cmd = exec.Command(os.Getenv("BROWSER"), u.String())
	case runtime.GOOS == "darwin":
		cmd = exec.Command("open", u.String())
	case runtime.GOOS == "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u.String())
	default:
		cmd = exec.Command("xdg-open", u.String())
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	go cmd.Wait() // reap it; we never care about the result
	return nil
}
