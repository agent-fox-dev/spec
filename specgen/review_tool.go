package specgen

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/schema"

	"github.com/agent-fox-dev/spec/envelope"
)

// ToolSubmitPRDReview is the terminating tool of the review phase.
const ToolSubmitPRDReview = "submit_prd_review"

// maxFindings is the most findings one review may raise. A finding costs the
// revision a decision, and past this a review is listing preferences.
const maxFindings = 30

// severities are a finding's severities, most serious first.
var severities = []string{envelope.SeverityCritical, envelope.SeverityMajor, envelope.SeverityMinor}

// reviewSink is the guarded destination the review tool writes into.
type reviewSink struct {
	mu       sync.Mutex
	findings []Finding
	done     bool
}

func (s *reviewSink) set(v []Finding) {
	s.mu.Lock()
	s.findings, s.done = v, true
	s.mu.Unlock()
}

func (s *reviewSink) get() ([]Finding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.findings, s.done
}

// reviewSchema is the review phase's contract: one list of findings, every
// field of a finding required. It has no optional property, so it adds no
// union parameter to a strict request.
func reviewSchema() *schema.Schema {
	limit := maxFindings
	findings := schema.Array(schema.Object(
		schema.Prop("severity", schema.Enum(
			"critical: the package would specify the wrong thing. major: a requirement would be "+
				"missing or untestable. minor: wording or clarity.", severities...)),
		schema.Prop("section", schema.String("The PRD section the flaw is in, e.g. 'Goals' or 'Requirements > Rounds'")),
		schema.Prop("flaw", schema.String("The flaw, in one or two sentences")),
		schema.Prop("evidence", schema.String(
			"What shows it: a quote from the PRD or the input, or a file path and what it shows. "+
				"A finding you cannot support is one to withdraw.")),
		schema.Prop("remedy", schema.String("What the PRD should say instead")),
		schema.Prop("reopens", schema.String(
			"The id of an earlier REJECTED finding this one raises again with evidence its rejection "+
				"did not answer, e.g. R1-F2; an empty string otherwise. Always empty in round 1.")),
	), "Every flaw you found, most serious first. An empty list says the PRD stands as it is.")
	findings.MaxItems = &limit
	return schema.Object(schema.Prop("findings", findings))
}

// newReviewTool is the terminator of round round's review. earlier are the
// findings of the rounds before it, with their dispositions, so a repeat of
// a rejected finding can be told apart from a new one.
//
// Every check runs over the whole submission and one error names every
// offender, so the reviewer corrects them in one turn. A submission that
// fails any check records nothing.
func newReviewTool(sink *reviewSink, round int, earlier []Finding) core.Tool {
	byID := make(map[string]Finding, len(earlier))
	for _, f := range earlier {
		byID[f.ID] = f
	}
	return core.Tool{
		Name: ToolSubmitPRDReview,
		Description: "Submit the flaws you found in the PRD and end this phase. Call this once, after you " +
			"have checked the PRD against the input and the code.",
		InputSchema: reviewSchema(),
		Terminating: true,
		PromptGuidelines: []string{
			"Submit the review by calling " + ToolSubmitPRDReview + "; do not write it as prose.",
			"Every finding needs its evidence: a quote, or a file path and what it shows.",
		},
		Execute: func(_ context.Context, in json.RawMessage) core.ToolResult {
			var payload struct {
				Findings []Finding `json:"findings"`
			}
			if err := json.Unmarshal(in, &payload); err != nil {
				return core.ErrResult("invalid_arguments", err.Error())
			}
			if n := len(payload.Findings); n > maxFindings {
				return core.ErrResult("too_many_findings", fmt.Sprintf(
					"%d findings were submitted, and a review raises at most %d. Keep the %d that matter "+
						"most and submit again.", n, maxFindings, maxFindings))
			}

			var problems []string
			unsupported := false
			for i := range payload.Findings {
				p, empty := checkFinding(&payload.Findings[i], i+1, round, earlier, byID)
				problems = append(problems, p...)
				unsupported = unsupported || empty
			}
			if len(problems) > 0 {
				msg := "the review was not accepted: " + strings.Join(problems, "; ") + "."
				if unsupported {
					msg += " For each finding with an empty field, support or withdraw it: fill in every field, or leave the finding out."
				}
				return core.ErrResult("invalid_findings", msg)
			}

			findings := payload.Findings
			if findings == nil {
				findings = []Finding{}
			}
			for i := range findings {
				findings[i].ID = fmt.Sprintf("R%d-F%d", round, i+1)
			}
			sink.set(findings)
			res := core.OKResult(map[string]any{"accepted": true, "findings": len(findings)})
			res.Terminate = true
			return res
		},
	}
}

// checkFinding normalises the finding at position pos of round round's
// review and says what is wrong with it, and whether a field is empty.
// earlier are the earlier rounds' findings, byID the same by id.
func checkFinding(f *Finding, pos, round int, earlier []Finding, byID map[string]Finding) (problems []string, empty bool) {
	for _, s := range []*string{&f.Severity, &f.Section, &f.Flaw, &f.Evidence, &f.Remedy, &f.Reopens} {
		*s = strings.TrimSpace(*s)
	}
	f.ID, f.Disposition, f.Reason = "", "", ""

	var missing []string
	for _, field := range []struct{ name, val string }{
		{"severity", f.Severity}, {"section", f.Section}, {"flaw", f.Flaw},
		{"evidence", f.Evidence}, {"remedy", f.Remedy},
	} {
		if field.val == "" {
			missing = append(missing, field.name)
		}
	}
	if len(missing) > 0 {
		problems = append(problems, fmt.Sprintf("finding %d has an empty %s", pos, strings.Join(missing, ", ")))
	}
	if f.Severity != "" && !slices.Contains(severities, f.Severity) {
		problems = append(problems, fmt.Sprintf("finding %d has severity %q; it is one of %s",
			pos, f.Severity, strings.Join(severities, ", ")))
	}

	switch {
	case f.Reopens != "" && round == 1:
		problems = append(problems, fmt.Sprintf("finding %d reopens %s, and round 1 has no earlier "+
			"finding to reopen; leave reopens empty", pos, f.Reopens))
	case f.Reopens != "":
		was, ok := byID[f.Reopens]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("finding %d reopens %s, which is no earlier "+
				"finding of this PRD", pos, f.Reopens))
		case was.Disposition == envelope.DispositionRejected && sameEvidence(was.Evidence, f.Evidence):
			problems = append(problems, fmt.Sprintf("finding %d reopens %s with the evidence its "+
				"rejection already answered; reopen it only with evidence the rejection did not "+
				"answer, or withdraw it", pos, was.ID))
		}
	default:
		for _, was := range earlier {
			if was.Disposition == envelope.DispositionRejected &&
				sameEvidence(was.Section, f.Section) && sameEvidence(was.Flaw, f.Flaw) {
				problems = append(problems, fmt.Sprintf("finding %d repeats %s, which was rejected; "+
					"raise it again only by naming %s in reopens with evidence the rejection did not "+
					"answer, or withdraw it", pos, was.ID, was.ID))
				break
			}
		}
	}
	return problems, len(missing) > 0
}

// normaliseEvidence trims s and collapses every run of whitespace inside it
// to one space, so a finding is not told apart from another by layout.
func normaliseEvidence(s string) string { return strings.Join(strings.Fields(s), " ") }

// sameEvidence reports whether a and b differ in whitespace only.
func sameEvidence(a, b string) bool { return normaliseEvidence(a) == normaliseEvidence(b) }
