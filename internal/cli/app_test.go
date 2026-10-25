package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/provider/faux"

	"github.com/agent-fox-dev/spec/internal/agentrun"
)

func TestMain_RunsExecWithTheResolvedDeps(t *testing.T) {
	scripted(t)
	app, seen := newApp(t, nil)
	env, code, _ := runApp(t, app, []string{"--dir", t.TempDir(), "--model", "ADVANCED", "an idea"}, "")
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
	if len(warningsOf(env.Warnings, WarnCodeSearchUnavailable)) != 1 {
		t.Errorf("warnings = %+v, want code_search_unavailable", env.Warnings)
	}
}

// Every invocation that cannot hold is a usage error, before the input is read,
// a model resolved or Exec run, and the message names what was wrong.
func TestMain_RefusesAWrongInvocationBeforeAnythingRuns(t *testing.T) {
	outDir := t.TempDir()
	for _, tc := range []struct {
		name    string
		argv    []string
		mention string
	}{
		{"unknown detail", []string{"--detail", "brief", "x"}, "--detail"},
		{"negative budget", []string{"--budget", "-1", "x"}, "--budget cannot be negative"},
		{"negative total budget", []string{"--total-budget", "-1", "x"}, "--total-budget cannot be negative"},
		{"negative max turns", []string{"--max-turns", "-1", "x"}, "--max-turns cannot be negative"},
		{"negative max tokens", []string{"--max-tokens", "-1", "x"}, "--max-tokens cannot be negative"},
		{"unknown input kind", []string{"--input-kind", "url", "x"}, "--input-kind"},
		{"unknown effort", []string{"--effort", "huge", "x"}, "huge"},
		{"undefined flag", []string{"--no-such-flag", "x"}, "no-such-flag"},
		{"two inputs", []string{"one", "two"}, "expected one input"},
		{"output to stdin", []string{"--output", "-", "x"}, "--output"},
		{"output to a directory", []string{"--output", outDir, "x"}, "directory"},
		{"phase model naming no phase", []string{"--phase-model", "review=ADVANCED", "x"}, "names no phase"},
		{"phase model without a model", []string{"--phase-model", "tasks", "x"}, "PHASE=MODEL"},
		{"phase model with only an effort", []string{"--phase-model", "tasks=:high", "x"}, "names no model"},
		{"input and context over the bound", []string{"--context", strings.Repeat("x", MaxInputBytes), "x"}, "exceed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, code, _ := runApp(t, refusingApp(t), tc.argv, "")
			if code != ExitUsage || env.Status != "usage" || env.OK || env.Error == nil || env.Error.Category != "usage" {
				t.Fatalf("code=%d env=%+v", code, env)
			}
			if !strings.Contains(env.Error.Message, tc.mention) {
				t.Errorf("message %q does not mention %q", env.Error.Message, tc.mention)
			}
		})
	}
}

// --output is checked before PreCheck, the workspace, the input and the model:
// a --dir that does not exist and no credential would otherwise fail first.
func TestMain_ChecksOutputBeforeAnythingElse(t *testing.T) {
	var preCheck, checkInput bool
	app := refusingApp(t)
	app.PreCheck = func(*Common) error { preCheck = true; return nil }
	app.CheckInput = func(Input) error { checkInput = true; return nil }

	_, stderr, code := runMain(t, app, []string{"--dir", filepath.Join(t.TempDir(), "missing"), "--output", "-", "x"}, "")
	if code != ExitUsage {
		t.Fatalf("code = %d, want %d", code, ExitUsage)
	}
	if preCheck || checkInput {
		t.Errorf("PreCheck=%v CheckInput=%v, want neither called", preCheck, checkInput)
	}
	if !strings.Contains(stderr.String(), "--output") {
		t.Errorf("stderr %q does not name --output", stderr.String())
	}
}

// A model that cannot be resolved, for the run or for one phase, is a model
// failure at preflight that names the tiers.
func TestMain_AnUnknownModelIsAModelFailure(t *testing.T) {
	for name, argv := range map[string][]string{
		"--model":       {"--model", "gpt-9", "x"},
		"--phase-model": {"--phase-model", "tasks=gpt-9", "x"},
	} {
		t.Run(name, func(t *testing.T) {
			scripted(t)
			env, code, _ := runApp(t, refusingApp(t), append([]string{"--dir", t.TempDir()}, argv...), "")
			if code != ExitFailed || env.Error == nil || env.Error.Category != agentrun.CategoryModel || env.Error.Stage != "preflight" {
				t.Fatalf("code=%d error=%+v", code, env.Error)
			}
			if env.Error.FixHint == nil || env.Error.FixHint.Flag != "--model" {
				t.Errorf("fix hint = %+v", env.Error.FixHint)
			}
		})
	}
}

func TestMain_AMissingCredentialIsAnAuthFailure(t *testing.T) {
	for _, v := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_OAUTH_TOKEN",
		"CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_BEDROCK"} {
		t.Setenv(v, "")
	}
	env, code, _ := runApp(t, refusingApp(t), []string{"--dir", t.TempDir(), "x"}, "")
	if code != ExitFailed || env.Error == nil || env.Error.Category != agentrun.CategoryAuth {
		t.Fatalf("code=%d error=%+v", code, env.Error)
	}
	if env.Error.FixHint == nil || len(env.Error.FixHint.Env) == 0 {
		t.Errorf("fix hint = %+v", env.Error.FixHint)
	}
}

// With a credential in the environment and no provider set, the run builds
// its model client from the environment, without a request, and Exec gets a
// Runner.
func TestMain_ACredentialInTheEnvironmentReachesTheRunner(t *testing.T) {
	requests := noNetwork(t)
	withoutIndex(t)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	app, deps := newApp(t, nil)
	env, code, _ := runApp(t, app, []string{"--dir", t.TempDir(), "x"}, "")
	if code != ExitOK || !env.OK {
		t.Fatalf("code=%d error=%+v", code, env.Error)
	}
	if deps.Runner == nil {
		t.Error("Exec got no Runner")
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("%d network requests before any phase ran", n)
	}
}

// --input-kind skips the guessing: a mismatch is a usage error before anything
// is fetched, a model resolved (no credential is set, so that would fail
// differently) or Exec run, and a match is read as that kind.
func TestMain_InputKindForcesTheClassification(t *testing.T) {
	requests := noNetwork(t)
	t.Setenv("ANTHROPIC_API_KEY", "")
	dir := t.TempDir()
	for _, m := range []struct{ kind, arg string }{
		{"file", "https://github.com/a/b/issues/1"},
		{"file", filepath.Join(dir, "missing.txt")},
		{"file", dir},
		{"issue", "a plain string"},
		{"issue", "https://example.com/not/a/forge/issues/1"},
		{"stdin", "not-a-dash"},
	} {
		t.Run(m.kind+" "+m.arg, func(t *testing.T) {
			env, code, _ := runApp(t, refusingApp(t), []string{"--dir", dir, "--input-kind", m.kind, m.arg}, "piped")
			if code != ExitUsage || env.Error == nil || env.Error.Category != "usage" {
				t.Errorf("code=%d error=%+v", code, env.Error)
			}
		})
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("%d HTTP requests were made", n)
	}

	t.Run("stdin -", func(t *testing.T) {
		scripted(t)
		app, seen := newApp(t, nil)
		env, code, _ := runApp(t, app, []string{"--dir", dir, "--input-kind", "stdin", "-"}, "piped")
		if code != ExitOK || env.Input == nil || env.Input.Kind != string(KindStdin) || seen.Input.Body != "piped" {
			t.Errorf("code=%d input=%+v", code, env.Input)
		}
	})
}

// A single-phase command folds --total-budget into the per-phase ceiling; a
// multi-phase one keeps the per-phase --budget.
func TestMain_TotalBudgetLowersTheOnePhasesCeiling(t *testing.T) {
	for _, tc := range []struct {
		name   string
		single bool
		want   float64
	}{{"single phase", true, 4}, {"several phases", false, 10}} {
		t.Run(tc.name, func(t *testing.T) {
			scripted(t)
			app, seen := newApp(t, nil)
			app.SinglePhase = tc.single
			app.DefaultBounds = agentrun.Bounds{MaxTurns: 10, MaxBudgetUSD: 2}
			_, code, _ := runApp(t, app, []string{"--dir", t.TempDir(), "--budget", "10", "--total-budget", "4", "x"}, "")
			if code != ExitOK {
				t.Fatalf("code = %d", code)
			}
			if _, got := seen.Runner.ResolvedBounds(); got != tc.want {
				t.Errorf("ceiling = %v, want %v", got, tc.want)
			}
		})
	}
}

// --max-tokens is the cap on one response of every phase, and it has to reach
// the request a phase sends, not just the parsed flags. A phase that asks for
// its own cap gets the flag's instead; with no flag it keeps its own.
func TestMain_MaxTokensReachesTheRequestsOfTheRun(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		want int
	}{
		{"no flag keeps the phase's own cap", nil, 65536},
		{"the flag replaces it", []string{"--max-tokens", "12345"}, 12345},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withoutIndex(t)
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

// The effort comes from --effort, then $AF_MODEL_EFFORT; "off" sends none, and
// an invalid environment value is a usage error naming the variable.
func TestMain_EffortComesFromTheFlagThenTheEnvironment(t *testing.T) {
	t.Run("environment", func(t *testing.T) {
		scripted(t)
		t.Setenv(EffortEnv, "low")
		app, seen := newApp(t, nil)
		_, _, _ = runApp(t, app, []string{"--dir", t.TempDir(), "x"}, "")
		if seen.Models.Default.Effort != "low" {
			t.Errorf("effort = %q, want low", seen.Models.Default.Effort)
		}
	})
	t.Run("the flag wins", func(t *testing.T) {
		scripted(t)
		t.Setenv(EffortEnv, "low")
		app, seen := newApp(t, nil)
		_, _, _ = runApp(t, app, []string{"--dir", t.TempDir(), "--effort", "off", "x"}, "")
		if seen.Models.Default.Effort != "" {
			t.Errorf("--effort off = %q, want none", seen.Models.Default.Effort)
		}
	})
	t.Run("an invalid environment value", func(t *testing.T) {
		scripted(t)
		t.Setenv(EffortEnv, "loud")
		env, code, _ := runApp(t, refusingApp(t), []string{"--dir", t.TempDir(), "x"}, "")
		if code != ExitUsage || env.Error == nil || !strings.Contains(env.Error.Message, EffortEnv) {
			t.Errorf("code=%d error=%+v", code, env.Error)
		}
	})
}

// An effort the model does not take is lowered to the highest one it does, and
// one warning per model names every phase it applies to, the review phases
// included on a run that never asks for a review: the warning describes the
// resolved plan, not the phases that ran.
func TestMain_AClampedEffortIsWarnedOncePerModel(t *testing.T) {
	scripted(t)
	app, seen := newApp(t, nil)
	env, _, _ := runApp(t, app, []string{"--dir", t.TempDir(), "--model", "claude-haiku-4-5", "--effort", "max", "x"}, "")
	if seen.Models.Default.Effort != "high" {
		t.Errorf("effort = %q, want high", seen.Models.Default.Effort)
	}
	clamps := warningsOf(env.Warnings, WarnEffortClamped)
	if len(clamps) != 1 || !strings.Contains(clamps[0].Message, "prd, prd_review, prd_revision, requirements, test_spec, tasks, architecture") {
		t.Errorf("clamp warnings = %+v", clamps)
	}
}

// --phase-model chooses one phase's model and effort; the others keep the
// tier's, and the envelope records every phase of the plan.
func TestMain_PhaseModelChoosesOnePhasesModel(t *testing.T) {
	scripted(t)
	app, seen := newApp(t, nil)
	env, code, _ := runApp(t, app, []string{"--dir", t.TempDir(),
		"--phase-model", "tasks=claude-haiku-4-5:low", "--phase-model", "prd=ADVANCED", "x"}, "")
	if code != ExitOK {
		t.Fatalf("code = %d: %+v", code, env.Error)
	}
	for phase, want := range map[string]string{
		agentrun.PhaseTasks:        "claude-haiku-4-5/low",
		agentrun.PhasePRD:          "claude-opus-5-5/xhigh",
		agentrun.PhaseRequirements: "claude-sonnet-5-5/high",
	} {
		if got := seen.Runner.ModelFor(phase).String(); got != want {
			t.Errorf("%s runs on %s, want %s", phase, got, want)
		}
	}
	if env.Model == nil || len(env.Model.Phases) != len(agentrun.Phases) ||
		env.Model.Phases["tasks"] != (PhaseModelInfo{ID: "claude-haiku-4-5", Effort: "low"}) {
		t.Fatalf("envelope model = %+v", env.Model)
	}
	for _, phase := range []string{agentrun.PhasePRDReview, agentrun.PhasePRDRevision} {
		if _, ok := env.Model.Phases[phase]; !ok {
			t.Errorf("model.phases has no %s", phase)
		}
	}
	if env.Model.ID != "claude-sonnet-5-5" || env.Model.Effort != "high" {
		t.Errorf("the default = %s/%s", env.Model.ID, env.Model.Effort)
	}
}

// summaryResult has a trimmed view, so --detail picks between the two.
type summaryResult struct {
	Full   string `json:"full"`
	Detail string `json:"detail"`
}

func (r *summaryResult) SetDetail(d string) { r.Detail = d }
func (r *summaryResult) SummaryView() any   { return map[string]string{"detail": "summary"} }

func TestMain_DetailPicksTheResultView(t *testing.T) {
	exec := func(context.Context, Deps) (int, any, *ErrorInfo) {
		return ExitOK, &summaryResult{Full: "everything"}, nil
	}
	for detail, want := range map[string]string{"summary": `{"detail":"summary"}`, "full": `{"full":"everything","detail":"full"}`} {
		t.Run(detail, func(t *testing.T) {
			scripted(t)
			app, _ := newApp(t, exec)
			stdout, _, _ := runMain(t, app, []string{"--dir", t.TempDir(), "--detail", detail, "x"}, "")
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
				t.Errorf("result = %s, want %s", got.String(), want)
			}
		})
	}
}

func TestMain_OutputWritesTheSameEnvelopeBeforeStdout(t *testing.T) {
	scripted(t)
	out := filepath.Join(t.TempDir(), "nested", "env.json")
	app, _ := newApp(t, nil)
	stdout, _, code := runMain(t, app, []string{"--dir", t.TempDir(), "--output", out, "x"}, "")
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

// A failed --output write is one low-severity output_not_written warning on
// the same run's stdout envelope, naming the path and why, and the exit code is
// unaffected.
func TestMain_AFailedOutputWriteIsAWarning(t *testing.T) {
	scripted(t)
	unwritable := unwritableOutput(t)
	app, _ := newApp(t, nil)
	env, code, _ := runApp(t, app, []string{"--dir", t.TempDir(), "--output", unwritable, "x"}, "")
	if code != ExitOK || !env.OK || env.ExitCode != ExitOK || env.Status != "done" {
		t.Errorf("code=%d ok=%v exit_code=%d status=%q, want a clean success", code, env.OK, env.ExitCode, env.Status)
	}
	ws := warningsOf(env.Warnings, WarnOutputNotWritten)
	if len(ws) != 1 {
		t.Fatalf("output_not_written warnings = %+v, want one", ws)
	}
	if w := ws[0]; w.Severity != "low" || w.Stage != "emit" || !strings.Contains(w.Message, unwritable) ||
		strings.TrimSpace(strings.TrimPrefix(w.Message, unwritable)) == "" {
		t.Errorf("warning = %+v, want low, emit, naming the path and the cause", w)
	}
	if _, err := os.Stat(unwritable); err == nil {
		t.Error("a file exists at the unwritable path")
	}
}

// A bare invocation prints the help to stderr and, for a program reading
// stdout, a usage envelope.
func TestMain_ABareInvocationPrintsHelpAndAUsageEnvelope(t *testing.T) {
	env, code, stderr := runApp(t, refusingApp(t), nil, "")
	if code != ExitUsage || env.Error == nil || env.Error.Message != NoInputMessage {
		t.Errorf("code=%d env=%+v", code, env)
	}
	if !strings.Contains(stderr, "usage") {
		t.Errorf("stderr = %q, want the help text", stderr)
	}
}

func TestMain_HelpAndVersionPrintNoEnvelope(t *testing.T) {
	for _, tc := range []struct {
		flag, stdout string
	}{{"--help", ""}, {"--version", "tool test\n"}} {
		t.Run(tc.flag, func(t *testing.T) {
			if stdout, _, code := runMain(t, refusingApp(t), []string{tc.flag}, ""); code != ExitOK || stdout.String() != tc.stdout {
				t.Errorf("code=%d stdout=%q", code, stdout.String())
			}
		})
	}
}

// --preflight runs PreflightExec in place of Exec, and a command without one
// refuses it.
func TestMain_PreflightRunsPreflightExecInsteadOfExec(t *testing.T) {
	t.Run("supported", func(t *testing.T) {
		scripted(t)
		app := refusingApp(t)
		ran := false
		app.PreflightExec = func(context.Context, Deps) (int, any, *ErrorInfo) {
			ran = true
			return ExitOK, nil, nil
		}
		if _, code, _ := runApp(t, app, []string{"--dir", t.TempDir(), "--preflight", "x"}, ""); code != ExitOK || !ran {
			t.Errorf("code=%d ran=%v", code, ran)
		}
	})
	t.Run("unsupported", func(t *testing.T) {
		scripted(t)
		env, code, _ := runApp(t, refusingApp(t), []string{"--dir", t.TempDir(), "--preflight", "x"}, "")
		if code != ExitFailed || env.Error == nil || !strings.Contains(env.Error.Message, "does not support --preflight") {
			t.Errorf("code=%d error=%+v", code, env.Error)
		}
	})
}

func TestMain_ContextIsCarriedBesideTheInput(t *testing.T) {
	scripted(t)
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
