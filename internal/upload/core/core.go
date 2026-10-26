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
	"github.com/gold-forever/companion/internal/saves/backup"
	"github.com/gold-forever/companion/internal/saves/find"
	"github.com/gold-forever/companion/internal/saves/refused"
	"github.com/gold-forever/companion/internal/saves/watch"
	"github.com/gold-forever/companion/internal/upload/sendlog"
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
	Refused  *refused.List // the saves refused as too large, tried again daily
	Watcher  *watch.Watcher
	Saves    func() []find.Save
	IsOff    func(path string) bool
	Out      io.Writer // one line per thing that happened, for the player
	Imported func()    // called when the server imported an upload, so prices may have changed; may be nil

	waitUntil time.Time
	wait      time.Duration
}

// longWait is how long uploads wait after a 403 before the next try.
const longWait = 30 * time.Minute

// Step looks once: it keeps a copy of every save that settled, then sends what is pending and the
// refused saves that are due again, unless the server asked it to wait. It returns an error only when
// uploads must stop: a token the server refuses.
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
		c.say(now, "could not list the copies to send: %v; trying again in %s", err, c.backOff(now, 0))
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
	return c.retry(ctx, now)
}

// retry sends again each save refused as too large that is due, unless the save has changed since: its
// newer copy goes out as pending, and that answer is the save's last word.
func (c *Core) retry(ctx context.Context, now time.Time) error {
	for _, r := range c.Refused.Due(now) {
		if c.IsOff(r.Source) {
			continue
		}
		copies, err := c.Backups.Copies(r.Source)
		if err != nil || len(copies) == 0 {
			continue
		}
		newest := copies[len(copies)-1]
		data, err := os.ReadFile(newest.Path)
		if err != nil {
			continue
		}
		if sha(data) != r.SHA256 {
			c.dropRefused(now, r.Source)
			continue
		}
		if stop, err := c.upload(ctx, now, newest, data); stop {
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
	return c.upload(ctx, now, p, data)
}

// upload sends a copy's data and acts on the answer, as send reports.
func (c *Core) upload(ctx context.Context, now time.Time, p backup.Copy, data []byte) (stop bool, err error) {
	label := find.At(p.Source).Label()
	answer, err := c.Uploader.Upload(ctx, data)
	var limited *api.RateLimitedError
	var refused *api.RefusedError
	var forbidden *api.ForbiddenError
	var tooLarge *api.TooLargeError
	switch {
	case ctx.Err() != nil:
		// The companion is stopping: the copy stays pending for the next start.
		return true, nil
	case err == nil:
		c.wait = 0
		c.done(now, p, label, data, answer.Status)
		if answer.Status == api.StatusImported && c.Imported != nil {
			c.Imported()
		}
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
		c.say(now, "%v; trying again in %s", err, c.backOff(now, api.RetryAfter(err)))
		return true, nil
	case errors.As(err, &refused):
		c.done(now, p, label, data, "refused: "+refused.Reason)
	case errors.As(err, &tooLarge):
		c.done(now, p, label, data, withReason("refused: too large", tooLarge.Reason))
		if err := c.Refused.Refuse(p.Source, sha(data), now); err != nil {
			c.say(now, "could not keep %s for a daily try: %v", label, err)
		}
	default:
		c.say(now, "%s: %v", label, err)
	}
	return false, nil
}

// done records the server's last word on a copy, so it is not sent again, and ends any refusal of
// the save it came from.
func (c *Core) done(now time.Time, p backup.Copy, label string, data []byte, answer string) {
	if err := c.Backups.MarkSent(p); err != nil {
		c.say(now, "could not mark %s as sent: %v", label, err)
	}
	c.dropRefused(now, p.Source)
	if err := c.Log.Append(sendlog.Entry{Time: now, Save: label, Size: len(data), SHA256: sha(data), Answer: answer}); err != nil {
		c.say(now, "could not write the log: %v", err)
	}
	c.say(now, "sent %s: %s", label, answer)
}

// backOff waits longer than after the last failed try, and at least as long as atLeast, the server's
// own Retry-After; it returns how long.
func (c *Core) backOff(now time.Time, atLeast time.Duration) time.Duration {
	c.wait = api.NextWait(c.wait)
	wait := max(c.wait, atLeast)
	c.waitUntil = now.Add(wait)
	return wait
}

func (c *Core) dropRefused(now time.Time, source string) {
	if err := c.Refused.Drop(source); err != nil {
		c.say(now, "could not update the refused saves: %v", err)
	}
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func withReason(what, reason string) string {
	if reason == "" {
		return what
	}
	return what + ": " + reason
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
