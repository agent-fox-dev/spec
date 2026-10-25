package agentrun

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/provider/faux"

	"github.com/agent-fox-dev/spec/internal/agentrun/indextest"
)

func TestNewRunner_RefusesWhatItCannotRunOn(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
		want string
	}{
		{"no model", Config{Provider: faux.New()}, CategoryModel},
		{"no client", Config{Model: listedModel(t)}, CategoryInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewRunner(tc.cfg); CategoryOf(err) != tc.want {
				t.Errorf("err = %v (%s), want %s", err, CategoryOf(err), tc.want)
			}
		})
	}
}

// Zero bounds take the defaults, and the Runner reports the ceilings it
// applies before any phase runs.
func TestBounds_Resolve(t *testing.T) {
	var zero Bounds
	if zero.ResolvedMaxTurns() != DefaultMaxTurns || zero.ResolvedMaxBudget() != DefaultMaxBudgetUSD || zero.ResolvedMaxTokens(100) != 100 {
		t.Errorf("defaults = %d, %v, %d", zero.ResolvedMaxTurns(), zero.ResolvedMaxBudget(), zero.ResolvedMaxTokens(100))
	}
	r := newRunner(t, faux.New(), Config{Bounds: Bounds{MaxTurns: 7, MaxBudgetUSD: 1.5}})
	if turns, budget := r.ResolvedBounds(); turns != 7 || budget != 1.5 {
		t.Errorf("ResolvedBounds = %d, %v; want 7, 1.5", turns, budget)
	}
}

func TestRun_EndsWhenTheTerminatorAccepts(t *testing.T) {
	p := faux.New(
		call("1", terminator, `{"answer":"wrong"}`),
		call("2", terminator, `{"answer":"right"}`),
	)
	var got string
	r := newRunner(t, p, Config{})
	phase := answerPhase(&got)
	phase.BuiltinTools = ReadOnlyFileTools
	res, err := r.Run(context.Background(), phase)
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

// progress records what an Observer is told.
type progress struct {
	details
	mu     sync.Mutex
	events []string
	raw    strings.Builder
}

func (p *progress) note(s string) { p.mu.Lock(); p.events = append(p.events, s); p.mu.Unlock() }
func (p *progress) Raw(s string)  { p.mu.Lock(); p.raw.WriteString(s); p.mu.Unlock() }
func (p *progress) PhaseStart(phase, model string, _ int, _ float64) {
	p.note("start " + phase + " " + model)
}
func (p *progress) PhaseEnd(phase, stop string, _ int, _ float64, _ time.Duration) {
	p.note("end " + phase + " " + stop)
}
func (p *progress) Turn(phase string, _ int, _ float64) { p.note("turn " + phase) }

// A person watching sees the phase start on its model, each turn, the model's
// prose under --show-text, and the phase end.
func TestRun_ReportsProgressToTheObserver(t *testing.T) {
	p := faux.New(
		faux.Turn{Blocks: []core.ContentBlock{faux.FauxText("Thinking aloud."), faux.FauxToolCall("1", terminator, `{"answer":"right"}`)},
			StopReason: core.StopReasonToolUse},
	)
	var got string
	obs := &progress{}
	r := newRunner(t, p, Config{Observer: obs, ShowText: true})
	if _, err := r.Run(context.Background(), answerPhase(&got)); err != nil {
		t.Fatal(err)
	}
	want := []string{"start answer claude-opus-5-5", "turn answer", "end answer " + string(core.RunStopToolTerminate)}
	if !slices.Equal(obs.events, want) {
		t.Errorf("events = %v, want %v", obs.events, want)
	}
	if !strings.Contains(obs.raw.String(), "Thinking aloud.") {
		t.Errorf("--show-text wrote %q", obs.raw.String())
	}
}

func TestRun_OffersOnlyReadOnlyToolsAndSaysThereIsNoShell(t *testing.T) {
	p := faux.New()
	var got string
	r := newRunner(t, p, Config{})
	phase := answerPhase(&got)
	phase.BuiltinTools = ReadOnlyFileTools
	_, _ = r.Run(context.Background(), phase)
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

// A phase the runner cannot run safely fails before any request: no
// terminator, a tool that is not read-only (a shell can never be granted), or
// file tools with no workspace to root them.
func TestRun_RefusesAPhaseItCannotRunSafely(t *testing.T) {
	var got string
	for _, tc := range []struct {
		name    string
		noWS    bool
		tools   []string
		noTerm  bool
		mention string
	}{
		{name: "no terminator", noTerm: true, mention: "no terminating tool"},
		{name: "a shell", tools: []string{"read_file", "execute"}, mention: "execute"},
		{name: "file tools without a workspace", noWS: true, tools: []string{"read_file"}, mention: "no workspace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := faux.New()
			cfg := Config{Model: listedModel(t), Provider: p}
			if !tc.noWS {
				cfg.Workspace = openWorkspace(t, t.TempDir())
			}
			r, err := NewRunner(cfg)
			if err != nil {
				t.Fatal(err)
			}
			phase := answerPhase(&got)
			phase.BuiltinTools = tc.tools
			if tc.noTerm {
				phase.Terminator = ""
			}
			_, err = r.Run(context.Background(), phase)
			if CategoryOf(err) != CategoryInternal || !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("err = %v, want an internal error naming %q", err, tc.mention)
			}
			if p.Calls() != 0 {
				t.Errorf("%d requests were sent", p.Calls())
			}
		})
	}
}

// The API totals its strict-mode limits over a request, including a compiled
// grammar size it does not publish, and refuses the whole request past them: a
// generation phase's artifact tool was refused beside four file tools. The
// strict budget goes to the phase's own tools; the built-in read-only tools,
// whose arguments are a path and a line range, are sent without the flag.
func TestRun_OnlyThePhasesOwnToolsAreStrict(t *testing.T) {
	var body []byte
	var got string
	r, err := NewRunner(Config{Model: listedModel(t), Client: capturingClient(&body), Workspace: openWorkspace(t, t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	phase := answerPhase(&got)
	phase.BuiltinTools = ReadOnlyFileTools
	_, _ = r.Run(context.Background(), phase)
	var req struct {
		Tools []struct {
			Name   string `json:"name"`
			Strict bool   `json:"strict"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &req); err != nil || len(req.Tools) == 0 {
		t.Fatalf("no request body was captured (%v): %s", err, body)
	}
	strict := map[string]bool{}
	for _, tl := range req.Tools {
		strict[tl.Name] = tl.Strict
	}
	if !strict[terminator] {
		t.Errorf("%s is not strict: the phase's own tool is the one the budget is for", terminator)
	}
	for _, name := range ReadOnlyFileTools {
		if _, sent := strict[name]; !sent {
			t.Errorf("%s was not sent", name)
		} else if strict[name] {
			t.Errorf("%s is strict, and spends the budget of the phase's own tools", name)
		}
	}
}

func TestRun_OffersCodeSearchFromTheIndex(t *testing.T) {
	p := faux.New()
	var got string
	idx := &indextest.Index{}
	r := newRunner(t, p, Config{Index: idx})
	if r.Index() != idx {
		t.Error("the Runner does not report its index")
	}
	phase := answerPhase(&got)
	phase.BuiltinTools = WithCodeSearch(ReadOnlyFileTools, true)
	_, _ = r.Run(context.Background(), phase)
	if !slices.ContainsFunc(p.Requests()[0].Tools, func(w core.ToolWire) bool { return w.Name == ToolCodeSearch }) {
		t.Error("code_search was not offered")
	}
}

// A model that answers in prose ends the run without an error; the caller
// reports that no result came.
func TestRun_AProseAnswerEndsWithoutAResult(t *testing.T) {
	p := faux.New(faux.FauxAssistantMessage(core.StopReasonStop, faux.FauxText("Here is my answer in prose.")))
	var got string
	r := newRunner(t, p, Config{})
	res, err := r.Run(context.Background(), answerPhase(&got))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "" || res.StopReason != core.RunStopEndTurn {
		t.Errorf("got %q, stop %s", got, res.StopReason)
	}
}

// A response that reaches max_tokens before its first tool call ends the run
// as an end_turn, because the loop goes on only while there is a tool call to
// answer. Only the response's own stop reason says the phase ran out of room,
// so the result carries it and --verbose says so.
func TestRun_APhaseCutOffAtTheOutputLimitSaysSo(t *testing.T) {
	p := faux.New(
		call("1", "list_files", `{}`),
		faux.FauxAssistantMessage(core.StopReasonLength, faux.FauxThinking("Drafting the whole document first…", "sig")),
	)
	var got string
	obs := &details{}
	r := newRunner(t, p, Config{Observer: obs})
	phase := answerPhase(&got)
	phase.BuiltinTools = ReadOnlyFileTools
	res, err := r.Run(context.Background(), phase)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "" || res.StopReason != core.RunStopEndTurn || res.Turns != 2 {
		t.Fatalf("got %q, stop %s after %d turns; want no answer, end_turn, 2 turns", got, res.StopReason, res.Turns)
	}
	if res.LastReason != core.StopReasonLength {
		t.Errorf("LastReason = %q, want %q", res.LastReason, core.StopReasonLength)
	}
	if !strings.Contains(obs.text(), "output token limit") {
		t.Errorf("--verbose never says a response was cut off:\n%s", obs.text())
	}
}

// The remedy NoResultError names follows what stopped the phase. A response
// cut off at the output limit reached neither ceiling, so raising one would
// send the operator the wrong way.
func TestNoResultError_NamesTheRemedyForTheStop(t *testing.T) {
	for _, tc := range []struct {
		name      string
		res       Result
		want, not []string
	}{
		{"prose", Result{StopReason: core.RunStopEndTurn}, []string{"--max-turns or --budget"}, nil},
		{"budget", Result{StopReason: core.RunStopBudgetExceeded}, []string{"raise --budget"}, []string{"--max-turns"}},
		{"turns", Result{StopReason: core.RunStopMaxTurns}, []string{"raise --max-turns"}, []string{"--budget"}},
		{"output limit", Result{StopReason: core.RunStopEndTurn, LastReason: core.StopReasonLength},
			[]string{"output token limit", "--max-tokens", "effort"}, []string{"--max-turns", "--budget"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := NoResultError("answer", terminator, tc.res)
			var e *Error
			if !errors.As(err, &e) || e.Category() != CategoryNoResult || e.RunStopReason() != tc.res.StopReason {
				t.Fatalf("err = %v", err)
			}
			msg := err.Error()
			if !strings.HasPrefix(msg, "answer: ") || !strings.Contains(msg, terminator) {
				t.Errorf("the message does not name the phase and its terminator: %s", msg)
			}
			for _, w := range tc.want {
				if !strings.Contains(msg, w) {
					t.Errorf("the message lacks %q: %s", w, msg)
				}
			}
			for _, bad := range tc.not {
				if strings.Contains(msg, bad) {
					t.Errorf("the message sends the operator to %s: %s", bad, msg)
				}
			}
		})
	}
}

// The cap on a response is the phase's own until a bound replaces it, which is
// how an operator gives a phase more room than the tool does, or less.
func TestRun_TheMaxTokensBoundReplacesThePhasesOwnCap(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bound, phase int
		want         int
	}{
		{"no bound leaves the phase's own cap", 0, 65536, 65536},
		{"a lower bound replaces it", 8000, 65536, 8000},
		{"a higher bound replaces it too", 100000, 65536, 100000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := faux.New()
			var got string
			r := newRunner(t, p, Config{Bounds: Bounds{MaxTokens: tc.bound}})
			phase := answerPhase(&got)
			phase.MaxTokens = tc.phase
			_, _ = r.Run(context.Background(), phase)

			reqs := p.Requests()
			if len(reqs) == 0 {
				t.Fatal("no request was sent")
			}
			if n := reqs[0].MaxTokens; n == nil || *n != tc.want {
				t.Errorf("max_tokens = %v, want %d", n, tc.want)
			}
		})
	}
}

func TestRun_StopsAtTheTurnCeiling(t *testing.T) {
	p := faux.New(
		call("1", "list_files", `{}`),
		call("2", "list_files", `{}`),
		call("3", "list_files", `{}`),
	)
	var got string
	r := newRunner(t, p, Config{Bounds: Bounds{MaxTurns: 2}})
	phase := answerPhase(&got)
	phase.BuiltinTools = ReadOnlyFileTools
	res, err := r.Run(context.Background(), phase)
	var e *Error
	if !errors.As(err, &e) || e.Category() != CategoryMaxTurns || e.RunStopReason() != core.RunStopMaxTurns {
		t.Fatalf("err = %v", err)
	}
	if res.Turns != 2 {
		t.Errorf("turns = %d", res.Turns)
	}
}

func TestRun_StopsAtTheSpendCeiling(t *testing.T) {
	expensive := call("1", "list_files", `{}`)
	expensive.Usage = core.Usage{InputTokens: 1_000_000, OutputTokens: 10}
	p := faux.New(expensive, call("2", "list_files", `{}`))
	var got string
	r := newRunner(t, p, Config{Bounds: Bounds{MaxBudgetUSD: 1}})
	phase := answerPhase(&got)
	phase.BuiltinTools = ReadOnlyFileTools
	res, err := r.Run(context.Background(), phase)
	if CategoryOf(err) != CategoryBudget {
		t.Fatalf("err = %v", err)
	}
	if p.Calls() != 1 || res.Usage.CostUSD < 1 {
		t.Errorf("calls = %d, cost = %v", p.Calls(), res.Usage.CostUSD)
	}
}

func TestRun_ACancelledContextIsAborted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var got string
	r := newRunner(t, faux.New(), Config{})
	if _, err := r.Run(ctx, answerPhase(&got)); CategoryOf(err) != CategoryAborted {
		t.Errorf("err = %v", err)
	}
}

// The repository's AGENTS.md reaches the system prompt only under
// --trust-project: otherwise a repository that is merely the current directory
// would author part of the prompt that reads it.
func TestRun_AdmitsProjectContextOnlyUnderTrust(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Always write tests first."), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := openWorkspace(t, dir)
	for _, trust := range []bool{false, true} {
		t.Run(map[bool]string{false: "untrusted", true: "trusted"}[trust], func(t *testing.T) {
			p := faux.New()
			var got string
			r := newRunner(t, p, Config{Workspace: ws, TrustProject: trust})
			phase := answerPhase(&got)
			phase.LoadProjectContext = true
			_, _ = r.Run(context.Background(), phase)
			if has := strings.Contains(systemText(p.Requests()[0]), "Always write tests first."); has != trust {
				t.Errorf("AGENTS.md in the system prompt = %v", has)
			}
		})
	}
}

// A phase runs on the model of its kind, and a phase with no kind on the
// default.
func TestRun_EachPhaseRunsOnItsOwnModel(t *testing.T) {
	plan, err := ResolvePlan("ADVANCED", "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := faux.New()
	var got string
	r := newRunner(t, p, Config{Model: plan.Default, PhaseModels: plan.Phases})
	phase := answerPhase(&got)
	phase.Name, phase.Kind = "generate:tasks", PhaseTasks
	res, _ := r.Run(context.Background(), phase)
	if res.Model.String() != "claude-sonnet-5-5/high" {
		t.Errorf("result model = %s", res.Model)
	}
	if reqs := p.Requests(); len(reqs) == 0 || reqs[0].Effort != core.EffortHigh {
		t.Errorf("the request's effort = %v", reqs)
	}
	if r.ModelFor("").String() != "claude-opus-5-5/xhigh" || r.Model().String() != "claude-opus-5-5/xhigh" {
		t.Errorf("a phase with no kind runs on %s", r.ModelFor(""))
	}
}

// A phase's Prefix reaches the request as the driver's cached prefix, ahead
// of the prompt, and an empty one sends none.
func TestRun_APhasePrefixIsTheRequestsCachedPrefix(t *testing.T) {
	for name, prefix := range map[string]string{"prefix": "Shared context.", "none": ""} {
		t.Run(name, func(t *testing.T) {
			p := faux.New()
			var got string
			r := newRunner(t, p, Config{})
			phase := answerPhase(&got)
			phase.Prefix = prefix
			_, _ = r.Run(context.Background(), phase)
			req := p.Requests()[0]
			if prefix == "" {
				if len(req.Prefix) != 0 {
					t.Errorf("an empty prefix sent %v", req.Prefix)
				}
				return
			}
			if len(req.Prefix) != 1 || !strings.Contains(messageText(req.Prefix[0]), prefix) {
				t.Errorf("prefix = %+v", req.Prefix)
			}
			if strings.Contains(messageText(req.Messages[0]), prefix) {
				t.Error("the prefix was also sent in the prompt")
			}
		})
	}
}

// A traced line is its first line, cut on a rune boundary so the log never
// holds invalid UTF-8.
func TestFirstLine(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		limit    int
		want     string
	}{
		{"one line", "  hello  ", 10, "hello"},
		{"several lines", "first\nsecond", 0, "first"},
		{"cut", "abcdefgh", 4, "abcd…[+4 bytes]"},
		{"cut inside a rune", "aé", 2, "a…[+2 bytes]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstLine(tc.in, tc.limit); got != tc.want {
				t.Errorf("firstLine(%q, %d) = %q, want %q", tc.in, tc.limit, got, tc.want)
			}
		})
	}
}
