package app

import (
	"context"
	"time"

	release "github.com/gold-forever/companion/internal/platform/version"
	"github.com/gold-forever/companion/internal/server"
	"github.com/gold-forever/companion/internal/ui/page"
)

const checkVersionEvery = 24 * time.Hour

// checkVersion asks the server once a day whether a newer release exists, for the page's notice.
func (c *App) checkVersion(ctx context.Context) {
	client := &server.Client{BaseURL: c.url(), Version: c.version, OS: goos}
	for {
		if latest, err := client.Latest(ctx); err == nil && release.Newer(latest.Version, c.version) {
			c.mu.Lock()
			c.notice = &page.Notice{Version: latest.Version, URL: latest.URL}
			c.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(checkVersionEvery):
		}
	}
}
