package specrun

import (
	"bufio"
	"context"
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
	dir   string        // the repository
	stall time.Duration // how long it may write nothing
	lines func(string)  // called with each line of its stdout, as it arrives
}

// run starts the session for the package in specDir and waits for it to end.
// Its exit code decides nothing; the landing checks do.
func (s *session) run(specDir string) error {
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
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("claude could not start: %w", err)
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for sc.Scan() {
		if s.lines != nil {
			s.lines(sc.Text())
		}
	}
	_ = cmd.Wait()
	_ = endGroup(cmd)
	return nil
}
