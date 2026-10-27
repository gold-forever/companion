package instance

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// olderCopy stands in for v0.1.0-rc.12's settings server: every request needs the secret, in its
// header or cookie, and a ping answers only the version.
func olderCopy(t *testing.T, secret, version string, seen *[]string) Info {
	t.Helper()
	info := listening(t, secret, func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Method+" "+r.URL.Path+" "+r.Header.Get(SecretHeader))
		if r.Header.Get(SecretHeader) != secret {
			http.Error(w, "open the settings from the tray icon", http.StatusForbidden)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/ping":
			json.NewEncoder(w).Encode(map[string]string{"version": version})
		case "POST /api/quit":
			w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	})
	info.Version, info.PID = version, 4242
	return info
}

// whileRunning makes the process check say the copy's PID runs, for one test.
func whileRunning(t *testing.T, pid int) {
	t.Helper()
	was := isCompanion
	isCompanion = func(p int) bool { return p == pid }
	t.Cleanup(func() { isCompanion = was })
}

// A copy from before the ping carried a proof (v0.1.0-rc.12 and earlier) took no claim either: unless
// it is recognised, a newer one installs over it and both run (gold-forever/companion#26).
func TestACopyFromBeforeTheProofIsRunningWhileItsProcessIs(t *testing.T) {
	var seen []string
	info := olderCopy(t, "s3cret", "v0.1.0-rc.12", &seen)
	whileRunning(t, info.PID)

	if !Alive(t.Context(), info) {
		t.Fatalf("the rc.12 copy was not recognised; it saw %q", seen)
	}
	if err := Quit(t.Context(), info); err != nil {
		t.Errorf("the rc.12 copy was not asked to quit: %v", err)
	}
}

// The secret goes to the port only while the process that wrote the file still runs: after a crash, a
// program on its port is never sent it.
func TestACopyFromBeforeTheProofIsNotAskedWithItsSecretOnceItsProcessHasGone(t *testing.T) {
	var seen []string
	info := olderCopy(t, "s3cret", "v0.1.0-rc.12", &seen)
	whileRunning(t, 1)

	if Alive(t.Context(), info) {
		t.Error("a copy whose process has gone was taken for running")
	}
	for _, s := range seen {
		if strings.Contains(s, "s3cret") {
			t.Errorf("the secret was sent: %s", s)
		}
	}
}

func TestAnOlderAnswerNamingAnotherVersionIsNotTheCopy(t *testing.T) {
	var seen []string
	info := olderCopy(t, "s3cret", "v0.1.0-rc.12", &seen)
	whileRunning(t, info.PID)
	info.Version = "v0.1.0-rc.11"

	if Alive(t.Context(), info) {
		t.Error("an answer naming another version was taken for the copy the file records")
	}
}
