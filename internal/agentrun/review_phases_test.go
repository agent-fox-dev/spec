package agentrun

import (
	"testing"

	"github.com/agent-fox-dev/agentkit-go/core"
)

// TS-09-61 (unit): --phase-model accepts the two review phases, and each tier
// resolves them, when nothing chooses for them, to the PRD phase's model and
// effort.
//
// Verifies: 09-REQ-10.5
func TestTS09_61_TheReviewPhasesResolveToTheTiersBaseEntry(t *testing.T) {
	for _, raw := range []string{"prd_review=claude-opus-5-5", "prd_revision=SIMPLE:low"} {
		if _, err := ParsePhaseChoice(raw); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
	}
	for tier, want := range map[string]string{
		"SIMPLE":   "claude-sonnet-5-5/medium",
		"STANDARD": "claude-sonnet-5-5/high",
		"ADVANCED": "claude-opus-5-5/xhigh",
	} {
		plan, err := ResolvePlan(tier, "", false, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, phase := range []string{PhasePRDReview, PhasePRDRevision} {
			if got := plan.For(phase).String(); got != want {
				t.Errorf("%s %s = %s, want %s", tier, phase, got, want)
			}
		}
	}
	if PhasePRDReview != "prd_review" || PhasePRDRevision != "prd_revision" {
		t.Errorf("phase names = %q, %q", PhasePRDReview, PhasePRDRevision)
	}
	// Built in package order: the review comes right after the PRD it reviews.
	if len(Phases) < 3 || Phases[0] != PhasePRD || Phases[1] != PhasePRDReview || Phases[2] != PhasePRDRevision {
		t.Errorf("Phases = %v", Phases)
	}
}

// TS-09-62 (unit): --model and --effort apply to the review phases, a choice
// for prd does not move them, and a choice for prd_review moves only it.
//
// Verifies: 09-REQ-10.6
func TestTS09_62_ModelAndEffortReachTheReviewPhasesAndPRDChoicesDoNot(t *testing.T) {
	plan, err := ResolvePlan("claude-haiku-4-5", core.EffortHigh, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{PhasePRDReview, PhasePRDRevision} {
		if got := plan.For(phase).String(); got != "claude-haiku-4-5/high" {
			t.Errorf("--model/--effort: %s = %s", phase, got)
		}
	}

	plan, err = ResolvePlan("STANDARD", "", false, []PhaseChoice{mustChoice(t, "prd=claude-opus-5-5")})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.For(PhasePRD).String(); got != "claude-opus-5-5" {
		t.Errorf("prd = %s", got)
	}
	for _, phase := range []string{PhasePRDReview, PhasePRDRevision} {
		if got := plan.For(phase).String(); got != "claude-sonnet-5-5/high" {
			t.Errorf("--phase-model prd moved %s to %s", phase, got)
		}
	}

	plan, err = ResolvePlan("STANDARD", "", false, []PhaseChoice{mustChoice(t, "prd_review=claude-haiku-4-5:low")})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.For(PhasePRDReview).String(); got != "claude-haiku-4-5/low" {
		t.Errorf("prd_review = %s", got)
	}
	if got := plan.For(PhasePRDRevision).String(); got != "claude-sonnet-5-5/high" {
		t.Errorf("--phase-model prd_review moved prd_revision to %s", got)
	}
}
