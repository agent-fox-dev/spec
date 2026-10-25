package cli

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agent-fox-dev/agentkit-go/catalog"
	"github.com/agent-fox-dev/agentkit-go/core"

	"github.com/agent-fox-dev/spec/internal/agentrun"
)

// An envelope's error carries whether a re-run could help, whether it would
// resume, and the remedy for the ceiling the run stopped at.
func TestRunEnvelope_DerivesRetryableResumableAndTheFixHint(t *testing.T) {
	r := NewRun("fix", "v1")
	r.SetBounds(agentrun.Bounds{MaxBudgetUSD: 3.5, MaxTurns: 40})
	env := r.Envelope(ExitFailed, &resumable{}, &ErrorInfo{Stage: "implement", Category: "budget", Message: "budget exceeded"})
	if env.Error == nil || env.Error.Retryable || !env.Error.Resumable {
		t.Fatalf("error = %+v", env.Error)
	}
	if want := (FixHint{Flag: "--budget", Current: 3.5, Suggest: 7.0}); env.Error.FixHint == nil || !reflect.DeepEqual(*env.Error.FixHint, want) {
		t.Errorf("FixHint = %+v, want %+v", env.Error.FixHint, want)
	}

	api := r.Envelope(ExitFailed, nil, &ErrorInfo{Stage: "prd", Category: "api", Message: "overloaded"})
	if !api.Error.Retryable || api.Error.FixHint != nil {
		t.Errorf("api error = %+v", api.Error)
	}
}

type resumable struct{}

func (*resumable) Resumable() bool { return true }

// ok is true only for exit 0 without an error: a failure reported with exit 0
// is a failure.
func TestRunEnvelope_OKOnlyForASuccess(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    int
		failure *ErrorInfo
		ok      bool
		exit    int
		status  string
	}{
		{"success", ExitOK, nil, true, ExitOK, "done"},
		{"failure", ExitFailed, &ErrorInfo{Message: "m"}, false, ExitFailed, "failed"},
		{"usage", ExitUsage, &ErrorInfo{Message: "m"}, false, ExitUsage, "usage"},
		{"error beside exit 0", ExitOK, &ErrorInfo{Message: "m"}, false, ExitFailed, "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := NewRun("tool", "v").Envelope(tc.code, nil, tc.failure)
			if env.OK != tc.ok || env.ExitCode != tc.exit || env.Status != tc.status {
				t.Errorf("ok=%v exit=%d status=%q", env.OK, env.ExitCode, env.Status)
			}
		})
	}
}

// summarized is a result with its own summary, artifacts and next calls.
type summarized struct{ s string }

func (r summarized) Summary() string { return r.s }
func (r summarized) Artifacts() []Artifact {
	return []Artifact{{Kind: ArtifactMarkdownFile, Path: "/tmp/x.md"}}
}
func (r summarized) Next() []Next { return []Next{{Tool: "speca", Input: "01"}} }

// The summary is the result's own sentence or a fallback, with a clause
// counting high-severity warnings on a success, at most 200 characters.
func TestRunEnvelope_Summary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    int
		result  any
		failure *ErrorInfo
		high    int
		want    string
	}{
		{"the result's own", ExitOK, summarized{"wrote 01"}, nil, 0, "wrote 01"},
		{"a success without one", ExitOK, nil, nil, 0, "tool: done"},
		{"one high warning", ExitOK, summarized{"wrote 01"}, nil, 1, "wrote 01; 1 high-severity warning"},
		{"two high warnings", ExitOK, nil, nil, 2, "tool: done; 2 high-severity warnings"},
		{"a failure's message", ExitFailed, nil, &ErrorInfo{Message: "it broke"}, 1, "it broke"},
		{"a failure without a message", ExitFailed, nil, &ErrorInfo{}, 0, "tool: failed"},
		{"capped", ExitOK, summarized{strings.Repeat("é", 300)}, nil, 1, strings.Repeat("é", 200-len("; 1 high-severity warning")) + "; 1 high-severity warning"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRun("tool", "v")
			for range tc.high {
				r.Warn(WarnInputTruncated, "high", "cut")
			}
			r.Warn(WarnToolErrors, "low", "noise")
			if got := r.Envelope(tc.code, tc.result, tc.failure).Summary; got != tc.want {
				t.Errorf("summary = %q, want %q", got, tc.want)
			}
		})
	}
}

// The artifacts and next calls are read off the result.
func TestRunEnvelope_ReadsArtifactsAndNextOffTheResult(t *testing.T) {
	env := NewRun("tool", "v").Envelope(ExitOK, summarized{"s"}, nil)
	if len(env.Artifacts) != 1 || env.Artifacts[0].Path != "/tmp/x.md" || len(env.Next) != 1 || env.Next[0].Tool != "speca" {
		t.Errorf("artifacts = %+v, next = %+v", env.Artifacts, env.Next)
	}
	var none *summarized
	if env := NewRun("tool", "v").Envelope(ExitFailed, none, &ErrorInfo{Message: "m"}); env.Result != nil {
		t.Errorf("a typed nil result is written: %+v", env.Result)
	}
}

// Each phase's cost is recorded and summed into the run's usage; a phase with
// five or more failed tool calls says so in a warning.
func TestRun_AddPhaseSumsTheUsageAndWarnsOfToolErrors(t *testing.T) {
	r := NewRun("tool", "v")
	r.AddPhase(PhaseInfo{Name: "prd", Turns: 2, InputTokens: 100, OutputTokens: 10, CacheReadTokens: 5, CostUSD: 0.25,
		ToolErrors: map[string]int{"read_file": 4}})
	r.AddPhase(PhaseInfo{Name: "tasks", Turns: 3, InputTokens: 50, OutputTokens: 20, CacheCreationTokens: 7, CostUSD: 0.5,
		ToolErrors: map[string]int{"submit/rejected": 3, "read_file": 2}})
	if got := r.CostUSD(); got != 0.75 {
		t.Errorf("CostUSD = %v", got)
	}
	u := r.Envelope(ExitOK, nil, nil).Usage
	if u == nil || len(u.Phases) != 2 || u.Turns != 5 || u.InputTokens != 150 || u.OutputTokens != 30 ||
		u.CacheReadTokens != 5 || u.CacheCreationTokens != 7 || u.CostUSD != 0.75 {
		t.Errorf("usage = %+v", u)
	}
	ws := warningsOf(r.Warnings(), WarnToolErrors)
	if len(ws) != 1 || !strings.Contains(ws[0].Message, "the tasks phase made 5 tool calls that failed (read_file ×2, submit/rejected ×3)") {
		t.Errorf("tool_errors warnings = %+v", ws)
	}
	if got := WarningMessages(ws); len(got) != 1 || got[0] != ws[0].Message {
		t.Errorf("WarningMessages = %q", got)
	}
	if env := NewRun("tool", "v").Envelope(ExitOK, nil, nil); env.Usage != nil {
		t.Errorf("a run without phases reports usage %+v", env.Usage)
	}
}

// A forge write is recorded in order; a successful one never carries a warning
// and a failed one never a URL.
func TestRun_RecordSideEffect(t *testing.T) {
	r := NewRun("tool", "v")
	r.RecordSideEffect("comment", "prd", "o/r#1", "https://x/1", true, WarnCommentNotPosted)
	r.RecordSideEffect("comment", "prd", "o/r#2", "https://x/2", false, WarnCommentNotPosted)
	want := []SideEffect{
		{Action: "comment", Kind: "prd", Target: "o/r#1", URL: "https://x/1", OK: true},
		{Action: "comment", Kind: "prd", Target: "o/r#2", OK: false, Warning: WarnCommentNotPosted},
	}
	if got := r.SideEffects(); !reflect.DeepEqual(got, want) {
		t.Errorf("side effects = %+v, want %+v", got, want)
	}
	if env := r.Envelope(ExitOK, nil, nil); !reflect.DeepEqual(env.SideEffects, want) {
		t.Errorf("envelope side effects = %+v", env.SideEffects)
	}
}

// A pipeline reports into a nil Run without effect, so a caller that keeps no
// record needs no checks.
func TestRun_NilIsANoOp(t *testing.T) {
	var r *Run
	r.SetBounds(agentrun.Bounds{})
	r.SetInput(Input{})
	r.SetModels(agentrun.Plan{})
	r.Warn(WarnToolErrors, "low", "x")
	r.RecordSideEffect("a", "", "t", "", true, "")
	r.AddPhase(PhaseInfo{})
	if r.CostUSD() != 0 || r.Warnings() != nil || r.SideEffects() != nil {
		t.Error("a nil Run recorded something")
	}
}

type stageErr struct{ stage, category string }

func (e stageErr) Error() string        { return "failed at " + e.stage }
func (e stageErr) StageName() string    { return e.stage }
func (e stageErr) CategoryName() string { return e.category }

// The envelope's error takes the stage and category a failure carries, else
// the default stage and the category found by unwrapping, else internal.
func TestErrorFrom(t *testing.T) {
	budget := &agentrun.Error{Phase: "prd", Detail: "over", Cat: agentrun.CategoryBudget}
	for _, tc := range []struct {
		name            string
		err             error
		stage, category string
	}{
		{"staged", fmt.Errorf("run: %w", stageErr{"tasks", "invalid_spec"}), "tasks", "invalid_spec"},
		{"categorized", budget, "generate", agentrun.CategoryBudget},
		{"plain", errors.New("boom"), "generate", agentrun.CategoryInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := ErrorFrom("generate", tc.err)
			if info.Stage != tc.stage || info.Category != tc.category || info.Message != tc.err.Error() || !errors.Is(info, tc.err) {
				t.Errorf("info = %+v", info)
			}
		})
	}
}

func TestExitCodeFor(t *testing.T) {
	for category, want := range map[string]int{"usage": ExitUsage, "api": ExitFailed, "": ExitFailed} {
		if got := ExitCodeFor(category); got != want {
			t.Errorf("ExitCodeFor(%q) = %d, want %d", category, got, want)
		}
	}
}

// A next call names a small, stable input by itself, and anything else by the
// placeholder.
func TestResumePlaceholder(t *testing.T) {
	for _, tc := range []struct {
		in   Input
		want string
	}{
		{Input{Kind: KindFile, Origin: "report.md"}, "report.md"},
		{Input{Kind: KindIssue, Origin: "https://github.com/o/r/issues/1"}, "https://github.com/o/r/issues/1"},
		{Input{Kind: KindText, Origin: "argument"}, SameInputPlaceholder},
		{Input{Kind: KindStdin, Origin: "stdin"}, SameInputPlaceholder},
		{Input{Kind: KindFile}, SameInputPlaceholder},
	} {
		if got := ResumePlaceholder(tc.in); got != tc.want {
			t.Errorf("ResumePlaceholder(%+v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A finished phase is recorded with its model, effort, stop, tokens, cost and
// time, and its footer shows the cached tokens only when there were some.
func TestPhaseFromResultAndPhaseSummary(t *testing.T) {
	row, ok := catalog.Lookup("claude-opus-5-5")
	if !ok {
		t.Fatal("claude-opus-5-5 is not in the catalog")
	}
	res := agentrun.Result{
		Name: "prd", Model: agentrun.Model{Row: row, Effort: core.EffortHigh}, Turns: 4, StopReason: core.RunStopToolTerminate,
		Usage:   core.Usage{InputTokens: 12_500, OutputTokens: 900, CacheReadTokens: 2_000_000, CacheWriteTokens: 500, CostUSD: 1.234},
		Elapsed: 1500 * time.Millisecond, ToolCalls: map[string]int{"read_file": 3},
	}
	want := PhaseInfo{Name: "prd", Scope: "01_a", Model: "claude-opus-5-5", Effort: "high", Turns: 4, StopReason: string(core.RunStopToolTerminate),
		InputTokens: 12_500, OutputTokens: 900, CacheReadTokens: 2_000_000, CacheCreationTokens: 500, CostUSD: 1.234,
		DurationMS: 1500, ToolCalls: map[string]int{"read_file": 3}}
	if got := PhaseFromResult(res, "01_a"); !reflect.DeepEqual(got, want) {
		t.Errorf("PhaseFromResult = %+v\nwant %+v", got, want)
	}
	if got := PhaseSummary(res); got != "· 4 turns · 12.5k↑ (+2.0M cached) 900↓ · $1.23" {
		t.Errorf("PhaseSummary = %q", got)
	}
	res.Usage.CacheReadTokens, res.Usage.CacheWriteTokens = 0, 0
	if got := PhaseSummary(res); got != "· 4 turns · 12.5k↑ 900↓ · $1.23" {
		t.Errorf("PhaseSummary without cache = %q", got)
	}
}
