package envelope_test

import (
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
)

// A finding is written with reopens only when it has one and reason only when
// it was rejected; the other fields are always there.
func TestReviewFinding_WritesReopensAndReasonOnlyWhenTheyApply(t *testing.T) {
	base := envelope.ReviewFinding{ID: "R2-F1", Severity: envelope.SeverityMajor, Section: "Goals",
		Flaw: "untestable", Evidence: "quote", Remedy: "measure it"}
	for _, tc := range []struct {
		name            string
		disposition     string
		reason, reopens string
	}{
		{name: "accepted", disposition: envelope.DispositionAccepted},
		{name: "rejected", disposition: envelope.DispositionRejected, reason: "why", reopens: "R1-F1"},
		{name: "unanswered", disposition: envelope.DispositionUnanswered},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			f.Disposition, f.Reason, f.Reopens = tc.disposition, tc.reason, tc.reopens
			got := keysOf(t, f)
			for _, k := range []string{"id", "severity", "section", "flaw", "evidence", "remedy", "disposition"} {
				if _, ok := got[k]; !ok {
					t.Errorf("no %q in %v", k, sortedKeys(got))
				}
			}
			if string(got["disposition"]) != `"`+tc.disposition+`"` {
				t.Errorf("disposition = %s", got["disposition"])
			}
			for key, want := range map[string]string{"reason": tc.reason, "reopens": tc.reopens} {
				raw, ok := got[key]
				switch {
				case want == "" && ok:
					t.Errorf("%s is written: %s", key, raw)
				case want != "" && string(raw) != `"`+want+`"`:
					t.Errorf("%s = %s, want %q", key, raw, want)
				}
			}
		})
	}
}

// A package without an adversarial review writes no review key at all.
func TestPackage_WithoutAReviewWritesNone(t *testing.T) {
	if _, ok := keysOf(t, envelope.Package{})["review"]; ok {
		t.Error("a package without a review has one")
	}
}
