package specgen

import (
	"context"
	"encoding/json"
	"fmt"
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

// Disposition is the reviser's answer to one finding.
type Disposition struct {
	ID string `json:"id"`
	// Disposition is accepted or rejected.
	Disposition string `json:"disposition"`
	// Reason says why a rejected finding does not hold; empty when accepted.
	Reason string `json:"reason"`
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
		schema.Prop("dispositions", schema.Array(schema.Object(
			schema.Prop("id", schema.String("The finding's id, e.g. R1-F2")),
			schema.Prop("disposition", schema.Enum(
				"accepted: the body was changed to remedy it. rejected: it does not hold.",
				envelope.DispositionAccepted, envelope.DispositionRejected)),
			schema.Prop("reason", schema.String(
				"For a rejection, why the finding does not hold, with its evidence; not merely that you "+
					"disagree. An empty string for an acceptance.")),
		), "Exactly one answer for every finding of this round.")),
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

// newRevisionTool is the terminator of the revision of current, answering
// findings, the findings of this round.
//
// What the revision does not supply it keeps: an omitted optional field
// keeps the earlier value, so a reviser that forgets to repeat the open
// questions does not drop them from the PRD.
func newRevisionTool(sink *revisionSink, current PRD, findings []Finding, ws *workspace.Workspace) core.Tool {
	return core.Tool{
		Name: ToolSubmitPRDRevision,
		Description: "Submit the revised PRD and your answer to every finding, and end this phase. Call this " +
			"once, after you have weighed every finding against the PRD, the input and the code.",
		InputSchema: revisionSchema(),
		Terminating: true,
		PromptGuidelines: []string{
			"Submit the revision by calling " + ToolSubmitPRDRevision + "; do not write it as prose.",
			"Answer every finding exactly once; a rejection needs its reason.",
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
			for i := range payload.Dispositions {
				d := &payload.Dispositions[i]
				d.ID, d.Disposition, d.Reason = strings.TrimSpace(d.ID), strings.TrimSpace(d.Disposition), strings.TrimSpace(d.Reason)
				if d.Disposition == envelope.DispositionAccepted {
					d.Reason = ""
				}
			}
			if bad, ok := checkDispositions(findings, payload.Dispositions); !ok {
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

			accepted := slices.ContainsFunc(payload.Dispositions, func(d Disposition) bool {
				return d.Disposition == envelope.DispositionAccepted
			})
			if accepted && samePRD(current, revised) {
				return core.ErrResult("unchanged_prd",
					"the revision accepts findings, and the accepted findings left the PRD unchanged: the body, "+
						"open questions, relevant files and notes are the PRD's as it was. Accepting a finding means "+
						"changing the PRD to remedy it; change it, or reject the finding with its reason.")
			}

			sink.set(revised, payload.Dispositions)
			res := core.OKResult(map[string]any{"accepted": true})
			res.Terminate = true
			return res
		},
	}
}

// checkDispositions turns back a revision that does not answer every finding
// exactly once, answers one that is not this round's, or rejects one
// without a reason, naming every offending id in one error.
func checkDispositions(findings []Finding, dispositions []Disposition) (core.ToolResult, bool) {
	count := map[string]int{}
	for _, d := range dispositions {
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
	isFinding := func(id string) bool {
		return slices.ContainsFunc(findings, func(f Finding) bool { return f.ID == id })
	}
	var unknown, unexplained []string
	for _, d := range dispositions {
		if !isFinding(d.ID) && !slices.Contains(unknown, d.ID) {
			unknown = append(unknown, d.ID)
		}
		switch d.Disposition {
		case envelope.DispositionAccepted:
		case envelope.DispositionRejected:
			if d.Reason == "" {
				unexplained = append(unexplained, d.ID)
			}
		default:
			problems = append(problems, fmt.Sprintf("%s has disposition %q; it is accepted or rejected", d.ID, d.Disposition))
		}
	}
	if len(unknown) > 0 {
		problems = append(problems, strings.Join(unknown, ", ")+": not one of this round's findings")
	}
	if len(unexplained) > 0 {
		problems = append(problems, "rejected without a reason: "+strings.Join(unexplained, ", ")+
			"; say why each does not hold")
	}
	if len(problems) > 0 {
		return core.ErrResult("invalid_dispositions", "the revision was not accepted: "+strings.Join(problems, "; ")+
			". Answer every finding of this round exactly once."), false
	}
	return core.ToolResult{}, true
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
