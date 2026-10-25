package cli

import (
	"errors"
	"reflect"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/provider/anthropic"

	"github.com/agent-fox-dev/spec/internal/agentrun"
)

type totalBudgetStop struct{ total float64 }

func (t totalBudgetStop) Error() string           { return "budget exceeded" }
func (t totalBudgetStop) TotalBudgetUSD() float64 { return t.total }

// A failure with a mechanical remedy names it: the flag to raise and by how
// much for a ceiling, the variables for a missing credential, the tiers for an
// unknown model. One without a remedy has no hint.
func TestFixHintFor(t *testing.T) {
	bounds := agentrun.Bounds{MaxBudgetUSD: 2.0, MaxTurns: 20}
	noResult := func(stop core.RunStopReason) error {
		return agentrun.NoResultError("prd", "submit_prd", agentrun.Result{StopReason: stop})
	}
	for _, tc := range []struct {
		name            string
		category, stage string
		err             error
		want            *FixHint
	}{
		{"per-phase budget", "budget", "implement", nil, &FixHint{Flag: "--budget", Current: 2, Suggest: 4}},
		{"total budget", "budget", "budget", totalBudgetStop{total: 10}, &FixHint{Flag: "--total-budget", Current: 10, Suggest: 20}},
		{"max turns", "max_turns", "implement", nil, &FixHint{Flag: "--max-turns", Current: 20, Suggest: 40}},
		{"no result at the turn ceiling", "no_result", "prd", noResult(core.RunStopMaxTurns), &FixHint{Flag: "--max-turns", Current: 20, Suggest: 40}},
		{"no result at the budget", "no_result", "prd", noResult(core.RunStopBudgetExceeded), &FixHint{Flag: "--budget", Current: 2, Suggest: 4}},
		{"no result at the end of a turn", "no_result", "prd", noResult(core.RunStopEndTurn), nil},
		{"no result without a stop reason", "no_result", "prd", errors.New("x"), nil},
		{"forge credential", "auth", "preflight", errors.New("--comment needs a forge credential: set GITHUB_TOKEN, GH_TOKEN, or GITLAB_TOKEN"),
			&FixHint{Env: []string{"GITHUB_TOKEN", "GH_TOKEN", "GITLAB_TOKEN"}}},
		{"GitLab credential", "auth", "input", errors.New("gitlab: 401"), &FixHint{Env: []string{"GITLAB_TOKEN"}}},
		{"GitHub credential", "auth", "input", errors.New("github: 401"), &FixHint{Env: []string{"GITHUB_TOKEN", "GH_TOKEN"}}},
		{"model credential", "auth", "preflight", errors.New("anthropic: missing credentials"),
			&FixHint{Env: []string{anthropic.APIKeyVar, anthropic.AuthTokenVar, anthropic.VertexEnableVar, anthropic.BedrockEnableVar}}},
		{"unknown model", "model", "preflight", errors.New("unknown model spec: nonexistent"),
			&FixHint{Flag: "--model", Valid: []string{"SIMPLE", "STANDARD", "ADVANCED"}}},
		{"no remedy", "api", "prd", errors.New("overloaded"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FixHintFor(tc.category, tc.stage, bounds, tc.err); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("FixHintFor = %+v, want %+v", got, tc.want)
			}
		})
	}
}
