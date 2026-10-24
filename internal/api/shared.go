package api

import (
	"context"
	"errors"
)

// ErrNoShared means the server has no shared lists and profiles (404, a server from before them): the
// data file goes without them, and the addon keeps its own as an older companion would.
var ErrNoShared = errors.New("the server has no shared lists and profiles")

// The largest shared state to trust: 50 lists and 50 profiles of a few KB each.
const sharedLimit = 1 << 20

// SharedState fetches the account's shopping lists and sale profiles as the server sent them, with
// their ETag; internal/prices checks them. With the ETag of the state already held, an unchanged one
// is ErrNotModified.
func (c *Client) SharedState(ctx context.Context, etag string) ([]byte, string, error) {
	body, etag, err := c.conditionalGet(ctx, "/api/v1/shared/state", etag, sharedLimit)
	var refused *RefusedError
	if errors.As(err, &refused) && refused.Status == 404 {
		return nil, "", ErrNoShared
	}
	return body, etag, err
}
