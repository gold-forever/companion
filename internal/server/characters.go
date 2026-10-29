package server

import (
	"context"
	"errors"
)

// ErrNoCharacters means the server does not list the account's characters (404, a server from before
// the list): the data file goes without one, and the addon knows only the characters of its own save.
var ErrNoCharacters = errors.New("the server does not list the account's characters")

// The largest list of characters to trust: a few thousand realms and names.
const charactersLimit = 256 << 10

// Characters fetches the account's characters on every WoW account linked to it as the server sent
// them, with their ETag; internal/addondata checks them. With the ETag of the list already held, an
// unchanged one is ErrNotModified.
func (c *Client) Characters(ctx context.Context, etag string) ([]byte, string, error) {
	body, etag, err := c.conditionalGet(ctx, "/api/v1/companion/characters", etag, charactersLimit)
	var refused *RefusedError
	if errors.As(err, &refused) && refused.Status == 404 {
		return nil, "", ErrNoCharacters
	}
	return body, etag, err
}
