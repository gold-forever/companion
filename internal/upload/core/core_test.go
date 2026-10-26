package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/saves/backup"
	"github.com/gold-forever/companion/internal/saves/find"
	"github.com/gold-forever/companion/internal/saves/refused"
	"github.com/gold-forever/companion/internal/saves/watch"
	"github.com/gold-forever/companion/internal/server"
	"github.com/gold-forever/companion/internal/upload/sendlog"
)

const account = "SECRETACCOUNT"

var start = time.Date(2026, 11, 4, 18, 0, 0, 0, time.UTC)

func saveText(n string) string {
	return "GoldForeverDB = {\r\n[\"client\"] = {\r\n[\"interface\"] = 16001,\r\n},\r\n[\"n\"] = " + n + ",\r\n}\r\n"
}

// fakeServer answers each upload with the next status in answers (the last one repeats) and records
// every request.
type fakeServer struct {
	mu       sync.Mutex
	answers  []int
	paused   string // when set, a 503 is the server's pause, with this reason
	headers  []string
	bodies   []string
	requests []string
	ok       string // a 200's body; empty is imported
}

func (s *fakeServer) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	s.bodies = append(s.bodies, string(body))
	s.requests = append(s.requests, r.Method+" "+r.URL.RequestURI())
	var h strings.Builder
	for k, v := range r.Header {
		h.WriteString(k + ": " + strings.Join(v, ",") + "\n")
	}
	s.headers = append(s.headers, h.String())
	status := s.answers[0]
	if len(s.answers) > 1 {
		s.answers = s.answers[1:]
	}
	switch status {
	case http.StatusOK:
		body := `{"status":"imported","saves":1}`
		if s.ok != "" {
			body = s.ok
		}
		_, _ = io.WriteString(w, body)
	case http.StatusTooManyRequests:
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(status)
	case http.StatusServiceUnavailable:
		if s.paused != "" {
			w.Header().Set("Retry-After", "600")
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"status":"paused","reason":"`+s.paused+`"}`)
	case http.StatusRequestEntityTooLarge:
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"status":"too large","reason":"a forever save may be at most 10 MB"}`)
	case http.StatusBadRequest:
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"status":"unknown game","reason":"unknown game version 11507"}`)
	default:
		w.WriteHeader(status)
	}
}

type fixture struct {
	core    *Core
	srv     *fakeServer
	save    string
	log     *sendlog.Log
	refused string // the refused list's file
}

func setup(t *testing.T, answers ...int) *fixture {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "World of Warcraft", "_forever_", "WTF", "Account", account, "SavedVariables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "GoldForever.lua")
	if err := os.WriteFile(path, []byte(saveText("1")), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := &fakeServer{answers: answers}
	ts := httptest.NewServer(http.HandlerFunc(srv.handler))
	t.Cleanup(ts.Close)
	data := t.TempDir()
	clock := start
	log := &sendlog.Log{Path: filepath.Join(data, "sent.jsonl"), Keep: 30 * 24 * time.Hour}
	refusedAt := filepath.Join(data, "refused.json")
	c := &Core{
		Uploader: &server.Client{BaseURL: ts.URL, Token: "gf_t", Version: "0.1.0", OS: "linux"},
		Backups:  &backup.Store{Dir: filepath.Join(data, "backups"), Keep: 10, Now: func() time.Time { return clock }},
		Log:      log,
		Refused:  refused.Load(refusedAt),
		Watcher:  watch.New(2 * time.Second),
		Saves:    func() []find.Save { return find.Saves([]string{filepath.Join(root, "World of Warcraft")}) },
		IsOff:    func(string) bool { return false },
		Out:      io.Discard,
	}
	return &fixture{core: c, srv: srv, save: path, log: log, refused: refusedAt}
}

// settle runs the steps that see a save and then find it settled, ending at start+2s.
func (f *fixture) settle(t *testing.T) {
	t.Helper()
	for _, at := range []time.Duration{0, 2 * time.Second} {
		if err := f.core.Step(context.Background(), start.Add(at)); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *fixture) uploads() int {
	f.srv.mu.Lock()
	defer f.srv.mu.Unlock()
	return len(f.srv.bodies)
}

func TestASettledSaveIsUploadedOnceAndLogged(t *testing.T) {
	f := setup(t, http.StatusOK)

	f.settle(t)
	_ = f.core.Step(context.Background(), start.Add(10*time.Second))

	if f.uploads() != 1 || f.srv.bodies[0] != saveText("1") {
		t.Fatalf("uploads = %d, bodies = %q", f.uploads(), f.srv.bodies)
	}
	entries, _ := f.log.Read()
	if len(entries) != 1 || entries[0].Answer != "imported" || entries[0].Size != len(saveText("1")) {
		t.Fatalf("log = %+v", entries)
	}
}

func TestNoRequestCarriesThePathOrTheAccountName(t *testing.T) {
	f := setup(t, http.StatusOK)

	f.settle(t)

	if f.uploads() != 1 {
		t.Fatalf("uploads = %d", f.uploads())
	}
	sent := f.srv.requests[0] + "\n" + f.srv.headers[0]
	for _, secret := range []string{account, "World of Warcraft", "_forever_", "SavedVariables", filepath.Dir(f.save)} {
		if strings.Contains(sent, secret) {
			t.Fatalf("a request carries %q:\n%s", secret, sent)
		}
	}
}

func TestAnOfflineServerIsRetriedWithGrowingWaits(t *testing.T) {
	f := setup(t, http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusOK)

	f.settle(t) // first try: 503, wait 1 minute
	at := start.Add(2 * time.Second)
	_ = f.core.Step(context.Background(), at.Add(59*time.Second))
	if f.uploads() != 1 {
		t.Fatalf("retried before the minute was up: %d uploads", f.uploads())
	}
	_ = f.core.Step(context.Background(), at.Add(time.Minute)) // second try: 503, wait 2 minutes
	_ = f.core.Step(context.Background(), at.Add(2*time.Minute+59*time.Second))
	if f.uploads() != 2 {
		t.Fatalf("the wait did not double: %d uploads", f.uploads())
	}
	_ = f.core.Step(context.Background(), at.Add(3*time.Minute)) // third try: 200
	if f.uploads() != 3 {
		t.Fatalf("not retried after the doubled wait: %d uploads", f.uploads())
	}
	if pending, _ := f.core.Backups.Pending(); len(pending) != 0 {
		t.Fatalf("still pending after the server took it: %+v", pending)
	}
}

func TestAPausedServerIsAskedAgainWhenItSaysAndItsReasonShown(t *testing.T) {
	f := setup(t, http.StatusServiceUnavailable, http.StatusOK)
	f.srv.paused = "maintenance until 20:00"
	var out strings.Builder
	f.core.Out = &out

	f.settle(t)
	at := start.Add(2 * time.Second)
	_ = f.core.Step(context.Background(), at.Add(time.Minute))
	if f.uploads() != 1 {
		t.Fatalf("sent the whole save again before Retry-After: %d uploads", f.uploads())
	}
	if !strings.Contains(out.String(), "maintenance until 20:00") {
		t.Errorf("the operator's reason is not shown: %q", out.String())
	}
	_ = f.core.Step(context.Background(), at.Add(10*time.Minute))
	if f.uploads() != 2 {
		t.Fatalf("not sent again once Retry-After passed: %d uploads", f.uploads())
	}
}

func TestARateLimitWaitsForRetryAfter(t *testing.T) {
	f := setup(t, http.StatusTooManyRequests, http.StatusOK)

	f.settle(t)
	at := start.Add(2 * time.Second)
	_ = f.core.Step(context.Background(), at.Add(89*time.Second))
	if f.uploads() != 1 {
		t.Fatalf("sent again before Retry-After: %d uploads", f.uploads())
	}
	_ = f.core.Step(context.Background(), at.Add(90*time.Second))
	if f.uploads() != 2 {
		t.Fatalf("not sent once Retry-After passed: %d uploads", f.uploads())
	}
}

func TestARevokedTokenStopsTheCompanion(t *testing.T) {
	f := setup(t, http.StatusUnauthorized)

	_ = f.core.Step(context.Background(), start)
	err := f.core.Step(context.Background(), start.Add(2*time.Second))

	if !errors.Is(err, server.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if pending, _ := f.core.Backups.Pending(); len(pending) != 1 {
		t.Fatal("the save was dropped; it should wait for the player to link again")
	}
}

func TestARefusedSaveIsLoggedAndNotSentAgain(t *testing.T) {
	f := setup(t, http.StatusBadRequest)

	f.settle(t)
	_ = f.core.Step(context.Background(), start.Add(time.Hour))

	if f.uploads() != 1 {
		t.Fatalf("a refused save was sent %d times", f.uploads())
	}
	entries, _ := f.log.Read()
	if len(entries) != 1 || !strings.Contains(entries[0].Answer, "unknown game version 11507") {
		t.Fatalf("log = %+v", entries)
	}
}

func TestAnUnexpectedAnswerKeepsTheSavePending(t *testing.T) {
	f := setup(t, http.StatusNotFound, http.StatusOK)

	f.settle(t)
	if pending, _ := f.core.Backups.Pending(); len(pending) != 1 {
		t.Fatal("a 404 is not the server's answer about the save: it must stay pending")
	}
	_ = f.core.Step(context.Background(), start.Add(2*time.Second+time.Minute))
	if pending, _ := f.core.Backups.Pending(); f.uploads() != 2 || len(pending) != 0 {
		t.Fatalf("uploads = %d, pending = %+v: not sent again after the wait", f.uploads(), pending)
	}
}

func TestATooLargeSaveIsLoggedWithTheServersLimit(t *testing.T) {
	f := setup(t, http.StatusRequestEntityTooLarge)

	f.settle(t)

	entries, _ := f.log.Read()
	if len(entries) != 1 || entries[0].Answer != "refused: too large: a forever save may be at most 10 MB" {
		t.Fatalf("log = %+v", entries)
	}
}

func TestPendingSavesAreSentOldestFirst(t *testing.T) {
	f := setup(t, http.StatusServiceUnavailable, http.StatusOK)
	f.settle(t) // save 1 waits after a 503

	if err := os.WriteFile(f.save, []byte(saveText("2")), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(f.save, start.Add(time.Minute), start.Add(time.Minute))
	_ = f.core.Step(context.Background(), start.Add(10*time.Second))
	_ = f.core.Step(context.Background(), start.Add(12*time.Second)) // save 2 settles; still waiting
	_ = f.core.Step(context.Background(), start.Add(2*time.Minute))

	if f.uploads() != 3 || f.srv.bodies[1] != saveText("1") || f.srv.bodies[2] != saveText("2") {
		t.Fatalf("bodies = %q", f.srv.bodies)
	}
}

func TestASwitchedOffSaveIsNeitherKeptNorSent(t *testing.T) {
	f := setup(t, http.StatusOK)
	f.core.IsOff = func(path string) bool { return path == f.save }

	f.settle(t)

	if f.uploads() != 0 {
		t.Fatalf("a switched-off save was sent")
	}
	if pending, _ := f.core.Backups.Pending(); len(pending) != 0 {
		t.Fatalf("a switched-off save was kept: %+v", pending)
	}
}

func TestStoppingMidUploadLeavesTheSavePendingWithoutWaiting(t *testing.T) {
	f := setup(t, http.StatusOK)
	ctx, cancel := context.WithCancel(context.Background())
	_ = f.core.Step(ctx, start)
	cancel()

	if err := f.core.Step(ctx, start.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if pending, _ := f.core.Backups.Pending(); len(pending) != 1 {
		t.Fatalf("pending = %+v, want the save kept for the next start", pending)
	}
	if !f.core.waitUntil.IsZero() {
		t.Fatalf("stopping was taken for a server failure: waits until %v", f.core.waitUntil)
	}
}

// A suspension can be lifted, or the token's rights changed, without the player touching the companion.
func TestAForbiddenAccountKeepsTheSaveAndTriesAgainLater(t *testing.T) {
	f := setup(t, http.StatusForbidden, http.StatusOK)
	var out strings.Builder
	f.core.Out = &out

	f.settle(t) // 403 at start+2s
	if pending, _ := f.core.Backups.Pending(); len(pending) != 1 {
		t.Fatal("the save was dropped; it should wait until the account may upload again")
	}
	if !strings.Contains(out.String(), "uploads wait until the account may upload again") {
		t.Errorf("the player is not told why: %q", out.String())
	}
	at := start.Add(2 * time.Second)
	if err := f.core.Step(context.Background(), at.Add(29*time.Minute)); err != nil || f.uploads() != 1 {
		t.Fatalf("err = %v, uploads = %d: tried again too soon", err, f.uploads())
	}
	if err := f.core.Step(context.Background(), at.Add(30*time.Minute)); err != nil || f.uploads() != 2 {
		t.Fatalf("err = %v, uploads = %d: not tried again after 30 minutes", err, f.uploads())
	}
	if pending, _ := f.core.Backups.Pending(); len(pending) != 0 {
		t.Fatalf("still pending once the account may upload: %+v", pending)
	}
}

func TestBackupsThatCannotBeListedAreSaidAndTriedAgain(t *testing.T) {
	f := setup(t, http.StatusOK)
	var out strings.Builder
	f.core.Out = &out
	notAFolder := filepath.Join(t.TempDir(), "backups")
	if err := os.WriteFile(notAFolder, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f.core.Backups.Dir = notAFolder

	if err := f.core.Step(context.Background(), start); err != nil {
		t.Fatalf("a folder that cannot be read stopped the uploads: %v", err)
	}
	if !strings.Contains(out.String(), "could not list the copies to send") {
		t.Errorf("the player is not told: %q", out.String())
	}
	if !f.core.waitUntil.After(start) {
		t.Error("it must wait before trying again")
	}
}

func TestAnImportedUploadIsAnnounced(t *testing.T) {
	for _, c := range []struct {
		status int
		ok     string
		want   int
	}{
		{http.StatusOK, "", 1},
		{http.StatusOK, `{"status":"already imported","saves":1}`, 0},
		{http.StatusBadRequest, "", 0},
		{http.StatusServiceUnavailable, "", 0},
	} {
		status, want := c.status, c.want
		f := setup(t, status)
		f.srv.ok = c.ok
		announced := 0
		f.core.Imported = func() { announced++ }

		f.settle(t)

		if f.uploads() != 1 || announced != want {
			t.Errorf("answer %d: %d uploads, announced %d times, want %d", status, f.uploads(), announced, want)
		}
	}
}

// stepUntil steps every hour from start up to end and returns the times at which an upload went out.
func (f *fixture) stepUntil(t *testing.T, end time.Duration) []time.Duration {
	t.Helper()
	var sent []time.Duration
	for at := time.Hour; at <= end; at += time.Hour {
		before := f.uploads()
		if err := f.core.Step(context.Background(), start.Add(at)); err != nil {
			t.Fatal(err)
		}
		if f.uploads() > before {
			sent = append(sent, at)
		}
	}
	return sent
}

func TestASaveRefusedAsTooLargeIsSentAgainADayLater(t *testing.T) {
	f := setup(t, http.StatusRequestEntityTooLarge, http.StatusOK)

	f.settle(t)
	sent := f.stepUntil(t, 4*24*time.Hour)

	if len(sent) != 1 || sent[0] < refused.Every {
		t.Fatalf("sent again at %v, want once, a day or more after the refusal", sent)
	}
	if f.srv.bodies[1] != saveText("1") {
		t.Fatalf("sent again: %q", f.srv.bodies[1])
	}
	entries, _ := f.log.Read()
	if len(entries) != 2 || entries[0].Answer != "refused: too large: a forever save may be at most 10 MB" || entries[1].Answer != "imported" {
		t.Fatalf("log = %+v", entries)
	}
}

func TestASaveAlwaysTooLargeIsTriedOnlyOnceADay(t *testing.T) {
	f := setup(t, http.StatusRequestEntityTooLarge)

	f.settle(t)
	sent := f.stepUntil(t, 4*24*time.Hour)

	if len(sent) != 3 {
		t.Fatalf("tried again at %v, want once a day for three days", sent)
	}
	last := 2 * time.Second
	for _, at := range sent {
		if at-last < refused.Every {
			t.Fatalf("tried again at %v, less than a day after %v", at, last)
		}
		last = at
	}
	if !f.core.Refused.Holds(f.save) {
		t.Fatal("a save refused again is no longer held for the daily try")
	}
}

func TestANewSaveReplacesOneRefusedAsTooLarge(t *testing.T) {
	f := setup(t, http.StatusRequestEntityTooLarge, http.StatusOK)
	f.settle(t)

	if err := os.WriteFile(f.save, []byte(saveText("2")), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(f.save, start.Add(time.Minute), start.Add(time.Minute))
	_ = f.core.Step(context.Background(), start.Add(10*time.Second))
	_ = f.core.Step(context.Background(), start.Add(12*time.Second))
	sent := f.stepUntil(t, 4*24*time.Hour)

	if f.uploads() != 2 || f.srv.bodies[1] != saveText("2") || len(sent) != 0 {
		t.Fatalf("bodies = %q, later uploads at %v; want the new save once and the old one never again", f.srv.bodies, sent)
	}
	if f.core.Refused.Holds(f.save) {
		t.Fatal("the old refusal is still held")
	}
}

func TestASaveRefusedAsTooLargeIsTriedOnceAtTheNextStart(t *testing.T) {
	f := setup(t, http.StatusRequestEntityTooLarge, http.StatusOK)
	f.settle(t)

	restarted := *f.core
	restarted.Refused = refused.Load(f.refused)
	restarted.Watcher = watch.New(2 * time.Second)
	f.core = &restarted
	_ = f.core.Step(context.Background(), start.Add(time.Hour))

	if f.uploads() != 2 || f.srv.bodies[1] != saveText("1") {
		t.Fatalf("bodies = %q, want the refused save sent again at the start", f.srv.bodies)
	}
	if f.core.Refused.Holds(f.save) {
		t.Fatal("a save the server took is still held")
	}
}

func TestARefusalOfASaveThatHasChangedSinceIsDroppedUnsent(t *testing.T) {
	f := setup(t, http.StatusOK)
	f.settle(t) // the save as it is now went through
	if err := f.core.Refused.Refuse(f.save, "an older save's sha256", start.Add(-refused.Every)); err != nil {
		t.Fatal(err)
	}

	_ = f.core.Step(context.Background(), start.Add(time.Hour))

	if f.uploads() != 1 || f.core.Refused.Holds(f.save) {
		t.Fatalf("uploads = %d, held = %v; want the stale refusal dropped and nothing sent", f.uploads(), f.core.Refused.Holds(f.save))
	}
}
