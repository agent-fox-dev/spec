// Package cli is the shell of the spec command: one positional input, one
// JSON envelope on stdout, progress on stderr, the shared flags, and the
// checks that refuse a run before a token is spent.
package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	sdk "github.com/anthropics/anthropic-sdk-go"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/provider/anthropic"
	"github.com/agent-fox-dev/agentkit-go/tools"

	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/issuex"
)

// UsageError is a wrong invocation: nothing was fetched and nothing was
// written.
type UsageError struct{ msg string }

func (e *UsageError) Error() string { return e.msg }

// Category is "usage", so a UsageError is classified, and exits, as one.
func (e *UsageError) Category() string { return "usage" }

// Usagef builds a UsageError.
func Usagef(format string, args ...any) error {
	return &UsageError{msg: fmt.Sprintf(format, args...)}
}

// Deps are what an App's Exec receives: everything the shell has already
// resolved and checked.
type Deps struct {
	Common    *Common
	Input     Input
	Workspace *tools.Workspace
	Runner    *agentrun.Runner
	// Forge is the forge client — GitHub or GitLab, chosen from the input URL
	// or the origin remote — and is always non-nil. Whether it can write is
	// Forge.Authenticated().
	Forge    issuex.Client
	Run      *Run
	Progress *Progress
	// Models is the model and effort of every phase.
	Models agentrun.Plan
	// Index is the run's code-search index; nil when it could not be built,
	// and IndexUnavailable then says why.
	Index            tools.Index
	IndexUnavailable string
}

// App is the command: its name, help, own flags, default ceilings, and the
// function that does the work.
type App struct {
	Name    string
	Version string
	// Usage is the help text printed above the flag list.
	Usage string
	// Flags registers the command's own flags.
	Flags func(*flag.FlagSet)
	// DefaultBounds are the per-phase ceilings before the flags override
	// them.
	DefaultBounds agentrun.Bounds
	// SinglePhase folds --total-budget into the per-phase ceiling.
	SinglePhase bool
	// PreCheck runs after flag parsing and before anything is fetched.
	PreCheck func(*Common) error
	// CheckInput runs after the input is read and before the model is
	// resolved.
	CheckInput func(Input) error
	// Exec is the work: the exit code, the result, and the error object when
	// there is one.
	Exec func(context.Context, Deps) (int, any, *ErrorInfo)
	// PreflightExec runs every check Exec would make before its first model
	// call, and stops.
	PreflightExec func(context.Context, Deps) (int, any, *ErrorInfo)
}

// testProvider, when set, replaces the Anthropic client: an end-to-end test
// of the command runs the real shell against a scripted model.
var testProvider core.ProviderClient

// SetProvider makes every run use p instead of an Anthropic client, for a
// test. It returns the function that puts the previous value back.
func SetProvider(p core.ProviderClient) (restore func()) {
	old := testProvider
	testProvider = p
	return func() { testProvider = old }
}

// resolveClient builds the Anthropic client from the environment: the API,
// Vertex AI or Bedrock. It makes no network request.
func resolveClient() (*sdk.Client, error) {
	client, _, err := anthropic.Resolve(anthropic.OSEnv{})
	return client, err
}

// Main parses, resolves and runs. It returns the process exit code.
//
// It writes exactly one JSON object to stdout on every program-driven path.
// Two paths are for a person and print text instead: --version, and
// -h/--help or a bare invocation, which print the help to stderr. A bare
// invocation also writes a usage envelope when stdout is not a terminal, so a
// program piping stdout still gets one object to parse.
func (a App) Main(ctx context.Context, argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var common Common
	fs := flag.NewFlagSet(a.Name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), a.Usage)
		fs.PrintDefaults()
	}
	common.Register(fs)
	if a.Flags != nil {
		a.Flags(fs)
	}
	run := NewRun(a.Name, a.Version)

	// An envelope emitted before --output is validated still goes to it when
	// it names a writable file: a program reading that file then sees the
	// usage error too.
	early := func() string {
		path, err := common.ResolveOutput()
		if err != nil {
			return ""
		}
		return path
	}

	var parseOut bytes.Buffer
	fs.SetOutput(&parseOut)
	input, err := SplitArgs(fs, argv)
	fs.SetOutput(stderr)
	if err != nil {
		_, _ = io.Copy(stderr, &parseOut)
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK
		}
		fmt.Fprintf(stderr, "%s: %v\n", a.Name, err)
		return a.emit(stdout, &common, early(), run, ExitUsage, nil, usageInfo(err))
	}
	if common.Version {
		fmt.Fprintf(stdout, "%s %s\n", a.Name, a.Version)
		return ExitOK
	}
	run.SetBounds(common.Bounds(a.DefaultBounds, a.SinglePhase))
	if verr := common.Validate(); verr != nil {
		fmt.Fprintf(stderr, "%s: %v\n", a.Name, verr)
		return a.emit(stdout, &common, early(), run, ExitUsage, nil, usageInfo(verr))
	}
	if strings.TrimSpace(input) == "" {
		fs.Usage()
		if isTerminal(stdout) {
			return ExitUsage
		}
		return a.emit(stdout, &common, early(), run, ExitUsage, nil, &ErrorInfo{Stage: "usage", Category: "usage", Message: NoInputMessage})
	}
	outPath, oerr := common.ResolveOutput()
	if oerr != nil {
		fmt.Fprintf(stderr, "%s: %v\n", a.Name, oerr)
		return a.emit(stdout, &common, "", run, ExitUsage, nil, usageInfo(oerr))
	}

	progress := NewProgress(stderr, a.Name, common.Verbose, common.Quiet, common.ShowText)
	code, result, failure := a.execute(ctx, &common, input, stdin, run, progress)
	return a.emit(stdout, &common, outPath, run, code, result, failure)
}

func usageInfo(err error) *ErrorInfo {
	return &ErrorInfo{Stage: "usage", Category: "usage", Message: err.Error(), err: err}
}

// emit builds the envelope from the full result, swaps in the --detail view,
// writes the --output copy, then stdout.
func (a App) emit(stdout io.Writer, common *Common, outPath string, run *Run, code int, result any, failure *ErrorInfo) int {
	result = presentOrNil(result)
	if dr, ok := result.(DetailedResult); ok {
		dr.SetDetail("full")
	}
	env := run.Envelope(code, result, failure)
	if env.Result != nil && common.Detail != "full" {
		if s, ok := result.(Summarizable); ok {
			env.Result = s.SummaryView()
		}
	}
	if outPath != "" {
		b, _ := marshalEnvelope(env)
		if werr := writeAtomic(outPath, b); werr != nil {
			run.Warn(WarnOutputNotWritten, "low", "the --output file could not be written to %s: %v", outPath, werr)
			env.Warnings = run.Warnings()
		}
	}
	return Emit(stdout, env)
}

// execute reads the input, resolves the model and the client, builds the
// runner, and hands over to Exec — or to PreflightExec under --preflight.
// Every refusal before Exec is the same with and without --preflight.
func (a App) execute(ctx context.Context, c *Common, argument string, stdin io.Reader, run *Run, progress *Progress) (int, any, *ErrorInfo) {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if a.PreCheck != nil {
		if err := a.PreCheck(c); err != nil {
			return usage(err)
		}
	}
	ws, err := c.Workspace()
	if err != nil {
		return usage(err)
	}

	contextBlock := c.ContextBlock()
	if len(contextBlock) > 0 && len(argument)+len(contextBlock) > MaxInputBytes {
		return usage(fmt.Errorf("input and context together exceed %d bytes (%d bytes)",
			MaxInputBytes, len(argument)+len(contextBlock)))
	}

	forge, ferr := pickForge(argument, c.InputKind, ws.Root, a.Name+"/"+a.Version)
	if ferr != nil {
		return ExitFailed, nil, &ErrorInfo{Stage: "input", Category: "input", Message: ferr.Error(), err: ferr}
	}
	defer func() { _ = forge.Close() }()

	var in Input
	if c.InputKind != "" {
		in, err = ResolveForced(ctx, c.InputKind, argument, stdin, forge)
	} else {
		in, err = Resolve(ctx, argument, stdin, forge, run)
	}
	if err != nil {
		var ue *UsageError
		switch {
		case errors.Is(err, ErrNoInput):
			return usage(Usagef("%s", NoInputMessage))
		case errors.As(err, &ue):
			return usage(err)
		}
		return ExitFailed, nil, &ErrorInfo{Stage: "input", Category: "input", Message: err.Error(), err: err}
	}
	if len(contextBlock) > 0 && (in.Truncated || len(in.Body)+len(contextBlock) > MaxInputBytes) {
		return usage(fmt.Errorf("input and context together exceed %d bytes (%d bytes)",
			MaxInputBytes, len(in.Body)+len(contextBlock)))
	}
	if in.Truncated {
		run.Warn(WarnInputTruncated, "high", "the input was truncated at %d bytes", MaxInputBytes)
	}
	if in.CommentsTruncated {
		run.Warn(WarnCommentsTruncated, "low", "the issue has more comments than were read; the later ones are not in the input")
	}
	if in.Thread != nil && in.Thread.CommentsErr != nil {
		run.Warn(WarnCommentsUnreadable, "low", "the issue's comments could not be read: %v", in.Thread.CommentsErr)
	}
	in.Context = contextBlock
	run.SetInput(in)
	progress.Detail("input: %s (%s, %d bytes)", in.Kind, in.Origin, len(in.Body))

	if a.CheckInput != nil {
		if err := a.CheckInput(in); err != nil {
			return usage(err)
		}
	}

	plan, err := c.ResolveModels()
	if err != nil {
		var ue *UsageError
		if errors.As(err, &ue) {
			return usage(err)
		}
		return ExitFailed, nil, &ErrorInfo{Stage: "preflight", Category: agentrun.CategoryOf(err), Message: err.Error(), err: err}
	}
	warnClamped(run, plan)
	run.SetModels(plan)
	for _, phase := range agentrun.Phases {
		progress.Detail("model for %s: %s", phase, plan.For(phase))
	}

	cfg := agentrun.Config{
		Model:        plan.Default,
		PhaseModels:  plan.Phases,
		Provider:     testProvider,
		Workspace:    ws,
		TrustProject: c.TrustProject,
		Bounds:       c.Bounds(a.DefaultBounds, a.SinglePhase),
		Observer:     progress,
		ShowText:     c.ShowText,
	}
	if cfg.Provider == nil {
		client, cerr := resolveClient()
		if cerr != nil {
			return ExitFailed, nil, &ErrorInfo{Stage: "preflight", Category: agentrun.CategoryAuth, Message: cerr.Error(), err: cerr}
		}
		cfg.Client = client
	}

	// The run's one code-search index is built before the Runner, so every
	// phase can offer code_search, and closed on every way out of the run.
	index, unavailable, closeIndex := agentrun.OpenIndex(ws)
	defer closeIndex()
	if unavailable != "" {
		run.Warn(WarnCodeSearchUnavailable, "low", "code_search is unavailable and the run falls back to search_files: %s", unavailable)
	}
	cfg.Index = index

	runner, err := agentrun.NewRunner(cfg)
	if err != nil {
		return ExitFailed, nil, &ErrorInfo{Stage: "preflight", Category: agentrun.CategoryOf(err), Message: err.Error(), err: err}
	}
	deps := Deps{
		Common:           c,
		Input:            in,
		Workspace:        ws,
		Runner:           runner,
		Forge:            forge,
		Run:              run,
		Progress:         progress,
		Models:           plan,
		Index:            index,
		IndexUnavailable: unavailable,
	}
	if c.Preflight {
		if a.PreflightExec == nil {
			err := fmt.Errorf("%s does not support --preflight", a.Name)
			return ExitFailed, nil, &ErrorInfo{Stage: "preflight", Category: agentrun.CategoryInternal, Message: err.Error(), err: err}
		}
		return a.PreflightExec(ctx, deps)
	}
	return a.Exec(ctx, deps)
}

// warnClamped records, once per model, effort and phases, an effort a phase's
// model does not take and what it was lowered to.
func warnClamped(run *Run, plan agentrun.Plan) {
	type key struct{ id, asked, got string }
	var order []key
	phases := map[key][]string{}
	for _, phase := range agentrun.Phases {
		m := plan.For(phase)
		if m.Clamped == "" {
			continue
		}
		k := key{m.ID(), string(m.Clamped), string(m.Effort)}
		if _, seen := phases[k]; !seen {
			order = append(order, k)
		}
		phases[k] = append(phases[k], phase)
	}
	for _, k := range order {
		which := strings.Join(phases[k], ", ")
		if k.got == "" {
			run.Warn(WarnEffortClamped, "low", "model %s takes no effort at or below %s; none is sent (%s)", k.id, k.asked, which)
		} else {
			run.Warn(WarnEffortClamped, "low", "effort %s clamped to %s for model %s (%s)", k.asked, k.got, k.id, which)
		}
	}
}

func usage(err error) (int, any, *ErrorInfo) {
	return ExitUsage, nil, usageInfo(err)
}

// pickForge builds the forge client the input is read through. An issue URL
// names its forge, and that forge is the one used: when it cannot be told
// which forge serves the URL's host, that is the run's error. Without a URL
// the workspace's origin remote decides, and a workspace with no forge gets
// the no-op client.
func pickForge(argument, inputKind, root, userAgent string) (issuex.Client, error) {
	if inputKind == "" || inputKind == string(KindIssue) {
		if ref, ok := issuex.ParseIssueURL(argument); ok && ref.Repo.Host != "" {
			forge, err := issuex.NewWithOptions(issuex.Options{Repo: ref.Repo, UserAgent: userAgent, Dir: root})
			if err != nil {
				return nil, fmt.Errorf("cannot read %s: %w", argument, err)
			}
			return forge, nil
		}
	}
	opts := issuex.Options{UserAgent: userAgent, Dir: root}
	if repo, ok := issuex.DetectRepo(root); ok {
		opts.Repo = repo
	}
	forge, err := issuex.NewWithOptions(opts)
	if err != nil || forge == nil {
		return issuex.NewNoOp(), nil
	}
	return forge, nil
}
