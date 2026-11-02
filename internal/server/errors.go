package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// A 429 without Retry-After waits this long.
const defaultRetryAfter = time.Minute

// ErrUnauthorized means the token is wrong or revoked: uploads stop until the player links again.
var ErrUnauthorized = errors.New("the server does not accept this token")

// ErrTooLarge means the save is over the server's limit; sending it again helps only once the limit
// is raised.
var ErrTooLarge = errors.New("the save is too large for the server")

// TooLargeError is ErrTooLarge with the server's reason, which names the limit; "" when it gave none.
type TooLargeError struct{ Reason string }

func (e *TooLargeError) Error() string { return withReason(ErrTooLarge.Error(), e.Reason).Error() }

func (e *TooLargeError) Is(target error) bool { return target == ErrTooLarge }

// ForbiddenError means the account may not upload now (suspended, or a token without upload rights):
// uploads wait until that changes on the server.
type ForbiddenError struct{ Reason string }

func (e *ForbiddenError) Error() string { return "the server refuses uploads: " + e.Reason }

// RefusedError is a save the server read and refused, with its reason (an unknown game, a broken file).
type RefusedError struct {
	Status int
	Reason string
}

func (e *RefusedError) Error() string { return fmt.Sprintf("refused (%d): %s", e.Status, e.Reason) }

// RateLimitedError asks the companion to wait before the next upload.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("rate limited, retry after %s", e.RetryAfter)
}

// unavailableError is the server down, unreachable or failing: worth trying again later, and not
// before retryAfter when the server named a wait (a paused server says 600 s).
type unavailableError struct {
	cause      error
	retryAfter time.Duration
}

func (e *unavailableError) Error() string { return "server unavailable: " + e.cause.Error() }

func (e *unavailableError) Unwrap() error { return e.cause }

const (
	firstWait = time.Minute
	longWait  = 30 * time.Minute
)

// NextWait doubles the wait after a failed try that is Retryable, from one minute up to thirty.
func NextWait(wait time.Duration) time.Duration {
	if wait == 0 {
		return firstWait
	}
	return min(2*wait, longWait)
}

// Retryable says whether the same upload may succeed later without anything changing on this side.
func Retryable(err error) bool {
	var limited *RateLimitedError
	var unavailable *unavailableError
	return errors.As(err, &limited) || errors.As(err, &unavailable)
}

// RetryAfter is how long the server asked a failed try to wait before the next: 0 when it named no wait.
func RetryAfter(err error) time.Duration {
	var unavailable *unavailableError
	if errors.As(err, &unavailable) {
		return unavailable.retryAfter
	}
	return 0
}

// reasonOf is the refusal's reason, else its status, which carries it on some answers.
func reasonOf(r UploadRefusal) string {
	if r.Reason != "" {
		return r.Reason
	}
	return r.Status
}

// withReason is what failed, with the server's own words when it gave them.
func withReason(what, reason string) error {
	if reason == "" {
		return errors.New(what)
	}
	return fmt.Errorf("%s: %s", what, reason)
}

// retryAfter reads Retry-After in seconds, the only form the server sends; false when there is none.
func retryAfter(header string) (time.Duration, bool) {
	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second, true
	}
	return 0, false
}

// notFound says whether err is the server's 404 to a GET: a server from before the route.
func notFound(err error) bool {
	var refused *RefusedError
	return errors.As(err, &refused) && refused.Status == http.StatusNotFound
}
