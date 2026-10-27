//go:build !linux && !windows

package instance

import "errors"

// isCompanion is false where the companion does not install itself over another copy (macOS); tests
// replace it.
var isCompanion = func(int) bool { return false }

func stop(int) error { return errors.New("closing another copy is not supported here") }
