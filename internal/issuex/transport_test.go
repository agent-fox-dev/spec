package issuex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A response body is read to at most 8 MB, so a hostile or broken server
// cannot exhaust memory.
func TestReadResponseBody_IsBoundedTo8MB(t *testing.T) {
	large := bytes.Repeat([]byte{'x'}, 10<<20)
	for name, read := range map[string]func() ([]byte, error){
		"ReadResponseBody":  func() ([]byte, error) { return ReadResponseBody(bytes.NewReader(large)) },
		"LimitResponseBody": func() ([]byte, error) { return io.ReadAll(LimitResponseBody(bytes.NewReader(large))) },
		"ReadResponse": func() ([]byte, error) {
			return ReadResponse(&http.Response{Body: io.NopCloser(bytes.NewReader(large))})
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := read()
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 8<<20 {
				t.Errorf("read %d bytes, want %d", len(got), 8<<20)
			}
		})
	}
}

func TestReadResponseBody_ReadsASmallBodyWhole(t *testing.T) {
	if got, err := ReadResponseBody(strings.NewReader("hello world")); err != nil || string(got) != "hello world" {
		t.Errorf("ReadResponseBody = %q, %v", got, err)
	}
	for name, read := range map[string]func() ([]byte, error){
		"nil reader":   func() ([]byte, error) { return ReadResponseBody(nil) },
		"nil response": func() ([]byte, error) { return ReadResponse(nil) },
		"no body":      func() ([]byte, error) { return ReadResponse(&http.Response{}) },
		"empty reader": func() ([]byte, error) { return ReadResponseBody(strings.NewReader("")) },
		"empty body":   func() ([]byte, error) { return ReadResponse(&http.Response{Body: http.NoBody}) },
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := read(); err != nil || len(got) != 0 {
				t.Errorf("= %q, %v; want nothing", got, err)
			}
		})
	}
}

// The HTTPError built from a response keeps what a caller needs to decide
// whether and how long to wait: the request, the status and the rate-limit
// headers, whatever their spelling.
func TestHTTPErrorFromResponse_KeepsTheRequestAndRateLimitHeaders(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Status:     "429 Too Many Requests",
		Header: http.Header{
			"Retry-After":           {"30"},
			"x-ratelimit-reset":     {"1700000000"}, // not canonical
			"X-Ratelimit-Remaining": {"0"},
		},
		Request: &http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/repos/o/r/issues"}},
	}
	got := HTTPErrorFromResponse(resp)
	want := HTTPError{Method: "GET", Path: "/repos/o/r/issues", Status: 429, Message: "429 Too Many Requests",
		RetryAfterHeader: "30", RateLimitReset: "1700000000", RateLimitRemaining: "0"}
	if got == nil || *got != want {
		t.Errorf("HTTPErrorFromResponse = %+v, want %+v", got, want)
	}

	bare := HTTPErrorFromResponse(&http.Response{StatusCode: http.StatusTeapot})
	if bare == nil || bare.Message != "I'm a teapot" || bare.Method != "" || bare.Path != "" {
		t.Errorf("without a status line or request: %+v", bare)
	}
	if HTTPErrorFromResponse(nil) != nil {
		t.Error("HTTPErrorFromResponse(nil) is not nil")
	}
}

// HandleResponse maps a response to the package's sentinels the way every
// adapter does.
func TestHandleResponse_ClassifiesTheStatus(t *testing.T) {
	reset := fmt.Sprint(time.Now().Add(45 * time.Second).Unix())
	for _, tc := range []struct {
		name          string
		status        int
		header        http.Header
		authenticated bool
		check         func(error) bool
		private       bool
	}{
		{"200", 200, nil, false, func(err error) bool { return err == nil }, false},
		{"304", 304, nil, false, func(err error) bool { return err == nil }, false},
		{"unauthenticated 404", 404, nil, false, IsNotFound, true},
		{"authenticated 404", 404, nil, true, IsNotFound, false},
		{"405", 405, nil, true, func(err error) bool { return errors.Is(err, ErrConflict) }, false},
		{"409", 409, nil, true, IsConflict, false},
		{"406 is no conflict without a forge saying so", 406, nil, true, func(err error) bool { return err != nil && !IsConflict(err) }, false},
		{"429", 429, nil, true, func(err error) bool { return errors.Is(err, ErrRateLimited) }, false},
		{"403 with the quota spent", 403, http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {reset}}, true,
			func(err error) bool { return errors.Is(err, ErrRateLimited) }, false},
		{"403 refusal", 403, nil, true, func(err error) bool { return err != nil && !IsRateLimited(err) }, false},
		{"500", 500, nil, true, func(err error) bool {
			var he *HTTPError
			return errors.As(err, &he) && he.Status == 500 && !IsNotFound(err) && !IsConflict(err) && !IsRateLimited(err)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			header := tc.header
			if header == nil {
				header = http.Header{}
			}
			err := HandleResponse(&http.Response{StatusCode: tc.status, Header: header}, tc.authenticated)
			if !tc.check(err) {
				t.Errorf("HandleResponse = %v", err)
			}
			if got := err != nil && strings.Contains(err.Error(), "resource may be private"); got != tc.private {
				t.Errorf("private-resource guidance = %v, want %v: %v", got, tc.private, err)
			}
			if tc.header == nil {
				if again := HandleResponseStatus(tc.status, tc.authenticated); fmt.Sprint(again) != fmt.Sprint(err) {
					t.Errorf("HandleResponseStatus = %v, HandleResponse = %v", again, err)
				}
			}
		})
	}
	if err := HandleResponse(nil, false); err != nil {
		t.Errorf("HandleResponse(nil) = %v", err)
	}
}

// The backoff of a rate-limited 403 survives the wrapping, so a caller can
// still read how long to wait.
func TestHandleResponse_KeepsTheBackoffReadable(t *testing.T) {
	reset := fmt.Sprint(time.Now().Add(45 * time.Second).Unix())
	err := HandleResponse(&http.Response{StatusCode: 403,
		Header: http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {reset}}}, true)
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("no HTTPError in %v", err)
	}
	if d, ok := he.RetryAfter(); !ok || d < 40*time.Second || d > 50*time.Second {
		t.Errorf("RetryAfter = %v, %v; want about 46s", d, ok)
	}
}

// A rate limit whose backoff is within 120 s is waited out with the
// injected sleep and retried once, with the same request body.
func TestForgeHTTP_RetriesAShortRateLimitOnce(t *testing.T) {
	ok := func() *http.Response {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}
	}
	reset := fmt.Sprint(time.Now().Add(5 * time.Second).Unix())
	for _, tc := range []struct {
		name      string
		limited   *http.Response
		wantSleep func(time.Duration) bool
	}{
		{"429 with Retry-After", &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"10"}}},
			func(d time.Duration) bool { return d == 11*time.Second }},
		{"the 120 s boundary", &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"119"}}},
			func(d time.Duration) bool { return d == 120*time.Second }},
		{"403 with the quota spent", &http.Response{StatusCode: 403, Header: http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {reset}}},
			func(d time.Duration) bool { return d > 0 && d <= 7*time.Second }},
		{"403 secondary rate limit", &http.Response{StatusCode: 403, Header: http.Header{"Retry-After": {"3"}, "X-Ratelimit-Remaining": {"4999"}},
			Body: io.NopCloser(strings.NewReader(`{"message": "You have exceeded a secondary rate limit."}`))},
			func(d time.Duration) bool { return d == 4*time.Second }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var slept []time.Duration
			tt := &scriptedTransport{Responses: []*http.Response{tc.limited, ok()}}
			body := `{"title":"sample"}`
			resp, _, err := scriptedForge(tt, func(d time.Duration) { slept = append(slept, d) }).
				do(context.Background(), http.MethodPost, "/test", body, nil)
			if err != nil || resp.StatusCode != 200 {
				t.Fatalf("do = %v, %v; want the retried 200", resp, err)
			}
			if len(slept) != 1 || !tc.wantSleep(slept[0]) {
				t.Errorf("slept %v", slept)
			}
			if tt.RequestCount != 2 {
				t.Fatalf("%d requests, want the original and one retry", tt.RequestCount)
			}
			if retried, _ := io.ReadAll(tt.Requests[1].Body); string(retried) != body {
				t.Errorf("retried body = %q, want %q", retried, body)
			}
		})
	}
}

// A backoff over 120 s is not waited for, and a retry that is limited again
// is not retried: both are ErrRateLimited.
func TestForgeHTTP_GivesUpOnALongOrRepeatedRateLimit(t *testing.T) {
	limited := func(retryAfter string) *http.Response {
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {retryAfter}}}
	}
	for _, tc := range []struct {
		name         string
		responses    []*http.Response
		wantRequests int
		wantSleeps   int
	}{
		{"300 s", []*http.Response{limited("300")}, 1, 0},
		{"just over 120 s", []*http.Response{limited("120")}, 1, 0},
		{"limited again", []*http.Response{limited("5"), limited("5")}, 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sleeps := 0
			tt := &scriptedTransport{Responses: tc.responses}
			resp, _, err := scriptedForge(tt, func(time.Duration) { sleeps++ }).do(context.Background(), http.MethodGet, "/test", nil, nil)
			if resp != nil || !errors.Is(err, ErrRateLimited) {
				t.Errorf("do = %v, %v; want ErrRateLimited", resp, err)
			}
			var he *HTTPError
			if !errors.As(err, &he) || he.Status != 429 {
				t.Errorf("the error does not carry the 429: %v", err)
			}
			if tt.RequestCount != tc.wantRequests || sleeps != tc.wantSleeps {
				t.Errorf("%d requests and %d sleeps, want %d and %d", tt.RequestCount, sleeps, tc.wantRequests, tc.wantSleeps)
			}
		})
	}
}

// The wait ends when the run is cancelled, whether it was cancelled before or
// during it, and with the default sleep as with an injected one.
func TestForgeHTTP_RateLimitWaitHonoursCancellation(t *testing.T) {
	limited := func() *http.Response {
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"60"}}}
	}
	t.Run("cancelled before", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		tt := &scriptedTransport{Responses: []*http.Response{limited()}}
		if _, _, err := scriptedForge(tt, func(time.Duration) {}).do(ctx, http.MethodGet, "/test", nil, nil); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})
	t.Run("cancelled during an injected sleep", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		tt := &scriptedTransport{Responses: []*http.Response{limited()}}
		_, _, err := scriptedForge(tt, func(time.Duration) { cancel() }).do(ctx, http.MethodGet, "/test", nil, nil)
		if !errors.Is(err, context.Canceled) || tt.RequestCount != 1 {
			t.Errorf("err = %v after %d requests, want context.Canceled after 1", err, tt.RequestCount)
		}
	})
	t.Run("cancelled during the default sleep", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		tt := &scriptedTransport{Responses: []*http.Response{limited()}}
		start := time.Now()
		_, _, err := scriptedForge(tt, nil).do(ctx, http.MethodGet, "/test", nil, nil)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err = %v, want context.DeadlineExceeded", err)
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("the run waited %s past its cancellation", d)
		}
	})
}

// The request body may be given as bytes, text, a reader or any value to
// encode as JSON; only a non-empty body is sent, as JSON.
func TestForgeHTTP_SendsTheRequestBody(t *testing.T) {
	for _, tc := range []struct {
		name string
		body any
		want string
	}{
		{"bytes", []byte(`{"a":1}`), `{"a":1}`},
		{"string", `{"a":1}`, `{"a":1}`},
		{"reader", strings.NewReader(`{"a":1}`), `{"a":1}`},
		{"value", map[string]int{"a": 1}, `{"a":1}`},
		{"empty bytes", []byte{}, ""},
		{"empty string", "", ""},
		{"empty reader", strings.NewReader(""), ""},
		{"nil", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tt := &scriptedTransport{}
			if _, _, err := scriptedForge(tt, nil).do(context.Background(), http.MethodPost, "/x", tc.body, nil); err != nil {
				t.Fatal(err)
			}
			req := tt.Requests[0]
			var got []byte
			if req.Body != nil {
				got, _ = io.ReadAll(req.Body)
			}
			if string(got) != tc.want {
				t.Errorf("body = %q, want %q", got, tc.want)
			}
			if ct := req.Header.Get("Content-Type"); (ct == "application/json") != (tc.want != "") {
				t.Errorf("Content-Type = %q with body %q", ct, got)
			}
		})
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("disk on fire") }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

func TestForgeHTTP_ReportsWhatWentWrong(t *testing.T) {
	ok := func(body string) *http.Response {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}
	}
	for _, tc := range []struct {
		name    string
		h       func() forgeHTTP
		method  string
		path    string
		body    any
		target  any
		wantErr string
	}{
		{"an unreadable request body", func() forgeHTTP { return scriptedForge(&scriptedTransport{}, nil) },
			http.MethodPost, "/x", failingReader{}, nil, "reading request body: disk on fire"},
		{"a body JSON cannot encode", func() forgeHTTP { return scriptedForge(&scriptedTransport{}, nil) },
			http.MethodPost, "/x", make(chan int), nil, "encoding request body"},
		{"an invalid method", func() forgeHTTP { return scriptedForge(&scriptedTransport{}, nil) },
			"BAD METHOD", "/x", nil, nil, "invalid method"},
		{"a failed connection", func() forgeHTTP {
			h := scriptedForge(&scriptedTransport{}, nil)
			h.client = &http.Client{Transport: failingTransport{}}
			return h
		}, http.MethodGet, "/x", nil, nil, "connection refused"},
		{"a response that is not JSON", func() forgeHTTP {
			return scriptedForge(&scriptedTransport{Responses: []*http.Response{ok("<html>")}}, nil)
		}, http.MethodGet, "/x", nil, &map[string]any{}, "decoding response"},
		{"a writer that fails", func() forgeHTTP {
			return scriptedForge(&scriptedTransport{Responses: []*http.Response{ok("data")}}, nil)
		}, http.MethodGet, "/x", nil, failingWriter{}, "writing response: disk full"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.h().do(context.Background(), tc.method, tc.path, tc.body, tc.target)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want it to say %q", err, tc.wantErr)
			}
		})
	}
}

// A 2xx body is decoded into the target, or copied to it when it is a
// writer, and the body read is bounded to 8 MB either way.
func TestForgeHTTP_DeliversTheResponseBody(t *testing.T) {
	tt := &scriptedTransport{Responses: []*http.Response{
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"number": 7}`))},
		{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(bytes.Repeat([]byte{'x'}, 9<<20)))},
	}}
	h := scriptedForge(tt, nil)
	var issue struct{ Number int }
	if _, raw, err := h.do(context.Background(), http.MethodGet, "/x", nil, &issue); err != nil || issue.Number != 7 || string(raw) != `{"number": 7}` {
		t.Errorf("decoded %+v from %q, %v", issue, raw, err)
	}
	var buf bytes.Buffer
	if _, raw, err := h.do(context.Background(), http.MethodGet, "/x", nil, &buf); err != nil || buf.Len() != MaxResponseBodyBytes || len(raw) != MaxResponseBodyBytes {
		t.Errorf("wrote %d bytes and returned %d, %v; want %d", buf.Len(), len(raw), err, MaxResponseBodyBytes)
	}
}

// A path is joined to the base URL whether or not it starts with a slash; a
// full URL (a pagination link) is used as it is.
func TestForgeHTTP_ResolvesThePath(t *testing.T) {
	for path, want := range map[string]string{
		"/repos/o/r":                     "https://api.example.com/repos/o/r",
		"repos/o/r":                      "https://api.example.com/repos/o/r",
		"https://other.example.com/next": "https://other.example.com/next",
		"http://other.example.com/next":  "http://other.example.com/next",
	} {
		tt := &scriptedTransport{}
		h := scriptedForge(tt, nil)
		h.baseURL += "/"
		if _, _, err := h.do(context.Background(), http.MethodGet, path, nil, nil); err != nil {
			t.Fatal(err)
		}
		if got := tt.Requests[0].URL.String(); got != want {
			t.Errorf("%s: requested %s, want %s", path, got, want)
		}
	}
}
