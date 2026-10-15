package agentrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/provider/anthropic"
	"github.com/agent-fox-dev/agentkit-go/provider/faux"
	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// timedOut is the error a read on a dead connection ends with.
var timedOut = &net.OpError{Op: "read", Net: "tcp", Err: syscall.ETIMEDOUT}

// details records the tracing a phase writes under --verbose.
type details struct {
	mu    sync.Mutex
	lines []string
}

func (d *details) Detail(format string, args ...any) {
	d.mu.Lock()
	d.lines = append(d.lines, fmt.Sprintf(format, args...))
	d.mu.Unlock()
}
func (d *details) Raw(string)                                           {}
func (d *details) PhaseStart(string, string, int, float64)              {}
func (d *details) PhaseEnd(string, string, int, float64, time.Duration) {}
func (d *details) Turn(string, int, float64)                            {}

func (d *details) text() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return strings.Join(d.lines, "\n")
}

func failedTurn(err error, text string) faux.Turn {
	return faux.Turn{Blocks: []core.ContentBlock{faux.FauxText(text)},
		Usage: core.Usage{InputTokens: 1000, Requests: 1}, Err: err}
}

func answerPhase(got *string) Phase {
	return Phase{Name: "answer", System: "Answer.", User: "Go.", Terminator: terminator,
		Custom: []core.Tool{submitTool(got)}}
}

// A connection that drops twenty turns into a phase used to end the phase,
// and with it the run: the SDK retries a request that fails before its
// response starts, and nothing retried one that failed after.
func TestACallThatFailsMidStreamIsSentAgain(t *testing.T) {
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

func TestACallIsSentAgainAtMostOncePerDelay(t *testing.T) {
	p := faux.New(failedTurn(timedOut, ""), failedTurn(timedOut, ""), failedTurn(timedOut, ""))
	var got string
	r := newRunner(t, p, Config{RetryDelays: []time.Duration{0, 0}})
	_, err := r.Run(context.Background(), answerPhase(&got))
	if CategoryOf(err) != CategoryAPI || !strings.Contains(fmt.Sprint(err), "timed out") {
		t.Errorf("err = %v (%s), want the api failure", err, CategoryOf(err))
	}
	if p.Calls() != 3 {
		t.Errorf("calls = %d, want 3: the first and two repeats", p.Calls())
	}
}

func TestAFailureARepeatCannotFixIsNotRepeated(t *testing.T) {
	bad := &anthropic.StatusError{Code: http.StatusBadRequest, Text: "anthropic: HTTP 400 invalid_request_error"}
	p := faux.New(failedTurn(bad, ""), call("1", terminator, `{"answer":"right"}`))
	var got string
	r := newRunner(t, p, Config{RetryDelays: []time.Duration{0, 0}})
	_, err := r.Run(context.Background(), answerPhase(&got))
	if CategoryOf(err) != CategoryAPI || p.Calls() != 1 {
		t.Errorf("err = %v after %d calls, want the api failure after 1", err, p.Calls())
	}
}

func TestNoDelaysMeansNoRepeats(t *testing.T) {
	p := faux.New(failedTurn(timedOut, ""), call("1", terminator, `{"answer":"right"}`))
	var got string
	r := newRunner(t, p, Config{RetryDelays: []time.Duration{}})
	if _, err := r.Run(context.Background(), answerPhase(&got)); err == nil || p.Calls() != 1 {
		t.Errorf("err = %v after %d calls, want a failure after 1", err, p.Calls())
	}
}

// A run cancelled while a repeat waits is an abort, not the failure the
// repeat was for.
func TestACancelDuringTheWaitIsAnAbort(t *testing.T) {
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

// The failure that stopped the run in the field, through the real provider: a
// 200 whose body stops with a read timeout after the message has begun.
func TestADroppedConnectionThroughTheSDKIsSentAgain(t *testing.T) {
	var calls int
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var body io.Reader = strings.NewReader(sse(messageStart, toolUse("1", terminator, `{"answer":"right"}`), messageEnd))
		if calls == 1 {
			body = io.MultiReader(strings.NewReader(sse(messageStart)), errReader{timedOut})
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK",
			Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(body)}, nil
	})
	c := sdk.NewClient(option.WithAPIKey("test"), option.WithHTTPClient(&http.Client{Transport: rt}), option.WithMaxRetries(0))
	var got string
	r, err := NewRunner(Config{Model: listedModel(t), Client: &c, RetryDelays: []time.Duration{0}})
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

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

const (
	messageStart = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":1}}}`
	messageEnd = `event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}`
)

func toolUse(id, name, input string) string {
	return fmt.Sprintf(`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":%q,"name":%q,"input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":%q}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}`, id, name, input)
}

func sse(events ...string) string { return strings.Join(events, "\n\n") + "\n\n" }

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
		if got := transient(tc.err); got != tc.want {
			t.Errorf("transient(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
