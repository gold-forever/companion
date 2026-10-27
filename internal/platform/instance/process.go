package instance

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Running says whether the process that wrote i still runs and is the companion: a PID the system has
// since given to another program does not count.
func Running(i Info) bool { return i.PID > 0 && isCompanion(i.PID) }

// Close stops the copy i describes, for when it cannot be asked to quit. It refuses a process that is
// not the companion.
func Close(i Info) error {
	if !Running(i) {
		return fmt.Errorf("process %d is not a running companion", i.PID)
	}
	return stop(i.PID)
}

// companionName says whether exe is named as a release names the companion, on any system.
func companionName(exe string) bool {
	name := strings.ToLower(filepath.Base(exe))
	return strings.HasPrefix(name, "gold-forever-companion") || strings.HasPrefix(name, "goldforever-companion")
}
