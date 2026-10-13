package issuex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// scriptedTransport is an http.RoundTripper that answers with Responses in
// order, then with an empty 200, and records what it was asked.
type scriptedTransport struct {
	Responses    []*http.Response
	RequestCount int
	Requests     []*http.Request
}

func (tt *scriptedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tt.RequestCount++
	tt.Requests = append(tt.Requests, req)
	if len(tt.Responses) == 0 {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	}
	resp := tt.Responses[0]
	tt.Responses = tt.Responses[1:]
	if resp.Header == nil {
		resp.Header = make(http.Header)
	}
	if resp.Body == nil {
		resp.Body = io.NopCloser(strings.NewReader(""))
	}
	if resp.Request == nil {
		resp.Request = req
	}
	return resp, nil
}

// scriptedForge is the shared request executor over a scriptedTransport.
func scriptedForge(tt *scriptedTransport, sleep func(time.Duration)) forgeHTTP {
	return forgeHTTP{
		baseURL:  "https://api.example.com",
		client:   &http.Client{Transport: tt},
		sleep:    sleep,
		newError: newGitHubHTTPError,
	}
}

// TestSharedRequestRetryWithin120s_TS_01_24 verifies TS-01-24:
// The shared request executor invokes the injectable sleep and retries once for rate limits within 120s.
// Verifies: 01-REQ-6.2, 01-REQ-6.5
func TestSharedRequestRetryWithin120s_TS_01_24(t *testing.T) {
	var slept time.Duration
	sleep := func(d time.Duration) { slept = d }
	ok := func() *http.Response {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}
	}
	limited := func(h http.Header) *http.Response { return &http.Response{StatusCode: 429, Header: h} }

	tt := &scriptedTransport{Responses: []*http.Response{limited(http.Header{"Retry-After": {"10"}}), ok()}}
	resp, _, err := scriptedForge(tt, sleep).do(context.Background(), http.MethodGet, "/test", nil, nil)
	if err != nil {
		t.Fatalf("expected nil error on retry success, got: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected status code 200, got: %d", resp.StatusCode)
	}
	if slept != 11*time.Second {
		t.Fatalf("expected slept == 11s (10s + 1s buffer), got: %v", slept)
	}
	if tt.RequestCount != 2 {
		t.Fatalf("expected 2 requests (1 original + 1 retry), got: %d", tt.RequestCount)
	}

	// Boundary condition: Retry-After: 119s -> backoff is 120s <= 120s -> retried
	slept = 0
	tt = &scriptedTransport{Responses: []*http.Response{limited(http.Header{"Retry-After": {"119"}}), ok()}}
	resp, _, err = scriptedForge(tt, sleep).do(context.Background(), http.MethodGet, "/test", nil, nil)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("expected success on 120s backoff boundary, got resp=%v, err=%v", resp, err)
	}
	if slept != 120*time.Second {
		t.Fatalf("expected slept == 120s, got: %v", slept)
	}
	if tt.RequestCount != 2 {
		t.Fatalf("expected 2 requests for 120s boundary, got: %d", tt.RequestCount)
	}

	// 403 quota exhaustion with X-RateLimit-Reset within 120s
	slept = 0
	reset := fmt.Sprint(time.Now().Add(5 * time.Second).Unix())
	tt = &scriptedTransport{Responses: []*http.Response{
		{StatusCode: 403, Header: http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {reset}}},
		ok(),
	}}
	resp, _, err = scriptedForge(tt, sleep).do(context.Background(), http.MethodGet, "/test", nil, nil)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("expected success on 403 quota retry, got resp=%v, err=%v", resp, err)
	}
	if slept <= 0 {
		t.Fatalf("expected positive sleep for 403 quota exhaustion, got: %v", slept)
	}
	if tt.RequestCount != 2 {
		t.Fatalf("expected 2 requests for 403 quota exhaustion, got: %d", tt.RequestCount)
	}

	// The request body survives the retry
	reqBody := `{"title":"sample"}`
	tt = &scriptedTransport{Responses: []*http.Response{limited(http.Header{"Retry-After": {"1"}}), ok()}}
	resp, _, err = scriptedForge(tt, func(time.Duration) {}).do(context.Background(), http.MethodPost, "/test", reqBody, nil)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("expected success on request with body, got err=%v", err)
	}
	if tt.RequestCount != 2 {
		t.Fatalf("expected 2 requests with body, got: %d", tt.RequestCount)
	}
	retried, err := io.ReadAll(tt.Requests[1].Body)
	if err != nil {
		t.Fatalf("failed reading body on retried request: %v", err)
	}
	if string(retried) != reqBody {
		t.Fatalf("expected retried body %q, got %q", reqBody, string(retried))
	}
}

// TestSharedRequestAbortsExceeding120s_TS_01_25 verifies TS-01-25:
// The shared request executor aborts with ErrRateLimited when the backoff exceeds 120s or the retry is limited again.
// Verifies: 01-REQ-6.3
func TestSharedRequestAbortsExceeding120s_TS_01_25(t *testing.T) {
	var slept time.Duration
	sleep := func(d time.Duration) { slept = d }
	limited := func(retryAfter string) *http.Response {
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {retryAfter}}}
	}

	tt := &scriptedTransport{Responses: []*http.Response{limited("300")}}
	resp, _, err := scriptedForge(tt, sleep).do(context.Background(), http.MethodGet, "/test", nil, nil)
	if resp != nil {
		t.Fatalf("expected nil response on abort, got: %v", resp)
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected errors.Is(err, ErrRateLimited) == true, got: %v", err)
	}
	if tt.RequestCount != 1 {
		t.Fatalf("expected exactly 1 request without retry, got: %d", tt.RequestCount)
	}
	if slept != 0 {
		t.Fatalf("expected no sleep on abort, slept: %v", slept)
	}

	// Boundary condition: Retry-After: 120s -> backoff is 121s > 120s -> aborts without retry
	tt = &scriptedTransport{Responses: []*http.Response{limited("120")}}
	_, _, err = scriptedForge(tt, sleep).do(context.Background(), http.MethodGet, "/test", nil, nil)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited on 121s backoff, got: %v", err)
	}
	if tt.RequestCount != 1 {
		t.Fatalf("expected exactly 1 request for 121s backoff, got: %d", tt.RequestCount)
	}

	// Retries exhausted: first request 429 with 5s, retry also returns 429
	tt = &scriptedTransport{Responses: []*http.Response{limited("5"), limited("5")}}
	_, _, err = scriptedForge(tt, func(time.Duration) {}).do(context.Background(), http.MethodGet, "/test", nil, nil)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited when retries exhausted, got: %v", err)
	}
	if tt.RequestCount != 2 {
		t.Fatalf("expected exactly 2 requests before exhausting retries, got: %d", tt.RequestCount)
	}
}

// TestSharedRequestContextCancellation verifies that a canceled context aborts the backoff.
func TestSharedRequestContextCancellation(t *testing.T) {
	tt := &scriptedTransport{Responses: []*http.Response{
		{StatusCode: 429, Header: http.Header{"Retry-After": {"10"}}},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := scriptedForge(tt, func(time.Duration) {}).do(ctx, http.MethodGet, "/test", nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}
