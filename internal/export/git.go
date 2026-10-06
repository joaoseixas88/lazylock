package export

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Exposure uint8

const (
	OutsideRepo Exposure = iota
	Ignored
	Committable
)

type gitRunner func(ctx context.Context, dir string, args ...string) (stdout string, exitCode int, err error)

// GitExposure reports whether a file written at path would show up in git as
// something to commit. A machine without git, or a path outside any work tree,
// is OutsideRepo.
func GitExposure(ctx context.Context, path string) (Exposure, error) {
	return gitExposure(ctx, path, runGit)
}

func gitExposure(ctx context.Context, path string, run gitRunner) (Exposure, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	dir := filepath.Dir(path)
	out, code, err := run(ctx, dir, "rev-parse", "--is-inside-work-tree")
	if err != nil || code != 0 || strings.TrimSpace(out) != "true" {
		return OutsideRepo, nil
	}
	_, code, err = run(ctx, dir, "check-ignore", "-q", "--", filepath.Base(path))
	switch {
	case err != nil:
		return Committable, err
	case code == 0:
		return Ignored, nil
	case code == 1:
		return Committable, nil
	}
	return Committable, fmt.Errorf("git check-ignore exited with status %d", code)
}

func runGit(ctx context.Context, dir string, args ...string) (string, int, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return string(out), exit.ExitCode(), nil
	}
	if err != nil {
		return "", -1, err
	}
	return string(out), 0, nil
}
