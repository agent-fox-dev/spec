package specgen

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

func summaryKeys(t *testing.T, v any) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(mustJSON(t, v), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func keysOf(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// The summary view keeps what a caller acts on next: validation and
// traceability reduced to the verdict and the gaps, the first package's
// review without its rounds, and a preflight's checklist and estimate; it
// drops follow_on_specs and split_plan, which split[] covers.
func TestSummaryView_KeepsWhatACallerActsOn(t *testing.T) {
	r := &Result{}
	r.Package = Package{
		SpecDir: ".specs/12_thing", SpecID: "12", SpecName: "thing", Title: "Thing", Status: "draft", Source: "text",
		Artifacts: []string{"prd.md", "requirements.json"}, Requirements: 4, Criteria: 8, Tests: 10, Tasks: 5,
		Validation: ValidationReport{Valid: false, ErrorCount: 1, WarningCount: 2,
			Errors:   []ValidationItem{{Check: "x", Message: "missing y"}},
			Warnings: []ValidationItem{{Check: "w", Message: "minor"}}},
		Traceability: TraceReport{CriteriaCovered: 6, CriteriaUncovered: []string{"C-1"},
			PathsCovered: 3, PathsUncovered: []string{"P-2"}, TestsUnowned: []string{"TS-1"}},
		OpenQuestions: []OpenQuestion{{Question: "what about x?", Decision: "assume x", Why: "no reply"}},
		CommentURL:    "https://github.com/o/r/issues/1#comment",
		Review: &Review{RoundsRun: 2, RoundsAllowed: 3, StopReason: "no_findings", FindingsCount: 4,
			AcceptedCount: 2, RejectedCount: 1, UnansweredCount: 1,
			Rounds: []ReviewRound{{Round: 1, Findings: []Finding{{ID: "R1-F1"}}}, {Round: 2, Findings: []Finding{}}}},
	}
	r.FollowOnSpecs = []Package{{SpecDir: ".specs/13_other"}}
	r.Split = []ScopeReport{{Name: "scope-a", Status: ScopeDone}}
	r.SplitPlan = "plan text"
	r.Stage, r.Preflight, r.Estimate = "preflight", []cli.PreflightCheck{{Check: "schemas_valid", OK: true}}, &cli.Estimate{Phases: 4}

	m := summaryKeys(t, r.SummaryView())
	if got, want := keysOf(m), []string{"artifacts", "detail", "estimate", "open_questions", "preflight", "review", "spec_dir",
		"spec_id", "spec_name", "split", "stage", "status", "traceability", "validation"}; !slices.Equal(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
	if got := keysOf(m["validation"].(map[string]any)); !slices.Equal(got, []string{"error_count", "errors", "valid"}) {
		t.Errorf("validation keys = %v", got)
	}
	if got := keysOf(m["traceability"].(map[string]any)); !slices.Equal(got,
		[]string{"criteria_covered", "criteria_uncovered", "paths_covered", "paths_uncovered", "tests_unowned"}) {
		t.Errorf("traceability keys = %v", got)
	}
	if got := keysOf(m["review"].(map[string]any)); slices.Contains(got, "rounds") || !slices.Contains(got, "unanswered_count") {
		t.Errorf("review keys = %v; want the counts without the rounds", got)
	}
	if m["detail"] != "summary" {
		t.Errorf("detail = %v", m["detail"])
	}

	// It still decodes into the envelope's result, and the full result keeps
	// its rounds.
	var sr envelope.SpecResult
	if err := json.Unmarshal(mustJSON(t, r.SummaryView()), &sr); err != nil || sr.Review == nil ||
		sr.Review.FindingsCount != 4 || sr.Estimate == nil || sr.Estimate.Phases != 4 {
		t.Errorf("decoded %+v (%v)", sr, err)
	}
	if len(r.Review.Rounds) != 2 {
		t.Error("the summary view changed the full result's review")
	}

	// Without a review there is none in the view.
	if _, ok := summaryKeys(t, (&Result{}).SummaryView())["review"]; ok {
		t.Error("a run without a review has one in its summary view")
	}
}
