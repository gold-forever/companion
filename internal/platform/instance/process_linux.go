package instance

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

// isCompanion says whether pid runs a binary named as the companion, replaced since or not; tests
// replace it.
var isCompanion = func(pid int) bool {
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	return err == nil && companionName(strings.TrimSuffix(exe, " (deleted)"))
}

// stop interrupts pid, which every release takes as Quit: its upload finishes first.
func stop(pid int) error { return syscall.Kill(pid, syscall.SIGINT) }
