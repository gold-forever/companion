package app

import (
	"context"
	"time"

	"github.com/gold-forever/companion/internal/server"
)

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

// followAccount asks whom client's token uploads to, unless that is known, trying again while the
// server is unreachable, until ctx ends.
func (c *App) followAccount(ctx context.Context, client *server.Client) {
	c.mu.Lock()
	known := c.account.token == client.Token
	c.mu.Unlock()
	if known {
		return
	}
	var wait time.Duration
	for err := c.askAccount(ctx, client); server.Retryable(err); err = c.askAccount(ctx, client) {
		wait = server.NextWait(wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(max(wait, server.RetryAfter(err))):
		}
	}
}

// askAccount asks once; any failure, a server without the route among them, leaves what is shown as it is.
func (c *App) askAccount(ctx context.Context, client *server.Client) error {
	a, err := client.Account(ctx)
	if err != nil {
		return err
	}
	name := a.PublicName
	if name == "" {
		name = a.BattleTag
	}
	c.mu.Lock()
	c.account = knownAccount{token: client.Token, name: name}
	c.mu.Unlock()
	return nil
}
