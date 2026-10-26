package app

import (
	"runtime"
	"strings"
)

// stillRunning is the stack of every goroutine inside the upload or price loops: "" when none is.
func stillRunning() string {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	var left []string
	for g := range strings.SplitSeq(string(buf), "\n\n") {
		if strings.Contains(g, "companion/internal/prices.") || strings.Contains(g, "companion/internal/core.") {
			left = append(left, g)
		}
	}
	return strings.Join(left, "\n\n")
}
