// Package core is the companion's loop: find the saves, notice when the game has written one, keep a
// copy, and send the copies the server has not answered for yet, oldest first.
package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gold-forever/companion/internal/api"
	"github.com/gold-forever/companion/internal/backup"
	"github.com/gold-forever/companion/internal/find"
	"github.com/gold-forever/companion/internal/sendlog"
	"github.com/gold-forever/companion/internal/watch"
)

const (
	firstWait = time.Minute
	longWait  = 30 * time.Minute
)

// Uploader sends one save; api.Client is the real one.
type Uploader interface {
	Upload(ctx context.Context, save []byte) (api.Answer, error)
}

// Core holds what one loop needs. Saves is asked on every step, so a WoW folder added or a new
// account appears without a restart.
type Core struct {
	Uploader Uploader
	Backups  *backup.Store
	Log      *sendlog.Log
	Watcher  *watch.Watcher
	Saves    func() []find.Save
	IsOff    func(path string) bool
	Out      io.Writer // one line per thing that happened, for the player

	waitUntil time.Time
	wait      time.Duration
}

// Step looks once: it keeps a copy of every save that settled, then sends what is pending unless the
// server asked it to wait. It returns an error only when uploads must stop: a token the server refuses.
func (c *Core) Step(ctx context.Context, now time.Time) error {
	var paths []string
	for _, s := range c.Saves() {
		if !c.IsOff(s.Path) {
			paths = append(paths, s.Path)
		}
	}
	for _, s := range c.Watcher.Poll(now, paths) {
		if _, added, err := c.Backups.Add(s.Path, s.Data); err != nil {
			c.say(now, "could not keep a copy of %s: %v", find.At(s.Path).Label(), err)
		} else if added {
			c.say(now, "new save: %s (%s)", find.At(s.Path).Label(), find.Game(s.Data))
		}
	}
	if now.Before(c.waitUntil) {
		return nil
	}
	pending, err := c.Backups.Pending()
	if err != nil {
		c.say(now, "could not list the copies to send: %v; trying again in %s", err, c.backOff(now))
		return nil
	}
	for _, p := range pending {
		if c.IsOff(p.Source) {
			continue
		}
		if stop, err := c.send(ctx, now, p); stop {
			return err
		}
	}
	return nil
}

// send uploads one copy. It reports whether the rest must wait (stop), and an error when they must
// wait for the player to link again.
func (c *Core) send(ctx context.Context, now time.Time, p backup.Copy) (stop bool, err error) {
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return false, nil
	}
	label := find.At(p.Source).Label()
	answer, err := c.Uploader.Upload(ctx, data)
	var limited *api.RateLimitedError
	var refused *api.RefusedError
	var forbidden *api.ForbiddenError
	switch {
	case ctx.Err() != nil:
		// The companion is stopping: the copy stays pending for the next start.
		return true, nil
	case err == nil:
		c.wait = 0
		c.done(now, p, label, data, answer.Status)
	case errors.Is(err, api.ErrUnauthorized):
		c.say(now, "the server no longer accepts this token: link the companion again")
		return true, err
	case errors.As(err, &forbidden):
		// Nothing on this side can change it, but the server can: a suspension ends, a token's rights change.
		c.waitUntil = now.Add(longWait)
		c.say(now, "%s: uploads wait until the account may upload again; trying again in %s", forbidden.Reason, longWait)
		return true, nil
	case errors.As(err, &limited):
		c.waitUntil = now.Add(limited.RetryAfter)
		c.say(now, "the server asked to wait %s", limited.RetryAfter)
		return true, nil
	case api.Retryable(err):
		c.say(now, "%v; trying again in %s", err, c.backOff(now))
		return true, nil
	case errors.As(err, &refused):
		c.done(now, p, label, data, "refused: "+refused.Reason)
	case errors.Is(err, api.ErrTooLarge):
		c.done(now, p, label, data, "refused: too large")
	default:
		c.say(now, "%s: %v", label, err)
	}
	return false, nil
}

// done records the server's last word on a copy, so it is not sent again.
func (c *Core) done(now time.Time, p backup.Copy, label string, data []byte, answer string) {
	sum := sha256.Sum256(data)
	if err := c.Backups.MarkSent(p); err != nil {
		c.say(now, "could not mark %s as sent: %v", label, err)
	}
	if err := c.Log.Append(sendlog.Entry{Time: now, Save: label, Size: len(data), SHA256: hex.EncodeToString(sum[:]), Answer: answer}); err != nil {
		c.say(now, "could not write the log: %v", err)
	}
	c.say(now, "sent %s: %s", label, answer)
}

// backOff waits longer than after the last failed try, and returns how long.
func (c *Core) backOff(now time.Time) time.Duration {
	c.wait = nextBackoff(c.wait)
	c.waitUntil = now.Add(c.wait)
	return c.wait
}

// nextBackoff doubles the wait after a failed try, from one minute up to thirty.
func nextBackoff(wait time.Duration) time.Duration {
	if wait == 0 {
		return firstWait
	}
	return min(2*wait, longWait)
}

func (c *Core) say(now time.Time, format string, args ...any) {
	fmt.Fprintf(c.Out, "%s %s\n", now.Local().Format("15:04:05"), fmt.Sprintf(format, args...))
}

// Run steps every interval until ctx ends or the token is refused.
func (c *Core) Run(ctx context.Context, every time.Duration) error {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if err := c.Step(ctx, time.Now()); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
