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

// A release ranks above its candidates, which rank rc > beta > alpha (v1.0.0-rc.1 < v1.0.0).
var stages = map[string]int{"alpha": 0, "beta": 1, "rc": 2}

const release = 3

// parse reads major.minor.patch, with an optional leading v and an optional -alpha.N, -beta.N or -rc.N;
// anything else (dev, a git describe build like v0.1.0-3-gabc123) is not a release.
func parse(v string) ([5]int, bool) {
	var out [5]int
	core, pre, hasPre := strings.Cut(strings.TrimPrefix(v, "v"), "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, ok := number(p)
		if !ok {
			return out, false
		}
		out[i] = n
	}
	out[3] = release
	if !hasPre {
		return out, true
	}
	name, num, ok := strings.Cut(pre, ".")
	stage, known := stages[name]
	n, isNumber := number(num)
	if !ok || !known || !isNumber {
		return out, false
	}
	out[3], out[4] = stage, n
	return out, true
}

func number(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= 0
}
