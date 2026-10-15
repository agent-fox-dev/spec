package envelope

import (
	"bytes"
	"encoding/json"
	"errors"
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

// StatusFor maps an exit code to its status string: done, usage or failed.
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

// Envelope is the single JSON object a command writes to stdout. R is the
// type of the command's own result: SpecResult for spec, RenderResult for
// specr. Use a pointer type, as SpecEnvelope and RenderEnvelope do, to tell an
// absent result from an empty one; a failure that happened before there was
// anything to report has none.
type Envelope[R any] struct {
	Tool     string `json:"tool" description:"the command that wrote this envelope"`
	Version  string `json:"version" description:"the command's build identity"`
	OK       bool   `json:"ok" description:"true only when the command did the whole job it was asked to do; never true beside a non-zero exit_code or an error"`
	Status   string `json:"status" description:"the outcome in words, derived from exit_code: done (0), failed (1) or usage (2)"`
	ExitCode int    `json:"exit_code" description:"the process exit code: 0 done, 1 failed, 2 usage error"`
	Summary  string `json:"summary" description:"one sentence on the outcome, at most 200 characters"`

	Error       *ErrorInfo   `json:"error,omitempty" description:"what failed and where; present exactly when ok is false"`
	Warnings    []Warning    `json:"warnings,omitempty" description:"what went differently than intended without stopping the run"`
	Result      R            `json:"result,omitzero" description:"the command's own result"`
	Artifacts   []Artifact   `json:"artifacts,omitempty" description:"what the run produced, read off facts the command holds"`
	SideEffects []SideEffect `json:"side_effects,omitempty" description:"every write to a forge, in the order it happened"`
	Next        []Next       `json:"next,omitempty" description:"the invocations a caller would plausibly make next"`

	Input *InputInfo `json:"input,omitempty" description:"what the one positional argument turned out to be"`
	Model *ModelInfo `json:"model,omitempty" description:"the models the run used"`
	Usage *UsageInfo `json:"usage,omitempty" description:"the run's model cost, summed over every phase"`

	DurationMS int64  `json:"duration_ms" description:"wall-clock time of the run, in milliseconds"`
	StartedAt  string `json:"started_at" description:"when the run started, RFC 3339 in UTC"`
}

// SpecEnvelope is what the spec command writes.
type SpecEnvelope = Envelope[*SpecResult]

// RenderEnvelope is what the specr command writes.
type RenderEnvelope = Envelope[*RenderResult]

// Decode reads the envelope a command wrote. It fails when data is not one
// JSON object, or when a field has the wrong type; unknown fields are
// ignored. A command writes exactly one object, so data after it is an
// error too.
func Decode[R any](data []byte) (Envelope[R], error) {
	var env Envelope[R]
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&env); err != nil {
		return Envelope[R]{}, err
	}
	if dec.More() {
		return Envelope[R]{}, errors.New("envelope: data after the JSON object")
	}
	return env, nil
}

// DecodeSpec reads the envelope the spec command wrote. Under --detail
// summary the result is a trimmed view, which decodes into the same type with
// the fields it leaves out zero; SpecResult.Detail says which view it was.
func DecodeSpec(data []byte) (SpecEnvelope, error) { return Decode[*SpecResult](data) }

// DecodeSpecr reads the envelope the specr command wrote.
func DecodeSpecr(data []byte) (RenderEnvelope, error) { return Decode[*RenderResult](data) }

// ArtifactKind is the closed set of shapes an Artifact can take.
type ArtifactKind string

// The artifact kinds the commands produce.
const (
	ArtifactSpecPackage  ArtifactKind = "spec_package"
	ArtifactComment      ArtifactKind = "comment"
	ArtifactMarkdownFile ArtifactKind = "markdown_file"
)

// Artifact is one thing a run produced. Only the fields of its own Kind are
// written, and only those are read back, so a caller never has to know which
// fields to ignore.
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

// artifactWire is Artifact as it is written. The pointer fields are there to
// be left out: valid is false on an invalid package and must still appear.
type artifactWire struct {
	Kind   ArtifactKind `json:"kind"`
	Path   *string      `json:"path,omitempty"`
	ID     *string      `json:"id,omitempty"`
	Valid  *bool        `json:"valid,omitempty"`
	URL    *string      `json:"url,omitempty"`
	Role   string       `json:"role,omitempty"`
	DryRun bool         `json:"dry_run,omitempty"`
}

// MarshalJSON writes exactly the fields fixed for a's own Kind.
func (a Artifact) MarshalJSON() ([]byte, error) {
	w := artifactWire{Kind: a.Kind, DryRun: a.DryRun}
	switch a.Kind {
	case ArtifactSpecPackage:
		w.Path, w.ID, w.Valid = &a.Path, &a.ID, &a.Valid
	case ArtifactComment:
		w.URL, w.Role = &a.URL, a.Role
	case ArtifactMarkdownFile:
		w.Path = &a.Path
	}
	return compactJSON(w)
}

// UnmarshalJSON reads what MarshalJSON writes.
func (a *Artifact) UnmarshalJSON(data []byte) error {
	var w artifactWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*a = Artifact{Kind: w.Kind, Role: w.Role, DryRun: w.DryRun}
	if w.Path != nil {
		a.Path = *w.Path
	}
	if w.ID != nil {
		a.ID = *w.ID
	}
	if w.Valid != nil {
		a.Valid = *w.Valid
	}
	if w.URL != nil {
		a.URL = *w.URL
	}
	return nil
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

// Next is one suggested follow-up invocation: the tool, its single input, the
// flags to add, and why.
type Next struct {
	Tool  string   `json:"tool" description:"the command to run"`
	Input string   `json:"input" description:"its one positional input"`
	Flags []string `json:"flags" description:"the flags to add, each value its own element"`
	Why   string   `json:"why" description:"why a caller would run it"`
}

// SameInputPlaceholder stands for the original input in a suggested
// invocation whose input may be arbitrarily large (raw text, stdin).
const SameInputPlaceholder = "<same input>"

// Warning is one thing that went differently than intended but did not stop
// the run. Code is stable; Stage is looked up from the code.
type Warning struct {
	Code     WarnCode `json:"code" description:"a stable code naming the cause; an open set"`
	Severity string   `json:"severity" description:"high when the result is not quite what it appears to be, low when informational"`
	Stage    string   `json:"stage" description:"the step that raised it"`
	Message  string   `json:"message" description:"what happened, in words"`
}

// Warning severities.
const (
	// SeverityHigh means the result is not quite what it appears to be.
	SeverityHigh = "high"
	// SeverityLow means the warning is informational.
	SeverityLow = "low"
)

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

// compactJSON is json.Marshal without the escaping of <, > and &: the
// commands' output is read by programs and models, never embedded in HTML.
func compactJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte{'\n'}), nil
}
