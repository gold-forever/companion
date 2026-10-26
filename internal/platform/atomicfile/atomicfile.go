// Package atomicfile replaces a file whole or not at all: a crash mid-write leaves the old file, never
// a truncated one.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write puts data at path, readable by its owner only, through a temporary file beside it that is
// renamed over path once it is on disk. The file is owner-only from its first byte.
func Write(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // gone already once renamed
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
