package specgen

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/core"
)

// reviewInput is one finding as the reviewer submits it.
type reviewInput struct {
	Severity string `json:"severity"`
	Section  string `json:"section"`
	Flaw     string `json:"flaw"`
	Evidence string `json:"evidence"`
	Remedy   string `json:"remedy"`
	Reopens  string `json:"reopens"`
}

func wellFormed(n int) reviewInput {
	return reviewInput{Severity: "major", Section: fmt.Sprintf("Goals %d", n), Flaw: fmt.Sprintf("flaw %d", n),
		Evidence: fmt.Sprintf("evidence %d", n), Remedy: "fix it"}
}

func submitReview(t *testing.T, tool core.Tool, findings ...reviewInput) core.ToolResult {
	t.Helper()
	if findings == nil {
		findings = []reviewInput{}
	}
	raw, err := json.Marshal(map[string]any{"findings": findings})
	if err != nil {
		t.Fatal(err)
	}
	return tool.Execute(context.Background(), raw)
}

func many(n int) []reviewInput {
	out := make([]reviewInput, n)
	for i := range out {
		out[i] = wellFormed(i + 1)
	}
	return out
}

// rejectedEarlier is round 1 as the round loop hands it to round 2: R1-F1
// rejected, R1-F2 accepted.
func rejectedEarlier() []Finding {
	return []Finding{
		{ID: "R1-F1", Severity: "major", Section: "Goals", Flaw: "X is untestable", Evidence: "a  b\nc",
			Remedy: "r", Disposition: "rejected", Reason: "it is tested by Y"},
		{ID: "R1-F2", Severity: "minor", Section: "Intent", Flaw: "wordy", Evidence: "e", Remedy: "r",
			Disposition: "accepted"},
	}
}

// TS-09-10 (unit): the review tool accepts an empty list and a well-formed
// finding, and its schema requires every field of a finding.
//
// Verifies: 09-REQ-2.4
func TestTS09_10_TheReviewToolAcceptsEmptyAndWellFormedFindings(t *testing.T) {
	var sink reviewSink
	tool := newReviewTool(&sink, 1, nil)
	if tool.Name != ToolSubmitPRDReview || !tool.Terminating {
		t.Errorf("tool = %s, terminating %v", tool.Name, tool.Terminating)
	}
	r := submitReview(t, tool)
	if !r.OK || !r.Terminate {
		t.Fatalf("an empty list: %+v", r)
	}
	if got, ok := sink.get(); !ok || len(got) != 0 {
		t.Errorf("sink = %v, %v", got, ok)
	}

	sink = reviewSink{}
	tool = newReviewTool(&sink, 1, nil)
	r = submitReview(t, tool, wellFormed(1))
	if !r.OK || !r.Terminate {
		t.Fatalf("a well-formed finding: %+v", r)
	}
	got, _ := sink.get()
	if len(got) != 1 || got[0].Flaw != "flaw 1" || got[0].Severity != "major" {
		t.Errorf("sink = %+v", got)
	}

	s := reviewSchema()
	if !slices.Equal(s.Required, []string{"findings"}) {
		t.Errorf("required = %v", s.Required)
	}
	item := s.Properties["findings"].Items
	want := []string{"severity", "section", "flaw", "evidence", "remedy", "reopens"}
	if !slices.Equal(item.Required, want) {
		t.Errorf("a finding requires %v, want %v", item.Required, want)
	}
	var enum []string
	for _, v := range item.Properties["severity"].Enum {
		var s string
		_ = json.Unmarshal(v, &s)
		enum = append(enum, s)
	}
	if !slices.Equal(enum, []string{"critical", "major", "minor"}) {
		t.Errorf("severity enum = %v", enum)
	}
}

// TS-09-11 (unit): accepted findings are numbered R<round>-F<n> in the order
// submitted.
//
// Verifies: 09-REQ-2.5
func TestTS09_11_FindingsAreNumberedByRoundInSubmittedOrder(t *testing.T) {
	var sink reviewSink
	tool := newReviewTool(&sink, 2, nil)
	if r := submitReview(t, tool, wellFormed(1), wellFormed(2), wellFormed(3)); !r.OK {
		t.Fatalf("%+v", r)
	}
	got, _ := sink.get()
	var ids []string
	for _, f := range got {
		ids = append(ids, f.ID)
	}
	if !slices.Equal(ids, []string{"R2-F1", "R2-F2", "R2-F3"}) || got[1].Flaw != "flaw 2" {
		t.Errorf("ids = %v (%+v)", ids, got)
	}
}

// TS-09-13 (unit): 31 findings are turned back naming the count and the
// limit, and nothing is recorded; 30 are accepted.
//
// Verifies: 09-REQ-3.1
func TestTS09_13_MoreThanThirtyFindingsAreTurnedBack(t *testing.T) {
	var sink reviewSink
	tool := newReviewTool(&sink, 1, nil)
	r := submitReview(t, tool, many(31)...)
	if r.OK || r.Terminate || !strings.Contains(r.Detail, "31") || !strings.Contains(r.Detail, "30") {
		t.Errorf("31 findings: %+v", r)
	}
	if _, ok := sink.get(); ok {
		t.Error("a turned-back submission was recorded")
	}
	if r := submitReview(t, tool, many(30)...); !r.OK || !r.Terminate {
		t.Errorf("30 findings: %+v", r)
	}
}

// TS-09-14 (unit): a finding with an empty severity, section, flaw, evidence
// or remedy is turned back naming its position and its empty fields.
//
// Verifies: 09-REQ-3.2
func TestTS09_14_AFindingWithAnEmptyFieldIsTurnedBack(t *testing.T) {
	var sink reviewSink
	tool := newReviewTool(&sink, 1, nil)
	noEvidence := wellFormed(2)
	noEvidence.Evidence = "  "
	noSeverityNoRemedy := wellFormed(3)
	noSeverityNoRemedy.Severity, noSeverityNoRemedy.Remedy = "", ""
	r := submitReview(t, tool, wellFormed(1), noEvidence, noSeverityNoRemedy)
	if r.OK || r.Terminate {
		t.Fatalf("%+v", r)
	}
	for _, want := range []string{"finding 2", "evidence", "finding 3", "severity", "remedy", "support or withdraw"} {
		if !strings.Contains(r.Detail, want) {
			t.Errorf("the error does not name %q: %s", want, r.Detail)
		}
	}
	if strings.Contains(r.Detail, "finding 1 ") {
		t.Errorf("the error names the well-formed finding: %s", r.Detail)
	}
	if _, ok := sink.get(); ok {
		t.Error("a turned-back submission was recorded")
	}
}

// TS-09-15 (unit): a round-1 finding that reopens anything is turned back.
//
// Verifies: 09-REQ-3.3
func TestTS09_15_ARoundOneFindingCannotReopen(t *testing.T) {
	var sink reviewSink
	tool := newReviewTool(&sink, 1, nil)
	f := wellFormed(1)
	f.Reopens = "R1-F1"
	r := submitReview(t, tool, f)
	if r.OK || !strings.Contains(r.Detail, "finding 1") || !strings.Contains(r.Detail, "R1-F1") {
		t.Errorf("%+v", r)
	}
	if _, ok := sink.get(); ok {
		t.Error("a turned-back submission was recorded")
	}
}

// TS-09-16 (unit): a later round's reopens that names no earlier finding is
// turned back with the unknown id.
//
// Verifies: 09-REQ-3.4
func TestTS09_16_ReopeningAnUnknownFindingIsTurnedBack(t *testing.T) {
	var sink reviewSink
	tool := newReviewTool(&sink, 2, rejectedEarlier())
	f := wellFormed(1)
	f.Reopens = "R1-F9"
	r := submitReview(t, tool, f)
	if r.OK || !strings.Contains(r.Detail, "finding 1") || !strings.Contains(r.Detail, "R1-F9") {
		t.Errorf("%+v", r)
	}
	if _, ok := sink.get(); ok {
		t.Error("a turned-back submission was recorded")
	}
}

// TS-09-17 (unit): reopening a rejected finding with the evidence its
// rejection answered, whitespace aside, is turned back; new evidence is
// accepted.
//
// Verifies: 09-REQ-3.5
func TestTS09_17_ReopeningWithTheSameEvidenceIsTurnedBack(t *testing.T) {
	var sink reviewSink
	tool := newReviewTool(&sink, 2, rejectedEarlier())
	f := wellFormed(1)
	f.Reopens, f.Evidence = "R1-F1", " a b c "
	r := submitReview(t, tool, f)
	if r.OK || !strings.Contains(r.Detail, "finding 1") || !strings.Contains(r.Detail, "R1-F1") {
		t.Errorf("same evidence: %+v", r)
	}
	if _, ok := sink.get(); ok {
		t.Error("a turned-back submission was recorded")
	}
	f.Evidence = "new: specgen/prd.go:146 has no such check"
	if r := submitReview(t, tool, f); !r.OK || !r.Terminate {
		t.Errorf("new evidence: %+v", r)
	}
	got, _ := sink.get()
	if len(got) != 1 || got[0].Reopens != "R1-F1" || got[0].ID != "R2-F1" {
		t.Errorf("sink = %+v", got)
	}
}

// TS-09-18 (unit): a later finding that reopens nothing but repeats a
// rejected finding's section and flaw is turned back; repeating an accepted
// one is not.
//
// Verifies: 09-REQ-3.6
func TestTS09_18_RepeatingARejectedFindingIsTurnedBack(t *testing.T) {
	var sink reviewSink
	tool := newReviewTool(&sink, 2, rejectedEarlier())
	f := reviewInput{Severity: "major", Section: "Goals", Flaw: "X is untestable", Evidence: "other", Remedy: "r"}
	r := submitReview(t, tool, f)
	if r.OK || !strings.Contains(r.Detail, "finding 1") || !strings.Contains(r.Detail, "R1-F1") {
		t.Errorf("a repeat of a rejected finding: %+v", r)
	}
	if _, ok := sink.get(); ok {
		t.Error("a turned-back submission was recorded")
	}
	same := reviewInput{Severity: "minor", Section: "Intent", Flaw: "wordy", Evidence: "e", Remedy: "r"}
	if r := submitReview(t, tool, same); !r.OK {
		t.Errorf("a repeat of an accepted finding: %+v", r)
	}
}

// TS-09-19 (property): two evidence strings are the same exactly when they
// differ in whitespace only.
//
// Verifies: 09-REQ-3.7
func TestTS09_19_EvidenceComparisonIgnoresOnlyWhitespace(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	spaces := []string{" ", "  ", "\t", "\n", " \n\t", "\r\n"}
	letters := []rune("abcxyz019-_./:()`\"'é→")
	word := func() string {
		r := make([]rune, 1+rng.Intn(6))
		for i := range r {
			r[i] = letters[rng.Intn(len(letters))]
		}
		return string(r)
	}
	for range 2000 {
		words := make([]string, 1+rng.Intn(8))
		for i := range words {
			words[i] = word()
		}
		a := strings.Join(words, " ")

		var b strings.Builder
		b.WriteString(spaces[rng.Intn(len(spaces))])
		for i, w := range words {
			if i > 0 {
				b.WriteString(spaces[rng.Intn(len(spaces))])
			}
			b.WriteString(w)
		}
		b.WriteString(spaces[rng.Intn(len(spaces))])
		if !sameEvidence(a, b.String()) {
			t.Fatalf("%q and %q differ only in whitespace", a, b.String())
		}

		r := []rune(a)
		i := rng.Intn(len(r))
		for r[i] == ' ' {
			i = (i + 1) % len(r)
		}
		orig := r[i]
		for r[i] == orig {
			r[i] = letters[rng.Intn(len(letters))]
		}
		if sameEvidence(a, string(r)) {
			t.Fatalf("%q and %q differ in a non-whitespace character", a, string(r))
		}
	}
}
