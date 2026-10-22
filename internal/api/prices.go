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

// A market's prices are about 100 KB and an answer holds at most 8; far more is not one to trust.
const maxPricesBytes = 4 << 20

// ErrNotModified means the market's prices are still those of the ETag sent: the file stays as it is.
var ErrNotModified = errors.New("the prices have not changed")

// Prices fetches the pooled prices as the server sent them, with their ETag; internal/prices checks
// them. Without a market the answer holds every market of the account's characters in game. With the
// ETag of the prices already held, an unchanged answer is ErrNotModified.
func (c *Client) Prices(ctx context.Context, game, market, etag string) ([]byte, string, error) {
	query := url.Values{"game": {game}}
	if market != "" {
		query.Set("market", market)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimSuffix(c.BaseURL, "/")+"/api/v1/prices?"+query.Encode(), nil)
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
	body, err := io.ReadAll(io.LimitReader(res.Body, maxPricesBytes+1))
	if err != nil {
		return nil, "", &unavailableError{err}
	}
	if res.StatusCode != http.StatusOK {
		_, err := answer(res, body)
		return nil, "", err
	}
	if len(body) > maxPricesBytes {
		return nil, "", fmt.Errorf("the price answer is over %d bytes", maxPricesBytes)
	}
	return body, res.Header.Get("ETag"), nil
}
