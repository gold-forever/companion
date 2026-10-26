package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/api"
	"github.com/gold-forever/companion/internal/core"
	"github.com/gold-forever/companion/internal/saves/backup"
	"github.com/gold-forever/companion/internal/saves/find"
	"github.com/gold-forever/companion/internal/saves/refused"
	"github.com/gold-forever/companion/internal/saves/watch"
	"github.com/gold-forever/companion/internal/sendlog"
)

// Two game clients on one install, one account each, both writing their save while the companion runs.
// The account folders are named as the game names them in either setup.
var setups = map[string][2]string{
	"one Battle.net login, two WoW accounts": {"123456789#1", "123456789#2"},
	"two Battle.net logins, one install":     {"123456789#1", "987654321#1"},
}

// inBothSetups runs test once per setup, with the setup's two account folders.
func inBothSetups(t *testing.T, test func(t *testing.T, folders [2]string)) {
	for name, folders := range setups {
		t.Run(name, func(t *testing.T) { test(t, folders) })
	}
}

var launch = time.Date(2026, 11, 4, 18, 0, 0, 0, time.UTC)

func accountSave(account, n string) string {
	return "GoldForeverDB = {\r\n[\"client\"] = {\r\n[\"interface\"] = 16001,\r\n},\r\n[\"" + account + "\"] = " + n + ",\r\n}\r\n"
}

// uploads is the server: it answers each upload with the next status (the last repeats), calls
// during, if set, while the upload is in flight, and records every body it was sent.
type uploads struct {
	mu      sync.Mutex
	answers []int
	during  func(n int) // n is the upload's number, from 1
	bodies  []string
}

func (u *uploads) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	u.mu.Lock()
	u.bodies = append(u.bodies, string(body))
	n, status := len(u.bodies), u.answers[0]
	if len(u.answers) > 1 {
		u.answers = u.answers[1:]
	}
	during := u.during
	u.mu.Unlock()
	if during != nil {
		during(n)
	}
	switch status {
	case http.StatusOK:
		_, _ = io.WriteString(w, `{"status":"imported","saves":2}`)
	case http.StatusTooManyRequests:
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(status)
	default:
		w.WriteHeader(status)
	}
}

func (u *uploads) sent() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.bodies)
}

// twoAccounts is one install with the saves of two accounts, ONE and TWO, and the upload loop watching it
// as the companion wires it, stepped by hand.
type twoAccounts struct {
	loop     *core.Core
	srv      *uploads
	one, two string // the saves' paths
	clock    time.Time
}

func newTwoAccounts(t *testing.T, folders [2]string, answers ...int) *twoAccounts {
	t.Helper()
	install := filepath.Join(t.TempDir(), "World of Warcraft")
	a := &twoAccounts{srv: &uploads{answers: answers}, clock: launch}
	var paths []string
	for _, folder := range folders {
		dir := filepath.Join(install, "_forever_", "WTF", "Account", folder, "SavedVariables")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, filepath.Join(dir, "GoldForever.lua"))
	}
	a.one, a.two = paths[0], paths[1]
	ts := httptest.NewServer(http.HandlerFunc(a.srv.handler))
	t.Cleanup(ts.Close)
	data := t.TempDir()
	a.loop = &core.Core{
		Uploader: &api.Client{BaseURL: ts.URL, Token: "gf_t", Version: "0.1.0", OS: "linux"},
		Backups:  &backup.Store{Dir: filepath.Join(data, "backups"), Keep: KeepCopies, Now: func() time.Time { return a.clock }},
		Log:      &sendlog.Log{Path: filepath.Join(data, "sent.jsonl"), Keep: KeepLogFor},
		Refused:  refused.Load(filepath.Join(data, RefusedFile)),
		Watcher:  watch.New(PollEvery),
		Saves:    func() []find.Save { return find.Saves([]string{install}) },
		IsOff:    func(string) bool { return false },
		Out:      io.Discard,
	}
	return a
}

// write puts text in a save as the game does, stamped with the companion's clock.
func (a *twoAccounts) write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, a.clock, a.clock); err != nil {
		t.Fatal(err)
	}
}

// step polls once at d after launch.
func (a *twoAccounts) step(t *testing.T, d time.Duration) {
	t.Helper()
	a.clock = launch.Add(d)
	if err := a.loop.Step(context.Background(), a.clock); err != nil {
		t.Fatal(err)
	}
}

func (a *twoAccounts) pending(t *testing.T) int {
	t.Helper()
	p, err := a.loop.Backups.Pending()
	if err != nil {
		t.Fatal(err)
	}
	return len(p)
}

func sameUploads(t *testing.T, got []string, want ...string) {
	t.Helper()
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("uploaded %q, want each of %q once", got, want)
	}
}

func TestTwoSavesThatSettleInTheSamePollAreEachUploadedOnceWithTheirOwnContent(t *testing.T) {
	inBothSetups(t, func(t *testing.T, folders [2]string) {
		a := newTwoAccounts(t, folders, http.StatusOK)
		a.write(t, a.one, accountSave("ONE", "1"))
		a.write(t, a.two, accountSave("TWO", "1"))

		a.step(t, 0)
		a.step(t, PollEvery)
		a.step(t, 2*PollEvery)
		a.step(t, time.Minute)

		sameUploads(t, a.srv.sent(), accountSave("ONE", "1"), accountSave("TWO", "1"))
		if n := a.pending(t); n != 0 {
			t.Errorf("%d copies still pending", n)
		}
	})
}

func TestASaveThatChangesWhileTheOthersUploadIsInFlightIsUploadedToo(t *testing.T) {
	inBothSetups(t, func(t *testing.T, folders [2]string) {
		a := newTwoAccounts(t, folders, http.StatusOK)
		a.write(t, a.one, accountSave("ONE", "1"))
		a.write(t, a.two, accountSave("TWO", "1"))
		a.srv.during = func(n int) {
			if n == 1 { // TWO's game writes again while the first upload is in flight
				a.write(t, a.two, accountSave("TWO", "2"))
			}
		}
		a.step(t, 0)
		for d := PollEvery; d <= 4*PollEvery; d += PollEvery {
			a.step(t, d)
		}

		sameUploads(t, a.srv.sent(), accountSave("ONE", "1"), accountSave("TWO", "1"), accountSave("TWO", "2"))
		if n := a.pending(t); n != 0 {
			t.Errorf("%d copies still pending", n)
		}
	})
}

func TestASaveThatChangesWhileTheOtherWaitsOnARateLimitIsUploadedOnceTheWaitEnds(t *testing.T) {
	inBothSetups(t, func(t *testing.T, folders [2]string) {
		a := newTwoAccounts(t, folders, http.StatusTooManyRequests, http.StatusOK)
		a.write(t, a.one, accountSave("ONE", "1"))
		a.step(t, 0)
		a.step(t, PollEvery) // ONE settles; its upload gets a 429: wait 90 seconds

		a.write(t, a.two, accountSave("TWO", "1"))
		a.step(t, 2*PollEvery)
		a.step(t, 3*PollEvery) // TWO settles during the wait and is kept, not sent
		if got := a.srv.sent(); len(got) != 1 {
			t.Fatalf("sent during the server's wait: %q", got)
		}
		a.step(t, PollEvery+90*time.Second)
		a.step(t, 2*time.Minute)

		got := a.srv.sent()
		sameUploads(t, got[1:], accountSave("ONE", "1"), accountSave("TWO", "1"))
		if n := a.pending(t); n != 0 {
			t.Errorf("%d copies still pending", n)
		}
	})
}

func TestASaveThatChangesWhileTheOtherWaitsOnARetryIsUploadedOnceTheWaitEnds(t *testing.T) {
	inBothSetups(t, func(t *testing.T, folders [2]string) {
		a := newTwoAccounts(t, folders, http.StatusServiceUnavailable, http.StatusOK)
		a.write(t, a.one, accountSave("ONE", "1"))
		a.step(t, 0)
		a.step(t, PollEvery) // ONE settles; the server is down: try again in a minute

		a.write(t, a.two, accountSave("TWO", "1"))
		a.step(t, 2*PollEvery)
		a.step(t, 3*PollEvery)
		a.step(t, PollEvery+time.Minute)
		a.step(t, 2*time.Minute)

		got := a.srv.sent()
		if len(got) != 3 || got[0] != accountSave("ONE", "1") {
			t.Fatalf("uploaded %q, want ONE refused once by the server, then ONE and TWO once each", got)
		}
		sameUploads(t, got[1:], accountSave("ONE", "1"), accountSave("TWO", "1"))
		if n := a.pending(t); n != 0 {
			t.Errorf("%d copies still pending", n)
		}
	})
}

func TestACompleteSaveGoesUpAtOnceWhileTheOtherIsStillBeingWritten(t *testing.T) {
	inBothSetups(t, func(t *testing.T, folders [2]string) {
		a := newTwoAccounts(t, folders, http.StatusOK)
		whole := accountSave("TWO", "1")
		a.write(t, a.one, accountSave("ONE", "1"))
		a.write(t, a.two, whole[:len(whole)/2]) // TWO's game is still writing

		a.step(t, 0)
		a.step(t, PollEvery)
		sameUploads(t, a.srv.sent(), accountSave("ONE", "1"))
		a.step(t, time.Minute)
		sameUploads(t, a.srv.sent(), accountSave("ONE", "1")) // TWO waits for its next change

		a.write(t, a.two, whole)
		a.step(t, time.Minute+PollEvery)
		a.step(t, time.Minute+2*PollEvery)

		sameUploads(t, a.srv.sent(), accountSave("ONE", "1"), whole)
	})
}
