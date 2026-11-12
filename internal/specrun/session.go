package specrun

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// sessionArgs is the command line of a session for the package in specDir:
// af-impl, headless, its stream as JSON, every permission granted, and no
// timer that would keep a finished session alive.
func sessionArgs(specDir string) []string {
	return []string{"-p", "/af-impl " + specDir, "--output-format", "stream-json", "--verbose",
		"--permission-mode", "bypassPermissions", "--disallowedTools", "ScheduleWakeup,CronCreate"}
}

// session is one claude -p process.
type session struct {
	dir    string        // the repository
	stall  time.Duration // how long it may write nothing
	stream io.Writer     // where every line of its stdout goes
}

// outcome is how a session ended.
type outcome struct {
	stalled bool
	final   string // the result text of its last result event
}

// run starts the session for the package in specDir and waits for it to end.
// Every line it writes goes to the stream as it arrives; one that writes
// nothing for the stall time is ended with every process it started. Its exit
// code decides nothing: the landing checks do.
func (s *session) run(specDir string) (outcome, error) {
	var o outcome
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", sessionArgs(specDir)...)
	cmd.Dir = s.dir
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS="+strconv.FormatInt(s.stall.Milliseconds(), 10))
	cmd.Stderr = io.Discard
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
				Type   string `json:"type"`
				Result string `json:"result"`
			}
			if json.Unmarshal([]byte(line), &ev) == nil && ev.Type == "result" {
				o.final = ev.Result
			}
		case <-timer.C:
			o.stalled = true
			_ = endGroup(cmd)
			for range lines {
			}
			break read
		}
	}
	_ = cmd.Wait()
	_ = endGroup(cmd)
	return o, nil
}
