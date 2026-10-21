package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const complete = "GoldForeverDB = {\r\n[\"characters\"] = {\r\n},\r\n}\r\n"

var start = time.Date(2026, 11, 4, 18, 0, 0, 0, time.UTC)

func write(t *testing.T, path, text string, mod time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestASaveIsReportedOnceItHasSettled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "GoldForever.lua")
	write(t, path, complete, start)
	w := New(2 * time.Second)

	if got := w.Poll(start, []string{path}); len(got) != 0 {
		t.Fatalf("reported at first sight: %v", got)
	}
	if got := w.Poll(start.Add(time.Second), []string{path}); len(got) != 0 {
		t.Fatalf("reported before settling: %v", got)
	}
	got := w.Poll(start.Add(2*time.Second), []string{path})
	if len(got) != 1 || got[0].Path != path || string(got[0].Data) != complete {
		t.Fatalf("settled = %+v", got)
	}
	if again := w.Poll(start.Add(10*time.Second), []string{path}); len(again) != 0 {
		t.Fatalf("an unchanged save was reported twice: %v", again)
	}
}

func TestAChangeWhileSettlingStartsTheWaitAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "GoldForever.lua")
	write(t, path, complete, start)
	w := New(2 * time.Second)
	w.Poll(start, []string{path})

	write(t, path, complete+"\n", start.Add(time.Second))
	if got := w.Poll(start.Add(2*time.Second), []string{path}); len(got) != 0 {
		t.Fatalf("reported a file that changed a second ago: %v", got)
	}
	if got := w.Poll(start.Add(4*time.Second), []string{path}); len(got) != 1 {
		t.Fatalf("not reported once settled: %v", got)
	}
}

func TestAnIncompleteSaveWaitsForTheNextChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "GoldForever.lua")
	write(t, path, "GoldForeverDB = {\r\n[\"characters\"] = {\r\n},\r\n", start)
	w := New(2 * time.Second)
	reads := 0
	w.read = func(p string) ([]byte, error) { reads++; return os.ReadFile(p) }

	w.Poll(start, []string{path})
	if got := w.Poll(start.Add(2*time.Second), []string{path}); len(got) != 0 {
		t.Fatalf("reported a cut-off save: %v", got)
	}
	w.Poll(start.Add(4*time.Second), []string{path})
	if reads != 1 {
		t.Fatalf("read an unchanged broken file %d times, want once", reads)
	}

	write(t, path, complete, start.Add(5*time.Second))
	w.Poll(start.Add(5*time.Second), []string{path})
	if got := w.Poll(start.Add(7*time.Second), []string{path}); len(got) != 1 {
		t.Fatalf("the finished save was not reported: %v", got)
	}
}

func TestAFileThatDisappearsIsForgotten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "GoldForever.lua")
	write(t, path, complete, start)
	w := New(2 * time.Second)
	w.Poll(start, []string{path})
	w.Poll(start.Add(2*time.Second), []string{path})

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	w.Poll(start.Add(3*time.Second), []string{path})
	write(t, path, complete, start)
	w.Poll(start.Add(4*time.Second), []string{path})
	if got := w.Poll(start.Add(6*time.Second), []string{path}); len(got) != 1 {
		t.Fatalf("a save written again after being removed was not reported: %v", got)
	}
}

func TestComplete(t *testing.T) {
	cases := map[string]bool{
		complete:                           true,
		"GoldForeverDB = {\n}\n":           true,
		"\xef\xbb\xbfGoldForeverDB = {\n}": true,  // a byte-order mark, no final newline
		"GoldForeverDB = {\n[1] = {\n},\n": false, // cut off after a nested table
		"GoldForeverDB = {\n[1] = 2,\n":    false,
		"":                                 false,
		"OtherAddonDB = {\n}\n":            false,
	}
	for text, want := range cases {
		if got := Complete([]byte(text)); got != want {
			t.Errorf("Complete(%q) = %v, want %v", text, got, want)
		}
	}
}
