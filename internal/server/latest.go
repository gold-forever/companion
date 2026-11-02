package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Latest asks the server for the newest release, without the token. No release named (404) is an
// empty Release; the player downloads a newer one themselves, as the companion never updates itself.
func (c *Client) Latest(ctx context.Context) (Release, error) {
	var r Release
	url := strings.TrimSuffix(c.BaseURL, "/") + "/api/v1/companion/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return r, err
	}
	req.Header.Set("User-Agent", c.userAgent())
	res, err := c.httpClient().Do(req)
	if err != nil {
		return r, err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
		err = json.NewDecoder(io.LimitReader(res.Body, maxAnswerBytes)).Decode(&r)
		if err == nil && r.URL != "" && !releasePage(c.BaseURL, r.URL) {
			r, err = Release{}, fmt.Errorf("the version check named a page that is not a release: %q", r.URL)
		}
	case http.StatusNotFound:
	default:
		err = fmt.Errorf("the version check answered %d", res.StatusCode)
	}
	return r, err
}
