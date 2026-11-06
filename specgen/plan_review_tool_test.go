package specgen

import (
	"fmt"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/core"

	"github.com/agent-fox-dev/spec/afspec"
)

// planUnderReview is the fixture's plan: its tasks, the derived integration
// task among them, its criteria and its execution paths are the ids a plan
// finding's section may name.
func planUnderReview(t *testing.T) *afspec.PartialSpec {
	t.Helper()
	artifacts, _ := loadFixture(t, "01", "test_feature")
	return acceptedArtifacts(t, artifacts, afspec.GenerationSteps...)
}

// planFacts are grounding facts for one path that exists and one that does
// not.
var planFacts = []groundingFact{{"afspec/afspec.go", true}, {"afspec/missing_test.go", false}}

// planFinding is a well-formed plan finding about task 1.
func planFinding() reviewInput {
	return reviewInput{Severity: "major", Rubric: "zero_decision", Section: "task 1", Flaw: "The step leaves the file open.",
		Evidence: "the step says 'as appropriate'", Remedy: "name the file"}
}

func submitPlanReview(t *testing.T, plan *afspec.PartialSpec, steering string, findings ...reviewInput) (core.ToolResult, bool) {
	t.Helper()
	var sink reviewSink
	r := submitReview(t, newPlanReviewTool(&sink, 1, nil, plan, planFacts, steering), findings)
	_, recorded := sink.get()
	return r, recorded
}

// planCase runs one submission and checks it was accepted, or turned back
// saying each of says.
func planCase(t *testing.T, plan *afspec.PartialSpec, steering string, f reviewInput, says ...string) {
	t.Helper()
	r, recorded := submitPlanReview(t, plan, steering, f)
	if len(says) == 0 {
		if !r.OK || !recorded {
			t.Errorf("turned back: %+v", r)
		}
		return
	}
	if r.OK || recorded {
		t.Fatalf("accepted: %+v", r)
	}
	for _, s := range says {
		if !strings.Contains(r.Detail, s) {
			t.Errorf("the error does not say %q: %s", s, r.Detail)
		}
	}
}

func TestPlanReviewTool_TakesARubricAndASeverityOfItsSets(t *testing.T) {
	plan := planUnderReview(t)
	for _, tc := range []struct {
		rubric, severity string
		says             []string
	}{
		{"zero_decision", "major", nil},
		{"grounding", "critical", nil},
		{"steering", "major", nil},
		{"style", "major", []string{"finding 1", "zero_decision, grounding or steering"}},
		{"", "major", []string{"finding 1", "zero_decision, grounding or steering"}},
		{"zero_decision", "minor", nil},
		{"zero_decision", "blocker", []string{"finding 1", "critical, major, minor"}},
		{"zero_decision", "", []string{"finding 1", "critical, major, minor"}},
	} {
		t.Run(tc.rubric+"/"+tc.severity, func(t *testing.T) {
			f := planFinding()
			f.Rubric, f.Severity = tc.rubric, tc.severity
			if tc.rubric == "steering" {
				f.Evidence = `"` + steeringDirective + `" against task 1`
			}
			planCase(t, plan, steeringBody, f, tc.says...)
		})
	}
}

func TestPlanReviewTool_TakesASectionThatNamesAPlaceInThePlan(t *testing.T) {
	plan := planUnderReview(t)
	criterion, path := plan.Requirements.Requirements[0].Criteria[0].Id, plan.Requirements.ExecutionPaths[0].Id
	// The fixture stores two tasks; the integration task is the derived
	// third, a place in the plan the reviewer reads.
	spec := &afspec.Spec{Profile: plan.Profile, Requirements: plan.Requirements, Tasks: plan.Tasks}
	integration, ok := spec.IntegrationTaskID()
	if !ok || integration != 3 {
		t.Fatalf("the integration task is %d, %v; want task 3", integration, ok)
	}
	for _, tc := range []struct {
		section string
		ok      bool
	}{
		{criterion, true}, {path, true}, {"task 2", true}, {fmt.Sprintf("task %d", integration), true},
		{"REQ-99.9", false}, {"PATH-99", false}, {"task 99", false}, {fmt.Sprintf("task %d", integration+1), false}, {"Goals", false},
	} {
		t.Run(tc.section, func(t *testing.T) {
			f := planFinding()
			f.Section = tc.section
			if tc.ok {
				planCase(t, plan, "", f)
				return
			}
			planCase(t, plan, "", f, "finding 1", fmt.Sprintf("%q", tc.section), "a task by its number, a criterion id or an execution path id")
		})
	}
}

func TestPlanReviewTool_HoldsAMissingPathFindingToTheGroundingFacts(t *testing.T) {
	plan := planUnderReview(t)
	missing := func(rubric, path string) reviewInput {
		f := planFinding()
		f.Rubric, f.Flaw, f.Evidence = rubric, "The path `"+path+"` does not exist and no step creates it.", "grounding fact: `"+path+"` does not exist"
		return f
	}
	planCase(t, plan, "", missing("grounding", "afspec/missing_test.go"))
	planCase(t, plan, "", missing("grounding", "afspec/afspec.go"))
	planCase(t, plan, "", missing("grounding", "cmd/other.go"), "finding 1", "cmd/other.go", "grounding fact")
	planCase(t, plan, "", missing("zero_decision", "cmd/other.go"))
}

func TestPlanReviewTool_NeedsEveryFieldAndAtMostThirtyFindings(t *testing.T) {
	plan := planUnderReview(t)
	for _, empty := range [][]string{{"severity"}, {"section"}, {"flaw"}, {"evidence"}, {"remedy"}, {"section", "flaw", "remedy"}} {
		t.Run(strings.Join(empty, ","), func(t *testing.T) {
			f := planFinding()
			for _, field := range empty {
				switch field {
				case "severity":
					f.Severity = ""
				case "section":
					f.Section = ""
				case "flaw":
					f.Flaw = ""
				case "evidence":
					f.Evidence = ""
				case "remedy":
					f.Remedy = ""
				}
			}
			planCase(t, plan, "", f, "finding 1 has an empty "+strings.Join(empty, ", "), "support or withdraw")
		})
	}
	for _, n := range []int{29, 30, 31} {
		findings := make([]reviewInput, n)
		for i := range findings {
			findings[i] = planFinding()
		}
		r, recorded := submitPlanReview(t, plan, "", findings...)
		if ok := n <= maxFindings; r.OK != ok || recorded != ok {
			t.Errorf("%d findings: accepted %v; want %v", n, r.OK, ok)
		}
		if n > maxFindings && (!strings.Contains(r.Detail, "31") || !strings.Contains(r.Detail, "30")) {
			t.Errorf("%d findings: %s", n, r.Detail)
		}
	}
}

func TestPlanReviewTool_HoldsASteeringFindingToItsDirective(t *testing.T) {
	plan := planUnderReview(t)
	steering := func(severity, evidence string) reviewInput {
		f := planFinding()
		f.Rubric, f.Severity, f.Evidence = "steering", severity, evidence
		return f
	}
	planCase(t, plan, steeringBody, steering("major", `"`+steeringDirective+`" against REQ-1.1's check`))
	planCase(t, plan, steeringBody, steering("major", "`Tests are   organised by\nbehaviour, not by spec.` against task 1"))
	planCase(t, plan, steeringBody, steering("major", `"Every test lives alone."`), "finding 1", "quote the directive it contradicts")
	planCase(t, plan, steeringBody, steering("critical", `"`+steeringDirective+`"`))
	planCase(t, plan, steeringBody, steering("minor", `"`+steeringDirective+`"`), "finding 1", "critical or major")
	f := planFinding()
	f.Severity = "minor"
	planCase(t, plan, steeringBody, f)
}
