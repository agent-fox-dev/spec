package envelope_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
)

// A finding is written with every field of the review, and with only the
// answer fields its disposition has.
func TestReviewFinding_WritesTheAnswerFieldsOfItsDisposition(t *testing.T) {
	base := envelope.ReviewFinding{ID: "R2-F1", Severity: envelope.SeverityMajor, Rubric: envelope.RubricPRD, Section: "Goals",
		Flaw: "untestable", Evidence: "quote", Remedy: "measure it"}
	for _, tc := range []struct {
		disposition string
		set         func(*envelope.ReviewFinding)
		want        []string
	}{
		{envelope.DispositionFixed, func(*envelope.ReviewFinding) {}, nil},
		{envelope.DispositionContested, func(f *envelope.ReviewFinding) { f.Reason, f.CounterEvidence = "why", "a.go: shows it" },
			[]string{"reason", "counter_evidence"}},
		{envelope.DispositionEscalated, func(f *envelope.ReviewFinding) {
			f.Question, f.Options, f.Decision, f.Reason = "q", []string{"a", "b"}, "a", "unsure"
		}, []string{"question", "options", "decision", "reason"}},
		{envelope.DispositionDeclined, func(f *envelope.ReviewFinding) { f.Reason = "spec 18 carries it" }, []string{"reason"}},
		{envelope.DispositionUnanswered, func(*envelope.ReviewFinding) {}, nil},
	} {
		t.Run(tc.disposition, func(t *testing.T) {
			f := base
			f.Disposition = tc.disposition
			tc.set(&f)
			got := keysOf(t, f)
			for _, k := range append([]string{"id", "severity", "rubric", "section", "flaw", "evidence", "remedy", "disposition"}, tc.want...) {
				if _, ok := got[k]; !ok {
					t.Errorf("no %q in %v", k, sortedKeys(got))
				}
			}
			for _, k := range []string{"reason", "counter_evidence", "question", "options", "decision", "reopens"} {
				if _, ok := got[k]; ok && !slices.Contains(tc.want, k) {
					t.Errorf("%q is written for a %s finding", k, tc.disposition)
				}
			}
		})
	}
}

// A reviewed result names a stop reason of the three, and carries none of the
// vocabulary the four dispositions replaced.
func TestReview_CarriesNoneOfTheReplacedVocabulary(t *testing.T) {
	for _, stop := range []string{envelope.ReviewStopConverged, envelope.ReviewStopRoundsExhausted, envelope.ReviewStopIncomplete} {
		review := &envelope.Review{RoundsRun: 2, RoundsAllowed: 3, StopReason: stop, FindingsCount: 5,
			FixedCount: 1, ContestedCount: 1, EscalatedCount: 1, DeclinedCount: 1, UnansweredCount: 1,
			Rounds: []envelope.ReviewRound{{Round: 1, Findings: []envelope.ReviewFinding{}}}}
		for i, d := range []string{envelope.DispositionFixed, envelope.DispositionContested, envelope.DispositionEscalated,
			envelope.DispositionDeclined, envelope.DispositionUnanswered} {
			review.Rounds[0].Findings = append(review.Rounds[0].Findings, envelope.ReviewFinding{ID: fmt.Sprintf("R1-F%d", i+1),
				Severity: envelope.SeverityMinor, Rubric: envelope.RubricPRD, Disposition: d, Reason: "r"})
		}
		b, err := json.Marshal(envelope.SpecResult{Package: envelope.Package{Review: review}})
		if err != nil {
			t.Fatal(err)
		}
		for _, token := range []string{`"accepted"`, `"rejected"`, "accepted_count", "rejected_count", "no_findings", "no_serious_findings"} {
			if bytes.Contains(b, []byte(token)) {
				t.Errorf("stop %s: the result holds %s", stop, token)
			}
		}
		for _, key := range []string{"fixed_count", "contested_count", "escalated_count", "declined_count", "unanswered_count", "findings_count"} {
			if !bytes.Contains(b, []byte(key)) {
				t.Errorf("stop %s: the result has no %s", stop, key)
			}
		}
	}
}

// A package without an adversarial review writes no review key at all.
func TestPackage_WithoutAReviewWritesNone(t *testing.T) {
	if _, ok := keysOf(t, envelope.Package{})["review"]; ok {
		t.Error("a package without a review has one")
	}
}
