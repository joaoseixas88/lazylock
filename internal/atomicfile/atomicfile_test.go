package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteLandsWithTheRequestedMode(t *testing.T) {
	for _, perm := range []os.FileMode{0o600, 0o644} {
		path := filepath.Join(t.TempDir(), "out.env")
		if err := Write(path, []byte("A=1\n"), perm); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != perm {
			t.Fatalf("mode = %#o, want %#o", info.Mode().Perm(), perm)
		}
		if got, _ := os.ReadFile(path); string(got) != "A=1\n" {
			t.Fatalf("contents = %q", got)
		}
	}
}

func TestWriteReplacesAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.env")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "new" {
		t.Fatalf("contents = %q", got)
	}
}

func TestWriteReplacesASymlinkInsteadOfWritingThroughIt(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := Write(link, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "untouched" {
		t.Fatalf("the symlink target was written through: %q", got)
	}
	if info, _ := os.Lstat(link); info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("the symlink must be replaced by a regular file")
	}
}

func TestFailedWriteLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	if err := Write(filepath.Join(dir, "missing", "out.env"), []byte("x"), 0o600); err == nil {
		t.Fatal("writing into a missing directory must fail")
	}
	target := filepath.Join(dir, "out.env")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Write(target, []byte("x"), 0o600); err == nil {
		t.Fatal("replacing a directory must fail")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftovers in %s: %v", dir, entries)
	}
}
