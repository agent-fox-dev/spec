// Package agentrun runs one model phase of the spec pipeline on AgentKit's
// driver.
//
// A phase is a system prompt, a user prompt, the read-only file tools and one
// terminating tool whose arguments are the phase's result. Each phase is its
// own agentkit.Agent: sharing a transcript would carry one phase's reasoning
// into the next, and the next phase is meant to start from the first one's
// conclusion rather than from how it got there.
//
// The package is deliberately small. AgentKit owns the loop, the retries of a
// request that fails before its response starts (the Anthropic SDK's), the
// prompt cache breakpoints and the pruning of old tool results; this package
// owns what is specific to a spec phase: the per-phase ceilings, the tool
// grant, the repeat of a call that fails mid-stream, the project context, the
// error vocabulary the envelope reports and the progress a person watches.
package agentrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	sdk "github.com/anthropics/anthropic-sdk-go"

	agentkit "github.com/agent-fox-dev/agentkit-go"
	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/provider/anthropic"
	"github.com/agent-fox-dev/agentkit-go/tools"
	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/afspec"
)

// Bounds are the ceilings on one phase. They are not the model's to choose,
// and they fail differently: turns catch a model looping cheaply, the budget
// catches one reading large files a few expensive times, and the timeout
// catches a provider that never answers.
type Bounds struct {
	// MaxTurns bounds one phase. Zero means DefaultMaxTurns.
	//
	// It is also the repair budget. A schema or rule violation is a tool
	// error the loop hands back, so "how many attempts does the model get"
	// and "how many turns may this phase take" are the same number.
	MaxTurns int
	// MaxBudgetUSD bounds one phase's spend. Zero means DefaultMaxBudgetUSD.
	MaxBudgetUSD float64
	// Timeout is the wall-clock ceiling on one phase. Zero means no ceiling
	// beyond the caller's context.
	Timeout time.Duration
}

// Defaults applied when Bounds leaves a field at zero.
const (
	// DefaultMaxTurns is generous because a phase spends most of its turns
	// reading, and stingy enough that a model looping on a validation error
	// it cannot fix stops costing money.
	DefaultMaxTurns = 60
	// DefaultMaxBudgetUSD bounds one phase, not a whole run.
	DefaultMaxBudgetUSD = 5.00
)

// Prune ages old tool results out of the request once it reaches this
// fraction of the model's context window, keeping the last pruneKeepTurns
// turns whole. A phase that reads a dozen large files would otherwise fill
// the window before it reached its terminating tool.
const (
	pruneThreshold = 0.35
	pruneKeepTurns = 4
)

// ResolvedMaxTurns is the turn ceiling after defaulting.
func (b Bounds) ResolvedMaxTurns() int {
	if b.MaxTurns > 0 {
		return b.MaxTurns
	}
	return DefaultMaxTurns
}

// ResolvedMaxBudget is the spend ceiling after defaulting.
func (b Bounds) ResolvedMaxBudget() float64 {
	if b.MaxBudgetUSD > 0 {
		return b.MaxBudgetUSD
	}
	return DefaultMaxBudgetUSD
}

// Observer receives a phase's progress. The CLI's progress writer satisfies
// it; a test passes nil.
type Observer interface {
	// Detail reports one line of tracing, shown only under --verbose.
	Detail(format string, args ...any)
	// Raw writes model prose verbatim, shown only under --show-text.
	Raw(s string)
	// PhaseStart reports that a model phase begins, with the model and effort
	// it runs on and its ceilings.
	PhaseStart(phase, model string, maxTurns int, budgetUSD float64)
	// PhaseEnd reports that the phase ended.
	PhaseEnd(phase, stopReason string, turns int, costUSD float64, elapsed time.Duration)
	// Turn reports one finished model turn and the phase's spend so far.
	Turn(phase string, turn int, costUSD float64)
}

// Config is what a Runner is built with: everything that is the same for
// every phase of one run.
type Config struct {
	// Model is the resolved default model. Required.
	Model Model
	// PhaseModels holds the model of each phase kind that runs on another
	// one than Model (see Phases). A kind it does not hold runs on Model.
	PhaseModels map[string]Model
	// Client is the Anthropic SDK client every phase sends through. Required
	// unless Provider is set.
	Client *sdk.Client
	// Provider replaces Client: a test passes provider/faux here and needs no
	// network and no key.
	Provider core.ProviderClient
	// Workspace roots the file tools. Every path a tool is handed is
	// resolved against it, symlinks included, so a path that escapes is
	// refused by the tool rather than by a paragraph in a prompt.
	Workspace *workspace.Workspace
	// Index is the code-search index shared by every phase of the run. When
	// set, phases that grant ToolCodeSearch get code_search. The caller
	// closes it.
	Index tools.Index
	// TrustProject admits the repository's AGENTS.md and CLAUDE.md into the
	// system prompt. It is off by default because a repository that is merely
	// the current directory would otherwise author part of the prompt that
	// reads it.
	TrustProject bool
	// Bounds are the per-phase ceilings.
	Bounds Bounds
	// RetryDelays are the waits before each repeat of a model call that
	// failed on the way, once the SDK's own retries are spent or no longer
	// apply; their number is the number of repeats. Nil means
	// DefaultRetryDelays, an empty slice no repeats.
	RetryDelays []time.Duration
	// Observer receives progress. May be nil.
	Observer Observer
	// ShowText streams the model's own prose to the observer.
	ShowText bool
}

// Phase is one model-facing step.
type Phase struct {
	// Name identifies the step in errors, progress and the cost report.
	Name string
	// Kind is the phase's entry in Phases, which picks its model. Empty runs
	// on the default model.
	Kind string
	// System and User are the two prompts.
	System string
	User   string
	// Prefix is context sent before User, as the same user turn, with a
	// prompt-cache breakpoint of its own after it. Phases that share their
	// system prompt, their tools and this text share the cached prefix too,
	// so the context several phases read is paid for at the cache-read rate
	// after the first.
	Prefix string
	// RepoMap is the repository map the caller put into User. The runner
	// never injects it; the field exists so its size can be logged.
	RepoMap string
	// Terminator is the name of the tool that ends the phase by producing
	// its result. A run that ends without it is a failure, not a partial
	// success.
	Terminator string
	// Custom are the phase's own tools, the terminator included.
	Custom []core.Tool
	// BuiltinTools names the read-only built-in tools to grant: a subset of
	// ReadOnlyFileTools, plus ToolCodeSearch.
	BuiltinTools []string
	// MaxTokens caps one response. Zero leaves AgentKit's default.
	MaxTokens int
	// LoadProjectContext admits the project context files under
	// Config.TrustProject.
	LoadProjectContext bool
}

// Result is what a finished phase reports.
type Result struct {
	Name string
	// Model is the model and effort the phase ran on.
	Model      Model
	Turns      int
	StopReason core.RunStopReason
	Usage      core.Usage
	Elapsed    time.Duration
	// ToolErrors counts the tool calls that came back as errors, keyed "tool"
	// or "tool/code" when the error carries a code. Each one cost the model a
	// turn.
	ToolErrors map[string]int
	// ToolCalls counts the calls to each tool by name. Nil when there were
	// none.
	ToolCalls map[string]int
}

// Runner builds and drives one agent per phase.
type Runner struct {
	cfg Config
}

// NewRunner returns a Runner. The model must already be resolved and the
// client built: both happen before any prompt is, so an operator learns about
// a missing key in the first second.
func NewRunner(cfg Config) (*Runner, error) {
	if cfg.Model.ID() == "" {
		return nil, newError("", CategoryModel, nil, "no model resolved")
	}
	if cfg.Client == nil && cfg.Provider == nil {
		return nil, newError("", CategoryInternal, nil, "no model client configured")
	}
	return &Runner{cfg: cfg}, nil
}

// Model reports the default model: the one a phase with no model of its own
// runs on.
func (r *Runner) Model() Model { return r.cfg.Model }

// ModelFor reports the model a phase of the given kind runs on.
func (r *Runner) ModelFor(kind string) Model {
	if m, ok := r.cfg.PhaseModels[kind]; ok && m.ID() != "" {
		return m
	}
	return r.cfg.Model
}

// Index reports the run's code-search index, nil when there is none.
func (r *Runner) Index() tools.Index { return r.cfg.Index }

// ResolvedBounds reports the per-phase ceilings this Runner applies, for a
// caller that reports them before any phase runs.
func (r *Runner) ResolvedBounds() (maxTurns int, maxBudgetUSD float64) {
	return r.cfg.Bounds.ResolvedMaxTurns(), r.cfg.Bounds.ResolvedMaxBudget()
}

// Run drives one phase to its terminating tool.
//
// The (Result, error) pair is the honest return: a run can end without a
// result — budget, turn limit, a model that answered in prose — and the caller
// has to be able to tell that apart from a finished phase. Whether the
// terminator accepted a submission is the caller's sink to say; the error
// names a stop the phase could not get past.
func (r *Runner) Run(ctx context.Context, p Phase) (Result, error) {
	if o := r.cfg.Observer; o != nil {
		o.PhaseStart(p.Name, r.ModelFor(p.Kind).String(), r.cfg.Bounds.ResolvedMaxTurns(), r.cfg.Bounds.ResolvedMaxBudget())
	}
	out, err := r.run(ctx, p)
	out.Model = r.ModelFor(p.Kind)
	if o := r.cfg.Observer; o != nil {
		o.PhaseEnd(p.Name, string(out.StopReason), out.Turns, out.Usage.CostUSD, out.Elapsed)
	}
	return out, err
}

func (r *Runner) run(ctx context.Context, p Phase) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{Name: p.Name}, newError(p.Name, CategoryAborted, err, "%v", err)
	}
	if p.Terminator == "" {
		return Result{Name: p.Name}, newError(p.Name, CategoryInternal, nil, "phase declares no terminating tool")
	}
	if p.RepoMap != "" {
		r.detail("repo map: %d tokens", afspec.EstimateTokens(p.RepoMap))
	}

	agent, err := r.newAgent(p)
	if err != nil {
		return Result{Name: p.Name}, err
	}

	start := time.Now()
	stream, err := agent.Stream(ctx, p.User)
	if err != nil {
		return Result{Name: p.Name, Elapsed: time.Since(start)}, r.wrap(p, core.RunResult{}, err)
	}
	tr := tracer{r: r, phase: p.Name}
	for e := range stream.Events() {
		tr.event(e)
	}
	res, runErr := stream.RunResult()
	out := Result{
		Name:       p.Name,
		Turns:      res.TurnCount,
		StopReason: res.StopReason,
		Usage:      res.Usage,
		Elapsed:    time.Since(start),
		ToolErrors: tr.errs.snapshot(),
		ToolCalls:  tr.calls.snapshot(),
	}
	return out, r.wrap(p, res, runErr)
}

// newAgent configures the agent for one phase.
func (r *Runner) newAgent(p Phase) (*agentkit.Agent, error) {
	granted, err := r.grantedTools(p)
	if err != nil {
		return nil, err
	}
	system := p.System
	if p.LoadProjectContext && r.cfg.TrustProject && r.cfg.Workspace != nil {
		system += projectContextBlock(r.cfg.Workspace.Root())
	}
	system += toolsNote(granted)
	model := r.ModelFor(p.Kind)

	agent, err := agentkit.New(agentkit.Config{
		Client:     r.cfg.Client,
		Provider:   r.providerFor(model, p),
		Model:      model.ID(),
		Effort:     model.Effort,
		System:     system,
		Toolset:    agentkit.Toolset{Tools: granted},
		Prefix:     prefixMessages(p.Prefix),
		MaxTurns:   r.cfg.Bounds.ResolvedMaxTurns(),
		MaxCostUSD: r.cfg.Bounds.ResolvedMaxBudget(),
		Timeout:    r.cfg.Bounds.Timeout,
		Prune:      agentkit.PruneOptions{Threshold: pruneThreshold, KeepTurns: pruneKeepTurns},
		MaxTokens:  p.MaxTokens,
	})
	if err != nil {
		return nil, newError(p.Name, CategoryInternal, err, "building the agent: %v", err)
	}
	return agent, nil
}

// providerFor is the provider a phase sends through: the configured one, or
// the Anthropic provider the driver would have built over the client, with the
// phase's built-in tools kept out of strict mode.
//
// The API totals its strict-mode limits over a request: the number of strict
// tools, their union parameters, and a compiled grammar size it does not
// publish. A request past any is refused whole. A generation phase's artifact
// tool is the largest schema in the request, and was refused beside four file
// tools (the PRD, test spec and tasks phases fit, with no margin). The file
// tools take a path and a line range, are validated when they arrive, and gain
// little from the API's guarantee that the first call is valid, so the budget
// is left to the phase's own tools, which are declared first.
//
// Either is wrapped so that a call that fails on the way is sent again (see
// retrying).
func (r *Runner) providerFor(model Model, p Phase) core.ProviderClient {
	delays := r.cfg.RetryDelays
	if delays == nil {
		delays = DefaultRetryDelays
	}
	inner := r.cfg.Provider
	if inner == nil {
		inner = anthropic.Provider(model.Row, anthropic.Options{Client: r.cfg.Client, NonStrictTools: p.BuiltinTools})
	}
	return &retrying{inner: inner, delays: delays}
}

// prefixMessages is a phase's Prefix as the driver takes it: one user
// message, or none.
func prefixMessages(prefix string) []core.Message {
	if strings.TrimSpace(prefix) == "" {
		return nil
	}
	return []core.Message{core.UserMessage{Content: core.Content{core.TextBlock{Text: prefix}}}}
}

// grantedTools is the set one phase declares: its own tools, then the named
// read-only built-ins. A shell tool can never be granted, so no phase needs a
// Guard, and agentkit.New refuses the run if one ever slipped through.
func (r *Runner) grantedTools(p Phase) ([]core.Tool, error) {
	out := append([]core.Tool(nil), p.Custom...)
	if len(p.BuiltinTools) == 0 {
		return out, nil
	}
	for _, name := range p.BuiltinTools {
		if name != ToolCodeSearch && !slices.Contains(ReadOnlyFileTools, name) {
			return nil, newError(p.Name, CategoryInternal, nil, "phase asks for %q, which is not a read-only tool", name)
		}
	}
	if r.cfg.Workspace == nil {
		return nil, newError(p.Name, CategoryInternal, nil, "phase asks for built-in tools but no workspace is configured")
	}
	built, err := tools.For(r.cfg.Workspace, tools.Options{Index: r.cfg.Index})
	if err != nil {
		return nil, newError(p.Name, CategoryInternal, err, "building the file tools: %v", err)
	}
	for _, t := range built {
		if slices.Contains(p.BuiltinTools, t.Name) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (r *Runner) detail(format string, args ...any) {
	if r.cfg.Observer != nil {
		r.cfg.Observer.Detail(format, args...)
	}
}

// wrap turns a finished run into this package's error vocabulary. The
// category is derived from the RunStopReason the driver set, not from a
// stop_reason string parsed out of one response.
func (r *Runner) wrap(p Phase, res core.RunResult, err error) error {
	if err == nil {
		return nil
	}
	var already *Error
	if errors.As(err, &already) {
		return already
	}
	category := CategoryAPI
	switch {
	case res.StopReason == core.RunStopMaxTurns || errors.Is(err, core.ErrMaxTurns):
		category = CategoryMaxTurns
	case res.StopReason == core.RunStopBudgetExceeded || errors.Is(err, core.ErrBudgetExceeded):
		category = CategoryBudget
	case res.StopReason == core.RunStopRefusal || errors.Is(err, core.ErrRefusal):
		category = CategoryRefusal
	case res.StopReason == core.RunStopAborted, res.StopReason == core.RunStopTimeout,
		errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		category = CategoryAborted
	}
	e := newError(p.Name, category, err, "%v", err)
	e.StopReason = res.StopReason
	return e
}

// NoResultError is the error a caller reports when a phase finished but its
// terminator never accepted a submission. It is separated from a transport
// failure because the remedy is different: raise the bounds, or narrow the
// task.
func NoResultError(phase, terminator string, res Result) error {
	hint := "raise --max-turns or --budget, or narrow the input"
	switch res.StopReason {
	case core.RunStopBudgetExceeded:
		hint = "raise --budget, or choose a cheaper model"
	case core.RunStopMaxTurns:
		hint = "raise --max-turns; the model was still working"
	}
	err := newError(phase, CategoryNoResult, nil,
		"the phase ended (%s) without calling %s: %s", res.StopReason, terminator, hint)
	err.StopReason = res.StopReason
	return err
}

// tracer turns a phase's event stream into progress and counts.
type tracer struct {
	r     *Runner
	phase string
	turn  int
	cost  float64
	errs  counter
	calls counter
}

func (t *tracer) event(e core.Event) {
	r := t.r
	switch v := e.(type) {
	case core.TurnEndEvent:
		t.turn++
		t.cost += v.Usage.CostUSD
		if r.cfg.Observer != nil {
			r.cfg.Observer.Turn(t.phase, t.turn, t.cost)
		}
	case core.ToolCallEndEvent:
		r.detail("→ %s %s", v.Block.Name, firstLine(string(v.Block.Input), 100))
	case core.ToolExecutionEndEvent:
		if v.ParentToolUseID != "" {
			break
		}
		status := "ok"
		if v.IsError {
			status = "ERROR"
		}
		r.detail("← %s: %s (%dms)", v.Name, status, v.ElapsedMS)
	case core.ToolResultEvent:
		t.calls.add(v.Message.ToolName)
		if v.Message.IsError {
			// Counted here and not at ToolExecutionEndEvent: a call to a tool
			// that does not exist never executes, and still costs a turn.
			t.errs.add(errorKey(v.Message.ToolName, v.Message.Content.Text()))
			r.detail("   %s", firstLine(v.Message.Content.Text(), 160))
		}
	case core.TextDeltaEvent:
		if r.cfg.ShowText && r.cfg.Observer != nil {
			r.cfg.Observer.Raw(v.Delta)
		}
	case core.TextEndEvent:
		if r.cfg.ShowText && r.cfg.Observer != nil {
			r.cfg.Observer.Raw("\n")
		}
	case core.ErrorEvent:
		r.detail("[stream error] %s", v.Message)
	}
}

// errorKey is "tool/code" when a failed result's JSON names an error code,
// and "tool" otherwise.
func errorKey(tool, text string) string {
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(text), &body) == nil && body.Error != "" {
		return tool + "/" + body.Error
	}
	return tool
}

// counter counts by key across the goroutines a batch runs on.
type counter struct {
	mu sync.Mutex
	n  map[string]int
}

func (c *counter) add(key string) {
	c.mu.Lock()
	if c.n == nil {
		c.n = map[string]int{}
	}
	c.n[key]++
	c.mu.Unlock()
}

func (c *counter) snapshot() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.n) == 0 {
		return nil
	}
	out := make(map[string]int, len(c.n))
	for k, v := range c.n {
		out[k] = v
	}
	return out
}

func firstLine(s string, limit int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if limit > 0 && len(s) > limit {
		// Cut on a rune boundary: a byte offset can land inside a multi-byte
		// character and leave invalid UTF-8 in the log.
		cut := limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		return fmt.Sprintf("%s…[+%d bytes]", s[:cut], len(s)-cut)
	}
	return s
}

// toolsNote closes a phase's system prompt with the tools it has, generated
// from the granted set so it cannot drift from it. A model that is not told
// there is no shell reaches for `bash` and loses a turn to an unknown-tool
// error. The tools' own guidelines follow it: AgentKit's prompt builder
// renders them after the system prompt.
func toolsNote(granted []core.Tool) string {
	names := make([]string, 0, len(granted))
	for _, t := range granted {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	return "\n\nYour tools are exactly: " + strings.Join(names, ", ") + ". Calling any other tool is an error." +
		" There is no shell: read with the file tools, and do not call `bash`, `execute` or `run_command`." +
		" When you know several files or searches you need, ask for them in one response: calls made" +
		" together run together, and every extra turn re-reads the whole conversation."
}
