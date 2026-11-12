package gate

import (
	"math"

	"github.com/agent-fox-dev/spec/envelope"
)

// DefaultBudgetSeconds is the budget of a full gate when the steering sets
// none: the measured suite crossed it when the slowdown first showed.
const DefaultBudgetSeconds = 120

// findingGrowth is the growth over the base branch's tree past which a full
// gate is a test-cost finding.
const findingGrowth = 0.5

// MeasureBudget measures a gate's seconds against a budget: exceeded only
// when it took longer, and then by how much.
func MeasureBudget(seconds float64, budget int) envelope.GateBudget {
	b := envelope.GateBudget{Seconds: budget}
	if over := roundMillis(seconds - float64(budget)); over > 0 {
		b.Exceeded, b.OverBySeconds = true, &over
	}
	return b
}

// MeasureGrowth is how much longer a gate of seconds took than one of base
// seconds, as a fraction of base.
func MeasureGrowth(seconds, base float64) envelope.GateGrowth {
	return envelope.GateGrowth{BaseSeconds: base, GrewBy: math.Round((seconds-base)/base*10000) / 10000}
}

// TestCost is what a full gate's budget and growth say of the suite's cost:
// a finding when it exceeded the budget or grew by more than half.
func TestCost(budget envelope.GateBudget, growth *envelope.GateGrowth) envelope.GateTestCost {
	c := envelope.GateTestCost{BudgetSeconds: budget.Seconds, Exceeded: budget.Exceeded}
	if growth != nil {
		g := growth.GrewBy
		c.GrewBy = &g
	}
	c.Finding = c.Exceeded || (c.GrewBy != nil && *c.GrewBy > findingGrowth)
	return c
}

// BaseTree is the tree of the merge base of HEAD and the base branch in the
// repository at top: the tree the branch started from, whose full gate is the
// branch's baseline.
func BaseTree(top string) (string, error) {
	base, err := BaseBranch(top)
	if err != nil {
		return "", err
	}
	mb, err := MergeBase(top, base)
	if err != nil {
		return "", err
	}
	return gitOut(top, nil, "rev-parse", "--verify", "--quiet", mb+"^{tree}")
}
