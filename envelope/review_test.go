package envelope_test

import (
	"encoding/json"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
)

// TS-09-51 (unit): a finding is written with reopens only when it has one and
// reason only when it was rejected; the other fields are always there.
//
// Verifies: 09-REQ-9.2
func TestTS09_51_AFindingSerialisesReopensAndReasonOnlyWhenTheyApply(t *testing.T) {
	base := envelope.ReviewFinding{ID: "R2-F1", Severity: envelope.SeverityMajor, Section: "Goals",
		Flaw: "untestable", Evidence: "quote", Remedy: "measure it"}
	accepted := base
	accepted.Disposition = envelope.DispositionAccepted
	rejected := base
	rejected.Disposition, rejected.Reason, rejected.Reopens = envelope.DispositionRejected, "why", "R1-F1"
	unanswered := base
	unanswered.Disposition = envelope.DispositionUnanswered

	decode := func(f envelope.ReviewFinding) map[string]any {
		t.Helper()
		b, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"id", "severity", "section", "flaw", "evidence", "remedy", "disposition"} {
			if _, ok := m[k]; !ok {
				t.Errorf("%s: no %q in %s", f.Disposition, k, b)
			}
		}
		return m
	}

	a := decode(accepted)
	if _, ok := a["reopens"]; ok {
		t.Errorf("accepted finding has reopens: %v", a)
	}
	if _, ok := a["reason"]; ok {
		t.Errorf("accepted finding has a reason: %v", a)
	}
	r := decode(rejected)
	if r["reason"] != "why" || r["reopens"] != "R1-F1" || r["disposition"] != "rejected" {
		t.Errorf("rejected finding = %v", r)
	}
	u := decode(unanswered)
	if _, ok := u["reason"]; ok || u["disposition"] != "unanswered" {
		t.Errorf("unanswered finding = %v", u)
	}

	// A Package without a review writes no review key at all.
	b, _ := json.Marshal(envelope.Package{})
	var p map[string]any
	_ = json.Unmarshal(b, &p)
	if _, ok := p["review"]; ok {
		t.Errorf("a package without a review has one: %s", b)
	}
}
