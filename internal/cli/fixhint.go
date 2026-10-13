package cli

import (
	"errors"
	"strings"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/provider/anthropic"

	"github.com/agent-fox-dev/spec/internal/agentrun"
)

// FixHintFor computes a machine-usable remedy for a failure: the flag to
// raise for a ceiling, the environment variables for a missing credential,
// the legal values for an unknown model. It returns nil when no mechanical
// remedy is known.
func FixHintFor(category, stage string, bounds agentrun.Bounds, err error) *FixHint {
	switch category {
	case agentrun.CategoryBudget:
		if stage == "budget" {
			var current float64
			var tb interface{ TotalBudgetUSD() float64 }
			if errors.As(err, &tb) {
				current = tb.TotalBudgetUSD()
			}
			return &FixHint{Flag: "--total-budget", Current: current, Suggest: current * 2}
		}
		return budgetHint(bounds)
	case agentrun.CategoryMaxTurns:
		return turnsHint(bounds)
	case agentrun.CategoryNoResult:
		var holder interface{ RunStopReason() core.RunStopReason }
		if !errors.As(err, &holder) {
			return nil
		}
		switch holder.RunStopReason() {
		case core.RunStopBudgetExceeded:
			return budgetHint(bounds)
		case core.RunStopMaxTurns:
			return turnsHint(bounds)
		}
		return nil
	case agentrun.CategoryAuth:
		msg := ""
		if err != nil {
			msg = strings.ToLower(err.Error())
		}
		switch {
		case strings.Contains(msg, "forge"):
			return &FixHint{Env: []string{"GITHUB_TOKEN", "GH_TOKEN", "GITLAB_TOKEN"}}
		case strings.Contains(msg, "gitlab"):
			return &FixHint{Env: []string{"GITLAB_TOKEN"}}
		case strings.Contains(msg, "github"):
			return &FixHint{Env: []string{"GITHUB_TOKEN", "GH_TOKEN"}}
		}
		return &FixHint{Env: []string{anthropic.APIKeyVar, anthropic.AuthTokenVar, anthropic.VertexEnableVar, anthropic.BedrockEnableVar}}
	case agentrun.CategoryModel:
		valid := make([]string, len(agentrun.Tiers))
		for i, t := range agentrun.Tiers {
			valid[i] = string(t)
		}
		return &FixHint{Flag: "--model", Valid: valid}
	}
	return nil
}

func budgetHint(b agentrun.Bounds) *FixHint {
	curr := b.ResolvedMaxBudget()
	return &FixHint{Flag: "--budget", Current: curr, Suggest: curr * 2}
}

func turnsHint(b agentrun.Bounds) *FixHint {
	curr := float64(b.ResolvedMaxTurns())
	return &FixHint{Flag: "--max-turns", Current: curr, Suggest: curr * 2}
}
