package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/provider/faux"
	"github.com/agent-fox-dev/agentkit-go/tools"

	"github.com/agent-fox-dev/spec/internal/agentrun"
)

// withoutIndex makes the index builder fail, so these tests neither build a
// real index nor depend on one.
func withoutIndex(t *testing.T) {
	t.Helper()
	old := agentrun.NewIndex
	agentrun.NewIndex = func(*tools.Workspace) (tools.Index, error) { return nil, errors.New("no index in tests") }
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
	if seen.Model.ID() != "claude-opus-5-5" || seen.Model.Effort != "xhigh" {
		t.Errorf("model = %+v", seen.Model)
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

func TestUsageErrorsAreRefusedBeforeAnythingRuns(t *testing.T) {
	for _, argv := range [][]string{
		{"--detail", "brief", "x"},
		{"--budget", "-1", "x"},
		{"--total-budget", "-1", "x"},
		{"--max-turns", "-1", "x"},
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
	if seen.Model.Effort != "low" {
		t.Errorf("env effort = %q, want low", seen.Model.Effort)
	}
	app, seen = newApp(t, nil)
	_, _, _ = runApp(t, app, []string{"--dir", t.TempDir(), "--effort", "off", "x"}, "")
	if seen.Model.Effort != "" {
		t.Errorf("--effort off = %q, want none", seen.Model.Effort)
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
	if seen.Model.Effort != "high" {
		t.Errorf("effort = %q, want high", seen.Model.Effort)
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
