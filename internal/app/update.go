package app

import (
	"context"
	"time"

	release "github.com/gold-forever/companion/internal/platform/version"
	"github.com/gold-forever/companion/internal/server"
	"github.com/gold-forever/companion/internal/ui/page"
)

const checkVersionEvery = 24 * time.Hour

// checkVersion checks for an update once a day.
func (c *App) checkVersion(ctx context.Context) {
	client := &server.Client{BaseURL: c.url(), Version: c.version, OS: goos}
	for {
		_ = c.checkForUpdate(ctx, client)
		select {
		case <-ctx.Done():
			return
		case <-time.After(checkVersionEvery):
		}
	}
}

// CheckForUpdate asks the server whether a newer release exists, for the page's notice.
func (c *App) CheckForUpdate(ctx context.Context) error {
	return c.checkForUpdate(ctx, &server.Client{BaseURL: c.url(), Version: c.version, OS: goos})
}

func (c *App) checkForUpdate(ctx context.Context, client *server.Client) error {
	latest, err := client.Latest(ctx)
	if err != nil {
		return err
	}
	if release.Newer(latest.Version, c.version) {
		c.mu.Lock()
		c.notice = &page.Notice{Version: latest.Version, URL: latest.URL}
		c.mu.Unlock()
	}
	return nil
}
