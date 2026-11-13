package specrun

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

// sessionArgs is the command line of a session with the prompt: headless,
// its stream as JSON, every permission granted, and no timer that would keep
// a finished session alive.
func sessionArgs(prompt string) []string {
	return []string{"-p", prompt, "--output-format", "stream-json", "--verbose",
		"--permission-mode", "bypassPermissions", "--disallowedTools", "ScheduleWakeup,CronCreate"}
}

// session is one claude -p process.
type session struct {
	host   host          // whose PATH finds claude, and whose environment it inherits
	runID  string        // the run it is a step of, given to it as SPECRUN_RUN_ID
	dir    string        // the repository
	stall  time.Duration // how long it may write nothing
	stream io.Writer     // where every line of its stdout goes
	errs   io.Writer     // where its stderr goes
}

// outcome is how a session ended.
type outcome struct {
	stalled     bool
	interrupted bool
	final       string // the result text of its last result event
	sessionID   string // the first session_id its stream gave
	failed      bool   // its last result event says it ended in an error
	apiStatus   int    // the HTTP status of the API error it ended with; 0 when none
}

// explain is the reason a step stops, given the reason the repository checks
// found: when the session ended in an error, that error, because the check
// that failed only says where the error left the work. A step whose checks
// all passed keeps "" whatever the session ended with.
func (o outcome) explain(reason string) string {
	if reason == "" || !o.failed {
		return reason
	}
	said := o.final
	if i := strings.IndexByte(said, '\n'); i >= 0 {
		said = said[:i]
	}
	if said = strings.TrimSpace(said); len([]rune(said)) > 200 {
		said = string([]rune(said)[:200]) + "…"
	}
	why := reasonSessionError
	if o.apiStatus != 0 {
		why = fmt.Sprintf("%s (%d)", reasonAPIError, o.apiStatus)
	}
	if said == "" {
		return why
	}
	return why + ": " + said
}

// run starts the session for the package in specDir and waits for it to end.
// Every line it writes goes to the stream as it arrives; one that writes
// nothing for the stall time is ended with every process it started. Its exit
// code decides nothing: the landing checks do, and the error its last result
// event reports, if any, explains a check that failed.
func (s *session) run(ctx context.Context, prompt string) (outcome, error) {
	var o outcome
	claude, err := s.host.lookPath("claude")
	if err != nil {
		return o, fmt.Errorf("claude could not start: %w", err)
	}
	cmd := exec.CommandContext(ctx, claude, sessionArgs(prompt)...)
	cmd.Dir = s.dir
	cmd.Env = append(slices.Clone(s.host.env), "CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS="+strconv.FormatInt(s.stall.Milliseconds(), 10))
	if s.runID != "" {
		// What the session commits or writes can name the run it was part of.
		cmd.Env = append(cmd.Env, "SPECRUN_RUN_ID="+s.runID)
	}
	cmd.Stderr = io.Discard
	if s.errs != nil {
		cmd.Stderr = s.errs
	}
	cmd.WaitDelay = 5 * time.Second
	isolate(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return o, err
	}
	if err := cmd.Start(); err != nil {
		return o, fmt.Errorf("claude could not start: %w", err)
	}
	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	timer := time.NewTimer(s.stall)
	defer timer.Stop()
read:
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				break read
			}
			timer.Reset(s.stall)
			if s.stream != nil {
				_, _ = io.WriteString(s.stream, line+"\n")
			}
			var ev struct {
				Type      string `json:"type"`
				Result    string `json:"result"`
				SessionID string `json:"session_id"`
				IsError   bool   `json:"is_error"`
				APIStatus int    `json:"api_error_status"`
			}
			if json.Unmarshal([]byte(line), &ev) == nil {
				if ev.Type == "result" {
					o.final, o.failed, o.apiStatus = ev.Result, ev.IsError, ev.APIStatus
				}
				if o.sessionID == "" {
					o.sessionID = ev.SessionID
				}
			}
		case <-timer.C:
			o.stalled = true
			s.end(cmd, out, lines)
			break read
		case <-ctx.Done():
			o.interrupted = true
			s.end(cmd, out, lines)
			break read
		}
	}
	_ = cmd.Wait()
	_ = endGroup(cmd)
	return o, nil
}

// end ends the session's process group and stops reading its output: closing
// the read end lets the reader finish even when a process that left the
// group still holds the output open.
func (s *session) end(cmd *exec.Cmd, out io.Closer, lines <-chan string) {
	_ = endGroup(cmd)
	_ = out.Close()
	for range lines {
	}
}
