package export

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func fakeGit(insideRepo bool, checkIgnoreExit int) gitRunner {
	return func(_ context.Context, _ string, args ...string) (string, int, error) {
		if args[0] == "rev-parse" {
			if insideRepo {
				return "true\n", 0, nil
			}
			return "", 128, nil
		}
		return "", checkIgnoreExit, nil
	}
}

func TestCheckIgnoreExitCodesMapToExposure(t *testing.T) {
	for exit, want := range map[int]Exposure{0: Ignored, 1: Committable} {
		got, err := gitExposure(context.Background(), "/repo/.env", fakeGit(true, exit))
		if err != nil || got != want {
			t.Fatalf("exit %d: got %v, %v; want %v", exit, got, err, want)
		}
	}
	if _, err := gitExposure(context.Background(), "/repo/.env", fakeGit(true, 128)); err == nil {
		t.Fatal("an unexpected check-ignore status must be an error")
	}
}

func TestOutsideAWorkTreeIsOutsideRepo(t *testing.T) {
	got, err := gitExposure(context.Background(), "/tmp/.env", fakeGit(false, 1))
	if err != nil || got != OutsideRepo {
		t.Fatalf("got %v, %v; want OutsideRepo", got, err)
	}
}

func TestGitMissingCountsAsOutsideARepo(t *testing.T) {
	missing := func(context.Context, string, ...string) (string, int, error) { return "", -1, exec.ErrNotFound }
	got, err := gitExposure(context.Background(), "/repo/.env", missing)
	if err != nil || got != OutsideRepo {
		t.Fatalf("got %v, %v; want OutsideRepo", got, err)
	}
}

func TestCheckIgnoreFailureIsReported(t *testing.T) {
	boom := errors.New("signal: killed")
	run := func(_ context.Context, _ string, args ...string) (string, int, error) {
		if args[0] == "rev-parse" {
			return "true\n", 0, nil
		}
		return "", -1, boom
	}
	if _, err := gitExposure(context.Background(), "/repo/.env", run); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestGitExposureAgainstARealRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.env\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]Exposure{"dev.env": Ignored, "secrets.json": Committable} {
		got, err := GitExposure(context.Background(), filepath.Join(dir, name))
		if err != nil || got != want {
			t.Fatalf("%s: got %v, %v; want %v", name, got, err, want)
		}
	}
	if got, _ := GitExposure(context.Background(), filepath.Join(t.TempDir(), "dev.env")); got != OutsideRepo {
		t.Fatalf("a directory outside any repo: got %v", got)
	}
}
