package app

import (
	"context"
	"time"

	"github.com/gold-forever/companion/internal/server"
)

// accountAsker says whom its token uploads to.
type accountAsker interface {
	Account(ctx context.Context) (server.Account, error)
}

// knownAccount is whom token uploads to, as the server last said: kept through a failed ask so what the
// page and tray show does not flicker. The name stays in the companion's own UI, never in a log.
type knownAccount struct {
	token string
	name  string
}

// Account is whom the current token uploads to: the account's public name, else its BattleTag; "" until
// the server has said, or when it cannot.
func (c *App) Account() string {
	token := c.settings.Get().Token
	c.mu.Lock()
	defer c.mu.Unlock()
	if token == "" || c.account.token != token {
		return ""
	}
	return c.account.name
}

// followAccount asks whom token uploads to, unless that is known, trying again while the server is
// unreachable, until ctx ends.
func (c *App) followAccount(ctx context.Context, token string, asker accountAsker) {
	c.mu.Lock()
	known := c.account.token == token
	c.mu.Unlock()
	if known {
		return
	}
	var wait time.Duration
	for err := c.askAccount(ctx, token, asker); server.Retryable(err); err = c.askAccount(ctx, token, asker) {
		wait = server.NextWait(wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(max(wait, server.RetryAfter(err))):
		}
	}
}

// askAccount asks once; any failure, a server without the route among them, leaves what is shown as it is.
func (c *App) askAccount(ctx context.Context, token string, asker accountAsker) error {
	a, err := asker.Account(ctx)
	if err != nil {
		return err
	}
	name := a.PublicName
	if name == "" {
		name = a.BattleTag
	}
	c.mu.Lock()
	c.account = knownAccount{token: token, name: name}
	c.mu.Unlock()
	return nil
}
