package instance

import (
	"golang.org/x/sys/windows"
)

// stillActive is the exit code GetExitCodeProcess reports for a process that has not exited.
const stillActive = 259

// isCompanion says whether pid runs a binary named as the companion; tests replace it.
var isCompanion = func(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil || code != stillActive {
		return false
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &size) != nil {
		return false
	}
	return companionName(windows.UTF16ToString(buf[:size]))
}

// stop ends pid: Windows has no interrupt to send another program's process.
func stop(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}
