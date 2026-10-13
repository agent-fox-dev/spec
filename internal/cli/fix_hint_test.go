package cli_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/cli"
)

type totalBudgetErrHolder struct {
	total float64
}

func (t totalBudgetErrHolder) Error() string           { return "budget exceeded" }
func (t totalBudgetErrHolder) TotalBudgetUSD() float64 { return t.total }

// TS-05-27 (unit): fix_hint for the budget and max_turns categories names the flag, the resolved current bound, and a suggestion double it
func TestTS05_27_FixHintBudgetAndMaxTurns(t *testing.T) {
	bounds := agentrun.Bounds{MaxBudgetUSD: 2.0, MaxTurns: 20}

	hintB := cli.FixHintFor("budget", "implement", bounds, nil)
	if hintB == nil {
		t.Fatal("expected non-nil FixHint for budget")
	}
	wantB := cli.FixHint{Flag: "--budget", Current: 2.0, Suggest: 4.0}
	if !reflect.DeepEqual(*hintB, wantB) {
		t.Errorf("hintB = %+v, want %+v", *hintB, wantB)
	}

	hintT := cli.FixHintFor("max_turns", "implement", bounds, nil)
	if hintT == nil {
		t.Fatal("expected non-nil FixHint for max_turns")
	}
	wantT := cli.FixHint{Flag: "--max-turns", Current: 20.0, Suggest: 40.0}
	if !reflect.DeepEqual(*hintT, wantT) {
		t.Errorf("hintT = %+v, want %+v", *hintT, wantT)
	}
}

// TS-05-30 (unit): fix_hint for the model category is the static tier list, regardless of what model spec was asked for
func TestTS05_30_FixHintModel(t *testing.T) {
	bounds := agentrun.Bounds{MaxBudgetUSD: 2.0, MaxTurns: 20}
	unresolvableModelErr := errors.New("unknown model spec: nonexistent")

	hint := cli.FixHintFor("model", "preflight", bounds, unresolvableModelErr)
	if hint == nil {
		t.Fatal("expected non-nil FixHint for model")
	}
	if hint.Flag != "--model" {
		t.Errorf("hint.Flag = %q, want %q", hint.Flag, "--model")
	}
	wantValid := []string{"SIMPLE", "STANDARD", "ADVANCED"}
	if !reflect.DeepEqual(hint.Valid, wantValid) {
		t.Errorf("hint.Valid = %v, want %v", hint.Valid, wantValid)
	}
}

// TS-05-32 (unit): A budget-category error whose stage is literally "budget" gets a --total-budget fix_hint instead of the per-phase --budget one
func TestTS05_32_FixHintTotalBudget(t *testing.T) {
	bounds := agentrun.Bounds{MaxBudgetUSD: 2.0, MaxTurns: 20}
	totalBudgetErr := totalBudgetErrHolder{total: 10.0}

	h1 := cli.FixHintFor("budget", "budget", bounds, totalBudgetErr)
	if h1 == nil {
		t.Fatal("expected non-nil FixHint for total budget error")
	}
	if h1.Flag != "--total-budget" || h1.Current != 10.0 || h1.Suggest != 20.0 {
		t.Errorf("h1 = %+v, want Flag: --total-budget, Current: 10.0, Suggest: 20.0", h1)
	}

	h2 := cli.FixHintFor("budget", "implement", bounds, nil)
	if h2 == nil {
		t.Fatal("expected non-nil FixHint for per-phase budget error")
	}
	if h2.Flag != "--budget" || h2.Current != 2.0 || h2.Suggest != 4.0 {
		t.Errorf("h2 = %+v, want Flag: --budget, Current: 2.0, Suggest: 4.0", h2)
	}
}

func TestEnvelopeAttachesFixHint(t *testing.T) {
	r := cli.NewRun("fix", "v1")
	r.SetBounds(agentrun.Bounds{MaxBudgetUSD: 3.5, MaxTurns: 40})

	env := r.Envelope(cli.ExitFailed, nil, &cli.ErrorInfo{
		Stage:    "implement",
		Category: "budget",
		Message:  "budget exceeded",
	})
	if env.Error == nil {
		t.Fatal("expected non-nil Error")
	}
	if env.Error.FixHint == nil {
		t.Fatal("expected non-nil FixHint on envelope error")
	}
	want := cli.FixHint{Flag: "--budget", Current: 3.5, Suggest: 7.0}
	if !reflect.DeepEqual(*env.Error.FixHint, want) {
		t.Errorf("FixHint = %+v, want %+v", *env.Error.FixHint, want)
	}
}

// A phase that ended without a result at a ceiling names the flag that
// raises that ceiling; at any other stop there is no mechanical remedy.
func TestFixHintNoResultNamesTheCeilingItStoppedAt(t *testing.T) {
	bounds := agentrun.Bounds{MaxBudgetUSD: 2.0, MaxTurns: 20}
	h := cli.FixHintFor("no_result", "prd", bounds,
		agentrun.NoResultError("prd", "submit_prd", agentrun.Result{StopReason: core.RunStopMaxTurns}))
	if h == nil || h.Flag != "--max-turns" || h.Current != 20 || h.Suggest != 40 {
		t.Errorf("max turns: hint = %+v", h)
	}
	h = cli.FixHintFor("no_result", "prd", bounds,
		agentrun.NoResultError("prd", "submit_prd", agentrun.Result{StopReason: core.RunStopBudgetExceeded}))
	if h == nil || h.Flag != "--budget" || h.Current != 2 {
		t.Errorf("budget: hint = %+v", h)
	}
	if h := cli.FixHintFor("no_result", "prd", bounds,
		agentrun.NoResultError("prd", "submit_prd", agentrun.Result{StopReason: core.RunStopEndTurn})); h != nil {
		t.Errorf("end turn: hint = %+v, want none", h)
	}
}

// A missing credential names the variables that supply one: the forge's for
// a forge credential, Anthropic's deployments otherwise.
func TestFixHintAuthNamesTheVariables(t *testing.T) {
	var b agentrun.Bounds
	forge := cli.FixHintFor("auth", "preflight", b, errors.New("--comment needs a forge credential: set GITHUB_TOKEN, GH_TOKEN, or GITLAB_TOKEN"))
	if forge == nil || !reflect.DeepEqual(forge.Env, []string{"GITHUB_TOKEN", "GH_TOKEN", "GITLAB_TOKEN"}) {
		t.Errorf("forge hint = %+v", forge)
	}
	model := cli.FixHintFor("auth", "preflight", b, errors.New("anthropic: missing credentials"))
	if model == nil || len(model.Env) == 0 || model.Env[0] != "ANTHROPIC_API_KEY" {
		t.Errorf("model hint = %+v", model)
	}
}
