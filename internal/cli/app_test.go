package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/provider/faux"
	"github.com/agent-fox-dev/agentkit-go/tools"
	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/internal/agentrun"
)

// withoutIndex makes the index builder fail, so these tests neither build a
// real index nor depend on one.
func withoutIndex(t *testing.T) {
	t.Helper()
	old := agentrun.NewIndex
	agentrun.NewIndex = func(*workspace.Workspace) (tools.Index, error) { return nil, errors.New("no index in tests") }
	t.Cleanup(func() { agentrun.NewIndex = old })
}

// scripted makes the shell use a scripted model in place of Anthropic's.
func scripted(t *testing.T) {
	t.Helper()
	t.Cleanup(SetProvider(faux.New()))
}

func TestMainRunsExecWithTheResolvedDeps(t *testing.T) {
	scripted(t)
	withoutIndex(t)
	app, seen := newApp(t, nil)
	dir := t.TempDir()
	env, code, _ := runApp(t, app, []string{"--dir", dir, "--model", "ADVANCED", "an idea"}, "")
	if code != ExitOK || !env.OK || env.Status != "done" {
		t.Fatalf("code=%d env=%+v", code, env)
	}
	if seen.Input.Kind != KindText || seen.Input.Body != "an idea" {
		t.Errorf("input = %+v", seen.Input)
	}
	if seen.Models.Default.ID() != "claude-opus-5-5" || seen.Models.Default.Effort != "xhigh" {
		t.Errorf("model = %+v", seen.Models.Default)
	}
	if env.Model == nil || env.Model.Spec != "ADVANCED" || env.Model.ID != "claude-opus-5-5" {
		t.Errorf("envelope model = %+v", env.Model)
	}
	if seen.Runner == nil || seen.Forge == nil {
		t.Error("Exec was not given a runner and a forge client")
	}
	if seen.Index != nil || seen.IndexUnavailable == "" {
		t.Errorf("an index that could not be built: Index=%v reason=%q", seen.Index, seen.IndexUnavailable)
	}
	if !hasWarning(env, WarnCodeSearchUnavailable) {
		t.Errorf("warnings = %+v, want code_search_unavailable", env.Warnings)
	}
}

func TestTotalBudgetLowersTheOnePhasesCeiling(t *testing.T) {
	scripted(t)
	withoutIndex(t)
	for _, tc := range []struct {
		single bool
		want   float64
	}{{true, 4}, {false, 10}} {
		app, seen := newApp(t, nil)
		app.SinglePhase = tc.single
		app.DefaultBounds = agentrun.Bounds{MaxTurns: 10, MaxBudgetUSD: 2}
		_, code, _ := runApp(t, app, []string{"--dir", t.TempDir(), "--budget", "10", "--total-budget", "4", "x"}, "")
		if code != ExitOK {
			t.Fatalf("code = %d", code)
		}
		if _, got := seen.Runner.ResolvedBounds(); got != tc.want {
			t.Errorf("single=%v: ceiling = %v, want %v", tc.single, got, tc.want)
		}
	}
}

// --max-tokens is the cap on one response of every phase, and it has to reach
// the request a phase sends, not just the parsed flags. A phase that asks for
// its own cap gets the flag's instead; with no flag it keeps its own.
func TestMaxTokensReachesTheRequestsOfTheRun(t *testing.T) {
	withoutIndex(t)
	for _, tc := range []struct {
		name string
		argv []string
		want int
	}{
		{"no flag keeps the phase's own cap", nil, 65536},
		{"the flag replaces it", []string{"--max-tokens", "12345"}, 12345},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := faux.New()
			t.Cleanup(SetProvider(p))
			app, _ := newApp(t, func(ctx context.Context, d Deps) (int, any, *ErrorInfo) {
				_, _ = d.Runner.Run(ctx, agentrun.Phase{
					Name: "answer", User: "Go.", Terminator: "submit", MaxTokens: 65536,
				})
				return ExitOK, nil, nil
			})
			argv := append([]string{"--dir", t.TempDir()}, tc.argv...)
			if _, code, _ := runApp(t, app, append(argv, "x"), ""); code != ExitOK {
				t.Fatalf("code = %d", code)
			}
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

func TestUsageErrorsAreRefusedBeforeAnythingRuns(t *testing.T) {
	for _, argv := range [][]string{
		{"--detail", "brief", "x"},
		{"--budget", "-1", "x"},
		{"--total-budget", "-1", "x"},
		{"--max-turns", "-1", "x"},
		{"--max-tokens", "-1", "x"},
		{"--input-kind", "url", "x"},
		{"--effort", "huge", "x"},
		{"--no-such-flag", "x"},
		{"one", "two"},
		{"--output", "-", "x"},
	} {
		app, _ := newApp(t, func(context.Context, Deps) (int, any, *ErrorInfo) {
			t.Fatalf("%v: Exec ran", argv)
			return 0, nil, nil
		})
		env, code, _ := runApp(t, app, argv, "")
		if code != ExitUsage || env.Status != "usage" || env.OK || env.Error == nil || env.Error.Category != "usage" {
			t.Errorf("%v: code=%d env=%+v", argv, code, env)
		}
	}
}

// The table above cannot tell a negative value from a flag that does not exist:
// both are usage errors. This one can.
func TestANegativeMaxTokensIsRefusedByName(t *testing.T) {
	app, _ := newApp(t, nil)
	env, code, _ := runApp(t, app, []string{"--max-tokens", "-1", "x"}, "")
	if code != ExitUsage || env.Error == nil || !strings.Contains(env.Error.Message, "--max-tokens cannot be negative") {
		t.Errorf("code=%d error=%+v", code, env.Error)
	}
}

func TestAnUnknownModelIsAModelFailure(t *testing.T) {
	scripted(t)
	app, _ := newApp(t, nil)
	env, code, _ := runApp(t, app, []string{"--dir", t.TempDir(), "--model", "gpt-9", "x"}, "")
	if code != ExitFailed || env.Error == nil || env.Error.Category != agentrun.CategoryModel || env.Error.Stage != "preflight" {
		t.Fatalf("code=%d error=%+v", code, env.Error)
	}
	if env.Error.FixHint == nil || env.Error.FixHint.Flag != "--model" {
		t.Errorf("fix hint = %+v", env.Error.FixHint)
	}
}

func TestAMissingCredentialIsAnAuthFailure(t *testing.T) {
	for _, v := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_OAUTH_TOKEN",
		"CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_BEDROCK"} {
		t.Setenv(v, "")
	}
	app, _ := newApp(t, nil)
	env, code, _ := runApp(t, app, []string{"--dir", t.TempDir(), "x"}, "")
	if code != ExitFailed || env.Error == nil || env.Error.Category != agentrun.CategoryAuth {
		t.Fatalf("code=%d error=%+v", code, env.Error)
	}
	if env.Error.FixHint == nil || len(env.Error.FixHint.Env) == 0 {
		t.Errorf("fix hint = %+v", env.Error.FixHint)
	}
}

func TestEffortComesFromTheFlagThenTheEnvironment(t *testing.T) {
	scripted(t)
	withoutIndex(t)
	t.Setenv(EffortEnv, "low")
	app, seen := newApp(t, nil)
	_, _, _ = runApp(t, app, []string{"--dir", t.TempDir(), "x"}, "")
	if seen.Models.Default.Effort != "low" {
		t.Errorf("env effort = %q, want low", seen.Models.Default.Effort)
	}
	app, seen = newApp(t, nil)
	_, _, _ = runApp(t, app, []string{"--dir", t.TempDir(), "--effort", "off", "x"}, "")
	if seen.Models.Default.Effort != "" {
		t.Errorf("--effort off = %q, want none", seen.Models.Default.Effort)
	}
	t.Setenv(EffortEnv, "loud")
	app, _ = newApp(t, nil)
	env, code, _ := runApp(t, app, []string{"--dir", t.TempDir(), "x"}, "")
	if code != ExitUsage || !strings.Contains(env.Error.Message, EffortEnv) {
		t.Errorf("an invalid $%s: code=%d error=%+v", EffortEnv, code, env.Error)
	}
}

func TestAnEffortTheModelDoesNotTakeIsClampedWithAWarning(t *testing.T) {
	scripted(t)
	withoutIndex(t)
	app, seen := newApp(t, nil)
	env, _, _ := runApp(t, app, []string{"--dir", t.TempDir(), "--model", "claude-haiku-4-5", "--effort", "max", "x"}, "")
	if seen.Models.Default.Effort != "high" {
		t.Errorf("effort = %q, want high", seen.Models.Default.Effort)
	}
	if !hasWarning(env, WarnEffortClamped) {
		t.Errorf("warnings = %+v", env.Warnings)
	}
}

// summaryResult has a trimmed view, so --detail picks between the two.
type summaryResult struct {
	Full   string `json:"full"`
	Detail string `json:"detail"`
}

func (r *summaryResult) SetDetail(d string) { r.Detail = d }
func (r *summaryResult) SummaryView() any   { return map[string]string{"detail": "summary"} }

func TestDetailPicksTheResultView(t *testing.T) {
	scripted(t)
	withoutIndex(t)
	exec := func(context.Context, Deps) (int, any, *ErrorInfo) {
		return ExitOK, &summaryResult{Full: "everything"}, nil
	}
	for detail, want := range map[string]string{"summary": `{"detail":"summary"}`, "full": `{"full":"everything","detail":"full"}`} {
		app, _ := newApp(t, exec)
		var stdout bytes.Buffer
		app.Main(context.Background(), []string{"--dir", t.TempDir(), "--detail", detail, "x"}, strings.NewReader(""), &stdout, &bytes.Buffer{})
		var raw struct {
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		var got bytes.Buffer
		if err := json.Compact(&got, raw.Result); err != nil {
			t.Fatal(err)
		}
		if got.String() != want {
			t.Errorf("--detail %s: result = %s, want %s", detail, got.String(), want)
		}
	}
}

func TestOutputWritesTheSameEnvelopeBeforeStdout(t *testing.T) {
	scripted(t)
	withoutIndex(t)
	out := filepath.Join(t.TempDir(), "nested", "env.json")
	app, _ := newApp(t, nil)
	var stdout bytes.Buffer
	code := app.Main(context.Background(), []string{"--dir", t.TempDir(), "--output", out, "x"}, strings.NewReader(""), &stdout, &bytes.Buffer{})
	if code != ExitOK {
		t.Fatalf("code = %d", code)
	}
	file, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(file, stdout.Bytes()) {
		t.Errorf("--output differs from stdout:\n%s\n---\n%s", file, stdout.String())
	}
}

func TestABareInvocationPrintsHelpAndAUsageEnvelope(t *testing.T) {
	app, _ := newApp(t, nil)
	env, code, stderr := runApp(t, app, nil, "")
	if code != ExitUsage || env.Error == nil || env.Error.Message != NoInputMessage {
		t.Errorf("code=%d env=%+v", code, env)
	}
	if !strings.Contains(stderr, "usage") {
		t.Errorf("stderr = %q, want the help text", stderr)
	}
}

func TestHelpAndVersionPrintNoEnvelope(t *testing.T) {
	app, _ := newApp(t, nil)
	var stdout, stderr bytes.Buffer
	if code := app.Main(context.Background(), []string{"--help"}, strings.NewReader(""), &stdout, &stderr); code != ExitOK || stdout.Len() != 0 {
		t.Errorf("--help: code=%d stdout=%q", code, stdout.String())
	}
	stdout.Reset()
	if code := app.Main(context.Background(), []string{"--version"}, strings.NewReader(""), &stdout, &stderr); code != ExitOK || stdout.String() != "tool test\n" {
		t.Errorf("--version: code=%d stdout=%q", code, stdout.String())
	}
}

func TestPreflightRunsPreflightExecInsteadOfExec(t *testing.T) {
	scripted(t)
	withoutIndex(t)
	app, _ := newApp(t, func(context.Context, Deps) (int, any, *ErrorInfo) {
		t.Fatal("Exec ran under --preflight")
		return 0, nil, nil
	})
	ran := false
	app.PreflightExec = func(context.Context, Deps) (int, any, *ErrorInfo) {
		ran = true
		return ExitOK, nil, nil
	}
	if _, code, _ := runApp(t, app, []string{"--dir", t.TempDir(), "--preflight", "x"}, ""); code != ExitOK || !ran {
		t.Errorf("code=%d ran=%v", code, ran)
	}
}

func TestContextIsCarriedBesideTheInput(t *testing.T) {
	scripted(t)
	withoutIndex(t)
	app, seen := newApp(t, nil)
	env, _, _ := runApp(t, app, []string{"--dir", t.TempDir(), "--context", "first", "--context", "second", "x"}, "")
	want := "## Additional context from the caller\n\nfirst\n\nsecond\n"
	if seen.Input.Context != want || seen.Input.Body != "x" {
		t.Errorf("context = %q, body = %q", seen.Input.Context, seen.Input.Body)
	}
	if env.Input == nil || env.Input.ContextBytes != len(want) {
		t.Errorf("input = %+v", env.Input)
	}
}

func hasWarning(env Envelope, code WarnCode) bool {
	for _, w := range env.Warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}

func TestPhaseModelChoosesOnePhasesModel(t *testing.T) {
	scripted(t)
	withoutIndex(t)
	app, seen := newApp(t, nil)
	env, code, _ := runApp(t, app, []string{"--dir", t.TempDir(),
		"--phase-model", "tasks=claude-haiku-4-5:low", "--phase-model", "prd=ADVANCED", "x"}, "")
	if code != ExitOK {
		t.Fatalf("code = %d: %+v", code, env.Error)
	}
	if got := seen.Runner.ModelFor(agentrun.PhaseTasks).String(); got != "claude-haiku-4-5/low" {
		t.Errorf("tasks runs on %s", got)
	}
	if got := seen.Runner.ModelFor(agentrun.PhasePRD).String(); got != "claude-opus-5-5/xhigh" {
		t.Errorf("prd runs on %s", got)
	}
	if got := seen.Runner.ModelFor(agentrun.PhaseRequirements).String(); got != "claude-sonnet-5-5/high" {
		t.Errorf("requirements runs on %s", got)
	}
	if env.Model == nil || len(env.Model.Phases) != len(agentrun.Phases) ||
		env.Model.Phases["tasks"] != (PhaseModelInfo{ID: "claude-haiku-4-5", Effort: "low"}) {
		t.Errorf("envelope model = %+v", env.Model)
	}
	if env.Model.ID != "claude-sonnet-5-5" || env.Model.Effort != "high" {
		t.Errorf("the default = %s/%s", env.Model.ID, env.Model.Effort)
	}
}

func TestABadPhaseModelIsAUsageError(t *testing.T) {
	for _, v := range []string{"review=ADVANCED", "tasks", "tasks=:high"} {
		app, _ := newApp(t, nil)
		env, code, _ := runApp(t, app, []string{"--phase-model", v, "x"}, "")
		if code != ExitUsage || env.Error == nil || env.Error.Category != "usage" {
			t.Errorf("%q: code=%d error=%+v", v, code, env.Error)
		}
	}
	scripted(t)
	app, _ := newApp(t, nil)
	env, code, _ := runApp(t, app, []string{"--dir", t.TempDir(), "--phase-model", "tasks=gpt-9", "x"}, "")
	if code != ExitFailed || env.Error == nil || env.Error.Category != agentrun.CategoryModel {
		t.Errorf("an unknown phase model: code=%d error=%+v", code, env.Error)
	}
}

func TestClampedEffortsAreWarnedOncePerModel(t *testing.T) {
	scripted(t)
	withoutIndex(t)
	app, _ := newApp(t, nil)
	env, _, _ := runApp(t, app, []string{"--dir", t.TempDir(), "--model", "claude-haiku-4-5", "--effort", "max", "x"}, "")
	var clamps []Warning
	for _, w := range env.Warnings {
		if w.Code == WarnEffortClamped {
			clamps = append(clamps, w)
		}
	}
	if len(clamps) != 1 || !strings.Contains(clamps[0].Message, "prd, prd_review, prd_revision, requirements, test_spec, tasks, architecture") {
		t.Errorf("clamp warnings = %+v", clamps)
	}
}

// TS-09-64 (integration): model.phases, the --phase-model help and the
// effort_clamped warning list prd_review and prd_revision, on a run that
// never mentions --adversarial: they describe the resolved plan, not the
// phases that ran.
//
// Verifies: 09-REQ-10.7
func TestTS09_64_TheReviewPhasesAreListedWithoutAdversarial(t *testing.T) {
	scripted(t)
	withoutIndex(t)
	app, _ := newApp(t, nil)
	env, code, _ := runApp(t, app, []string{"--dir", t.TempDir(), "x"}, "")
	if code != ExitOK || env.Model == nil {
		t.Fatalf("code=%d model=%+v", code, env.Model)
	}
	for _, phase := range []string{agentrun.PhasePRDReview, agentrun.PhasePRDRevision} {
		if _, ok := env.Model.Phases[phase]; !ok {
			t.Errorf("model.phases has no %s: %+v", phase, env.Model.Phases)
		}
	}

	fs := flag.NewFlagSet("tool", flag.ContinueOnError)
	(&Common{}).Register(fs)
	help := fs.Lookup("phase-model").Usage
	if !strings.Contains(help, "prd_review") || !strings.Contains(help, "prd_revision") {
		t.Errorf("--phase-model help = %q", help)
	}

	app, _ = newApp(t, nil)
	env, _, _ = runApp(t, app, []string{"--dir", t.TempDir(), "--model", "claude-haiku-4-5", "--effort", "max", "x"}, "")
	var clamped string
	for _, w := range env.Warnings {
		if w.Code == WarnEffortClamped {
			clamped = w.Message
		}
	}
	if !strings.Contains(clamped, "prd_review") || !strings.Contains(clamped, "prd_revision") {
		t.Errorf("effort_clamped = %q", clamped)
	}
}

// TS-13-1: SiblingCommands is an untyped string constant with the exact sentence,
// so a help text built of raw-string constants can carry it.
func TestSiblingCommandsIsAnUntypedConstantWithTheSentence_TS13_1(t *testing.T) {
	const probe = "a\n" + SiblingCommands // does not compile unless it is a constant string
	const want = "spec writes a package from an idea, specr renders one, speca reports and changes its state."
	if SiblingCommands != want {
		t.Errorf("SiblingCommands = %q; want %q", SiblingCommands, want)
	}
	if probe != "a\n"+want {
		t.Errorf("the constant does not concatenate: %q", probe)
	}
}
