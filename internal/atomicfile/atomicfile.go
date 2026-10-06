// Package atomicfile writes a file so that readers only ever see the old
// contents or the complete new ones, at the requested mode from the first byte.
package atomicfile

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Write replaces path through a temporary file in the same directory. A symlink
// at path is replaced, never written through. The directory must exist.
func Write(path string, data []byte, perm fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
