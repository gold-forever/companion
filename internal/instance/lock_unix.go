//go:build !windows

package instance

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

const lockFile = "instance.lock"

// Claim takes this user's one-copy lock in dir, held until release or until the process ends: the
// system lets go of it even after a crash. ErrRunning means another copy holds it.
func Claim(dir string) (release func(), err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, lockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrRunning
		}
		return nil, err
	}
	return func() { f.Close() }, nil
}
