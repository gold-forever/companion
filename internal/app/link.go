package app

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/gold-forever/companion/internal/account/config"
	"github.com/gold-forever/companion/internal/server"
	"github.com/gold-forever/companion/internal/ui/page"
)

// A token as /me/tokens makes one: gf_ and 43 letters and digits.
var tokenShape = regexp.MustCompile(`^gf_[0-9A-Za-z]{43}$`)

func (c *App) Link(token string) error {
	token = strings.TrimSpace(token)
	if !tokenShape.MatchString(token) {
		return page.BadRequest("that is not a token: a token starts with gf_ and is 46 letters and digits, from your tokens page")
	}
	return c.update(func(cfg *config.Config) error { cfg.Token = token; return nil })
}

// linkTimeout outlasts a code's ten minutes, so the server's expiry is what ends a link.
const linkTimeout = 15 * time.Minute

// StartDeviceLink starts linking by code in the background, opening the approval page in the browser;
// the page follows it in State. A press while one waits starts nothing new.
func (c *App) StartDeviceLink() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.linking != nil && (c.linking.State == "starting" || c.linking.State == "waiting") {
		return nil
	}
	c.linking = &page.Linking{State: "starting"}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), linkTimeout)
		defer cancel()
		show := func(code server.DeviceCode) {
			// The browser first, so a page that sees "waiting" knows the approval page was opened.
			if code.VerificationURIComplete != "" {
				c.openURL(code.VerificationURIComplete)
			}
			c.setLinking(page.Linking{State: "waiting", UserCode: code.UserCode, URL: code.VerificationURIComplete})
		}
		save := func(token string) error {
			return c.update(func(cfg *config.Config) error { cfg.Token = token; return nil })
		}
		err := c.linker(ctx, show, save)
		switch {
		case err == nil:
			c.setLinking(page.Linking{State: "linked"})
		case errors.Is(err, server.ErrLinkDenied):
			c.setLinking(page.Linking{State: "denied"})
		case errors.Is(err, server.ErrLinkExpired), errors.Is(err, context.DeadlineExceeded):
			c.setLinking(page.Linking{State: "expired"})
		default:
			c.setLinking(page.Linking{State: "failed", Error: err.Error()})
		}
	}()
	return nil
}

func (c *App) setLinking(l page.Linking) {
	c.mu.Lock()
	c.linking = &l
	c.mu.Unlock()
}

func (c *App) Unlink() error {
	return c.update(func(cfg *config.Config) error { cfg.Token = ""; return nil })
}
