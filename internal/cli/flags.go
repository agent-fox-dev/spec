package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/internal/agentrun"
)

// Environment variables the shared flags read when they are not given.
const (
	// ModelEnv selects the model. AGENTKIT_MODEL is honoured as a fallback.
	ModelEnv         = "AF_MODEL"
	ModelEnvFallback = "AGENTKIT_MODEL"
	// EffortEnv supplies the effort when --effort is not given.
	EffortEnv = "AF_MODEL_EFFORT"
)

// DefaultModel is the tier a run uses unless told otherwise. It is a tier
// rather than a model id so that spec keeps working when a model is retired.
const DefaultModel = "STANDARD"

// DefaultRepoMapTokens is the default token budget of the repository map.
const DefaultRepoMapTokens = 6000

// Common is the flag set every run shares: where to work, which model, the
// ceilings, the input and how the result is printed.
type Common struct {
	Dir            string
	Model          string
	Effort         string
	MaxTurns       int
	Budget         float64
	Timeout        time.Duration
	TotalBudgetUSD float64
	TrustProject   bool
	Verbose        bool
	Quiet          bool
	ShowText       bool
	Version        bool
	Context        []string
	// Detail selects the result view on stdout: "summary" (the default) or
	// "full".
	Detail string
	// InputKind forces how the argument is classified: "file", "text",
	// "issue" or "stdin". Empty means guess.
	InputKind string
	// DryRun makes no change on disk or on a forge.
	DryRun bool
	// Output is where a second, atomic copy of the stdout envelope is
	// written, resolved against the process's working directory.
	Output string
	// RepoMapTokens is the token budget of the repository map in every
	// phase's prompt; 0 disables the map.
	RepoMapTokens int
	// Preflight runs every check that would refuse the run, then stops
	// before any model phase.
	Preflight bool
	// PhaseModels are the --phase-model values, in the order given.
	PhaseModels []agentrun.PhaseChoice
}

type contextFlag []string

func (f *contextFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ", ")
}

func (f *contextFlag) Set(val string) error {
	*f = append(*f, val)
	return nil
}

// phaseModelFlag collects --phase-model values, each checked as it is
// parsed so a bad one is a usage error before anything runs.
type phaseModelFlag struct{ val *[]agentrun.PhaseChoice }

func (f *phaseModelFlag) String() string {
	if f.val == nil {
		return ""
	}
	parts := make([]string, len(*f.val))
	for i, c := range *f.val {
		parts[i] = c.Phase + "=" + c.Model
	}
	return strings.Join(parts, ", ")
}

func (f *phaseModelFlag) Set(raw string) error {
	c, err := agentrun.ParsePhaseChoice(raw)
	if err != nil {
		return err
	}
	*f.val = append(*f.val, c)
	return nil
}

// effortFlag validates --effort at parse time and stores it in lower case.
type effortFlag struct{ val *string }

func (f *effortFlag) String() string {
	if f.val == nil {
		return ""
	}
	return *f.val
}

func (f *effortFlag) Set(raw string) error {
	if _, err := agentrun.ParseEffort(raw); err != nil {
		return err
	}
	*f.val = strings.ToLower(strings.TrimSpace(raw))
	return nil
}

// Register adds the shared flags to fs.
func (c *Common) Register(fs *flag.FlagSet) {
	fs.StringVar(&c.Dir, "dir", ".", "the repository to work in; the file tools cannot reach outside it")
	fs.StringVar(&c.Model, "model", "", "model tier (SIMPLE, STANDARD, ADVANCED) or catalog model id; default $"+ModelEnv+", else "+DefaultModel)
	fs.Var(&effortFlag{val: &c.Effort}, "effort", "reasoning effort: "+strings.Join(agentrun.EffortValues(), ", ")+"; default $"+EffortEnv+", else the tier's own effort")
	fs.Var(&phaseModelFlag{val: &c.PhaseModels}, "phase-model", "PHASE=MODEL[:EFFORT]: run one phase ("+strings.Join(agentrun.Phases, ", ")+") on another model (a tier or a catalog id) and effort; repeatable")
	fs.IntVar(&c.MaxTurns, "max-turns", 0, "per-phase turn ceiling, also the repair budget")
	fs.Float64Var(&c.Budget, "budget", 0, "per-phase spend ceiling, in dollars")
	fs.DurationVar(&c.Timeout, "phase-timeout", 0, "wall-clock ceiling on one phase")
	fs.Float64Var(&c.TotalBudgetUSD, "total-budget", 0, "spend ceiling for the whole run across every phase, in dollars; 0 means only the per-phase --budget")
	fs.BoolVar(&c.TrustProject, "trust-project", false, "admit the repository's AGENTS.md and CLAUDE.md into the system prompt")
	fs.BoolVar(&c.Verbose, "verbose", false, "trace tool calls and timings on stderr")
	fs.BoolVar(&c.Quiet, "quiet", false, "print nothing on stderr")
	fs.BoolVar(&c.ShowText, "show-text", false, "stream the model's prose to stderr")
	fs.BoolVar(&c.Version, "version", false, "print the build identity and exit")
	fs.Var((*contextFlag)(&c.Context), "context", "additional context for the model, repeatable")
	fs.StringVar(&c.Detail, "detail", "summary", "result view: summary (a trimmed subset) or full (everything computed)")
	fs.StringVar(&c.InputKind, "input-kind", "", "force how the argument is classified: file, text, issue or stdin; a mismatch is a usage error (default: guess)")
	fs.BoolVar(&c.DryRun, "dry-run", false, "write nothing to disk or to a forge; report what would have been written")
	fs.StringVar(&c.Output, "output", "", "also write a copy of the stdout envelope to this file, atomically and before stdout")
	fs.IntVar(&c.RepoMapTokens, "repo-map-tokens", DefaultRepoMapTokens, "token budget of the repository map in every phase's prompt; 0 disables the map")
	fs.BoolVar(&c.Preflight, "preflight", false, "run every check that would refuse the run, then stop before any model phase")
}

// ContextBlock renders the --context values into one labelled block, or ""
// when none were given.
func (c *Common) ContextBlock() string {
	if c == nil || len(c.Context) == 0 {
		return ""
	}
	return "## Additional context from the caller\n\n" + strings.Join(c.Context, "\n\n") + "\n"
}

// Validate refuses the flag values that cannot hold, before anything is
// fetched: an unknown --detail or --input-kind, a negative ceiling, a
// negative map budget.
func (c *Common) Validate() error {
	var errs []error
	switch c.Detail {
	case "", "summary", "full":
	default:
		errs = append(errs, fmt.Errorf("--detail must be %q or %q, got %q", "summary", "full", c.Detail))
	}
	switch c.InputKind {
	case "", string(KindFile), string(KindText), string(KindIssue), string(KindStdin):
	default:
		errs = append(errs, fmt.Errorf("--input-kind must be one of %q, %q, %q or %q, got %q",
			KindFile, KindText, KindIssue, KindStdin, c.InputKind))
	}
	if c.Budget < 0 {
		errs = append(errs, errors.New("--budget cannot be negative"))
	}
	if c.TotalBudgetUSD < 0 {
		errs = append(errs, errors.New("--total-budget cannot be negative"))
	}
	if c.MaxTurns < 0 {
		errs = append(errs, errors.New("--max-turns cannot be negative"))
	}
	if c.Timeout < 0 {
		errs = append(errs, errors.New("--phase-timeout cannot be negative"))
	}
	if c.RepoMapTokens < 0 {
		errs = append(errs, errors.New("--repo-map-tokens cannot be negative"))
	}
	return errors.Join(errs...)
}

// ResolveOutput validates --output and returns the absolute path it names, or
// "" when none was given. "-" (stdin, for the input) and an existing
// directory are usage errors. A missing parent is created when the envelope
// is written.
func (c *Common) ResolveOutput() (string, error) {
	if c.Output == "" {
		return "", nil
	}
	if c.Output == "-" {
		return "", Usagef("--output cannot be %q: that means stdin for the input, not a file", "-")
	}
	abs, err := filepath.Abs(c.Output)
	if err != nil {
		return "", Usagef("resolving --output %q: %v", c.Output, err)
	}
	if info, serr := os.Stat(abs); serr == nil && info.IsDir() {
		return "", Usagef("--output %s is a directory, not a file", abs)
	}
	return abs, nil
}

// Bounds are the per-phase ceilings: the flags over the tool's defaults. A
// single-phase run folds --total-budget in, since the two then bound the
// same spend and the lower one is the phase's ceiling.
func (c *Common) Bounds(defaults agentrun.Bounds, singlePhase bool) agentrun.Bounds {
	b := defaults
	if c.MaxTurns > 0 {
		b.MaxTurns = c.MaxTurns
	}
	if c.Budget > 0 {
		b.MaxBudgetUSD = c.Budget
	}
	if c.Timeout > 0 {
		b.Timeout = c.Timeout
	}
	if singlePhase && c.TotalBudgetUSD > 0 && c.TotalBudgetUSD < b.ResolvedMaxBudget() {
		b.MaxBudgetUSD = c.TotalBudgetUSD
	}
	return b
}

// Workspace resolves --dir and roots the file tools at it.
func (c *Common) Workspace() (*workspace.Workspace, error) {
	abs, err := filepath.Abs(c.Dir)
	if err != nil {
		return nil, fmt.Errorf("resolving --dir %q: %w", c.Dir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("--dir %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("--dir %s is not a directory", abs)
	}
	ws, err := workspace.Open(abs, workspace.Options{})
	if err != nil {
		return nil, fmt.Errorf("rooting the file tools at %s: %w", abs, err)
	}
	return ws, nil
}

// ModelSpec is the model the run asks for, after the environment.
func (c *Common) ModelSpec() string {
	if c.Model != "" {
		return c.Model
	}
	if v := os.Getenv(ModelEnv); v != "" {
		return v
	}
	if v := os.Getenv(ModelEnvFallback); v != "" {
		return v
	}
	return DefaultModel
}

// ResolveModels resolves the model and effort of every phase: --model (or
// its environment) for all of them, --effort (then $AF_MODEL_EFFORT, then each
// phase's tier effort) for all of them, and each --phase-model for its own
// phase. An invalid environment value is a usage error, like an invalid flag
// value.
func (c *Common) ResolveModels() (agentrun.Plan, error) {
	raw, from := c.Effort, "--effort"
	if raw == "" {
		raw, from = os.Getenv(EffortEnv), "$"+EffortEnv
	}
	var effort core.Effort
	explicit := raw != ""
	if explicit {
		e, err := agentrun.ParseEffort(raw)
		if err != nil {
			return agentrun.Plan{}, Usagef("%s: %v", from, err)
		}
		effort = e
	}
	return agentrun.ResolvePlan(c.ModelSpec(), effort, explicit, c.PhaseModels)
}

// SplitArgs separates the one positional argument from the flags, allowing
// flags on either side of it. Anything after the first non-flag token that
// itself starts with "-" is still parsed as a flag, except the bare "-",
// which means stdin.
func SplitArgs(fs *flag.FlagSet, argv []string) (positional string, err error) {
	var rest, positionals []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "--":
			positionals = append(positionals, argv[i+1:]...)
			i = len(argv)
		case a == "-":
			positionals = append(positionals, a)
		case strings.HasPrefix(a, "-"):
			rest = append(rest, a)
			// "--name value" consumes the next token; "--name=value" and a
			// boolean flag do not.
			if !strings.Contains(a, "=") && i+1 < len(argv) && needsValue(fs, a) {
				i++
				rest = append(rest, argv[i])
			}
		default:
			positionals = append(positionals, a)
		}
	}
	if err := fs.Parse(rest); err != nil {
		return "", err
	}
	switch len(positionals) {
	case 0:
		return "", nil
	case 1:
		return positionals[0], nil
	default:
		return "", fmt.Errorf("expected one input, got %d: %s — quote an input that has spaces in it",
			len(positionals), strings.Join(positionals, " "))
	}
}

// needsValue reports whether a flag takes a separate value token.
func needsValue(fs *flag.FlagSet, arg string) bool {
	f := fs.Lookup(strings.TrimLeft(arg, "-"))
	if f == nil {
		return false // unknown: let Parse report it
	}
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return !ok || !bf.IsBoolFlag()
}
