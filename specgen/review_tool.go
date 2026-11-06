package specgen

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
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

// rubrics are the standards a finding of the PRD review applies.
var rubrics = []string{envelope.RubricPRD, envelope.RubricSteering}

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

// rubricDescriptions say what each rubric is, for a schema's enum.
var rubricDescriptions = map[string]string{
	envelope.RubricPRD: "prd: a flaw of the PRD itself.",
	envelope.RubricSteering: "steering: it contradicts a directive of the project's steering; the evidence quotes " +
		"the directive verbatim, and the severity is critical or major.",
	envelope.RubricZeroDecision: "zero_decision: the plan leaves a design choice to its coder.",
	envelope.RubricGrounding: "grounding: the plan names a path, symbol or claim that is not there; for a path, " +
		"the evidence is the grounding fact.",
}

// reviewSchema is a review phase's contract: one list of findings, every
// field of a finding required, with a rubric of the allowed ones. It has no
// optional property, so it adds no union parameter to a strict request.
func reviewSchema(allowed []string, section string) *schema.Schema {
	limit := maxFindings
	var described []string
	for _, r := range allowed {
		described = append(described, rubricDescriptions[r])
	}
	findings := schema.Array(schema.Object(
		schema.Prop("severity", schema.Enum(
			"critical: the package would specify the wrong thing. major: a requirement would be "+
				"missing or untestable. minor: wording or clarity.", severities...)),
		schema.Prop("rubric", schema.Enum(strings.Join(described, " "), allowed...)),
		schema.Prop("section", schema.String(section)),
		schema.Prop("flaw", schema.String("The flaw, in one or two sentences")),
		schema.Prop("evidence", schema.String(
			"What shows it: a quote, or a file path and what it shows. "+
				"A finding you cannot support is one to withdraw.")),
		schema.Prop("remedy", schema.String("What should be said instead")),
		schema.Prop("reopens", schema.String(
			"The id of an earlier CONTESTED finding this one raises again with evidence the contest "+
				"did not answer, e.g. R1-F2; an empty string otherwise. Always empty in round 1.")),
	), "Every flaw you found, most serious first. An empty list says it stands as it is.")
	findings.MaxItems = &limit
	return schema.Object(schema.Prop("findings", findings))
}

// newReviewTool is the terminator of round round's review. earlier are the
// findings of the rounds before it, with their dispositions, so a repeat of
// a settled finding can be told apart from a new one. steering is the body of
// the project's steering, which a steering finding must quote; with none, no
// finding can carry that rubric.
//
// Every check runs over the whole submission and one error names every
// offender, so the reviewer corrects them in one turn. A submission that
// fails any check records nothing.
func newReviewTool(sink *reviewSink, round int, earlier []Finding, steering string) core.Tool {
	return findingsTool(findingsContract{
		name: ToolSubmitPRDReview,
		description: "Submit the flaws you found in the PRD and end this phase. Call this once, after you " +
			"have checked the PRD against the input and the code.",
		schema:   reviewSchema(rubrics, "The PRD section the flaw is in, e.g. 'Goals' or 'Requirements > Rounds'"),
		rubrics:  rubrics,
		steering: steering,
	}, sink, round, earlier)
}

// findingsContract is what a review tool checks a submission against: its
// name and schema, the rubrics a finding may carry, the steering a steering
// finding quotes, and any check of the stage's own.
type findingsContract struct {
	name, description string
	schema            *schema.Schema
	rubrics           []string
	steering          string
	// extra says what else is wrong with the finding at position pos.
	extra func(f *Finding, pos int) []string
}

// findingsTool is the terminator of a review round: the PRD's or the plan's.
// Every check runs over the whole submission and one error names every
// offender, so the reviewer corrects them in one turn. A submission that
// fails any check records nothing.
func findingsTool(c findingsContract, sink *reviewSink, round int, earlier []Finding) core.Tool {
	byID := make(map[string]Finding, len(earlier))
	for _, f := range earlier {
		byID[f.ID] = f
	}
	return core.Tool{
		Name:        c.name,
		Description: c.description,
		InputSchema: c.schema,
		Terminating: true,
		PromptGuidelines: []string{
			"Submit the review by calling " + c.name + "; do not write it as prose.",
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
				p, empty := checkFinding(&payload.Findings[i], i+1, round, earlier, byID, c.steering, c.rubrics)
				problems = append(problems, p...)
				if c.extra != nil {
					problems = append(problems, c.extra(&payload.Findings[i], i+1)...)
				}
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
func checkFinding(f *Finding, pos, round int, earlier []Finding, byID map[string]Finding, steering string, allowed []string) (problems []string, empty bool) {
	for _, s := range []*string{&f.Severity, &f.Rubric, &f.Section, &f.Flaw, &f.Evidence, &f.Remedy, &f.Reopens} {
		*s = strings.TrimSpace(*s)
	}
	// The fields of the answer are the reviser's, and the id is the run's.
	f.ID, f.Disposition, f.Reason, f.CounterEvidence, f.Question, f.Options, f.Decision = "", "", "", "", "", nil, ""

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
	if !slices.Contains(severities, f.Severity) {
		problems = append(problems, fmt.Sprintf("finding %d has severity %q; it is one of %s",
			pos, f.Severity, strings.Join(severities, ", ")))
	}
	if !slices.Contains(allowed, f.Rubric) {
		problems = append(problems, fmt.Sprintf("finding %d has rubric %q; it is %s", pos, f.Rubric, orList(allowed)))
	}
	if f.Rubric == envelope.RubricSteering {
		if f.Severity == envelope.SeverityMinor {
			problems = append(problems, fmt.Sprintf("finding %d has the rubric steering and the severity minor: "+
				"a steering finding is critical or major", pos))
		}
		if f.Evidence != "" && !quotesSteering(f.Evidence, steering) {
			problems = append(problems, fmt.Sprintf("finding %d has the rubric steering, and its evidence quotes no "+
				"passage of the steering: the evidence of a steering finding must quote the directive it contradicts, "+
				"verbatim", pos))
		}
	}

	// A declined or escalated finding is settled: it may not be raised again,
	// through reopens or otherwise.
	settled := false
	for _, was := range earlier {
		if (was.Disposition == envelope.DispositionDeclined || was.Disposition == envelope.DispositionEscalated) &&
			sameEvidence(was.Section, f.Section) && sameEvidence(was.Flaw, f.Flaw) {
			problems = append(problems, fmt.Sprintf("finding %d repeats %s, which was %s: such a finding "+
				"cannot be raised again; withdraw it", pos, was.ID, was.Disposition))
			settled = true
			break
		}
	}

	switch {
	case settled:
	case f.Reopens != "" && round == 1:
		problems = append(problems, fmt.Sprintf("finding %d reopens %s, and round 1 has no earlier "+
			"finding to reopen; leave reopens empty", pos, f.Reopens))
	case f.Reopens != "":
		was, ok := byID[f.Reopens]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("finding %d reopens %s, which is no earlier "+
				"finding of this PRD", pos, f.Reopens))
		case was.Disposition != envelope.DispositionContested:
			problems = append(problems, fmt.Sprintf("finding %d reopens %s, which was %s: only a contested finding "+
				"may be reopened; a flaw an earlier round fixed and the PRD still has is a new finding, with reopens empty",
				pos, was.ID, was.Disposition))
		case sameEvidence(was.Evidence, f.Evidence):
			problems = append(problems, fmt.Sprintf("finding %d reopens %s with the evidence its "+
				"contest already answered; reopen it only with evidence the contest did not "+
				"answer, or withdraw it", pos, was.ID))
		}
	default:
		for _, was := range earlier {
			if was.Disposition == envelope.DispositionContested &&
				sameEvidence(was.Section, f.Section) && sameEvidence(was.Flaw, f.Flaw) {
				problems = append(problems, fmt.Sprintf("finding %d repeats %s, which was contested; "+
					"raise it again only by naming %s in reopens with evidence the contest did not "+
					"answer, or withdraw it", pos, was.ID, was.ID))
				break
			}
		}
	}
	return problems, len(missing) > 0
}

// orList is "a or b", "a, b or c".
func orList(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
}

// quoted is the text a piece of evidence quotes: between double quotes,
// typographic quotes or backticks, or on a line opening with >.
var quoted = regexp.MustCompile("\"([^\"]+)\"|“([^”]+)”|`([^`]+)`|(?m)^\\s*>\\s*(.+)$")

// quotesSteering reports whether evidence quotes a passage of the steering
// body, whitespace aside: one of its quotations, or, when it has none, the
// whole of it. A passage under ten characters is not a quotation of a
// directive. A steering that says nothing is quoted by nothing.
func quotesSteering(evidence, steering string) bool {
	body := normaliseEvidence(steering)
	if body == "" {
		return false
	}
	var passages []string
	for _, m := range quoted.FindAllStringSubmatch(evidence, -1) {
		for _, g := range m[1:] {
			if g != "" {
				passages = append(passages, g)
			}
		}
	}
	if len(passages) == 0 {
		passages = []string{evidence}
	}
	for _, p := range passages {
		if p = normaliseEvidence(p); len(p) >= 10 && strings.Contains(body, p) {
			return true
		}
	}
	return false
}

// normaliseEvidence trims s and collapses every run of whitespace inside it
// to one space, so a finding is not told apart from another by layout.
func normaliseEvidence(s string) string { return strings.Join(strings.Fields(s), " ") }

// sameEvidence reports whether a and b differ in whitespace only.
func sameEvidence(a, b string) bool { return normaliseEvidence(a) == normaliseEvidence(b) }
