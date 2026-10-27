package app

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gold-forever/companion/internal/platform/instance"
)

var rc12 = instance.Info{PID: 4242, Port: 50123, Version: "v0.1.0-rc.12"}

// olderRunning makes c see an older copy's process run until it is closed, and records the closing.
func olderRunning(c *App) *atomic.Bool {
	var closed atomic.Bool
	c.olderGrace = 0
	c.olderRuns = func(i instance.Info) bool { return i == rc12 && !closed.Load() }
	c.closeOlder = func(i instance.Info) error { closed.Store(i == rc12); return nil }
	return &closed
}

// An older copy that could not be asked to quit (gold-forever/companion#26) is not left running unseen
// beside this one: the page and the tray say so, and the desktop offers to close it.
func TestAnOlderCopyThatCannotBeAskedIsNamedAndOfferedToClose(t *testing.T) {
	c, _ := fixture(t)
	closed := olderRunning(c)
	var asked []string
	c.ask = func(_ context.Context, message, yes, no string) bool {
		asked = append(asked, message+" ["+yes+" / "+no+"]")
		if w := c.State().Warning; !strings.Contains(w, "v0.1.0-rc.12") {
			t.Errorf("while it runs, the page says %q", w)
		}
		if s := c.Status(); !strings.Contains(s, "older") || !strings.Contains(s, "running") {
			t.Errorf("while it runs, the tray says %q", s)
		}
		return true
	}

	c.offerToCloseOlder(t.Context(), rc12)

	if len(asked) != 1 || !strings.Contains(asked[0], "v0.1.0-rc.12") || !strings.Contains(asked[0], "Close it") {
		t.Fatalf("asked %q", asked)
	}
	if !closed.Load() {
		t.Fatal("the older copy was not closed on the player's yes")
	}
	if w := c.State().Warning; w != "" {
		t.Errorf("once it is closed, the page still says %q", w)
	}
}

func TestAnOlderCopyLeftRunningStaysNamed(t *testing.T) {
	c, _ := fixture(t)
	closed := olderRunning(c)
	c.ask = func(context.Context, string, string, string) bool { return false }

	c.offerToCloseOlder(t.Context(), rc12)

	if closed.Load() {
		t.Error("the older copy was closed on the player's no")
	}
	if w := c.State().Warning; !strings.Contains(w, "v0.1.0-rc.12") {
		t.Errorf("the page says %q", w)
	}
}

// A copy that quits as asked, or is quitting while this one starts, is never mentioned.
func TestAnOlderCopyThatQuitsWhileThisOneStartsIsNeverMentioned(t *testing.T) {
	c, _ := fixture(t)
	var polls atomic.Int32
	c.olderRuns = func(instance.Info) bool { return polls.Add(1) < 3 }
	c.olderGrace = olderPoll * 10
	c.ask = func(context.Context, string, string, string) bool {
		t.Error("the player was asked about a copy that quit")
		return false
	}

	c.offerToCloseOlder(t.Context(), rc12)

	if w := c.State().Warning; w != "" {
		t.Errorf("the page says %q", w)
	}
}
