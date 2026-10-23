package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// The largest answer of a game to trust: a Forever market is about 100 KB and an answer holds at most
// 8; a retail realm-group market of 8–13k items is about 1 MB, and an account has several.
func pricesLimit(game string) int64 {
	if game == "retail" {
		return 8 << 20
	}
	return 4 << 20
}

// ErrNotModified means the answer is still that of the ETag sent: the file stays as it is.
var ErrNotModified = errors.New("not changed since the ETag sent")

// Prices fetches the pooled prices as the server sent them, with their ETag; internal/prices checks
// them. Without a market the answer holds every market of the account's characters in game. With the
// ETag of the prices already held, an unchanged answer is ErrNotModified.
func (c *Client) Prices(ctx context.Context, game, market, etag string) ([]byte, string, error) {
	query := url.Values{"game": {game}}
	if market != "" {
		query.Set("market", market)
	}
	return c.conditionalGet(ctx, "/api/v1/prices?"+query.Encode(), etag, pricesLimit(game))
}

// conditionalGet fetches path with the token, sending etag as If-None-Match: a 304 is ErrNotModified
// with etag kept, an answer over limit bytes an error, any other status that of answer.
func (c *Client) conditionalGet(ctx context.Context, path, etag string, limit int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(c.BaseURL, "/")+path, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("User-Agent", c.userAgent())
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	res, err := c.httpClient().Do(req)
	if err != nil {
		return nil, "", &unavailableError{err}
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified {
		return nil, etag, ErrNotModified
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, "", &unavailableError{err}
	}
	if res.StatusCode != http.StatusOK {
		_, err := answer(res, body)
		return nil, "", err
	}
	if int64(len(body)) > limit {
		return nil, "", fmt.Errorf("the answer to %s is over %d bytes", path, limit)
	}
	return body, res.Header.Get("ETag"), nil
}
