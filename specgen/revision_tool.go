package specgen

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/schema"
	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/envelope"
)

// ToolSubmitPRDRevision is the terminating tool of the revision phase.
const ToolSubmitPRDRevision = "submit_prd_revision"

// Disposition is the reviser's answer to one finding. It has no field for the
// finding's severity or rubric, so a reviser has nowhere to change either.
type Disposition struct {
	ID string `json:"id"`
	// Disposition is fixed, contested, escalated or declined.
	Disposition string `json:"disposition"`
	// Reason is a contest's, a decline's and an escalation's.
	Reason string `json:"reason"`
	// CounterEvidence is a contest's.
	CounterEvidence string `json:"counter_evidence"`
	// Question, Options and Decision are an escalation's.
	Question string   `json:"question"`
	Options  []string `json:"options"`
	Decision string   `json:"decision"`
}

// dispositions are the four answers a reviser gives.
var dispositions = []string{envelope.DispositionFixed, envelope.DispositionContested, envelope.DispositionEscalated,
	envelope.DispositionDeclined}

// keep clears the fields d's disposition does not carry, so a record holds
// only what that answer has.
func (d *Disposition) keep() {
	switch d.Disposition {
	case envelope.DispositionContested:
		d.Question, d.Options, d.Decision = "", nil, ""
	case envelope.DispositionEscalated:
		d.CounterEvidence = ""
	case envelope.DispositionDeclined:
		d.CounterEvidence, d.Question, d.Options, d.Decision = "", "", nil, ""
	default:
		d.Reason, d.CounterEvidence, d.Question, d.Options, d.Decision = "", "", "", nil, ""
	}
}

// revisionSink is the guarded destination the revision tool writes into:
// the revised PRD and the dispositions that produced it.
type revisionSink struct {
	mu           sync.Mutex
	prd          PRD
	dispositions []Disposition
	done         bool
}

func (s *revisionSink) set(prd PRD, dispositions []Disposition) {
	s.mu.Lock()
	s.prd, s.dispositions, s.done = prd, dispositions, true
	s.mu.Unlock()
}

func (s *revisionSink) get() (PRD, []Disposition, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prd, s.dispositions, s.done
}

// revisionSchema is the revision phase's contract. It has no field for the
// spec name, the title or the split: the revised PRD carries over those of
// the PRD it revises, so a revision cannot change them, and three optional
// properties are all a strict request pays for.
func revisionSchema() *schema.Schema {
	return schema.Object(
		schema.Prop("body", schema.String(
			"The complete revised PRD in Markdown, WITHOUT YAML frontmatter, every section included, "+
				"the ones you did not change too. It must keep a '## Intent' section.")),
		schema.Prop("dispositions", dispositionsSchema()),
		schema.Opt("open_questions", openQuestionsSchema().Describe(
			"The open questions of the revised PRD, replacing the earlier ones; an empty list clears them. "+
				"Omit to keep them as they are.")),
		schema.Opt("relevant_files", relevantFilesArraySchema().Describe(
			"The relevant files of the revised PRD, replacing the earlier ones; an empty list clears them. "+
				"Omit to keep them as they are.")),
		schema.Opt("codebase_notes", schema.String(
			"Replacement codebase notes, when a finding showed a note to be wrong; at most about 1500 words. "+
				"Omit, or leave empty, to keep the earlier notes.")),
	)
}

// dispositionsSchema is a reviser's answers: one per finding of the round,
// with the fields each of the four dispositions needs. The PRD's revision and
// the plan's take the same answers.
func dispositionsSchema() *schema.Schema {
	return schema.Array(schema.Object(
		schema.Prop("id", schema.String("The finding's id, e.g. R1-F2")),
		schema.Prop("disposition", schema.Enum(
			"fixed: the body was changed to remedy it. contested: it does not hold, and counter_evidence "+
				"shows it. escalated: it holds, and remedying it takes a decision a person should own; take one "+
				"option provisionally. declined: it holds and is not remedied in this package; never for a "+
				"critical or a steering finding.", dispositions...)),
		schema.Prop("reason", schema.String(
			"contested: why the finding does not hold. declined: that it is outside this package's scope, or "+
				"the later package that carries it, by name. escalated: why you are unsure of the decision. "+
				"An empty string for fixed.")),
		schema.Prop("counter_evidence", schema.String(
			"contested: a quote that appears verbatim in the PRD or the input, or a repository path that "+
				"exists followed by what it shows. An empty string otherwise.")),
		schema.Prop("question", schema.String("escalated: the question a person should answer. An empty string otherwise.")),
		schema.Prop("options", schema.Array(schema.String("One option, in one sentence"),
			"escalated: two or three options. An empty list otherwise.")),
		schema.Prop("decision", schema.String(
			"escalated: the option you took, written verbatim into the body under ## Design Decisions, "+
				"opening with 'Provisional:'. An empty string otherwise.")),
	), "Exactly one answer for every finding of this round.")
}

// newRevisionTool is the terminator of the revision of current, answering
// findings, the findings of this round. input is the text the PRD was written
// from, and ws the repository: a contest's counter-evidence must be found in
// one of them or in current.
//
// What the revision does not supply it keeps: an omitted optional field
// keeps the earlier value, so a reviser that forgets to repeat the open
// questions does not drop them from the PRD.
func newRevisionTool(sink *revisionSink, current PRD, findings []Finding, input string, ws *workspace.Workspace) core.Tool {
	return core.Tool{
		Name: ToolSubmitPRDRevision,
		Description: "Submit the revised PRD and your answer to every finding, and end this phase. Call this " +
			"once, after you have weighed every finding against the PRD, the input and the code.",
		InputSchema: revisionSchema(),
		Terminating: true,
		PromptGuidelines: []string{
			"Submit the revision by calling " + ToolSubmitPRDRevision + "; do not write it as prose.",
			"Answer every finding exactly once, fixed, contested, escalated or declined, with the fields that answer needs.",
		},
		Execute: func(_ context.Context, in json.RawMessage) core.ToolResult {
			var payload struct {
				Body          string          `json:"body"`
				Dispositions  []Disposition   `json:"dispositions"`
				OpenQuestions *[]OpenQuestion `json:"open_questions"`
				RelevantFiles *[]RelevantFile `json:"relevant_files"`
				CodebaseNotes *string         `json:"codebase_notes"`
			}
			if err := json.Unmarshal(in, &payload); err != nil {
				return core.ErrResult("invalid_arguments", err.Error())
			}
			normalizeAnswers(payload.Dispositions)
			if bad, ok := checkDispositions(findings, payload.Dispositions, evidenceSources{current.Body, input, ws}, payload.Body); !ok {
				return bad
			}
			if bad, ok := checkPRDBody(payload.Body); !ok {
				return bad
			}

			revised := current
			revised.Body = payload.Body
			if payload.OpenQuestions != nil {
				revised.OpenQuestions = nilIfEmpty(*payload.OpenQuestions)
			}
			if payload.RelevantFiles != nil {
				if bad, ok := checkRelevantFiles(ws, *payload.RelevantFiles); !ok {
					return bad
				}
				revised.RelevantFiles = nilIfEmpty(*payload.RelevantFiles)
			}
			if payload.CodebaseNotes != nil && strings.TrimSpace(*payload.CodebaseNotes) != "" {
				revised.CodebaseNotes = boundNotes(*payload.CodebaseNotes)
			}

			fixes := slices.ContainsFunc(payload.Dispositions, func(d Disposition) bool {
				return d.Disposition == envelope.DispositionFixed
			})
			if fixes && samePRD(current, revised) {
				return core.ErrResult("unchanged_prd",
					"the revision answers findings fixed, and left the PRD unchanged: the body, open questions, "+
						"relevant files and notes are the PRD's as it was. Answering a finding fixed means changing the "+
						"PRD to remedy it; change it, or answer the finding another way.")
			}

			sink.set(revised, payload.Dispositions)
			res := core.OKResult(map[string]any{"accepted": true})
			res.Terminate = true
			return res
		},
	}
}

// normalizeAnswers trims every field of the answers, drops empty options, and
// clears the fields each answer's disposition does not carry.
func normalizeAnswers(answers []Disposition) {
	for i := range answers {
		d := &answers[i]
		for _, s := range []*string{&d.ID, &d.Disposition, &d.Reason, &d.CounterEvidence, &d.Question, &d.Decision} {
			*s = strings.TrimSpace(*s)
		}
		var options []string
		for _, o := range d.Options {
			if o = strings.TrimSpace(o); o != "" {
				options = append(options, o)
			}
		}
		d.Options = options
		d.keep()
	}
}

// evidenceSources are what a contest's counter-evidence is looked for in: the
// PRD the reviewer read, the input it was written from, and the repository.
type evidenceSources struct {
	prd, input string
	ws         *workspace.Workspace
}

// checkDispositions turns back a revision that does not answer every finding
// exactly once with one of the four dispositions, or whose answer lacks what
// its disposition needs, naming every offending id in one error. body is the
// revised body, which must hold every escalation's decision.
func checkDispositions(findings []Finding, answers []Disposition, src evidenceSources, body string) (core.ToolResult, bool) {
	count := map[string]int{}
	for _, d := range answers {
		count[d.ID]++
	}
	var problems []string
	var missing, twice []string
	for _, f := range findings {
		switch n := count[f.ID]; {
		case n == 0:
			missing = append(missing, f.ID)
		case n > 1:
			twice = append(twice, f.ID)
		}
	}
	if len(missing) > 0 {
		problems = append(problems, "no disposition for "+strings.Join(missing, ", "))
	}
	if len(twice) > 0 {
		problems = append(problems, "answered twice: "+strings.Join(twice, ", "))
	}
	byID := map[string]Finding{}
	for _, f := range findings {
		byID[f.ID] = f
	}
	var unknown, noReason, noDeclineReason, noCounter []string
	for _, d := range answers {
		f, isFinding := byID[d.ID]
		if !isFinding && !slices.Contains(unknown, d.ID) {
			unknown = append(unknown, d.ID)
		}
		switch d.Disposition {
		case envelope.DispositionFixed:
		case envelope.DispositionEscalated:
			problems = append(problems, checkEscalation(d, body)...)
		case envelope.DispositionContested:
			if d.Reason == "" {
				noReason = append(noReason, d.ID)
			}
			switch missing := src.counterEvidence(d.CounterEvidence); {
			case d.CounterEvidence == "":
				noCounter = append(noCounter, d.ID+" (it has none)")
			case missing != "":
				noCounter = append(noCounter, d.ID+" ("+missing+")")
			}
		case envelope.DispositionDeclined:
			if d.Reason == "" {
				noDeclineReason = append(noDeclineReason, d.ID)
			}
			switch {
			case isFinding && f.Severity == envelope.SeverityCritical:
				problems = append(problems, fmt.Sprintf("%s is critical and cannot be declined: such a finding is fixed, contested or escalated", d.ID))
			case isFinding && f.Rubric == envelope.RubricSteering:
				problems = append(problems, fmt.Sprintf("%s has the rubric steering and cannot be declined: such a finding is fixed, contested or escalated", d.ID))
			}
		default:
			problems = append(problems, fmt.Sprintf("%s has disposition %q; it is fixed, contested, escalated or declined", d.ID, d.Disposition))
		}
	}
	if len(unknown) > 0 {
		problems = append(problems, strings.Join(unknown, ", ")+": not one of this round's findings")
	}
	if len(noReason) > 0 {
		problems = append(problems, "contested without a reason: "+strings.Join(noReason, ", ")+"; say why each does not hold")
	}
	if len(noCounter) > 0 {
		problems = append(problems, "contested without counter-evidence the run can find: "+strings.Join(noCounter, ", ")+
			"; give a quote that appears verbatim in the PRD or the input, or a repository path that exists and what it shows")
	}
	if len(noDeclineReason) > 0 {
		problems = append(problems, "declined without a reason: "+strings.Join(noDeclineReason, ", ")+
			"; a decline must say the finding is outside this package's scope or name the later package that carries it")
	}
	if len(problems) > 0 {
		return core.ErrResult("invalid_dispositions", "the revision was not accepted: "+strings.Join(problems, "; ")+
			". Answer every finding of this round exactly once."), false
	}
	return core.ToolResult{}, true
}

// checkEscalation says what an escalated answer lacks: its question, two or
// three options, its decision, the reason it is uncertain, and its decision
// written into the revised body, whitespace aside, so the package stays
// complete.
func checkEscalation(d Disposition, body string) []string {
	var lacks, problems []string
	if d.Question == "" {
		lacks = append(lacks, "question")
	}
	if n := len(d.Options); n < 2 || n > 3 {
		lacks = append(lacks, fmt.Sprintf("options (it has %d)", n))
	}
	if d.Decision == "" {
		lacks = append(lacks, "decision")
	}
	if d.Reason == "" {
		lacks = append(lacks, "reason")
	}
	if len(lacks) > 0 {
		problems = append(problems, fmt.Sprintf("%s is escalated without its %s: an escalation carries a question, "+
			"two or three options each in one sentence, the decision taken and the reason it is uncertain",
			d.ID, strings.Join(lacks, ", ")))
	}
	if d.Decision != "" && !strings.Contains(normaliseEvidence(body), normaliseEvidence(d.Decision)) {
		problems = append(problems, fmt.Sprintf("%s's decision is not in the revised body: the decision belongs under "+
			"the PRD's ## Design Decisions, opening with Provisional:", d.ID))
	}
	return problems
}

// counterEvidence checks a contest's counter-evidence and says what is
// wrong with it, or "" when the run found it: a quote that appears in the PRD
// or the input, whitespace aside, or a path, its first word, that resolves
// inside the repository and exists, a directory as well as a file. Evidence
// whose first word looks like a path is told it was not found as a path;
// anything else, that the quote was not found.
func (src evidenceSources) counterEvidence(evidence string) string {
	if evidence == "" {
		return ""
	}
	quote := normaliseEvidence(strings.Trim(evidence, "\"'`“”‘’ "))
	if quote != "" && (strings.Contains(normaliseEvidence(src.prd), quote) || strings.Contains(normaliseEvidence(src.input), quote)) {
		return ""
	}
	path := pathOf(evidence)
	if path == "" {
		return "the quote was not found in the PRD or the input"
	}
	if src.ws != nil {
		if abs, err := src.ws.Resolve(path); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return ""
			}
		}
	}
	return fmt.Sprintf("the path %s does not exist under the repository", path)
}

// pathOf is the first word of evidence when it reads as a repository path —
// it holds a slash or a dot and no character a path would not — with a
// trailing colon, a :line suffix and quoting taken off; or "".
func pathOf(evidence string) string {
	fields := strings.Fields(evidence)
	if len(fields) == 0 {
		return ""
	}
	p := strings.Trim(fields[0], "`\"'“”‘’,;")
	p = strings.TrimSuffix(p, ":")
	if i := strings.Index(p, ":"); i > 0 {
		p = p[:i]
	}
	if p == "" || !strings.ContainsAny(p, "/.") || strings.ContainsAny(p, "()[]{}<>|*?") {
		return ""
	}
	return p
}

// samePRD reports whether a revision left everything it may change as it
// was. The body is compared as it would be written.
func samePRD(a, b PRD) bool {
	return normalizeBody(a.Body) == normalizeBody(b.Body) &&
		reflect.DeepEqual(nilIfEmpty(a.OpenQuestions), nilIfEmpty(b.OpenQuestions)) &&
		reflect.DeepEqual(nilIfEmpty(a.RelevantFiles), nilIfEmpty(b.RelevantFiles)) &&
		strings.TrimSpace(a.CodebaseNotes) == strings.TrimSpace(b.CodebaseNotes)
}

// nilIfEmpty is s, or nil when it has nothing in it, so an empty list a
// revision supplied and a list it cleared are written alike.
func nilIfEmpty[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}
