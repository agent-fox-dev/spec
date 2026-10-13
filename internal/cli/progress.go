package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/agent-fox-dev/spec/internal/agentrun"
)

// Progress writes human-readable progress to stderr.
//
// It is on stderr and never stdout, without an option to change that: stdout
// carries exactly one JSON object, and a tool that interleaves progress into
// it is a tool whose output cannot be parsed. A caller that wants silence
// passes --quiet; a caller that wants detail passes --verbose.
type Progress struct {
	mu       sync.Mutex
	w        io.Writer
	tool     string
	verbose  bool
	quiet    bool
	showText bool
	spin     *spinner
	// spinOwner is the step whose line the spinner sits on.
	spinOwner *begun
}

// begun is one step Begin started. inline is true while its done line can
// still finish the line Begin started; anything printed in between ends that
// line, and done then prints a whole line of its own. Steps of a split's
// scopes overlap, so a step can never assume the terminal's last line is its
// own.
type begun struct {
	label  string
	inline bool
}

// Progress is the phase runner's observer.
var _ agentrun.Observer = (*Progress)(nil)

// NewProgress returns a Progress writing to w, prefixing each line with the
// tool's name. A nil w means os.Stderr.
func NewProgress(w io.Writer, tool string, verbose, quiet, showText bool) *Progress {
	if w == nil {
		w = os.Stderr
	}
	return &Progress{w: w, tool: tool, verbose: verbose, quiet: quiet, showText: showText}
}

// Step prints one line of progress. stage names the pipeline step; it is not
// shown, and is kept so a call site reads the way the envelope names stages.
func (p *Progress) Step(stage, format string, args ...any) {
	if p == nil || p.quiet {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopSpinnerLocked()
	fmt.Fprintf(p.w, "[%s] %s\n", p.tool, strings.TrimSpace(fmt.Sprintf(format, args...)))
}

// Detail prints one line only under --verbose. It is indented, because a
// detail line belongs to the step above it.
func (p *Progress) Detail(format string, args ...any) {
	if p == nil || !p.verbose || p.quiet {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopSpinnerLocked()
	fmt.Fprintf(p.w, "  %s\n", strings.TrimSpace(fmt.Sprintf(format, args...)))
}

// Raw writes the model's prose with no prefix, only under --show-text.
func (p *Progress) Raw(s string) {
	if p == nil || s == "" || !p.showText || p.quiet {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopSpinnerLocked()
	fmt.Fprint(p.w, s)
}

// Begin starts a step that will take a while, returning the function that
// ends it. On a terminal, without --verbose, a spinner runs so an operator can
// tell a ten-minute model call from a hang; only one step owns the spinner at
// a time.
func (p *Progress) Begin(format string, args ...any) func(summary string) {
	if p == nil || p.quiet {
		return func(string) {}
	}
	b := &begun{label: strings.TrimSpace(fmt.Sprintf(format, args...))}
	start := time.Now()

	p.mu.Lock()
	p.stopSpinnerLocked()
	if !p.verbose && !p.showText && isTerminal(p.w) {
		fmt.Fprintf(p.w, "[%s] %s ", p.tool, b.label)
		b.inline = true
		p.spin, p.spinOwner = startSpinner(p.w, &p.mu), b
	} else {
		fmt.Fprintf(p.w, "[%s] %s\n", p.tool, b.label)
	}
	p.mu.Unlock()

	return func(summary string) {
		p.mu.Lock()
		defer p.mu.Unlock()
		line := fmt.Sprintf("(%s)", FormatDuration(time.Since(start)))
		if summary != "" {
			line += " " + summary
		}
		if p.spinOwner == b {
			p.spin.stopLocked()
			p.spin, p.spinOwner = nil, nil
		} else {
			p.stopSpinnerLocked()
		}
		if b.inline {
			fmt.Fprintf(p.w, "%s\n", line)
			return
		}
		fmt.Fprintf(p.w, "[%s] %s done %s\n", p.tool, b.label, line)
	}
}

// PhaseStart traces the model and the ceilings a phase runs under, under
// --verbose.
func (p *Progress) PhaseStart(phase, model string, maxTurns int, budgetUSD float64) {
	p.Detail("%s: %s, at most %d turns, $%.2f", phase, model, maxTurns, budgetUSD)
}

// PhaseEnd traces how a phase ended, under --verbose.
func (p *Progress) PhaseEnd(phase, stopReason string, turns int, costUSD float64, elapsed time.Duration) {
	p.Detail("%s: %s after %d turns, $%.2f, %s", phase, stopReason, turns, costUSD, FormatDuration(elapsed))
}

// Turn traces one finished model turn, under --verbose.
func (p *Progress) Turn(phase string, turn int, costUSD float64) {
	p.Detail("%s: turn %d, $%.2f so far", phase, turn, costUSD)
}

// stopSpinnerLocked stops the spinner, ends the line it sat on, and tells
// its owner that its done line must be a whole line.
func (p *Progress) stopSpinnerLocked() {
	if p.spin == nil {
		return
	}
	p.spin.stopLocked()
	p.spin = nil
	if p.spinOwner != nil {
		p.spinOwner.inline = false
		p.spinOwner = nil
		fmt.Fprintln(p.w)
	}
}

// FormatDuration renders a duration the way a run summary should read:
// sub-second precision below a second, whole units above it.
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	var parts []string
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%dh", h))
	}
	if m > 0 {
		parts = append(parts, fmt.Sprintf("%dm", m))
	}
	if s > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", s))
	}
	return strings.Join(parts, " ")
}

// FormatTokens abbreviates a token count for a one-line summary.
func FormatTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// ------------------------------------------------------------- spinner --

var spinnerFrames = []byte{'|', '/', '-', '\\'}

// spinner animates one character in place. It shares the Progress mutex so a
// line printed from another goroutine cannot land in the middle of a frame.
type spinner struct {
	w       io.Writer
	mu      *sync.Mutex
	ticker  *time.Ticker
	done    chan struct{}
	stopped chan struct{}
}

func startSpinner(w io.Writer, mu *sync.Mutex) *spinner {
	s := &spinner{
		w:       w,
		mu:      mu,
		ticker:  time.NewTicker(100 * time.Millisecond),
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	_, _ = s.w.Write([]byte{spinnerFrames[0]}) // caller holds mu
	go s.run()
	return s
}

func (s *spinner) run() {
	defer close(s.stopped)
	frame := 1
	for {
		select {
		case <-s.done:
			return
		case <-s.ticker.C:
			s.mu.Lock()
			_, _ = s.w.Write([]byte{'\b', spinnerFrames[frame]})
			s.mu.Unlock()
			frame = (frame + 1) % len(spinnerFrames)
		}
	}
}

// stopLocked stops the animation and erases the frame. The caller holds the
// mutex, so the goroutine's own Lock is what makes this wait for it.
func (s *spinner) stopLocked() {
	s.ticker.Stop()
	close(s.done)
	s.mu.Unlock()
	<-s.stopped
	s.mu.Lock()
	_, _ = s.w.Write([]byte{'\b', ' ', '\b'})
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}
