package agentrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/provider/anthropic"
	"github.com/agent-fox-dev/agentkit-go/provider/faux"
)

// A connection that drops twenty turns into a phase used to end the phase,
// and with it the run: the SDK retries a request that fails before its
// response starts, and nothing retried one that failed after.
func TestRetry_ACallThatFailsMidStreamIsSentAgain(t *testing.T) {
	p := faux.New(
		call("1", terminator, `{"answer":"wrong"}`),
		failedTurn(timedOut, "half an ans"),
		call("2", terminator, `{"answer":"right"}`),
	)
	var got string
	obs := &details{}
	r := newRunner(t, p, Config{RetryDelays: []time.Duration{0, 0}, Observer: obs})
	res, err := r.Run(context.Background(), answerPhase(&got))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "right" || res.Turns != 2 || p.Calls() != 3 {
		t.Errorf("got %q after %d turns and %d calls, want right after 2 turns and 3 calls", got, res.Turns, p.Calls())
	}
	// The failed attempt's input was billed, and is counted.
	if res.Usage.InputTokens < 1000 {
		t.Errorf("usage = %+v, want the failed attempt's 1000 input tokens counted", res.Usage)
	}
	if !strings.Contains(obs.text(), "sending the request again") {
		t.Errorf("the repeat was not traced:\n%s", obs.text())
	}
}

// A call is repeated at most once per delay, only for a failure a repeat can
// get past, and not at all with no delays; the failure that remains is the
// phase's api error.
func TestRetry_RepeatsOnlyWhatARepeatCanFix(t *testing.T) {
	bad := &anthropic.StatusError{Code: http.StatusBadRequest, Text: "anthropic: HTTP 400 invalid_request_error"}
	for _, tc := range []struct {
		name   string
		turns  []faux.Turn
		delays []time.Duration
		calls  int
		cause  string
	}{
		{"once per delay", []faux.Turn{failedTurn(timedOut, ""), failedTurn(timedOut, ""), failedTurn(timedOut, "")},
			[]time.Duration{0, 0}, 3, "timed out"},
		{"not a failure a repeat can fix", []faux.Turn{failedTurn(bad, ""), call("1", terminator, `{"answer":"right"}`)},
			[]time.Duration{0, 0}, 1, "invalid_request_error"},
		{"no delays", []faux.Turn{failedTurn(timedOut, ""), call("1", terminator, `{"answer":"right"}`)},
			[]time.Duration{}, 1, "timed out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := faux.New(tc.turns...)
			var got string
			r := newRunner(t, p, Config{RetryDelays: tc.delays})
			_, err := r.Run(context.Background(), answerPhase(&got))
			if CategoryOf(err) != CategoryAPI || !strings.Contains(fmt.Sprint(err), tc.cause) {
				t.Errorf("err = %v (%s), want the api failure naming %q", err, CategoryOf(err), tc.cause)
			}
			if p.Calls() != tc.calls {
				t.Errorf("calls = %d, want %d", p.Calls(), tc.calls)
			}
		})
	}
}

// cancelOnRepeat cancels the run when a repeat is announced.
type cancelOnRepeat struct {
	details
	cancel context.CancelFunc
}

func (c *cancelOnRepeat) Detail(format string, args ...any) {
	if strings.Contains(fmt.Sprintf(format, args...), "sending the request again") {
		c.cancel()
	}
}

// A run cancelled while a repeat waits is an abort, not the failure the
// repeat was for.
func TestRetry_ACancelDuringTheWaitIsAnAbort(t *testing.T) {
	p := faux.New(failedTurn(timedOut, ""), call("1", terminator, `{"answer":"right"}`))
	var got string
	ctx, cancel := context.WithCancel(context.Background())
	obs := &cancelOnRepeat{cancel: cancel}
	r := newRunner(t, p, Config{RetryDelays: []time.Duration{time.Minute}, Observer: obs})
	_, err := r.Run(ctx, answerPhase(&got))
	if CategoryOf(err) != CategoryAborted || p.Calls() != 1 {
		t.Errorf("err = %v (%s) after %d calls, want aborted after 1", err, CategoryOf(err), p.Calls())
	}
}

// The failure that stopped the run in the field, through the real provider: a
// 200 whose body stops with a read timeout after the message has begun.
func TestRetry_ADroppedConnectionThroughTheSDKIsSentAgain(t *testing.T) {
	var calls int
	client := sdkClient(func(r *http.Request) (*http.Response, error) {
		calls++
		var body io.Reader = strings.NewReader(sse(messageStart, toolUse("1", terminator, `{"answer":"right"}`), messageEnd))
		if calls == 1 {
			body = io.MultiReader(strings.NewReader(sse(messageStart)), errReader{timedOut})
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK",
			Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(body)}, nil
	})
	var got string
	r, err := NewRunner(Config{Model: listedModel(t), Client: client, RetryDelays: []time.Duration{0}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(context.Background(), answerPhase(&got)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "right" || calls != 2 {
		t.Errorf("got %q after %d requests, want right after 2", got, calls)
	}
}

func TestTransient(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{timedOut, true},
		{fmt.Errorf("anthropic: %w", timedOut), true},
		{io.ErrUnexpectedEOF, true},
		{anthropic.ErrStreamTruncated, true},
		{errors.New(`received error while streaming: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`), true},
		{&anthropic.StatusError{Code: 529}, true},
		{&anthropic.StatusError{Code: 429}, true},
		{&anthropic.StatusError{Code: 400}, false},
		{&anthropic.StatusError{Code: 401}, false},
		{context.Canceled, false},
		{core.ErrAborted, false},
		{errors.New("anthropic: building request: bad schema"), false},
	} {
		t.Run(fmt.Sprint(tc.err), func(t *testing.T) {
			if got := transient(tc.err); got != tc.want {
				t.Errorf("transient(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
