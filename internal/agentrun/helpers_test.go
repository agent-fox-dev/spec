package agentrun

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/agent-fox-dev/agentkit-go/catalog"
	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/provider/faux"
	"github.com/agent-fox-dev/agentkit-go/schema"
	"github.com/agent-fox-dev/agentkit-go/workspace"
	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const terminator = "submit_answer"

// submitTool accepts any answer but "wrong", which it refuses so the model
// has to try again.
func submitTool(got *string) core.Tool {
	return core.Tool{
		Name:        terminator,
		Description: "Submit the answer.",
		InputSchema: schema.Object(schema.Prop("answer", schema.String("The answer."))),
		Terminating: true,
		Execute: func(_ context.Context, in json.RawMessage) core.ToolResult {
			var p struct {
				Answer string `json:"answer"`
			}
			_ = json.Unmarshal(in, &p)
			if p.Answer == "wrong" {
				return core.ErrResult("rejected", "the answer is wrong")
			}
			*got = p.Answer
			res := core.OKResult(map[string]any{"accepted": true})
			res.Terminate = true
			return res
		},
	}
}

// answerPhase is a phase whose only tool is submitTool.
func answerPhase(got *string) Phase {
	return Phase{Name: "answer", System: "Answer.", User: "Go.", Terminator: terminator,
		Custom: []core.Tool{submitTool(got)}}
}

// call is a model turn that calls one tool.
func call(id, name, args string) faux.Turn {
	return faux.Turn{Blocks: []core.ContentBlock{faux.FauxToolCall(id, name, args)}, StopReason: core.StopReasonToolUse}
}

// timedOut is the error a read on a dead connection ends with.
var timedOut = &net.OpError{Op: "read", Net: "tcp", Err: syscall.ETIMEDOUT}

// failedTurn is a model turn whose stream fails with err after text, having
// billed its input.
func failedTurn(err error, text string) faux.Turn {
	return faux.Turn{Blocks: []core.ContentBlock{faux.FauxText(text)},
		Usage: core.Usage{InputTokens: 1000, Requests: 1}, Err: err}
}

func listedModel(t *testing.T) Model {
	t.Helper()
	row, ok := catalog.Lookup("claude-opus-5-5")
	if !ok {
		t.Fatal("claude-opus-5-5 is not in the catalog")
	}
	return Model{Spec: "claude-opus-5-5", Row: row}
}

// newRunner is a Runner on the faux provider p, with a listed model and an
// empty workspace unless cfg sets them.
func newRunner(t *testing.T, p *faux.Provider, cfg Config) *Runner {
	t.Helper()
	if cfg.Model.ID() == "" {
		cfg.Model = listedModel(t)
	}
	cfg.Provider = p
	if cfg.Workspace == nil {
		cfg.Workspace = openWorkspace(t, t.TempDir())
	}
	r, err := NewRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func openWorkspace(t *testing.T, dir string) *workspace.Workspace {
	t.Helper()
	ws, err := workspace.Open(dir, workspace.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func mustChoice(t *testing.T, raw string) PhaseChoice {
	t.Helper()
	c, err := ParsePhaseChoice(raw)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func systemText(req core.Request) string {
	var b strings.Builder
	for _, blk := range req.System {
		if tb, ok := blk.(core.TextBlock); ok {
			b.WriteString(tb.Text)
		}
	}
	return b.String()
}

func messageText(m core.Message) string {
	var b strings.Builder
	if um, ok := m.(core.UserMessage); ok {
		for _, blk := range um.Content {
			if tb, ok := blk.(core.TextBlock); ok {
				b.WriteString(tb.Text)
			}
		}
	}
	return b.String()
}

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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// sdkClient is an SDK client that sends every request through rt, without
// retries of its own.
func sdkClient(rt roundTripFunc) *sdk.Client {
	c := sdk.NewClient(option.WithAPIKey("test"), option.WithHTTPClient(&http.Client{Transport: rt}), option.WithMaxRetries(0))
	return &c
}

// capturingClient is an SDK client whose transport records the body of the
// request and answers it with an API error, so a phase ends after one request.
func capturingClient(body *[]byte) *sdk.Client {
	return sdkClient(func(r *http.Request) (*http.Response, error) {
		*body, _ = io.ReadAll(r.Body)
		return &http.Response{
			StatusCode: http.StatusBadRequest, Status: "400 Bad Request", Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"invalid_request_error","message":"stop"}}`)),
		}, nil
	})
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// The server-sent events of a streamed message.
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
