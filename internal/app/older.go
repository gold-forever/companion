package app

import (
	"context"
	"time"

	"github.com/gold-forever/companion/internal/platform/instance"
)

const (
	// olderGrace is how long an older copy that was asked to quit has to exit before the player hears of it.
	olderGrace = 10 * time.Second
	olderPoll  = 200 * time.Millisecond
)

// offerToCloseOlder follows the copy whose instance file this one replaced. One still running once
// olderGrace has passed could not be asked to quit: the page and the tray name it until it has gone,
// and the desktop offers to close it.
func (c *App) offerToCloseOlder(ctx context.Context, older instance.Info) {
	tick := time.NewTicker(olderPoll)
	defer tick.Stop()
	for deadline := c.now().Add(c.olderGrace); c.olderRuns(older) && c.now().Before(deadline); {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
	if !c.olderRuns(older) {
		return
	}
	c.mu.Lock()
	c.older = &older
	c.mu.Unlock()
	message := "An older Gold Forever Companion (" + older.Version + ") is still running beside this one and could not be asked to quit."
	// The desktop's question may not end with the companion, so it is waited for only until then.
	answer := make(chan bool, 1)
	go func() { answer <- c.ask(ctx, message, "Close it", "Leave it running") }()
	select {
	case yes := <-answer:
		if yes {
			_ = c.closeOlder(older)
		}
	case <-ctx.Done():
	}
}

// olderLine names the older copy still running beside this one: "" when there is none.
func (c *App) olderLine() string {
	c.mu.Lock()
	older := c.older
	c.mu.Unlock()
	if older == nil || !c.olderRuns(*older) {
		return ""
	}
	return "An older companion (" + older.Version + ") is still running beside this one: quit it from its tray icon, or close it when asked."
}
