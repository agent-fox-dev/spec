package issuex

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestHTTPError_Error(t *testing.T) {
	e := &HTTPError{Method: "GET", Path: "/repos", Status: 404, Message: "Not Found"}
	if got, want := e.Error(), "GET /repos: 404 Not Found"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// RetryAfter is the wait a rate-limited response asks for, plus a second's
// buffer; anything that is not a rate limit, or does not say how long, is no
// wait at all.
func TestHTTPError_RetryAfter(t *testing.T) {
	future := strconv.FormatInt(time.Now().Add(10*time.Second).Unix(), 10)
	past := strconv.FormatInt(time.Now().Add(-10*time.Second).Unix(), 10)
	about := func(lo, hi time.Duration) func(time.Duration) bool {
		return func(d time.Duration) bool { return d >= lo && d <= hi }
	}
	exactly := func(want time.Duration) func(time.Duration) bool {
		return func(d time.Duration) bool { return d == want }
	}
	for _, tc := range []struct {
		name   string
		err    *HTTPError
		wantOK bool
		want   func(time.Duration) bool
	}{
		{"429 with Retry-After", &HTTPError{Status: 429, RetryAfterHeader: "30"}, true, exactly(31 * time.Second)},
		{"429 with a reset time", &HTTPError{Status: 429, RateLimitReset: future}, true, about(9*time.Second, 12*time.Second)},
		{"403 with the quota spent and a reset time", &HTTPError{Status: 403, RateLimitRemaining: "0", RateLimitReset: future}, true, about(9*time.Second, 12*time.Second)},
		{"403 with the quota spent and Retry-After", &HTTPError{Status: 403, RateLimitRemaining: "0", RetryAfterHeader: "15"}, true, exactly(16 * time.Second)},
		{"a reset time already past", &HTTPError{Status: 403, RateLimitRemaining: "0", RateLimitReset: past}, true, exactly(time.Second)},
		{"GitHub's secondary rate limit", &HTTPError{Status: 403, RetryAfterHeader: "3", RateLimitRemaining: "4999",
			Message: "You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}, true, exactly(4 * time.Second)},
		{"an unreadable Retry-After", &HTTPError{Status: 429, RetryAfterHeader: "not-a-number"}, false, exactly(0)},
		{"429 without headers", &HTTPError{Status: 429}, false, exactly(0)},
		{"403 with requests remaining", &HTTPError{Status: 403, RateLimitRemaining: "5", RetryAfterHeader: "30"}, false, exactly(0)},
		{"500", &HTTPError{Status: 500, RetryAfterHeader: "30"}, false, exactly(0)},
		{"nil", nil, false, exactly(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := tc.err.RetryAfter()
			if ok != tc.wantOK || !tc.want(d) {
				t.Errorf("RetryAfter() = %v, %v", d, ok)
			}
		})
	}
}

// Each classifier recognises its sentinel, wrapped or not, and the HTTP
// status that means it, and nothing else.
func TestErrorClassifiers(t *testing.T) {
	type verdict struct{ notFound, conflict, rateLimited, noToken bool }
	for _, tc := range []struct {
		name string
		err  error
		want verdict
	}{
		{"ErrNotFound", ErrNotFound, verdict{notFound: true}},
		{"wrapped ErrNotFound", fmt.Errorf("wrapped: %w", ErrNotFound), verdict{notFound: true}},
		{"404", &HTTPError{Status: 404}, verdict{notFound: true}},
		{"wrapped 404", fmt.Errorf("api error: %w", &HTTPError{Status: 404}), verdict{notFound: true}},
		{"ErrConflict", ErrConflict, verdict{conflict: true}},
		{"wrapped ErrConflict", fmt.Errorf("wrapped: %w", ErrConflict), verdict{conflict: true}},
		{"409", &HTTPError{Status: 409}, verdict{conflict: true}},
		{"wrapped 409", fmt.Errorf("api error: %w", &HTTPError{Status: 409}), verdict{conflict: true}},
		{"ErrRateLimited", ErrRateLimited, verdict{rateLimited: true}},
		{"wrapped ErrRateLimited", fmt.Errorf("wrapped: %w", ErrRateLimited), verdict{rateLimited: true}},
		{"429", &HTTPError{Status: 429}, verdict{rateLimited: true}},
		{"403 with the quota spent", &HTTPError{Status: 403, RateLimitRemaining: "0"}, verdict{rateLimited: true}},
		{"403 secondary rate limit", &HTTPError{Status: http.StatusForbidden, RateLimitRemaining: "4999",
			Message: "You have exceeded a secondary rate limit."}, verdict{rateLimited: true}},
		{"403 with requests remaining", &HTTPError{Status: 403, RateLimitRemaining: "1"}, verdict{}},
		{"plain 403", &HTTPError{Status: 403}, verdict{}},
		{"ErrNoToken", ErrNoToken, verdict{noToken: true}},
		{"wrapped ErrNoToken", fmt.Errorf("wrapped: %w", ErrNoToken), verdict{noToken: true}},
		{"500", &HTTPError{Status: 500}, verdict{}},
		{"ErrUnsupportedForge", ErrUnsupportedForge, verdict{}},
		{"ErrAmbiguousForge", ErrAmbiguousForge, verdict{}},
		{"another error", errors.New("some other error"), verdict{}},
		{"nil", nil, verdict{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := verdict{IsNotFound(tc.err), IsConflict(tc.err), IsRateLimited(tc.err), IsNoToken(tc.err)}
			if got != tc.want {
				t.Errorf("(IsNotFound, IsConflict, IsRateLimited, IsNoToken) = %+v, want %+v", got, tc.want)
			}
		})
	}
}
