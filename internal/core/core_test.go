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

	"github.com/gold-forever/companion/internal/api"
	"github.com/gold-forever/companion/internal/backup"
	"github.com/gold-forever/companion/internal/find"
	"github.com/gold-forever/companion/internal/sendlog"
	"github.com/gold-forever/companion/internal/watch"
)

const account = "SECRETACCOUNT"

var start = time.Date(2026, 11, 4, 18, 0, 0, 0, time.UTC)

func saveText(n string) string {
	return "GoldForeverDB = {\r\n[\"client\"] = {\r\n[\"interface\"] = 16001,\r\n},\r\n[\"n\"] = " + n + ",\r\n}\r\n"
}

// server answers each upload with the next status in answers (the last one repeats) and records
// every request.
type server struct {
	mu       sync.Mutex
	answers  []int
	headers  []string
	bodies   []string
	requests []string
	ok       string // a 200's body; empty is imported
}

func (s *server) handler(w http.ResponseWriter, r *http.Request) {
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
	case http.StatusBadRequest:
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"status":"unknown game","reason":"unknown game version 11507"}`)
	default:
		w.WriteHeader(status)
	}
}

type fixture struct {
	core *Core
	srv  *server
	save string
	log  *sendlog.Log
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
	srv := &server{answers: answers}
	ts := httptest.NewServer(http.HandlerFunc(srv.handler))
	t.Cleanup(ts.Close)
	data := t.TempDir()
	clock := start
	log := &sendlog.Log{Path: filepath.Join(data, "sent.jsonl"), Keep: 30 * 24 * time.Hour}
	c := &Core{
		Uploader: &api.Client{BaseURL: ts.URL, Token: "gf_t", Version: "0.1.0", OS: "linux"},
		Backups:  &backup.Store{Dir: filepath.Join(data, "backups"), Keep: 10, Now: func() time.Time { return clock }},
		Log:      log,
		Watcher:  watch.New(2 * time.Second),
		Saves:    func() []find.Save { return find.Saves([]string{filepath.Join(root, "World of Warcraft")}) },
		IsOff:    func(string) bool { return false },
		Out:      io.Discard,
	}
	return &fixture{core: c, srv: srv, save: path, log: log}
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

	if !errors.Is(err, api.ErrUnauthorized) {
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

func TestAForbiddenAccountStopsTheCompanionAndKeepsTheSave(t *testing.T) {
	f := setup(t, http.StatusForbidden)

	_ = f.core.Step(context.Background(), start)
	err := f.core.Step(context.Background(), start.Add(2*time.Second))

	var forbidden *api.ForbiddenError
	if !errors.As(err, &forbidden) {
		t.Fatalf("err = %v, want a ForbiddenError", err)
	}
	if pending, _ := f.core.Backups.Pending(); len(pending) != 1 {
		t.Fatal("the save was dropped; it should wait until the account may upload again")
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
