package api

import (
	"context"
	"errors"
)

// ErrNoDings means the server has no dings state (404, a server from before it): the data file goes
// without one, and the addon leaves its switch as an older companion would.
var ErrNoDings = errors.New("the server has no dings state")

// DingsState fetches the site's side of the in-game "Announce dings on Discord" switch as the server
// sent it, with its ETag; internal/prices checks it. With the ETag of the state already held, an
// unchanged one is ErrNotModified.
func (c *Client) DingsState(ctx context.Context, etag string) ([]byte, string, error) {
	body, etag, err := c.conditionalGet(ctx, "/api/v1/dings/state", etag, maxAnswerBytes)
	var refused *RefusedError
	if errors.As(err, &refused) && refused.Status == 404 {
		return nil, "", ErrNoDings
	}
	return body, etag, err
}
