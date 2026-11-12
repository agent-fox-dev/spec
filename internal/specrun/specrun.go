// Package specrun is the implementation loop: it runs /af-impl on one spec
// package after another, each in a fresh claude -p session, until every
// package is done, and checks from the repository that each landed instead of
// trusting the session (ADR 23). It imports nothing that links a model
// client: the sessions are claude's.
package specrun

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/agent-fox-dev/aflib/envelope"
	"github.com/agent-fox-dev/spec"
	"github.com/agent-fox-dev/spec/afspec"
)

const tool = "specrun"

// Result is specrun's result.
type Result struct {
	// Landed is the packages that landed in this run, by directory name, in order.
	Landed []string `json:"landed"`
	// AllDone is true when no package is open.
	AllDone bool `json:"all_done"`
	// Waiting is the open packages that cannot start, each with why.
	Waiting []Waiting `json:"waiting,omitempty"`
	// Stopped is the step the loop stopped at, when it stopped.
	Stopped *Stopped `json:"stopped,omitempty"`
	// DryRun is true under --dry-run.
	DryRun bool `json:"dry_run,omitempty"`
	// NextStep is the step a dry run would take.
	NextStep *NextStep `json:"next_step,omitempty"`
}

// Waiting is an open package that cannot start, as speca --next words it.
type Waiting struct {
	SpecDir string   `json:"spec_dir"`
	SpecID  string   `json:"spec_id,omitempty"`
	WaitsOn []string `json:"waits_on,omitempty"`
	Reason  string   `json:"reason"`
}

// Stopped is the step the loop stopped at, and why.
type Stopped struct {
	Step    string `json:"step"`
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	Reason  string `json:"reason"`
}

// NextStep is a step: its kind and the package it is for.
type NextStep struct {
	Kind    string `json:"kind"`
	SpecDir string `json:"spec_dir"`
}

// The one kind of step in this version: implement the next package.
const kindImplement = "implement"

// run is one call of specrun.
type run struct {
	opts     *options
	stdout   io.Writer
	stderr   io.Writer
	started  time.Time
	root     string // the repository's top level
	specsDir string
	result   *Result
	warnings []envelope.Warning
}

// failure is a call that did not do its job: the stage, the category and the
// words of envelope.ErrorInfo.
func failure(stage, category, format string, args ...any) *envelope.ErrorInfo {
	return &envelope.ErrorInfo{Stage: stage, Category: category, Message: fmt.Sprintf(format, args...),
		Retryable: envelope.RetryableFor(category)}
}

// Main runs specrun with argv and returns its exit code. It writes exactly one
// envelope to stdout, except for --help and --version, which are for a person.
func Main(argv []string, stdout, stderr io.Writer) int {
	r := &run{stdout: stdout, stderr: stderr, started: time.Now()}
	opts, err := parseOptions(argv, os.Getenv)
	if errors.Is(err, flag.ErrHelp) {
		printUsage(stderr)
		return envelope.ExitOK
	}
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", tool, err)
		return r.emit(envelope.ExitUsage, "", failure("usage", envelope.CategoryUsage, "%v", err))
	}
	r.opts = opts
	if opts.version {
		fmt.Fprintf(stdout, "%s %s\n", tool, spec.Identity())
		return envelope.ExitOK
	}
	if f := r.locate(); f != nil {
		return r.emit(envelope.ExitFailed, "", f)
	}
	if opts.dryRun {
		return r.dryRun()
	}
	return r.loop()
}

// locate finds the repository the working directory is in, and its spec root.
func (r *run) locate() *envelope.ErrorInfo {
	if _, err := exec.LookPath("git"); err != nil {
		return failure("preflight", "environment", "git is not on PATH: specrun reads and moves the repository through it")
	}
	top, err := gitOutput("", "rev-parse", "--show-toplevel")
	if err != nil {
		return failure("preflight", "input", "the working directory is not in a git repository: %v", err)
	}
	r.root = top
	r.specsDir = afspec.ResolveSpecsDir("", top)
	r.result = &Result{Landed: []string{}}
	return nil
}

// display shows a path under the repository relative to it, with slashes, as
// speca shows a package directory.
func (r *run) display(path string) string {
	if rel, err := filepath.Rel(r.root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return path
}

// choose asks afspec for the next package, as speca --next does. It returns
// the package, or the failure that ends the call: nothing open is not one.
func (r *run) choose() (*afspec.NextPackage, *envelope.ErrorInfo) {
	choice, err := afspec.ChooseNext(r.specsDir, r.display)
	var cycle *afspec.CycleError
	switch {
	case errors.As(err, &cycle):
		return nil, failure("graph", "input", "%s", cycle.Error())
	case err != nil:
		return nil, failure("preflight", "input", "the spec root %s could not be read: %v", r.display(r.specsDir), err)
	}
	r.result.Waiting = nil
	for _, w := range choice.Waiting {
		r.result.Waiting = append(r.result.Waiting, Waiting{SpecDir: w.Dir, SpecID: w.SpecID, WaitsOn: w.WaitsOn, Reason: w.Reason})
	}
	r.result.AllDone = choice.AllDone
	if choice.Next == nil && !choice.AllDone {
		return nil, failure("waiting", "input", "no package can start: %d open package(s) wait on something no run can finish", len(choice.Waiting))
	}
	return choice.Next, nil
}

// dryRun reports the next step, or why there is none, and changes nothing.
func (r *run) dryRun() int {
	r.result.DryRun = true
	next, f := r.choose()
	if f != nil && f.Stage != "waiting" {
		return r.emit(envelope.ExitFailed, "", f)
	}
	switch {
	case next != nil:
		r.result.NextStep = &NextStep{Kind: kindImplement, SpecDir: next.Dir}
		return r.emit(envelope.ExitOK, fmt.Sprintf("%s: dry run; the next step is %s %s", tool, kindImplement, next.Dir), nil)
	case r.result.AllDone:
		return r.emit(envelope.ExitOK, tool+": dry run; every package is done", nil)
	default:
		return r.emit(envelope.ExitOK, fmt.Sprintf("%s: dry run; no package can start, %d waiting", tool, len(r.result.Waiting)), nil)
	}
}

// loop chooses the next package and ends when there is none to start.
// Running a session for one is the next part of the loop to be built.
func (r *run) loop() int {
	next, f := r.choose()
	if f != nil {
		return r.emit(envelope.ExitFailed, "", f)
	}
	if next == nil {
		return r.emit(envelope.ExitOK, fmt.Sprintf("%s: every package is done, landed %d", tool, len(r.result.Landed)), nil)
	}
	return r.emit(envelope.ExitFailed, "", failure("session", "internal", "the session for %s is not run: specrun runs no session yet", next.Dir))
}

// emit writes the one envelope and returns its exit code. An empty summary is
// the failure's message, or a count of what landed.
func (r *run) emit(code int, summary string, f *envelope.ErrorInfo) int {
	if summary == "" {
		switch {
		case f != nil:
			summary = tool + ": " + f.Message
		default:
			summary = fmt.Sprintf("%s: landed %d", tool, len(r.result.Landed))
		}
	}
	env := envelope.Envelope[*Result]{
		Tool: tool, Version: spec.Identity(), OK: code == envelope.ExitOK && f == nil, Status: envelope.StatusFor(code),
		ExitCode: code, Summary: shorten(summary, 200), Error: f, Warnings: r.warnings, Result: r.result,
		DurationMS: time.Since(r.started).Milliseconds(), StartedAt: r.started.UTC().Format(time.RFC3339),
	}
	b, err := envelope.Encode(env)
	if err != nil {
		fmt.Fprintf(r.stderr, "%s: the envelope could not be encoded: %v\n", tool, err)
		return envelope.ExitFailed
	}
	_, _ = r.stdout.Write(b)
	return code
}

// shorten cuts s to n characters, ending in an ellipsis when it was cut.
func shorten(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// gitOutput runs git in dir (the working directory when empty) and returns
// its output, trimmed; the error holds git's own words.
func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
