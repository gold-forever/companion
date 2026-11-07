package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// The save's file name as the server logs it; the path it was read from is never sent.
const saveName = "GoldForever.lua"

// The slowest uplink a save is given time for, in bytes per second: 32 MiB then takes 34 minutes.
const minUploadRate = 16 << 10

// Answer is what the server says about an upload it took.
type Answer = ImportResult

// StatusImported is an upload's status when the server took new data from it; "already imported" is
// one it had.
const (
	StatusImported        = "imported"
	statusAlreadyImported = "already imported"
)

// Upload sends one save, byte for byte, as the game wrote it.
func (c *Client) Upload(ctx context.Context, save []byte) (Answer, error) {
	ctx, cancel := context.WithTimeout(ctx, uploadDeadline(len(save)))
	defer cancel()
	url, err := c.endpoint("/api/v1/saves?name=" + saveName)
	if err != nil {
		return Answer{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(save))
	if err != nil {
		return Answer{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", c.userAgent())
	res, err := c.uploadClient().Do(req)
	if err != nil {
		return Answer{}, &unavailableError{cause: err}
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxAnswerBytes))
	return answer(res, body)
}

// answer reads the server's answer. Only the answers PROTOCOL.md names as final settle a save; any
// other, such as a proxy's 404 mid-deploy or a captive portal's 200, is tried again.
func answer(res *http.Response, body []byte) (Answer, error) {
	var a Answer
	unreadable := json.Unmarshal(body, &a) != nil
	var refusal UploadRefusal
	_ = json.Unmarshal(body, &refusal)
	switch {
	case res.StatusCode == http.StatusOK:
		if unreadable || (a.Status != StatusImported && a.Status != statusAlreadyImported) {
			return a, &unavailableError{cause: errors.New("an answer that is not the server's")}
		}
		return a, nil
	case res.StatusCode == http.StatusUnauthorized:
		return a, ErrUnauthorized
	case res.StatusCode == http.StatusForbidden:
		return a, &ForbiddenError{reasonOf(refusal)}
	case res.StatusCode == http.StatusRequestEntityTooLarge:
		return a, &TooLargeError{refusal.Reason}
	case res.StatusCode == http.StatusTooManyRequests:
		wait, ok := retryAfter(res.Header.Get("Retry-After"))
		if !ok {
			wait = defaultRetryAfter
		}
		return a, &RateLimitedError{wait}
	case res.StatusCode == http.StatusBadRequest, res.StatusCode == http.StatusUnsupportedMediaType,
		res.StatusCode == http.StatusUnprocessableEntity:
		return a, &RefusedError{res.StatusCode, reasonOf(refusal)}
	case res.StatusCode >= 500:
		wait, _ := retryAfter(res.Header.Get("Retry-After"))
		return a, &unavailableError{withReason(fmt.Sprintf("status %d", res.StatusCode), refusal.Reason), wait}
	default:
		return a, &unavailableError{cause: fmt.Errorf("unexpected answer %d", res.StatusCode)}
	}
}

// uploadDeadline bounds one upload of size bytes: sending it at minUploadRate, then the answer.
func uploadDeadline(size int) time.Duration {
	return answerTimeout + time.Duration(size/minUploadRate)*time.Second
}

// uploadTransport bounds each phase of an upload rather than the whole: a slow uplink still finishes.
var uploadTransport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = answerTimeout
	return t
}()

func (c *Client) uploadClient() *http.Client {
	if c.HTTP != nil {
		return c.httpClient()
	}
	return &http.Client{Transport: uploadTransport, CheckRedirect: carryToken}
}
