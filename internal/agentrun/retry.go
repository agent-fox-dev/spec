package agentrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/provider/anthropic"
)

// DefaultRetryDelays are the waits before each repeat of a model call that
// failed on the way: three repeats, about forty seconds in all.
//
// The SDK retries a request that fails before its response starts. Nothing
// below this package retries one that fails after: AgentKit's loop ends the
// run on the first failed turn, so one dropped connection twenty turns into a
// phase threw the phase away, and with it every package of a split still
// being generated on the same connection.
var DefaultRetryDelays = []time.Duration{2 * time.Second, 8 * time.Second, 30 * time.Second}

// retrying is a provider that sends a request again when its stream failed for
// a reason a second attempt can get past: a dropped or stalled connection, a
// truncated body, an overloaded or rate-limited API.
//
// The events of a failed attempt are forwarded as they arrive, except the two
// that end it, the terminal error and the failed message, which are held back
// until the attempt is known to be the last. A repeat is announced with a
// non-terminal ErrorEvent. The message that ends the stream carries the usage
// of the failed attempts too, since their input was billed.
type retrying struct {
	inner  core.ProviderClient
	delays []time.Duration
}

func (p *retrying) Stream(ctx context.Context, req core.Request) (<-chan core.StreamEvent, error) {
	ch, err := p.inner.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan core.StreamEvent, 16)
	go func() {
		defer close(out)
		var spent core.Usage // the failed attempts'
		for attempt := 0; ; attempt++ {
			held, end, failure := forward(ch, out)
			if failure == nil || attempt >= len(p.delays) || ctx.Err() != nil || !transient(failure) {
				flush(out, held, end, failure, spent)
				return
			}
			if end != nil {
				spent = spent.Add(end.Usage)
			}
			wait := p.delays[attempt]
			out <- core.StreamEvent{Event: core.ErrorEvent{
				Message: fmt.Sprintf("%v; sending the request again in %s (repeat %d of %d)",
					failure, wait, attempt+1, len(p.delays)),
				Err: failure,
			}}
			if !sleep(ctx, wait) {
				flush(out, held, aborted(end), ctx.Err(), spent)
				return
			}
			next, err := p.inner.Stream(ctx, req)
			if err != nil {
				flush(out, held, end, failure, spent)
				return
			}
			ch = next
		}
	}()
	return out, nil
}

// forward copies one attempt's events to out, holding back what ends it: the
// terminal ErrorEvent and the MessageEndEvent. It returns them, the final
// message, and the attempt's error.
func forward(ch <-chan core.StreamEvent, out chan<- core.StreamEvent) (held []core.Event, end *core.AssistantMessage, failure error) {
	for item := range ch {
		switch v := item.Event.(type) {
		case nil:
		case core.MessageEndEvent:
			m := v.Message
			end = &m
			held = append(held, v)
		case core.ErrorEvent:
			if v.Terminal {
				held = append(held, v)
				break
			}
			out <- core.StreamEvent{Event: v}
		default:
			out <- core.StreamEvent{Event: v}
		}
		if item.Err != nil {
			failure = item.Err
		}
	}
	return held, end, failure
}

// flush ends the stream with the last attempt's held events and error, the
// failed attempts' usage added to its message.
func flush(out chan<- core.StreamEvent, held []core.Event, end *core.AssistantMessage, failure error, spent core.Usage) {
	for _, e := range held {
		if _, ok := e.(core.MessageEndEvent); ok && end != nil {
			m := *end
			m.Usage = spent.Add(m.Usage)
			e = core.MessageEndEvent{Message: m}
		}
		out <- core.StreamEvent{Event: e}
	}
	if failure != nil {
		out <- core.StreamEvent{Err: failure}
	}
}

// aborted is the failed message as an abort, for a caller that gave up while
// a repeat was waiting: the loop then reports the cancellation or the
// timeout, not the failure the repeat was for.
func aborted(end *core.AssistantMessage) *core.AssistantMessage {
	if end == nil {
		return nil
	}
	m := *end
	m.StopReason = core.StopReasonAborted
	m.ErrorMessage = anthropic.AbortText
	return &m
}

func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// transient reports whether a failed stream is worth sending again. A
// refusal, an invalid request or a credential problem fails the same way the
// second time; a connection, an overloaded server or a rate limit may not.
func transient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, core.ErrAborted) {
		return false
	}
	var status *anthropic.StatusError
	if errors.As(err, &status) {
		switch {
		case status.Code == http.StatusRequestTimeout, status.Code == http.StatusConflict,
			status.Code == http.StatusTooManyRequests, status.Code >= 500:
			return true
		}
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) ||
		errors.Is(err, anthropic.ErrStreamTruncated) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ETIMEDOUT) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	// An error event in the middle of a 200 stream reaches here as text: the
	// SDK renders it "received error while streaming: {…}". A transport error
	// wrapped without %w on the way does too.
	text := err.Error()
	for _, s := range transientText {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

var transientText = []string{
	"overloaded_error", "api_error", "rate_limit_error", "timeout_error",
	"connection reset", "broken pipe", "operation timed out", "i/o timeout", "unexpected EOF",
	"http2: ",
}
