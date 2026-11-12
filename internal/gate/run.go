package gate

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"time"

	"github.com/agent-fox-dev/spec/envelope"
)

// The names of the gate's two commands, in the order they run.
// AffectedTests runs in place of AllTests at the task level of a package
// that names it.
const (
	Linter        = "linter"
	AllTests      = "all_tests"
	AffectedTests = "affected_tests"
)

// DefaultTimeout is how long one command may run when the caller sets no
// limit: below the 30 minutes a driver waits for an event, so that a hung test
// ends the gate and not the session.
const DefaultTimeout = 20 * time.Minute

// waitDelay is how long Wait waits, after a command has exited or been ended,
// for its output to close: a process it left behind may hold it open.
const waitDelay = 2 * time.Second

// Command is one command of a gate: its name and its command string, empty
// when the package names none.
type Command struct {
	Name string
	Line string
}

// Runner runs the commands of a gate.
type Runner struct {
	// Dir is the directory every command runs in.
	Dir string
	// Env is the commands' environment; nil inherits the caller's.
	Env []string
	// Timeout is how long one command may run; zero is DefaultTimeout.
	Timeout time.Duration
	// Now is the clock; nil is time.Now. A test replaces it.
	Now func() time.Time
	// LogDir is the existing directory each command's log is written to; with
	// none, or where a log cannot be created, the command runs without one.
	LogDir string
	// Units names the command whose output is read for its slowest units, the
	// all_tests of a full gate; empty reads none.
	Units string
}

// Run runs the commands in order and stops at the first that does not exit 0:
// a command that cannot start makes the verdict not_run, one that exits with
// another code or runs past the timeout makes it fail. The result holds an
// entry for every command that started, with its seconds, and the gate's total
// as their sum, and the slowest units of the Units command when it ran; its
// level and clean_env are the caller's to set.
func (r *Runner) Run(cmds []Command) envelope.GateResult {
	g := envelope.GateResult{Verdict: envelope.GateVerdictPass, Commands: []envelope.GateCommand{}}
	for _, c := range cmds {
		var u *units
		if c.Name == r.Units {
			u = &units{}
		}
		entry, reason := r.runOne(c, u)
		if u != nil && reason == "" {
			g.Slowest = u.Slowest()
		}
		if reason != "" {
			g.Verdict, g.Reason = envelope.GateVerdictNotRun, reason
			break
		}
		g.Commands = append(g.Commands, entry)
		g.Seconds = roundMillis(g.Seconds + entry.Seconds)
		if entry.TimedOut || *entry.ExitCode != 0 {
			g.Verdict = envelope.GateVerdictFail
			break
		}
	}
	return g
}

// runOne runs one command. A command that cannot start returns the reason and
// no entry.
func (r *Runner) runOne(c Command, u *units) (envelope.GateCommand, string) {
	entry := envelope.GateCommand{Name: c.Name}
	if c.Line == "" {
		return entry, fmt.Sprintf("the package's test_commands name no %s command", c.Name)
	}
	argv, err := Split(c.Line)
	if err != nil {
		return entry, fmt.Sprintf("the %s command %q cannot be split: %v", c.Name, c.Line, err)
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir, cmd.Env, cmd.WaitDelay = r.Dir, r.Env, waitDelay
	isolate(cmd)
	// None of a command's output reaches the caller's stdout: it goes to the
	// command's log, and its last lines are kept for a failure.
	out := &sink{units: u}
	if r.LogDir != "" {
		pattern := time.Now().UTC().Format("20060102T150405Z") + "-" + c.Name + "-*.log"
		if f, err := os.CreateTemp(r.LogDir, pattern); err == nil {
			defer f.Close()
			out.log, entry.Log = f, f.Name()
		}
	}
	cmd.Stdout, cmd.Stderr = out, out

	now := r.Now
	if now == nil {
		now = time.Now
	}
	start := now()
	if err := cmd.Start(); err != nil {
		if out.log != nil {
			_ = os.Remove(out.log.Name())
		}
		return entry, fmt.Sprintf("the %s command %q could not start: %v", c.Name, c.Line, err)
	}
	err = cmd.Wait()
	entry.Seconds = roundMillis(now().Sub(start).Seconds())
	// What the command left running dies with it: the gate is over.
	endGroup(cmd)

	var exit *exec.ExitError
	switch {
	case err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
		entry.TimedOut = true
	case err == nil:
		entry.ExitCode = new(int)
	case errors.As(err, &exit):
		code := exit.ExitCode()
		entry.ExitCode = &code
	case errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil:
		// The command exited, and a process it left behind held its output
		// open past waitDelay: its exit code is the answer.
		code := cmd.ProcessState.ExitCode()
		entry.ExitCode = &code
	default:
		// The command ran, but waiting for it failed (its output could not
		// be copied, or did not close in time): it did not exit cleanly.
		code := -1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		if code == 0 {
			code = -1
		}
		entry.ExitCode = &code
	}
	if entry.TimedOut || *entry.ExitCode != 0 {
		tail := out.tail.String()
		entry.Tail = &tail
	}
	return entry, ""
}

// roundMillis rounds seconds to the millisecond.
func roundMillis(s float64) float64 { return math.Round(s*1000) / 1000 }
