package app

import (
	"context"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/server"
)

// reminding has c find latest and records each reminder it asks, answering with answer.
func reminding(t *testing.T, c *App, latest *string, answer bool) *[]string {
	t.Helper()
	(&fakeServer{latest: func() (server.Release, error) {
		return server.Release{Version: *latest, URL: releaseTag + *latest}, nil
	}}).connects(c)
	var asked []string
	c.ask = func(_ context.Context, message, _, _ string) bool { asked = append(asked, message); return answer }
	return &asked
}

func checkAt(t *testing.T, c *App, at time.Time) {
	t.Helper()
	c.now = func() time.Time { return at }
	if err := c.CheckForUpdate(t.Context()); err != nil {
		t.Fatal(err)
	}
	c.remindOfUpdate(t.Context())
}

func TestANewerReleaseIsRemindedOfOnceAndThenWeekly(t *testing.T) {
	c, _ := fixture(t)
	latest := "v0.2.0"
	asked := reminding(t, c, &latest, false)
	start := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

	checkAt(t, c, start)
	checkAt(t, c, start.Add(24*time.Hour))
	checkAt(t, c, start.Add(6*24*time.Hour))
	if len(*asked) != 1 {
		t.Fatalf("within the week: asked %d times", len(*asked))
	}
	checkAt(t, c, start.Add(7*24*time.Hour))
	if len(*asked) != 2 {
		t.Errorf("a week on: asked %d times", len(*asked))
	}
}

func TestAnotherNewReleaseIsRemindedOfAtOnce(t *testing.T) {
	c, _ := fixture(t)
	latest := "v0.2.0"
	asked := reminding(t, c, &latest, false)
	start := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

	checkAt(t, c, start)
	latest = "v0.3.0"
	checkAt(t, c, start.Add(time.Hour))

	if len(*asked) != 2 {
		t.Errorf("asked %d times", len(*asked))
	}
}

// Later, or closing the reminder, holds for a week across restarts.
func TestLaterIsKeptAcrossARestart(t *testing.T) {
	c, _ := fixture(t)
	latest := "v0.2.0"
	reminding(t, c, &latest, false)
	start := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	checkAt(t, c, start)

	restarted, _ := fixture(t)
	restarted.dataDir = c.dataDir
	asked := reminding(t, restarted, &latest, false)
	checkAt(t, restarted, start.Add(time.Hour))

	if len(*asked) != 0 {
		t.Errorf("asked again after a restart: %v", *asked)
	}
}

func TestUpToDateIsNeverReminded(t *testing.T) {
	c, _ := fixture(t)
	latest := "v0.1.0"
	asked := reminding(t, c, &latest, true)

	checkAt(t, c, time.Now())

	if len(*asked) != 0 {
		t.Errorf("asked %v", *asked)
	}
}

func TestUpdateNowInTheReminderRunsTheUpdate(t *testing.T) {
	c, _ := fixture(t)
	companion := "the new companion"
	_, ran := releasing(t, c, map[string]string{linuxFile: companion, "SHA256SUMS": sum(companion) + "  " + linuxFile + "\n"})
	latest := "v0.2.0"
	asked := reminding(t, c, &latest, true)

	checkAt(t, c, time.Now())
	waitForOwned(t, c)

	if len(*asked) != 1 || len(*ran) != 1 {
		t.Errorf("asked %v, ran %v", *asked, *ran)
	}
}

func TestAReminderStillOpenDoesNotKeepTheCompanionRunning(t *testing.T) {
	c, _ := fixture(t)
	latest := "v0.2.0"
	reminding(t, c, &latest, false)
	closed := make(chan struct{}) // the player answers only once the test is over
	t.Cleanup(func() { close(closed) })
	c.ask = func(context.Context, string, string, string) bool { <-closed; return false }
	c.now = time.Now
	c.CheckForUpdate(t.Context())
	ctx, stop := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { c.remindOfUpdate(ctx); close(done) }()

	stop()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the reminder blocks the companion's stop")
	}
}
