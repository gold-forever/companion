package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// ErrNoNamesWanted means the server lists no names wanted for the locale: a 404 from a server before
// the list, or a 400 for a locale it does not serve (English, before gf#397's follow-up). The data
// file then goes without the list.
var ErrNoNamesWanted = errors.New("the server lists no names wanted for this locale")

// The largest list of names wanted to trust: 5 kinds of at most 2,000 IDs are about 70 KB.
const namesWantedLimit = 256 << 10

// NamesWanted fetches the IDs whose names in locale (a client's GetLocale()) the server lacks for
// game, as it sent them, with their ETag; internal/addondata checks them. With the ETag of the list
// already held, an unchanged one is ErrNotModified.
func (c *Client) NamesWanted(ctx context.Context, game, locale, etag string) ([]byte, string, error) {
	query := url.Values{"game": {game}, "locale": {locale}}
	body, etag, err := c.conditionalGet(ctx, "/api/v1/names/wanted?"+query.Encode(), etag, namesWantedLimit)
	var refused *RefusedError
	if errors.As(err, &refused) && (refused.Status == http.StatusNotFound || refused.Status == http.StatusBadRequest) {
		return nil, "", ErrNoNamesWanted
	}
	return body, etag, err
}
