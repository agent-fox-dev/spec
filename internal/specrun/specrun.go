// Package specrun is the implementation loop: it runs /af-impl on one spec
// package after another, each in a fresh claude -p session, until every
// package is done, and checks from the repository that each landed instead of
// trusting the session (ADR 23). It imports nothing that links a model
// client: the sessions are claude's.
package specrun

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/agent-fox-dev/aflib/envelope"
	"github.com/agent-fox-dev/aflib/issuex"
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
	// Versions is what the run runs.
	Versions *Versions `json:"versions,omitempty"`
	// WouldChange and WouldStop are, under --dry-run with a forge, what the
	// label rules would change as the labels now stand and the issues the
	// stop rule would name.
	WouldChange []labelChange `json:"would_change,omitzero"`
	WouldStop   []StopIssue   `json:"would_stop,omitzero"`
	// WouldAdopt is, under --dry-run, the issues a due reconcile would hand
	// over for adoption.
	WouldAdopt []adoptEntry `json:"would_adopt,omitzero"`
	// RepairQueue is, under --dry-run with a forge, the repair queue the
	// labels give now, in order and capped by --repair-max.
	RepairQueue []queued `json:"repair_queue,omitzero"`
	// OpenDeviations are the open deviations no person has settled, oldest
	// first, after a reconcile.
	OpenDeviations []OpenDeviation `json:"open_deviations,omitzero"`
}

// OpenDeviation is an open deviation that carries neither af:accepted nor
// af:rejected.
type OpenDeviation struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
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
	// Issues are the serious defects a stop on them names.
	Issues []StopIssue `json:"issues,omitempty"`
}

// StopIssue is an open defect the stop rule names.
type StopIssue struct {
	Number   int    `json:"number"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Severity string `json:"severity"`
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
	ctx      context.Context
	opts     *options
	stdout   io.Writer
	stderr   io.Writer
	started  time.Time
	root     string // the repository's top level
	specsDir string
	base     string // the base branch
	logDir   string
	// statePath is the state file, and state what it holds.
	statePath string
	state     state
	holding   bool // this run holds the repository, as state.Owner says
	forge     issuex.Client
	repo      issuex.Repo
	result    *Result
	warnings  []envelope.Warning
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
	// An interrupt or a termination ends the session with everything it
	// started, and the step is left to be taken up again.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r := &run{ctx: ctx, stdout: stdout, stderr: stderr, started: time.Now()}
	opts, err := parseOptions(argv, os.LookupEnv)
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
		_ = r.connect() // a dry run warns, and writes to no forge
		return r.dryRun()
	}
	if opts.ledger {
		return r.printLedger()
	}
	if opts.resetState {
		return r.resetState()
	}
	defer r.release()
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
	if f := r.wouldDo(); f != nil {
		return r.emit(envelope.ExitFailed, "", f)
	}
	next, f := r.choose()
	if f != nil && f.Stage != "waiting" {
		return r.emit(envelope.ExitFailed, "", f)
	}
	if r.forge != nil && r.reconcileDue(r.result.AllDone) {
		if f := r.wouldReconcile(); f != nil {
			return r.emit(envelope.ExitFailed, "", f)
		}
		return r.emit(envelope.ExitOK, fmt.Sprintf("%s: dry run; the next step is a reconcile, %d issue(s) to adopt", tool, len(r.result.WouldAdopt)), nil)
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

// loop runs one step at a time until every package is done, a step stops, or
// --max packages have landed. Before every step the base branch is checked
// out and pulled.
func (r *run) loop() int {
	if _, err := exec.LookPath("claude"); err != nil {
		return r.emit(envelope.ExitFailed, "", failure("preflight", "environment",
			"claude is not on PATH: every session is a claude -p process"))
	}
	if f := r.versions(); f != nil {
		return r.emit(envelope.ExitFailed, "", f)
	}
	if f := r.connect(); f != nil {
		return r.emit(envelope.ExitFailed, "", f)
	}
	if f := r.loadState(); f != nil {
		return r.emit(envelope.ExitFailed, "", f)
	}
	if code, f := r.prepareLogDir(); f != nil {
		return r.emit(code, "", f)
	}
	r.base = baseBranch(r.root)
	schemed := false // the scheme is made from the spec root the first pull brings
	for {
		if r.opts.max > 0 && len(r.result.Landed) >= r.opts.max {
			if _, f := r.choose(); f == nil && r.result.AllDone {
				return r.finalReconcile()
			}
			return r.emit(envelope.ExitOK, fmt.Sprintf("%s: landed %d, as --max %d asks", tool, len(r.result.Landed), r.opts.max), nil)
		}
		before, f := r.syncBase()
		if f != nil {
			return r.emit(envelope.ExitFailed, "", f)
		}
		r.hold()
		if !schemed {
			if f := r.createScheme(r.ctx); f != nil {
				return r.emit(envelope.ExitFailed, "", f)
			}
			schemed = true
		}
		if run := r.state.Running; run != nil && run.Kind == kindReconcile {
			if stop := r.reconcile(); stop != nil {
				return r.stop(stop)
			}
		}
		if run := r.state.Running; run != nil && run.Kind == kindRepair {
			n, _ := strconv.Atoi(run.Subject)
			if stop := r.rerunRepair(n); stop != nil {
				return r.stop(stop)
			}
		}
		next := r.resume()
		if stop := r.recheckLabels(); stop != nil {
			return r.stop(stop)
		}
		if stop := r.recheckDefects(); stop != nil {
			return r.stop(stop)
		}
		if stop := r.drainQueue(); stop != nil {
			return r.stop(stop)
		}
		// A reconcile already due, as after a restart, comes before any package.
		if next == nil && r.reconcileDue(false) {
			if stop := r.reconcile(); stop != nil {
				return r.stop(stop)
			}
		}
		if next == nil {
			var f *envelope.ErrorInfo
			if next, f = r.choose(); f != nil {
				return r.emit(envelope.ExitFailed, "", f)
			}
		}
		if next == nil {
			return r.finalReconcile()
		}
		if stop := r.implement(next, before); stop != nil {
			return r.stop(stop)
		}
		if r.reconcileDue(false) {
			if stop := r.reconcile(); stop != nil {
				return r.stop(stop)
			}
		}
	}
}

// stop ends the loop at the step.
func (r *run) stop(s *Stopped) int {
	r.result.Stopped = s
	return r.emit(envelope.ExitFailed, fmt.Sprintf("%s: stopped at %s: %s; landed %d", tool, s.Step, s.Reason,
		len(r.result.Landed)), failure(stopStage(s.Reason), "input", "%s %s: %s", s.Kind, s.Subject, s.Reason))
}

// recheckLabels applies the label rules a forge call cut short in an earlier
// run, before a step is chosen.
func (r *run) recheckLabels() *Stopped {
	dir := r.state.LabelCheck
	if dir == "" && len(r.state.PendingComments) == 0 {
		return nil
	}
	err := r.postPending()
	if err == nil && dir != "" {
		err = r.checkLabels(dir)
	}
	if err != nil {
		return &Stopped{Step: "labels-" + filepath.Base(dir), Kind: "labels", Subject: filepath.Base(dir), Reason: err.Error()}
	}
	r.state.LabelCheck = ""
	r.saveState()
	return nil
}

// recheckDefects stops before a step is chosen while the last package that
// landed still has an open defect of a severity in --stop-on.
func (r *run) recheckDefects() *Stopped {
	dir := r.state.LastLanded
	if dir == "" {
		return nil
	}
	issues, err := r.seriousDefects(dir)
	subject := filepath.Base(dir)
	// The same stop the landing made: its step is the package's last ledger line.
	s := &Stopped{Step: stepName(0, kindImplement, subject), Kind: kindImplement, Subject: subject}
	if lines, _ := readLedger(r.logDir); len(lines) > 0 {
		for i := len(lines) - 1; i >= 0; i-- {
			if lines[i].Kind == kindImplement && lines[i].Subject == subject {
				s.Step = stepName(i+1, kindImplement, subject)
				break
			}
		}
	}
	switch {
	case err != nil:
		s.Reason = err.Error()
	case len(issues) > 0:
		s.Reason, s.Issues = reasonSeriousDefect, issues
		if r.opts.repair {
			return r.repairSerious(dir, s)
		}
	default:
		return nil
	}
	return s
}

// stopStage is the stage of the failure a stopped step is reported with.
func stopStage(reason string) string {
	if reason == reasonSeriousDefect || reason == reasonRepairFailed {
		return "defect"
	}
	if strings.HasPrefix(reason, "serious defects:") {
		return "forge"
	}
	for _, rule := range []string{ruleIncompleteLabels, ruleAwaitsFinished, ruleAdoptionUnsettled, ruleDeviationAccepted, ruleDeviationRejected, "reconcile:"} {
		if strings.HasPrefix(reason, rule) {
			return "forge"
		}
	}
	switch reason {
	case reasonStalled:
		return "stall"
	case reasonNoSession:
		return "session"
	case reasonInterrupted:
		return "interrupt"
	}
	return "land"
}

// printLedger is --ledger: the ledger as a table on stdout, which is for a
// person, and nothing started.
func (r *run) printLedger() int {
	dir, err := logDirFor(r.opts, r.root)
	if err == nil {
		err = printLedger(r.stdout, dir)
	}
	if err != nil {
		fmt.Fprintf(r.stderr, "%s: the ledger could not be read: %v\n", tool, err)
		return envelope.ExitFailed
	}
	return envelope.ExitOK
}

// baseBranch is the branch refs/remotes/origin/HEAD names, else main.
func baseBranch(root string) string {
	if ref, err := gitOutput(root, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		if _, branch, ok := strings.Cut(ref, "/"); ok {
			return branch
		}
	}
	return "main"
}

// syncBase checks out the base branch and pulls it with --ff-only, and
// returns the commit HEAD is at then. A failure of either is the preflight's,
// in git's own words.
func (r *run) syncBase() (string, *envelope.ErrorInfo) {
	was, _ := gitOutput(r.root, "branch", "--show-current")
	if _, err := gitOutput(r.root, "switch", "--quiet", r.base); err != nil {
		return "", failure("preflight", "input", "the base branch %s could not be checked out: %v", r.base, err)
	}
	if _, err := gitOutput(r.root, "pull", "--quiet", "--ff-only", "origin", r.base); err != nil {
		// The repository is left as it was found.
		if was != "" && was != r.base {
			_, _ = gitOutput(r.root, "switch", "--quiet", was)
		}
		return "", failure("preflight", "input", "the base branch %s could not be brought up to date: %v", r.base, err)
	}
	head, err := gitOutput(r.root, "rev-parse", "HEAD")
	if err != nil {
		return "", failure("preflight", "input", "%v", err)
	}
	return head, nil
}

// implement runs the step that implements the package next: one session,
// its stream and final message saved, then the landing checks, and the
// step's ledger line. It returns the stop, or nil when the package landed.
func (r *run) implement(next *afspec.NextPackage, before string) *Stopped {
	earlier, _ := readLedger(r.logDir)
	subject := filepath.Base(next.Dir)
	step := stepName(len(earlier)+1, kindImplement, subject)
	started := now().UTC().Truncate(time.Second)
	r.startStep(kindImplement, next, before, started)

	reason := ""
	o, err := r.session(step).run(r.ctx, "/af-impl "+next.Dir)
	switch {
	case err != nil:
		reason = reasonNoSession
	case o.interrupted:
		reason = reasonInterrupted
	case o.stalled:
		reason = reasonStalled
	default:
		reason = r.landed(filepath.Join(r.root, filepath.FromSlash(next.Dir)), before)
	}
	_ = os.WriteFile(filepath.Join(r.logDir, step+".log"), []byte(o.final), 0o644)

	ended := now().UTC().Truncate(time.Second)
	line := ledgerLine{Kind: kindImplement, Subject: subject, StartedAt: started.Format(time.RFC3339),
		EndedAt: ended.Format(time.RFC3339), WallMinutes: ended.Sub(started).Minutes(), Outcome: outcomeLanded}
	short := ""
	var labelErr error
	var serious []StopIssue
	switch reason {
	case "":
		if t, ok := r.readTrailer(); ok {
			line.Gate, line.OpenItems = t.Gate, t.OpenItems
			short = trailerSummary(t)
		}
		serious, labelErr = r.afterLanding(next.Dir)
		line.Issues = serious
	case reasonStalled:
		line.Outcome = outcomeStalled
	default:
		line.Outcome = outcomeStopped
	}
	if err := appendLedger(r.logDir, line); err != nil {
		fmt.Fprintf(r.stderr, "%s: the ledger could not be written: %v\n", tool, err)
	}
	if reason != "" {
		r.releaseStep()
		fmt.Fprintf(r.stderr, "%s: %s %s in %.1f min: %s\n", tool, step, line.Outcome, line.WallMinutes, reason)
		return &Stopped{Step: step, Kind: kindImplement, Subject: subject, Reason: reason}
	}
	r.finishStep(kindImplement, subject)
	r.result.Landed = append(r.result.Landed, subject)
	fmt.Fprintf(r.stderr, "%s: %s landed in %.1f min%s\n", tool, step, line.WallMinutes, short)
	if median, slow := slowerThanUsual(earlier, line.WallMinutes); slow {
		fmt.Fprintf(r.stderr, "%s: warning: %s took %.1f min, more than twice the median of %.1f min of the packages landed before it\n",
			tool, subject, line.WallMinutes, median)
	}
	if labelErr != nil {
		reason := labelErr.Error()
		if r.ctx.Err() != nil {
			reason = reasonInterrupted
		}
		fmt.Fprintf(r.stderr, "%s: %s: %s\n", tool, step, reason)
		return &Stopped{Step: step, Kind: kindImplement, Subject: subject, Reason: reason}
	}
	if len(serious) > 0 {
		for _, is := range serious {
			fmt.Fprintf(r.stderr, "%s: %s left a %s defect open: #%d %s\n", tool, subject, is.Severity, is.Number, is.Title)
		}
		s := &Stopped{Step: step, Kind: kindImplement, Subject: subject, Reason: reasonSeriousDefect, Issues: serious}
		if r.opts.repair {
			return r.repairSerious(next.Dir, s)
		}
		return s
	}
	return nil
}

// afterLanding applies the label rules for the package in specDir, which
// has landed, and lists its serious defects. The check is owed in the state
// until it is done, so that a crash in it leaves it to the next start.
func (r *run) afterLanding(specDir string) ([]StopIssue, error) {
	r.state.LabelCheck = specDir
	r.saveState()
	if err := r.checkLabels(specDir); err != nil {
		return nil, err
	}
	r.state.LabelCheck = ""
	r.saveState()
	return r.seriousDefects(specDir)
}

// session is the session of the step, its stream going to <step>.jsonl and
// its stderr to <step>.stderr.
func (r *run) session(step string) *runningSession {
	return &runningSession{session: session{dir: r.root, stall: r.opts.stall}, base: filepath.Join(r.logDir, step)}
}

// runningSession is a session whose stream and stderr are saved to files.
type runningSession struct {
	session
	base string
}

func (s *runningSession) run(ctx context.Context, prompt string) (outcome, error) {
	stream, err := os.Create(s.base + ".jsonl")
	if err != nil {
		return outcome{}, err
	}
	defer stream.Close()
	errs, err := os.Create(s.base + ".stderr")
	if err != nil {
		return outcome{}, err
	}
	defer errs.Close()
	s.stream, s.errs = stream, errs
	return s.session.run(ctx, prompt)
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
