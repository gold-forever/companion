package app

import (
	"runtime"
	"slices"
	"strings"
)

// stillRunning is the stack of every other goroutine inside the app, the upload loop or the price loop,
// or started by one of them: "" when none is. A test's own frames do not count.
func stillRunning() string {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	var left []string
	for i, g := range strings.Split(string(buf), "\n\n") {
		if i > 0 && slices.ContainsFunc(strings.Split(g, "\n"), inTheCompanion) { // 0 is the caller's own
			left = append(left, g)
		}
	}
	return strings.Join(left, "\n\n")
}

func inTheCompanion(frame string) bool {
	if strings.Contains(frame, "companion/internal/app.Test") {
		return false
	}
	for _, pkg := range []string{"companion/internal/app.", "companion/internal/addondata.", "companion/internal/upload/core."} {
		if strings.Contains(frame, pkg) {
			return true
		}
	}
	return false
}
