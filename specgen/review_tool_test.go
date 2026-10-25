package specgen

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
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

func many(n int) []reviewInput {
	out := make([]reviewInput, n)
	for i := range out {
		out[i] = wellFormed(i + 1)
	}
	return out
}

func submitReview(t *testing.T, tool core.Tool, findings []reviewInput) core.ToolResult {
	t.Helper()
	if findings == nil {
		findings = []reviewInput{}
	}
	return tool.Execute(context.Background(), mustJSON(t, map[string]any{"findings": findings}))
}

// earlierRound is round 1 as the round loop hands it to round 2: R1-F1
// rejected, R1-F2 accepted.
func earlierRound() []Finding {
	return []Finding{
		{ID: "R1-F1", Severity: "major", Section: "Goals", Flaw: "X is untestable", Evidence: "a  b\nc",
			Remedy: "r", Disposition: "rejected", Reason: "it is tested by Y"},
		{ID: "R1-F2", Severity: "minor", Section: "Intent", Flaw: "wordy", Evidence: "e", Remedy: "r",
			Disposition: "accepted"},
	}
}

// An accepted review ends the phase and records its findings, numbered
// R<round>-F<n> in the order submitted; an empty list says the PRD stands.
func TestReviewTool_AcceptsAWellFormedReview(t *testing.T) {
	reopen := wellFormed(1)
	reopen.Reopens, reopen.Evidence = "R1-F1", "new: specgen/prd.go:146 has no such check"
	for _, tc := range []struct {
		name     string
		round    int
		earlier  []Finding
		findings []reviewInput
		wantIDs  []string
	}{
		{"no findings", 1, nil, nil, nil},
		{"findings, numbered by round", 2, nil, many(3), []string{"R2-F1", "R2-F2", "R2-F3"}},
		{"the most findings a review may raise", 1, nil, many(maxFindings), nil},
		{"a rejected finding reopened with new evidence", 2, earlierRound(), []reviewInput{reopen}, []string{"R2-F1"}},
		{"an accepted finding raised again", 2, earlierRound(),
			[]reviewInput{{Severity: "minor", Section: "Intent", Flaw: "wordy", Evidence: "e", Remedy: "r"}}, []string{"R2-F1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sink reviewSink
			r := submitReview(t, newReviewTool(&sink, tc.round, tc.earlier), tc.findings)
			if !r.OK || !r.Terminate {
				t.Fatalf("%+v", r)
			}
			got, ok := sink.get()
			if !ok || got == nil || len(got) != len(tc.findings) {
				t.Fatalf("recorded %v (%v)", got, ok)
			}
			for i, f := range got {
				if tc.wantIDs != nil && f.ID != tc.wantIDs[i] {
					t.Errorf("finding %d has id %q, want %q", i+1, f.ID, tc.wantIDs[i])
				}
				if f.Flaw != tc.findings[i].Flaw || f.Reopens != tc.findings[i].Reopens || f.Disposition != "" {
					t.Errorf("finding %d = %+v", i+1, f)
				}
			}
		})
	}
}

// A review that fails a check is turned back whole, every offender named in
// one error so the reviewer corrects them in one turn, and records nothing.
func TestReviewTool_TurnsBackAnInvalidReview(t *testing.T) {
	noEvidence := wellFormed(2)
	noEvidence.Evidence = "  "
	noSeverityNoRemedy := wellFormed(3)
	noSeverityNoRemedy.Severity, noSeverityNoRemedy.Remedy = "", ""
	badSeverity := wellFormed(1)
	badSeverity.Severity = "blocker"
	reopens := func(id, evidence string) reviewInput {
		f := wellFormed(1)
		f.Reopens, f.Evidence = id, evidence
		return f
	}
	for _, tc := range []struct {
		name     string
		round    int
		earlier  []Finding
		findings []reviewInput
		code     string
		want     []string
		notWant  []string
	}{
		{"more findings than a review may raise", 1, nil, many(maxFindings + 1), "too_many_findings", []string{"31", "30"}, nil},
		{"findings with empty fields", 1, nil, []reviewInput{wellFormed(1), noEvidence, noSeverityNoRemedy}, "invalid_findings",
			[]string{"finding 2 has an empty evidence", "finding 3 has an empty severity, remedy", "support or withdraw"},
			[]string{"finding 1 "}},
		{"a severity that is not one", 1, nil, []reviewInput{badSeverity}, "invalid_findings",
			[]string{"finding 1", `"blocker"`, "critical, major, minor"}, []string{"support or withdraw"}},
		{"a round-one finding that reopens", 1, nil, []reviewInput{reopens("R1-F1", "x")}, "invalid_findings",
			[]string{"finding 1 reopens R1-F1", "round 1"}, nil},
		{"reopening no earlier finding", 2, earlierRound(), []reviewInput{reopens("R1-F9", "x")}, "invalid_findings",
			[]string{"finding 1 reopens R1-F9", "no earlier"}, nil},
		{"reopening with the evidence the rejection answered", 2, earlierRound(), []reviewInput{reopens("R1-F1", " a b c ")},
			"invalid_findings", []string{"finding 1 reopens R1-F1", "already answered"}, nil},
		{"repeating a rejected finding", 2, earlierRound(),
			[]reviewInput{{Severity: "major", Section: "Goals", Flaw: "X  is untestable", Evidence: "other", Remedy: "r"}},
			"invalid_findings", []string{"finding 1 repeats R1-F1", "reopens"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sink reviewSink
			r := submitReview(t, newReviewTool(&sink, tc.round, tc.earlier), tc.findings)
			if r.OK || r.Terminate || r.Error != tc.code {
				t.Fatalf("result %+v, want %s", r, tc.code)
			}
			for _, want := range tc.want {
				if !strings.Contains(r.Detail, want) {
					t.Errorf("the error does not say %q: %s", want, r.Detail)
				}
			}
			for _, not := range tc.notWant {
				if strings.Contains(r.Detail, not) {
					t.Errorf("the error says %q: %s", not, r.Detail)
				}
			}
			if _, ok := sink.get(); ok {
				t.Error("a turned-back review was recorded")
			}
		})
	}
	var sink reviewSink
	if r := newReviewTool(&sink, 1, nil).Execute(context.Background(), json.RawMessage(`{"findings":"none"}`)); r.OK ||
		r.Error != "invalid_arguments" {
		t.Errorf("arguments that are not a review: %+v", r)
	}
}

// Two evidence strings are the same exactly when they differ in whitespace
// only, whatever the characters around it.
func TestSameEvidence_IgnoresOnlyWhitespace(t *testing.T) {
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
