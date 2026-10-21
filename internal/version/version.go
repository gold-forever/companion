// Package version compares release versions (v1.2.3), to decide whether a download replaces the
// installed companion and whether the server's latest release is worth a notice.
package version

import (
	"strconv"
	"strings"
)

// Newer says whether a is a newer release than b. An empty b (nothing installed) and a b that is not
// a release (dev) are older than any release; an a that is not a release is never newer.
func Newer(a, b string) bool {
	x, ok := parse(a)
	if !ok {
		return false
	}
	y, ok := parse(b)
	if !ok {
		return true
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

// parse reads major.minor.patch, with an optional leading v; anything else is not a release.
func parse(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
