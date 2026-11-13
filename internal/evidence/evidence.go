// Package evidence runs the tests a spec package maps its criteria to and
// reads each one's status: through the package's evidence_tests command, or,
// in a Go module that names none, through go test itself (ADR 26). It is
// speca's, like the gate, whose command splitting, process handling and
// timeout it shares.
package evidence

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/gate"
)

// Status is what a run found of one test.
type Status string

// The statuses a test can have.
const (
	// Pass is a test that ran and passed.
	Pass Status = "pass"
	// Fail is a test that ran and whose assertion failed.
	Fail Status = "fail"
	// Error is a test that could not be built, collected or started.
	Error Status = "error"
	// Missing is a test that does not exist, or that no result named.
	Missing Status = "missing"
	// Skipped is a test that exists and did not run.
	Skipped Status = "skipped"
)

// statuses are the statuses a result line may carry.
var statuses = map[Status]bool{Pass: true, Fail: true, Error: true, Missing: true, Skipped: true}

// Runner runs a set of tests in a directory.
type Runner struct {
	// Command is the package's evidence_tests command; empty, the tests are
	// run with go test when Dir holds a go.mod.
	Command string
	// Dir is the directory the tests run in.
	Dir string
	// Timeout is how long the whole run may take; zero is
	// gate.DefaultTimeout.
	Timeout time.Duration
	// LogDir is the existing directory the run's log is written to; with
	// none, or where a log cannot be created, the run keeps no log.
	LogDir string
}

// Result is what a run found: a status for every test it was given, and the
// log of its output, empty when none was kept.
type Result struct {
	Status map[string]Status
	Log    string
}

// FailureKind says why a run did not reach its end.
type FailureKind string

// The ways a run fails.
const (
	// NoRunner: the package names no evidence command and the directory is
	// not a Go module.
	NoRunner FailureKind = "no_runner"
	// NotStarted: the program could not be started (not found, say).
	NotStarted FailureKind = "not_started"
	// Unsplittable: the command string cannot be split, or holds no {tests}
	// argument of its own.
	Unsplittable FailureKind = "unsplittable"
	// TimedOut: the run was still going at the timeout, and was ended with
	// every process it started.
	TimedOut FailureKind = "timed_out"
)

// Failure is a run that could not run the tests to their end; no status of
// it is to be trusted.
type Failure struct {
	Kind    FailureKind
	Command string
	Message string
}

func (f *Failure) Error() string { return f.Message }

// Run runs the tests, each once, and returns a status for every one of them.
// A run that cannot reach its end returns a *Failure and no statuses.
func (r Runner) Run(tests []string) (Result, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = gate.DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var log *os.File
	if r.LogDir != "" {
		pattern := time.Now().UTC().Format("20060102T150405Z") + "-evidence-*.log"
		if f, err := os.CreateTemp(r.LogDir, pattern); err == nil {
			defer f.Close()
			log = f
		}
	}
	res := Result{}
	if log != nil {
		res.Log = log.Name()
	}
	var out io.Writer = io.Discard
	if log != nil {
		out = log
	}

	var err error
	switch {
	case r.Command != "":
		res.Status, err = r.runCommand(ctx, tests, out, timeout)
	case isGoModule(r.Dir):
		res.Status, err = r.runGo(ctx, tests, out, timeout)
	default:
		err = &Failure{Kind: NoRunner, Message: fmt.Sprintf("the package names no %s in the test_commands of its %s, and %s holds no go.mod "+
			"for the runner built into speca: nothing can run its tests", "evidence_tests", afspec.TasksFileName, r.Dir)}
	}
	if err != nil {
		return Result{Log: res.Log}, err
	}
	return res, nil
}

// isGoModule reports whether dir holds a go.mod.
func isGoModule(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil && !info.IsDir()
}

// runCommand runs the package's evidence command once over every test and
// reads their statuses from its stdout.
func (r Runner) runCommand(ctx context.Context, tests []string, log io.Writer, limit time.Duration) (map[string]Status, error) {
	argv, err := gate.Split(r.Command)
	if err != nil {
		return nil, &Failure{Kind: Unsplittable, Command: r.Command,
			Message: fmt.Sprintf("the evidence command %q cannot be split: %v", r.Command, err)}
	}
	at := afspec.EvidenceTestsArgument(r.Command)
	if at < 0 {
		return nil, &Failure{Kind: Unsplittable, Command: r.Command,
			Message: fmt.Sprintf("the evidence command %q holds no %s argument of its own, exactly once, to put the tests in",
				r.Command, afspec.EvidenceTestsPlaceholder)}
	}
	full := append(append(append([]string{}, argv[:at]...), tests...), argv[at+1:]...)
	stdout, err := run(ctx, r.Dir, r.Command, full, log, limit)
	if err != nil {
		return nil, err
	}
	return parseResults(stdout, tests), nil
}

// run starts argv in dir with no shell, in a process group of its own, and
// returns its stdout once it has ended, whatever its exit code; stdout and
// stderr also go to log. command names the run in a failure, and limit is the
// time limit ctx carries.
func run(ctx context.Context, dir, command string, argv []string, log io.Writer, limit time.Duration) ([]byte, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir, cmd.WaitDelay = dir, 2*time.Second
	gate.Isolate(cmd)
	var stdout bytes.Buffer
	cmd.Stdout, cmd.Stderr = io.MultiWriter(&stdout, log), log
	if err := cmd.Start(); err != nil {
		return nil, &Failure{Kind: NotStarted, Command: command,
			Message: fmt.Sprintf("the evidence command %q could not start: %v", command, err)}
	}
	err := cmd.Wait()
	gate.EndGroup(cmd)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, &Failure{Kind: TimedOut, Command: command,
			Message: fmt.Sprintf("the evidence command %q timed out: it was still running after %s, the --timeout, and was ended with every process it started",
				command, limit)}
	}
	// Any other end, an exit code that is not 0 included, decides nothing:
	// the result lines are the answer.
	return stdout.Bytes(), nil
}

// resultLine is one line of the evidence command's stdout that reports a test.
type resultLine struct {
	Test   *string `json:"test"`
	Status Status  `json:"status"`
}

// parseResults gives every test asked for the status of its result line: a
// JSON object whose test is the identifier as given and whose status is one
// of the five. Every other line is ignored, and so is a line for a test not
// asked for. A test with no line is missing; one with two lines of different
// status is an error.
func parseResults(stdout []byte, tests []string) map[string]Status {
	asked := make(map[string]bool, len(tests))
	for _, t := range tests {
		asked[t] = true
	}
	seen := map[string]Status{}
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var l resultLine
		if json.Unmarshal(line, &l) != nil || l.Test == nil || !asked[*l.Test] || !statuses[l.Status] {
			continue
		}
		if prior, ok := seen[*l.Test]; ok && prior != l.Status {
			seen[*l.Test] = Error
			continue
		}
		seen[*l.Test] = l.Status
	}
	out := make(map[string]Status, len(tests))
	for _, t := range tests {
		if s, ok := seen[t]; ok {
			out[t] = s
		} else {
			out[t] = Missing
		}
	}
	return out
}

// splitIdentifier cuts a Go test identifier, <package>:<Test>[/<subtest>...],
// into the package and the test name, false when it has no package or no
// test.
func splitIdentifier(id string) (pkg, test string, ok bool) {
	pkg, test, found := strings.Cut(id, ":")
	if !found || pkg == "" || test == "" {
		return "", "", false
	}
	return pkg, test, true
}
