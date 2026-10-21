package specgen

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// TS-06-19 (unit): spec's summary view reduces validation and traceability
// and drops follow_on_specs and split_plan (06-REQ-3.4).
func TestTS06_19_SummaryViewReducesValidationAndTraceabilityAndDropsFollowOnAndSplitPlan(t *testing.T) {
	r := &Result{
		Package: Package{
			SpecDir:        ".specs/12_thing",
			SpecID:         "12_thing",
			SpecName:       "thing",
			Title:          "Thing",
			Status:         "draft",
			Source:         "text",
			Artifacts:      []string{"prd.md", "requirements.json"},
			Requirements:   4,
			Criteria:       8,
			ExecutionPaths: 2,
			Tests:          10,
			Tasks:          5,
			Validation: ValidationReport{
				Valid: false, ErrorCount: 1, WarningCount: 2,
				Errors:   []ValidationItem{{Check: "x", Message: "missing y"}},
				Warnings: []ValidationItem{{Check: "w", Message: "minor"}},
			},
			Traceability: TraceReport{
				CriteriaCovered: 6, CriteriaUncovered: []string{"C-1"},
				PathsCovered: 3, PathsUncovered: []string{"P-2"},
				TestsUnowned: []string{"TS-1"},
			},
			OpenQuestions: []OpenQuestion{{Question: "what about x?", Decision: "assume x", Why: "no reply"}},
			CommentURL:    "https://github.com/o/r/issues/1#comment",
		},
		FollowOnSpecs: []Package{{SpecDir: ".specs/13_other"}},
		Split:         []ScopeReport{{Name: "scope-a", Status: ScopeDone}},
		SplitPlan:     "plan text",
	}

	sv := r.SummaryView()
	b, err := json.Marshal(sv)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	valAny, ok := m["validation"].(map[string]any)
	if !ok {
		t.Fatalf("validation is not an object: %v", m["validation"])
	}
	wantVal := map[string]bool{"valid": true, "error_count": true, "errors": true}
	for k := range valAny {
		if !wantVal[k] {
			t.Errorf("unexpected key %q in validation summary: %v", k, valAny)
		}
	}
	for k := range wantVal {
		if _, ok := valAny[k]; !ok {
			t.Errorf("validation summary is missing %q: %v", k, valAny)
		}
	}

	traceAny, ok := m["traceability"].(map[string]any)
	if !ok {
		t.Fatalf("traceability is not an object: %v", m["traceability"])
	}
	wantTrace := map[string]bool{"criteria_covered": true, "paths_covered": true,
		"criteria_uncovered": true, "paths_uncovered": true, "tests_unowned": true}
	for k := range traceAny {
		if !wantTrace[k] {
			t.Errorf("unexpected key %q in traceability summary: %v", k, traceAny)
		}
	}
	for k := range wantTrace {
		if _, ok := traceAny[k]; !ok {
			t.Errorf("traceability summary is missing %q: %v", k, traceAny)
		}
	}

	if v, ok := m["follow_on_specs"]; ok {
		t.Errorf("expected no follow_on_specs in summary view, got %v", v)
	}
	if v, ok := m["split_plan"]; ok {
		t.Errorf("expected no split_plan in summary view, got %v", v)
	}

	for _, key := range []string{
		"spec_dir", "spec_id", "spec_name", "status", "artifacts",
		"open_questions", "split", "detail",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("summary view is missing %q: %v", key, m)
		}
	}
}

// In a split run result describes the first package, so split[] carries every
// written package's own state (#57).
func TestSplitReportCarriesEachPackagesState(t *testing.T) {
	r := &Result{
		Package: Package{SpecDir: ".specs/02_a", Validation: ValidationReport{Valid: true},
			OpenQuestions: []OpenQuestion{{Question: "q"}}},
		FollowOnSpecs: []Package{{SpecDir: ".specs/03_b", Validation: ValidationReport{ErrorCount: 2},
			OpenQuestions: []OpenQuestion{{Question: "x"}, {Question: "y"}},
			Traceability:  TraceReport{CriteriaUncovered: []string{"C-1"}}}},
	}
	r.setSplit([]ScopeReport{
		{Name: "a", SpecDir: ".specs/02_a"}, {Name: "b", SpecDir: ".specs/03_b"}, {Name: "c"},
	})
	a, b, c := r.Split[0], r.Split[1], r.Split[2]
	if a.Valid == nil || !*a.Valid || a.OpenQuestionsCount != 1 {
		t.Errorf("scope a = %+v", a)
	}
	if b.Valid == nil || *b.Valid || b.ErrorCount != 2 || b.OpenQuestionsCount != 2 || b.TraceGaps != 1 {
		t.Errorf("scope b = %+v", b)
	}
	if c.Valid != nil {
		t.Errorf("an unwritten scope has no state: %+v", c)
	}
	if got := r.Summary(); !strings.Contains(got, "3 open questions across 2 packages") {
		t.Errorf("summary = %q", got)
	}
}

func reviewOf(rounds, findings, accepted int) *Review {
	return &Review{RoundsRun: rounds, RoundsAllowed: 5, StopReason: "no_findings", FindingsCount: findings,
		AcceptedCount: accepted, RejectedCount: findings - accepted,
		Rounds: []ReviewRound{{Round: 1, Findings: []Finding{}}}}
}

// TS-09-55 (unit): the summary gains one review clause, totalled over every
// package, singular at 1, without the accepted count when nothing was found,
// after the open questions and before the high-warning clause, inside the
// cap.
//
// Verifies: 09-REQ-9.6
func TestTS09_55_TheSummaryCountsTheReview(t *testing.T) {
	r := &Result{}
	r.SpecDir = ".specs/07_widget_cache"
	r.OpenQuestions = []OpenQuestion{{Question: "a"}, {Question: "b"}}
	r.Review = reviewOf(2, 9, 7)
	if got, want := r.Summary(), "spec: wrote .specs/07_widget_cache; 2 open questions; review: 2 rounds, 9 findings, 7 accepted"; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}

	one := &Result{}
	one.SpecDir = ".specs/07_widget_cache"
	one.Review = reviewOf(1, 1, 1)
	if got := one.Summary(); !strings.HasSuffix(got, "; review: 1 round, 1 finding, 1 accepted") {
		t.Errorf("summary = %q", got)
	}

	none := &Result{}
	none.SpecDir = ".specs/07_widget_cache"
	none.Review = reviewOf(1, 0, 0)
	if got := none.Summary(); !strings.HasSuffix(got, "; review: 1 round, 0 findings") {
		t.Errorf("summary = %q", got)
	}

	split := &Result{}
	split.SpecDir = ".specs/07_widget_cache"
	split.Review = reviewOf(2, 3, 2)
	split.FollowOnSpecs = []Package{{SpecDir: ".specs/08_b", Review: reviewOf(1, 2, 1)}, {SpecDir: ".specs/09_c"}}
	if got := split.Summary(); !strings.HasSuffix(got, "; review: 3 rounds, 5 findings, 3 accepted") {
		t.Errorf("totalled over the packages: %q", got)
	}

	plain := &Result{}
	plain.SpecDir = ".specs/07_widget_cache"
	if got := plain.Summary(); strings.Contains(got, "review:") {
		t.Errorf("a run without a review: %q", got)
	}

	long := &Result{}
	long.SpecDir = ".specs/07_" + strings.Repeat("very_long_name_", 12)
	long.Review = reviewOf(2, 9, 7)
	run := cli.NewRun("spec", "test")
	run.Warn(cli.WarnPRDReviewIncomplete, "high", "the review stopped")
	env := run.Envelope(cli.ExitOK, long, nil)
	if n := utf8.RuneCountInString(env.Summary); n > 200 || !strings.HasSuffix(env.Summary, "; 1 high-severity warning") {
		t.Errorf("%d runes: %q", n, env.Summary)
	}
	short := run.Envelope(cli.ExitOK, r, nil).Summary
	if !strings.Contains(short, "7 accepted; 1 high-severity warning") {
		t.Errorf("the review clause is not before the high-warning clause: %q", short)
	}
}

// TS-09-56 (unit): the summary view keeps the first package's review without
// its rounds, and still decodes into envelope.SpecResult.
//
// Verifies: 09-REQ-9.7
func TestTS09_56_TheSummaryViewKeepsTheReviewCounts(t *testing.T) {
	r := &Result{}
	r.SpecDir = ".specs/07_widget_cache"
	r.Review = &Review{RoundsRun: 2, RoundsAllowed: 3, StopReason: "no_findings", FindingsCount: 4,
		AcceptedCount: 2, RejectedCount: 1, UnansweredCount: 1,
		Rounds: []ReviewRound{{Round: 1, Findings: []Finding{{ID: "R1-F1"}}}, {Round: 2, Findings: []Finding{}}}}
	b, err := json.Marshal(r.SummaryView())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	review, ok := m["review"].(map[string]any)
	if !ok {
		t.Fatalf("the summary view has no review: %s", b)
	}
	for _, k := range []string{"rounds_run", "rounds_allowed", "stop_reason", "findings_count", "accepted_count",
		"rejected_count", "unanswered_count"} {
		if _, ok := review[k]; !ok {
			t.Errorf("the summary view's review has no %s", k)
		}
	}
	if _, ok := review["rounds"]; ok {
		t.Error("the summary view's review keeps its rounds")
	}
	var sr envelope.SpecResult
	if err := json.Unmarshal(b, &sr); err != nil || sr.Review == nil || sr.Review.FindingsCount != 4 || sr.Review.UnansweredCount != 1 {
		t.Errorf("decoded %+v (%v)", sr.Review, err)
	}
	if len(r.Review.Rounds) != 2 {
		t.Error("the summary view changed the full result's review")
	}
}
