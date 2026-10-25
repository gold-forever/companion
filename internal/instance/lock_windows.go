package instance

import (
	"errors"

	"golang.org/x/sys/windows"
)

// A named mutex rather than a file: Uninstall removes the data folder while this copy still runs, and
// Windows cannot delete an open file.
const mutexName = `Local\GoldForeverCompanion`

// Claim takes this user's one-copy lock, held until release or until the process ends: the system
// lets go of it even after a crash. ErrRunning means another copy holds it. dir is not used here.
func Claim(dir string) (release func(), err error) {
	name, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		windows.CloseHandle(h)
		return nil, ErrRunning
	}
	if err != nil {
		return nil, err
	}
	return func() { windows.CloseHandle(h) }, nil
}
