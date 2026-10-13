package agentrun

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/catalog"
	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/provider/faux"
	"github.com/agent-fox-dev/agentkit-go/schema"
	"github.com/agent-fox-dev/agentkit-go/tools"
)

const terminator = "submit_answer"

// submitTool accepts any answer but "wrong", which it refuses so the model
// has to try again.
func submitTool(got *string) core.Tool {
	return core.Tool{
		Name:        terminator,
		Description: "Submit the answer.",
		InputSchema: schema.Object(schema.Prop("answer", schema.String("The answer."))),
		Terminating: true,
		Execute: func(_ context.Context, in json.RawMessage) core.ToolResult {
			var p struct {
				Answer string `json:"answer"`
			}
			_ = json.Unmarshal(in, &p)
			if p.Answer == "wrong" {
				return core.ErrResult("rejected", "the answer is wrong")
			}
			*got = p.Answer
			res := core.OKResult(map[string]any{"accepted": true})
			res.Terminate = true
			return res
		},
	}
}

func call(id, name, args string) faux.Turn {
	return faux.Turn{Blocks: []core.ContentBlock{faux.FauxToolCall(id, name, args)}, StopReason: core.StopReasonToolUse}
}

func listedModel(t *testing.T) Model {
	t.Helper()
	row, ok := catalog.Lookup("claude-opus-5-5")
	if !ok {
		t.Fatal("claude-opus-5-5 is not in the catalog")
	}
	return Model{Spec: "claude-opus-5-5", Row: row}
}

func newRunner(t *testing.T, p *faux.Provider, cfg Config) *Runner {
	t.Helper()
	if cfg.Model.ID() == "" {
		cfg.Model = listedModel(t)
	}
	cfg.Provider = p
	if cfg.Workspace == nil {
		ws, err := tools.NewWorkspace(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		cfg.Workspace = ws
	}
	r, err := NewRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAPhaseEndsWhenItsTerminatorAccepts(t *testing.T) {
	p := faux.New(
		call("1", terminator, `{"answer":"wrong"}`),
		call("2", terminator, `{"answer":"right"}`),
	)
	var got string
	r := newRunner(t, p, Config{})
	res, err := r.Run(context.Background(), Phase{
		Name: "answer", System: "Answer.", User: "Go.", Terminator: terminator,
		Custom: []core.Tool{submitTool(&got)}, BuiltinTools: ReadOnlyFileTools,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "right" || res.StopReason != core.RunStopToolTerminate || res.Turns != 2 {
		t.Errorf("got %q, stop %s after %d turns", got, res.StopReason, res.Turns)
	}
	if res.ToolCalls[terminator] != 2 || res.ToolErrors[terminator+"/rejected"] != 1 {
		t.Errorf("calls = %v, errors = %v", res.ToolCalls, res.ToolErrors)
	}
}

func TestAPhaseIsOfferedOnlyReadOnlyToolsAndToldThereIsNoShell(t *testing.T) {
	p := faux.New()
	var got string
	r := newRunner(t, p, Config{})
	_, _ = r.Run(context.Background(), Phase{
		Name: "answer", System: "Answer.", User: "Go.", Terminator: terminator,
		Custom: []core.Tool{submitTool(&got)}, BuiltinTools: ReadOnlyFileTools,
	})
	reqs := p.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request was sent")
	}
	var names []string
	for _, w := range reqs[0].Tools {
		names = append(names, w.Name)
	}
	want := append([]string{terminator}, ReadOnlyFileTools...)
	slices.Sort(names)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}
	system := systemText(reqs[0])
	if !strings.Contains(system, "There is no shell") || !strings.Contains(system, "Your tools are exactly: ") {
		t.Errorf("system prompt lacks the tools note:\n%s", system)
	}
}

func TestAShellCanNeverBeGranted(t *testing.T) {
	var got string
	r := newRunner(t, faux.New(), Config{})
	_, err := r.Run(context.Background(), Phase{
		Name: "answer", User: "Go.", Terminator: terminator,
		Custom: []core.Tool{submitTool(&got)}, BuiltinTools: []string{"read_file", "execute"},
	})
	if CategoryOf(err) != CategoryInternal || !strings.Contains(err.Error(), "execute") {
		t.Errorf("err = %v", err)
	}
}

func TestCodeSearchComesFromTheIndex(t *testing.T) {
	p := faux.New()
	var got string
	r := newRunner(t, p, Config{Index: &stubIndex{}})
	_, _ = r.Run(context.Background(), Phase{
		Name: "answer", User: "Go.", Terminator: terminator,
		Custom: []core.Tool{submitTool(&got)}, BuiltinTools: WithCodeSearch(ReadOnlyFileTools, true),
	})
	if !slices.ContainsFunc(p.Requests()[0].Tools, func(w core.ToolWire) bool { return w.Name == ToolCodeSearch }) {
		t.Error("code_search was not offered")
	}
}

func TestAProseAnswerEndsWithoutAnErrorAndNoResult(t *testing.T) {
	p := faux.New(faux.FauxAssistantMessage(core.StopReasonStop, faux.FauxText("Here is my answer in prose.")))
	var got string
	r := newRunner(t, p, Config{})
	res, err := r.Run(context.Background(), Phase{
		Name: "answer", User: "Go.", Terminator: terminator, Custom: []core.Tool{submitTool(&got)},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "" || res.StopReason != core.RunStopEndTurn {
		t.Errorf("got %q, stop %s", got, res.StopReason)
	}
	nr := NoResultError("answer", terminator, res)
	if CategoryOf(nr) != CategoryNoResult || !strings.Contains(nr.Error(), terminator) {
		t.Errorf("NoResultError = %v", nr)
	}
}

func TestTheTurnCeilingIsMaxTurns(t *testing.T) {
	p := faux.New(
		call("1", "list_files", `{}`),
		call("2", "list_files", `{}`),
		call("3", "list_files", `{}`),
	)
	var got string
	r := newRunner(t, p, Config{Bounds: Bounds{MaxTurns: 2}})
	res, err := r.Run(context.Background(), Phase{
		Name: "answer", User: "Go.", Terminator: terminator,
		Custom: []core.Tool{submitTool(&got)}, BuiltinTools: ReadOnlyFileTools,
	})
	var e *Error
	if !errors.As(err, &e) || e.Category() != CategoryMaxTurns || e.RunStopReason() != core.RunStopMaxTurns {
		t.Fatalf("err = %v", err)
	}
	if res.Turns != 2 {
		t.Errorf("turns = %d", res.Turns)
	}
}

func TestTheSpendCeilingIsMaxBudget(t *testing.T) {
	expensive := call("1", "list_files", `{}`)
	expensive.Usage = core.Usage{InputTokens: 1_000_000, OutputTokens: 10}
	p := faux.New(expensive, call("2", "list_files", `{}`))
	var got string
	r := newRunner(t, p, Config{Bounds: Bounds{MaxBudgetUSD: 1}})
	res, err := r.Run(context.Background(), Phase{
		Name: "answer", User: "Go.", Terminator: terminator,
		Custom: []core.Tool{submitTool(&got)}, BuiltinTools: ReadOnlyFileTools,
	})
	if CategoryOf(err) != CategoryBudget {
		t.Fatalf("err = %v", err)
	}
	if p.Calls() != 1 || res.Usage.CostUSD < 1 {
		t.Errorf("calls = %d, cost = %v", p.Calls(), res.Usage.CostUSD)
	}
}

func TestACancelledContextIsAborted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var got string
	r := newRunner(t, faux.New(), Config{})
	_, err := r.Run(ctx, Phase{Name: "answer", User: "Go.", Terminator: terminator, Custom: []core.Tool{submitTool(&got)}})
	if CategoryOf(err) != CategoryAborted {
		t.Errorf("err = %v", err)
	}
}

func TestProjectContextIsAdmittedOnlyUnderTrust(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Always write tests first."), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := tools.NewWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, trust := range []bool{false, true} {
		p := faux.New()
		var got string
		r := newRunner(t, p, Config{Workspace: ws, TrustProject: trust})
		_, _ = r.Run(context.Background(), Phase{
			Name: "answer", System: "Answer.", User: "Go.", Terminator: terminator,
			Custom: []core.Tool{submitTool(&got)}, LoadProjectContext: true,
		})
		has := strings.Contains(systemText(p.Requests()[0]), "Always write tests first.")
		if has != trust {
			t.Errorf("trust=%v: AGENTS.md in the system prompt = %v", trust, has)
		}
	}
}

func TestResolveModel(t *testing.T) {
	m, err := ResolveModel("standard", "", false)
	if err != nil || m.ID() != "claude-sonnet-5-5" || m.Effort != core.EffortHigh || m.Spec != "standard" {
		t.Errorf("tier: %+v, %v", m, err)
	}
	m, err = ResolveModel("anthropic/claude-opus-5-5", "", false)
	if err != nil || m.ID() != "claude-opus-5-5" || m.Effort != "" {
		t.Errorf("id: %+v, %v", m, err)
	}
	m, err = ResolveModel("ADVANCED", "", true)
	if err != nil || m.Effort != "" {
		t.Errorf("an explicit empty effort sends none: %+v, %v", m, err)
	}
	m, err = ResolveModel("claude-haiku-4-5", core.EffortXHigh, true)
	if err != nil || m.Effort != core.EffortHigh || m.Clamped != core.EffortXHigh {
		t.Errorf("clamp: %+v, %v", m, err)
	}
	if _, err := ResolveModel("gpt-9", "", false); CategoryOf(err) != CategoryModel {
		t.Errorf("unknown model: %v", err)
	}
}

func TestParseEffort(t *testing.T) {
	for raw, want := range map[string]core.Effort{"off": "", "LOW": core.EffortLow, "max": core.EffortMax} {
		if got, err := ParseEffort(raw); err != nil || got != want {
			t.Errorf("%q = %q, %v", raw, got, err)
		}
	}
	if _, err := ParseEffort("minimal"); err == nil {
		t.Error("minimal is not an effort AgentKit sends")
	}
}

func TestBoundsDefaults(t *testing.T) {
	var b Bounds
	if b.ResolvedMaxTurns() != DefaultMaxTurns || b.ResolvedMaxBudget() != DefaultMaxBudgetUSD {
		t.Errorf("defaults = %d, %v", b.ResolvedMaxTurns(), b.ResolvedMaxBudget())
	}
}

func systemText(req core.Request) string {
	var b strings.Builder
	for _, blk := range req.System {
		if tb, ok := blk.(core.TextBlock); ok {
			b.WriteString(tb.Text)
		}
	}
	return b.String()
}

// stubIndex offers a code_search tool and answers nothing else.
type stubIndex struct{}

func (*stubIndex) Symbols(context.Context, tools.SymbolQuery) (tools.SymbolAnswer, bool, error) {
	return tools.SymbolAnswer{}, false, nil
}

func (*stubIndex) Tools() []core.Tool {
	return []core.Tool{{
		Name: ToolCodeSearch, Description: "ranked search",
		InputSchema: schema.Object(schema.Prop("query", schema.String("The query."))),
		Execute:     func(context.Context, json.RawMessage) core.ToolResult { return core.OKResult(map[string]any{}) },
	}}
}

func (*stubIndex) Invalidate(string) {}
func (*stubIndex) Close() error      { return nil }
