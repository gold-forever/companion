package version

import "testing"

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.1.1", "v0.1.0", true},
		{"v0.2.0", "v0.1.9", true},
		{"v1.0.0", "v0.99.99", true},
		{"0.1.10", "v0.1.9", true}, // the leading v is optional, and numbers compare as numbers
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.1.1", false},
		{"v0.1.0", "", true},                  // nothing installed yet: any release is newer
		{"dev", "v0.1.0", false},              // a local build never replaces a release
		{"v0.1.0", "dev", true},               // and a release replaces a local build
		{"v0.1.0-3-gabc123", "v0.1.0", false}, // a build between tags is not a newer release
		{"v0.1.0-rc.3", "v0.1.0-rc.2", true},  // release candidates update each other
		{"v0.1.0-rc.10", "v0.1.0-rc.9", true}, // and their numbers compare as numbers
		{"v0.1.0", "v0.1.0-rc.2", true},       // the release replaces its candidates
		{"v0.1.0-rc.2", "v0.1.0", false},      // and no candidate replaces the release
		{"v0.1.0-rc.1", "v0.0.9", true},       // a candidate is still newer than the release before
		{"v0.1.0-beta.1", "v0.1.0-rc.1", false},
		{"v0.1.0-rc.3-local", "v0.1.0-rc.2", false}, // anything else after the version is not a release
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
