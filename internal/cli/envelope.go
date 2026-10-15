package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/agent-fox-dev/spec/internal/agentrun"
)

// Exit codes. Something other than a human reads them.
const (
	// ExitOK means the tool did what it was asked.
	ExitOK = 0
	// ExitFailed means a step errored. The failing stage is named in the
	// envelope's error object.
	ExitFailed = 1
	// ExitUsage means the invocation was wrong: no input, an undefined flag,
	// a flag combination that cannot hold. Nothing was fetched and nothing
	// was written.
	ExitUsage = 2
)

// StatusFor maps an exit code to its status string.
func StatusFor(code int) string {
	switch code {
	case ExitOK:
		return "done"
	case ExitUsage:
		return "usage"
	default:
		return "failed"
	}
}

// ExitCodeFor maps an error category onto the exit table.
func ExitCodeFor(category string) int {
	if category == "usage" {
		return ExitUsage
	}
	return ExitFailed
}

// Summarizer is implemented by a result that supplies a one-sentence summary
// of the run.
type Summarizer interface {
	Summary() string
}

// Resumabler is implemented by a result that reports whether re-running the
// same input continues rather than restarts the work.
type Resumabler interface {
	Resumable() bool
}

// Summarizable is implemented by a result that supplies a trimmed view of
// itself, the one printed under --detail summary.
type Summarizable interface {
	SummaryView() any
}

// DetailedResult is implemented by a result that records which view
// ("summary" or "full") was emitted.
type DetailedResult interface {
	SetDetail(string)
}

// ArtifactsProvider is implemented by a result that lists what the run
// produced. Every entry is read off a fact the result holds, never off the
// model's account of its own work.
type ArtifactsProvider interface {
	Artifacts() []Artifact
}

// NextProvider is implemented by a result that suggests the invocations a
// caller would plausibly make next.
type NextProvider interface {
	Next() []Next
}

// ArtifactKind is the closed set of shapes an Artifact can take.
type ArtifactKind string

// The artifact kinds the commands produce.
const (
	ArtifactSpecPackage  ArtifactKind = "spec_package"
	ArtifactComment      ArtifactKind = "comment"
	ArtifactMarkdownFile ArtifactKind = "markdown_file"
)

// Artifact is one thing a run produced. MarshalJSON emits only the fields of
// its own Kind, so a caller never has to know which fields to ignore.
type Artifact struct {
	Kind ArtifactKind
	// spec_package, markdown_file
	Path  string
	ID    string
	Valid bool
	// comment
	URL  string
	Role string
	// DryRun marks an entry as hypothetical: it would have been written but
	// for --dry-run.
	DryRun bool
}

// MarshalJSON emits exactly the fields fixed for a's own Kind.
func (a Artifact) MarshalJSON() ([]byte, error) {
	m := map[string]any{"kind": string(a.Kind)}
	switch a.Kind {
	case ArtifactSpecPackage:
		m["path"] = a.Path
		m["id"] = a.ID
		m["valid"] = a.Valid
	case ArtifactComment:
		m["url"] = a.URL
		if a.Role != "" {
			m["role"] = a.Role
		}
	case ArtifactMarkdownFile:
		m["path"] = a.Path
	}
	if a.DryRun {
		m["dry_run"] = true
	}
	return compactJSON(m)
}

// JSONSchema describes what MarshalJSON writes: the fields of the entry's
// own kind.
func (Artifact) JSONSchema() SchemaObject {
	prop := func(typ, desc string) SchemaObject {
		return SchemaObject{{Key: "type", Value: typ}, {Key: "description", Value: desc}}
	}
	kind := prop("string", "what the entry is; its other fields are the ones of its kind")
	kind.Set("enum", []string{string(ArtifactSpecPackage), string(ArtifactComment), string(ArtifactMarkdownFile)})
	return SchemaObject{
		{Key: "type", Value: "object"},
		{Key: "properties", Value: SchemaObject{
			{Key: "kind", Value: kind},
			{Key: "path", Value: prop("string", "spec_package, markdown_file: the path written")},
			{Key: "id", Value: prop("string", "spec_package: the spec id")},
			{Key: "valid", Value: prop("boolean", "spec_package: whether the package validates")},
			{Key: "url", Value: prop("string", "comment: the comment's URL")},
			{Key: "role", Value: prop("string", "comment: which comment it is")},
			{Key: "dry_run", Value: prop("boolean", "true when it would have been written but for --dry-run")},
		}},
		{Key: "required", Value: []string{"kind"}},
	}
}

// SideEffect is one write a run made to a forge, in the order it happened.
type SideEffect struct {
	Action  string   `json:"action"`
	Kind    string   `json:"kind,omitempty"`
	Target  string   `json:"target"`
	URL     string   `json:"url,omitempty"`
	OK      bool     `json:"ok"`
	Warning WarnCode `json:"warning,omitempty"`
}

// SameInputPlaceholder stands for the original input in a suggested
// invocation whose input may be arbitrarily large (raw text, stdin).
const SameInputPlaceholder = "<same input>"

// Next is one suggested follow-up invocation: the tool, its single input, the
// flags to add, and why.
type Next struct {
	Tool  string   `json:"tool" description:"the command to run"`
	Input string   `json:"input" description:"its one positional input"`
	Flags []string `json:"flags" description:"the flags to add, each value its own element"`
	Why   string   `json:"why" description:"why a caller would run it"`
}

// ResumePlaceholder is how a next[] entry names in to re-run it: the literal
// origin (a file path or an issue URL) when the input had a small, stable
// one, and SameInputPlaceholder when it was raw text or stdin.
func ResumePlaceholder(in Input) string {
	switch in.Kind {
	case KindFile, KindIssue:
		if in.Origin != "" {
			return in.Origin
		}
	}
	return SameInputPlaceholder
}

// Warning is one thing that went differently than intended but did not stop
// the run. Code is stable; Stage is looked up from the code.
type Warning struct {
	Code     WarnCode `json:"code" description:"a stable code naming the cause; an open set"`
	Severity string   `json:"severity" description:"high when the result is not quite what it appears to be, low when informational"`
	Stage    string   `json:"stage" description:"the step that raised it"`
	Message  string   `json:"message" description:"what happened, in words"`
}

// PreflightCheck is one check --preflight performed and its outcome. A check
// whose failure refuses the run never produces an entry: the run fails
// instead, exactly as it would without --preflight.
type PreflightCheck struct {
	Check  string `json:"check"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Estimate bounds what the real run would spend at most.
type Estimate struct {
	Phases               int     `json:"phases"`
	MaxTurnsPerPhase     int     `json:"max_turns_per_phase"`
	MaxBudgetPerPhaseUSD float64 `json:"max_budget_per_phase_usd"`
	MaxTotalUSD          float64 `json:"max_total_usd"`
}

// Envelope is the single JSON object spec writes to stdout, on every
// program-driven path including the failing ones: a caller that is a program
// should not have to parse two formats and guess which one it got.
type Envelope struct {
	Tool     string `json:"tool" description:"the command that wrote this envelope"`
	Version  string `json:"version" description:"the command's build identity"`
	OK       bool   `json:"ok" description:"true only when the command did the whole job it was asked to do; never true beside a non-zero exit_code or an error"`
	Status   string `json:"status" description:"the outcome in words, derived from exit_code: done (0), failed (1) or usage (2)"`
	ExitCode int    `json:"exit_code" description:"the process exit code: 0 done, 1 failed, 2 usage error"`
	Summary  string `json:"summary" description:"one sentence on the outcome, at most 200 characters"`

	Error       *ErrorInfo   `json:"error,omitempty" description:"what failed and where; present exactly when ok is false"`
	Warnings    []Warning    `json:"warnings,omitempty" description:"what went differently than intended without stopping the run"`
	Result      any          `json:"result,omitempty" description:"the command's own result"`
	Artifacts   []Artifact   `json:"artifacts,omitempty" description:"what the run produced, read off facts the command holds"`
	SideEffects []SideEffect `json:"side_effects,omitempty" description:"every write to a forge, in the order it happened"`
	Next        []Next       `json:"next,omitempty" description:"the invocations a caller would plausibly make next"`

	Input *InputInfo `json:"input,omitempty" description:"what the one positional argument turned out to be"`
	Model *ModelInfo `json:"model,omitempty" description:"the models the run used"`
	Usage *UsageInfo `json:"usage,omitempty" description:"the run's model cost, summed over every phase"`

	DurationMS int64  `json:"duration_ms" description:"wall-clock time of the run, in milliseconds"`
	StartedAt  string `json:"started_at" description:"when the run started, RFC 3339 in UTC"`
}

// InputInfo records what the single argument turned out to be.
type InputInfo struct {
	Kind              string `json:"kind"`
	Origin            string `json:"origin"`
	Bytes             int    `json:"bytes"`
	Truncated         bool   `json:"truncated,omitempty"`
	CommentsTruncated bool   `json:"comments_truncated,omitempty"`
	ContextBytes      int    `json:"context_bytes,omitempty"`
}

// ModelInfo records which models served the run: what --model resolved to,
// and the model and effort of every phase.
type ModelInfo struct {
	Spec   string                    `json:"spec"`
	ID     string                    `json:"id"`
	Effort string                    `json:"effort,omitempty"`
	Phases map[string]PhaseModelInfo `json:"phases,omitempty"`
}

// PhaseModelInfo is the model one phase runs on.
type PhaseModelInfo struct {
	ID     string `json:"id"`
	Effort string `json:"effort,omitempty"`
}

// UsageInfo is the run's cost, summed over every phase.
type UsageInfo struct {
	InputTokens         int64       `json:"input_tokens"`
	OutputTokens        int64       `json:"output_tokens"`
	CacheReadTokens     int64       `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int64       `json:"cache_creation_tokens,omitempty"`
	CostUSD             float64     `json:"cost_usd"`
	Turns               int         `json:"turns"`
	Phases              []PhaseInfo `json:"phases,omitempty"`
}

// PhaseInfo is one model-facing step. InputTokens is net of the prompt
// cache; the cache figures are what make it agree with the cost.
type PhaseInfo struct {
	Name                string         `json:"name"`
	Scope               string         `json:"scope,omitempty"`
	Model               string         `json:"model,omitempty"`
	Effort              string         `json:"effort,omitempty"`
	Turns               int            `json:"turns"`
	StopReason          string         `json:"stop_reason"`
	InputTokens         int64          `json:"input_tokens"`
	OutputTokens        int64          `json:"output_tokens"`
	CacheReadTokens     int64          `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int64          `json:"cache_creation_tokens,omitempty"`
	CostUSD             float64        `json:"cost_usd"`
	DurationMS          int64          `json:"duration_ms"`
	ToolCalls           map[string]int `json:"tool_calls,omitempty"`
	ToolErrors          map[string]int `json:"tool_errors,omitempty"`
}

// PhaseFromResult is the envelope's record of one finished phase. scope
// names the spec the phase worked on and may be empty.
func PhaseFromResult(res agentrun.Result, scope string) PhaseInfo {
	return PhaseInfo{
		Name:                res.Name,
		Scope:               scope,
		Model:               res.Model.ID(),
		Effort:              string(res.Model.Effort),
		Turns:               res.Turns,
		StopReason:          string(res.StopReason),
		InputTokens:         res.Usage.InputTokens,
		OutputTokens:        res.Usage.OutputTokens,
		CacheReadTokens:     res.Usage.CacheReadTokens,
		CacheCreationTokens: res.Usage.CacheWriteTokens,
		CostUSD:             res.Usage.CostUSD,
		DurationMS:          res.Elapsed.Milliseconds(),
		ToolCalls:           res.ToolCalls,
		ToolErrors:          res.ToolErrors,
	}
}

// PhaseSummary is the footer a phase prints when it ends: turns, tokens in
// and out, and the cached tokens when there were any.
func PhaseSummary(res agentrun.Result) string {
	s := fmt.Sprintf("· %d turns · %s↑", res.Turns, FormatTokens(res.Usage.InputTokens))
	if cached := res.Usage.CacheReadTokens + res.Usage.CacheWriteTokens; cached > 0 {
		s += fmt.Sprintf(" (+%s cached)", FormatTokens(cached))
	}
	s += fmt.Sprintf(" %s↓ · $%.2f", FormatTokens(res.Usage.OutputTokens), res.Usage.CostUSD)
	return s
}

// FixHint is a machine-usable remedy for a failure.
type FixHint struct {
	Flag    string   `json:"flag,omitempty" description:"the flag to change"`
	Env     []string `json:"env,omitempty" description:"the environment variables, one of which supplies what is missing"`
	Valid   []string `json:"valid,omitempty" description:"the values the flag accepts here"`
	Current float64  `json:"current,omitempty" description:"the flag's value on this run"`
	Suggest float64  `json:"suggest,omitempty" description:"a value to try instead"`
}

// ErrorInfo says what failed and where. It is present exactly when ok is
// false.
type ErrorInfo struct {
	// Stage is the pipeline step that failed: usage, input, preflight, prd,
	// requirements, test_spec, tasks, plan, scaffold, write, validate, budget.
	Stage string `json:"stage" description:"the step that failed"`
	// Category says whether re-running could help.
	Category  string   `json:"category" description:"the kind of failure, which says whether re-running could help; an open set"`
	Message   string   `json:"message" description:"what failed, in words"`
	Retryable bool     `json:"retryable" description:"whether re-running the identical command could succeed"`
	Resumable bool     `json:"resumable" description:"whether re-running the same input continues the work rather than starting it over"`
	FixHint   *FixHint `json:"fix_hint,omitempty" description:"a mechanical remedy, where one is known"`
	err       error
}

func (e *ErrorInfo) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *ErrorInfo) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// TotalBudgetUSD is the --total-budget ceiling a budget stop was checked
// against, read from the failure it wraps; zero for any other failure.
func (e *ErrorInfo) TotalBudgetUSD() float64 {
	var tb interface{ TotalBudgetUSD() float64 }
	if e == nil || e.err == nil || !errors.As(e.err, &tb) {
		return 0
	}
	return tb.TotalBudgetUSD()
}

// RetryableFor reports whether an error of the given category can succeed if
// re-run unchanged. Only "api" and "aborted" are retryable.
func RetryableFor(category string) bool {
	return category == "api" || category == "aborted"
}

// StageError is a pipeline failure carrying a stage and a category.
type StageError interface {
	error
	StageName() string
	CategoryName() string
}

// ErrorFrom builds the envelope's error object from a pipeline failure. A
// failure that carries no stage is reported at defaultStage, and one that
// carries no category as internal, which is the honest default.
func ErrorFrom(defaultStage string, err error) *ErrorInfo {
	info := &ErrorInfo{Stage: defaultStage, Message: err.Error(), err: err}
	var se StageError
	if errors.As(err, &se) {
		info.Stage, info.Category = se.StageName(), se.CategoryName()
	} else {
		info.Category = agentrun.CategoryOf(err)
	}
	return info
}

// Run is the accumulating state of one invocation: what the pipeline reports
// into and what the envelope is built from. It is safe for concurrent use.
type Run struct {
	tool    string
	version string
	started time.Time

	mu       sync.Mutex
	warnings []Warning
	effects  []SideEffect
	phases   []PhaseInfo
	model    *ModelInfo
	input    *InputInfo
	bounds   agentrun.Bounds
}

// NewRun starts a run's bookkeeping.
func NewRun(tool, version string) *Run {
	return &Run{tool: tool, version: version, started: time.Now()}
}

// SetBounds records the resolved per-phase ceilings, for fix hints.
func (r *Run) SetBounds(b agentrun.Bounds) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bounds = b
}

// SetInput records the classified input.
func (r *Run) SetInput(in Input) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.input = &InputInfo{
		Kind:              in.Kind.String(),
		Origin:            in.Origin,
		Bytes:             len(in.Body),
		Truncated:         in.Truncated,
		CommentsTruncated: in.CommentsTruncated,
		ContextBytes:      len(in.Context),
	}
}

// SetModels records the resolved models.
func (r *Run) SetModels(plan agentrun.Plan) {
	if r == nil {
		return
	}
	info := &ModelInfo{Spec: plan.Default.Spec, ID: plan.Default.ID(), Effort: string(plan.Default.Effort),
		Phases: make(map[string]PhaseModelInfo, len(plan.Phases))}
	for phase, m := range plan.Phases {
		info.Phases[phase] = PhaseModelInfo{ID: m.ID(), Effort: string(m.Effort)}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.model = info
}

// Warn records a non-fatal problem. code's stage is looked up, never passed
// in, so a code cannot be recorded against two stages.
func (r *Run) Warn(code WarnCode, severity string, format string, args ...any) {
	if r == nil {
		return
	}
	stage, _ := WarnStage(code)
	w := Warning{Code: code, Severity: severity, Stage: stage, Message: strings.TrimSpace(fmt.Sprintf(format, args...))}
	r.mu.Lock()
	r.warnings = append(r.warnings, w)
	r.mu.Unlock()
}

// RecordSideEffect records one write to a forge, at the one place it
// happens, beside the Warn the same site makes on failure. An ok entry never
// carries a warning, and a failed one never a URL.
func (r *Run) RecordSideEffect(action, kind, target, url string, ok bool, warn WarnCode) {
	if r == nil {
		return
	}
	if ok {
		warn = ""
	} else {
		url = ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.effects = append(r.effects, SideEffect{Action: action, Kind: kind, Target: target, URL: url, OK: ok, Warning: warn})
}

// AddPhase records one model-facing step's cost. Five or more failed tool
// calls in one phase raise a tool_errors warning.
func (r *Run) AddPhase(p PhaseInfo) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.phases = append(r.phases, p)
	r.mu.Unlock()
	if n := p.toolErrorTotal(); n >= ToolErrorWarnThreshold {
		r.Warn(WarnToolErrors, "low", "the %s phase made %d tool calls that failed (%s); each cost a turn",
			p.Name, n, p.toolErrorSummary())
	}
}

// ToolErrorWarnThreshold is how many failed tool calls in one phase make the
// run say so.
const ToolErrorWarnThreshold = 5

func (p PhaseInfo) toolErrorTotal() int {
	n := 0
	for _, v := range p.ToolErrors {
		n += v
	}
	return n
}

func (p PhaseInfo) toolErrorSummary() string {
	keys := make([]string, 0, len(p.ToolErrors))
	for k := range p.ToolErrors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s ×%d", k, p.ToolErrors[k]))
	}
	return strings.Join(parts, ", ")
}

// CostUSD is the cumulative spend of every phase recorded so far: what
// --total-budget is checked against between phases.
func (r *Run) CostUSD() float64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var total float64
	for _, p := range r.phases {
		total += p.CostUSD
	}
	return total
}

// Warnings returns a copy of the warnings recorded so far.
func (r *Run) Warnings() []Warning {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Warning(nil), r.warnings...)
}

// WarningMessages returns the message text of each warning, in order.
func WarningMessages(ws []Warning) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.Message
	}
	return out
}

// SideEffects returns a copy of the forge writes recorded so far, in order.
func (r *Run) SideEffects() []SideEffect {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]SideEffect(nil), r.effects...)
}

// summaryMax is the longest summary, in characters.
const summaryMax = 200

// Envelope assembles the object to print from the full result. code decides
// OK: only ExitOK is a success, so ok can never be true beside a non-zero
// exit code or an error object.
func (r *Run) Envelope(code int, result any, failure *ErrorInfo) Envelope {
	r.mu.Lock()
	bounds := r.bounds
	env := Envelope{
		Tool:       r.tool,
		Version:    r.version,
		OK:         code == ExitOK && failure == nil,
		Status:     StatusFor(code),
		ExitCode:   code,
		Error:      failure,
		Input:      r.input,
		Model:      r.model,
		Result:     presentOrNil(result),
		Warnings:   append([]Warning(nil), r.warnings...),
		DurationMS: time.Since(r.started).Milliseconds(),
		StartedAt:  r.started.UTC().Format(time.RFC3339),
	}
	if len(r.effects) > 0 {
		env.SideEffects = append([]SideEffect(nil), r.effects...)
	}
	if len(r.phases) > 0 {
		u := &UsageInfo{Phases: append([]PhaseInfo(nil), r.phases...)}
		for _, p := range r.phases {
			u.InputTokens += p.InputTokens
			u.OutputTokens += p.OutputTokens
			u.CacheReadTokens += p.CacheReadTokens
			u.CacheCreationTokens += p.CacheCreationTokens
			u.CostUSD += p.CostUSD
			u.Turns += p.Turns
		}
		env.Usage = u
	}
	r.mu.Unlock()

	if code == ExitOK && failure != nil {
		env.ExitCode, env.Status = ExitFailed, StatusFor(ExitFailed)
	}
	if ap, ok := env.Result.(ArtifactsProvider); ok {
		env.Artifacts = ap.Artifacts()
	}
	if np, ok := env.Result.(NextProvider); ok {
		env.Next = np.Next()
	}
	env.Summary = summarize(env)

	if env.Error != nil {
		e := *env.Error
		e.Retryable = RetryableFor(e.Category)
		if res, ok := env.Result.(Resumabler); ok {
			e.Resumable = res.Resumable()
		}
		if e.FixHint == nil {
			var cause error = &e
			if e.err != nil {
				cause = e.err
			}
			e.FixHint = FixHintFor(e.Category, e.Stage, bounds, cause)
		}
		env.Error = &e
	}
	return env
}

// summarize is the result's own sentence, or a fallback, with a clause
// counting high-severity warnings on a success, capped at summaryMax
// characters.
func summarize(env Envelope) string {
	var summary string
	if s, ok := env.Result.(Summarizer); ok {
		summary = strings.TrimSpace(s.Summary())
	}
	if summary == "" {
		switch {
		case env.OK:
			summary = env.Tool + ": done"
		case env.Error != nil && env.Error.Message != "":
			summary = env.Error.Message
		default:
			summary = env.Tool + ": " + env.Status
		}
	}
	high := 0
	for _, w := range env.Warnings {
		if w.Severity == "high" {
			high++
		}
	}
	clause := ""
	switch {
	case env.OK && high == 1:
		clause = "; 1 high-severity warning"
	case env.OK && high > 1:
		clause = fmt.Sprintf("; %d high-severity warnings", high)
	}
	if room := summaryMax - utf8.RuneCountInString(clause); utf8.RuneCountInString(summary) > room {
		summary = string([]rune(summary)[:room])
	}
	return summary + clause
}

// Emit writes the envelope as one indented JSON document and a newline, and
// returns the exit code.
func Emit(w io.Writer, env Envelope) int {
	return EmitWithOutput(w, "", env)
}

// marshalEnvelope renders env as indented JSON and a trailing newline, with
// the exit code to report. Marshalling cannot be allowed to lose the exit
// code, so a result that cannot be encoded falls back to an envelope that
// can, carrying the failure, and ExitFailed.
func marshalEnvelope(env Envelope) ([]byte, int) {
	b, err := EncodeJSON(env)
	if err != nil {
		msg := "the result could not be encoded as JSON: " + err.Error()
		fallback, _ := EncodeJSON(Envelope{
			Tool: env.Tool, Version: env.Version, OK: false, Status: StatusFor(ExitFailed), ExitCode: ExitFailed,
			Summary: msg,
			Error:   &ErrorInfo{Stage: "emit", Category: agentrun.CategoryInternal, Message: msg},
		})
		return fallback, ExitFailed
	}
	return b, env.ExitCode
}

// EncodeJSON renders v as indented JSON and a trailing newline, the way
// every document on stdout is written. It does not escape <, > and & the way
// encoding/json does by default: the output is read by programs and models,
// never embedded in HTML, and Markdown in a string is full of them.
func EncodeJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// compactJSON is EncodeJSON on one line and without the newline, for a
// MarshalJSON method.
func compactJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte{'\n'}), nil
}

// presentOrNil drops a typed nil pointer, so a pipeline that failed before
// it had a result prints no "result": null.
func presentOrNil(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
	}
	return v
}
