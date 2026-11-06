package specgen

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/workspace"
)

const revisedIntentBody = "## Intent\n\nA revised intent.\n\n## Goals\n\n- a goal\n"

// prdUnderRevision is a PRD with every field the revision may keep or
// replace, and a split it must carry over.
func prdUnderRevision() PRD {
	return PRD{
		SpecName: "widget_cache",
		Title:    "Widget cache",
		Body:     "## Intent\n\nold intent\n\n## Goals\n\n- cache widgets\n",
		OpenQuestions: []OpenQuestion{
			{Question: "q1", Decision: "d1", Why: "w1"},
			{Question: "q2", Decision: "d2", Why: "w2"},
		},
		RelevantFiles:    []RelevantFile{{Path: "a.go", Why: "the loader"}, {Path: "go.mod", Why: "the module"}},
		CodebaseNotes:    "OLD",
		RecommendedSplit: []SplitScope{{Name: "widget_cache", Scope: "s1"}, {Name: "widget_evict", Scope: "s2"}},
	}
}

// revisionWorkspace holds a regular file a.go and a directory d.
func revisionWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	ws := newWorkspace(t)
	writeFiles(t, ws.Root(), map[string]string{"a.go": "package a\n"})
	if err := os.Mkdir(filepath.Join(ws.Root(), "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	return ws
}

func submitRevision(t *testing.T, tool core.Tool, payload map[string]any) core.ToolResult {
	t.Helper()
	return tool.Execute(context.Background(), mustJSON(t, payload))
}

// An accepted revision ends the phase with the revised body and every
// disposition. It keeps the spec name, the title and the split, which it has
// no field to change; it may change the Intent; and a revision that fixes no
// finding may leave the PRD as it was.
func TestRevisionTool_AcceptsAnAnsweredRevision(t *testing.T) {
	prd := prdUnderRevision()
	for _, tc := range []struct {
		name         string
		body         string
		dispositions []any
	}{
		{"a fixed finding", revisedIntentBody, []any{fixed("R1-F1")}},
		{"a changed Intent", strings.Replace(prd.Body, "old intent", "new intent", 1), []any{fixed("R1-F1")}},
		{"a contested finding and the PRD as it was", prd.Body, []any{contest("R1-F1", "it holds because Y", "cache widgets")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sink revisionSink
			tool := newRevisionTool(&sink, prd, roundFindings("R1-F1"), "", nil)
			r := submitRevision(t, tool, map[string]any{"body": tc.body, "dispositions": tc.dispositions})
			if !r.OK || !r.Terminate {
				t.Fatalf("%+v", r)
			}
			out, dispositions, ok := sink.get()
			if !ok || out.Body != tc.body || len(dispositions) != 1 || dispositions[0].ID != "R1-F1" {
				t.Fatalf("revised %+v, dispositions %+v", out, dispositions)
			}
			if out.SpecName != prd.SpecName || out.Title != prd.Title || !reflect.DeepEqual(out.RecommendedSplit, prd.RecommendedSplit) {
				t.Errorf("the revision changed the name, title or split: %+v", out)
			}
		})
	}
}

// What the revision does not supply it keeps: an omitted optional field keeps
// the earlier value, a supplied one replaces it, an empty list clears it, and
// empty notes keep the earlier ones. Replacement notes are bounded exactly as
// the PRD phase's are.
func TestRevisionTool_KeepsWhatItDoesNotReplace(t *testing.T) {
	ws := revisionWorkspace(t)
	prd := prdUnderRevision()
	big := strings.Repeat("a line of notes about specgen/prd.go\n", 20000/37+1)
	for _, tc := range []struct {
		name  string
		extra map[string]any
		want  func(PRD) bool
	}{
		{"omitted", nil, func(p PRD) bool {
			return reflect.DeepEqual(p.OpenQuestions, prd.OpenQuestions) && reflect.DeepEqual(p.RelevantFiles, prd.RelevantFiles) &&
				p.CodebaseNotes == "OLD"
		}},
		{"supplied", map[string]any{
			"open_questions": []map[string]string{{"question": "nq", "decision": "nd", "why_unsure": "nw"}},
			"relevant_files": []map[string]string{{"path": "a.go", "why": "new"}},
			"codebase_notes": "NEW",
		}, func(p PRD) bool {
			return len(p.OpenQuestions) == 1 && p.OpenQuestions[0].Why == "nw" && len(p.RelevantFiles) == 1 &&
				p.RelevantFiles[0].Why == "new" && p.CodebaseNotes == "NEW"
		}},
		{"cleared", map[string]any{"open_questions": []any{}, "relevant_files": []any{}}, func(p PRD) bool {
			return p.OpenQuestions == nil && p.RelevantFiles == nil
		}},
		{"empty notes", map[string]any{"codebase_notes": " "}, func(p PRD) bool { return p.CodebaseNotes == "OLD" }},
		{"notes over the bound", map[string]any{"codebase_notes": big}, func(p PRD) bool { return p.CodebaseNotes == boundNotes(big) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sink revisionSink
			tool := newRevisionTool(&sink, prd, roundFindings("R1-F1"), "", ws)
			payload := map[string]any{"body": revisedIntentBody, "dispositions": []any{fixed("R1-F1")}}
			for k, v := range tc.extra {
				payload[k] = v
			}
			if r := submitRevision(t, tool, payload); !r.OK {
				t.Fatalf("%+v", r)
			}
			if out, _, _ := sink.get(); !tc.want(out) {
				t.Errorf("revised = %+v", out)
			}
		})
	}
}

// A revision that does not answer every finding of the round exactly once,
// or whose PRD could not be written, is turned back naming every offender,
// and records nothing.
func TestRevisionTool_TurnsBackAnInvalidRevision(t *testing.T) {
	ws := revisionWorkspace(t)
	prd := prdUnderRevision()
	// The body checks are the PRD tool's, with its codes.
	prdToolCode := func(body string) string {
		var s prdSink
		return submitPRDTool(&s, nil).Execute(context.Background(),
			mustJSON(t, map[string]any{"spec_name": "widget_cache", "title": "Widget cache", "body": body})).Error
	}
	noIntent, frontmatter := "## Goals\n\n- no intent here\n", "---\ntitle: x\n---\n## Intent\n\nan intent\n"
	for _, tc := range []struct {
		name     string
		findings []string
		payload  map[string]any
		code     string
		want     []string
		notWant  []string
	}{
		{"missing, duplicated and unknown answers", []string{"R1-F1", "R1-F2", "R1-F3"},
			map[string]any{"body": revisedIntentBody, "dispositions": []any{fixed("R1-F1"), fixed("R1-F1"), fixed("R1-F2"), fixed("R1-F9")}},
			"invalid_dispositions", []string{"no disposition for R1-F3", "answered twice: R1-F1", "R1-F9: not one of"}, nil},
		{"a body with no Intent", []string{"R1-F1"},
			map[string]any{"body": noIntent, "dispositions": []any{fixed("R1-F1")}}, prdToolCode(noIntent), nil, nil},
		{"a body with frontmatter", []string{"R1-F1"},
			map[string]any{"body": frontmatter, "dispositions": []any{fixed("R1-F1")}}, prdToolCode(frontmatter), nil, nil},
		{"relevant files that are not regular files", []string{"R1-F1"},
			map[string]any{"body": revisedIntentBody, "dispositions": []any{fixed("R1-F1")},
				"relevant_files": []map[string]string{{"path": "missing.go", "why": "w"}, {"path": "d", "why": "w"}, {"path": "a.go", "why": "w"}}},
			"invalid_relevant_files", []string{"missing.go", ", d"}, []string{"a.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sink revisionSink
			r := submitRevision(t, newRevisionTool(&sink, prd, roundFindings(tc.findings...), "", ws), tc.payload)
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
					t.Errorf("the error names %q: %s", not, r.Detail)
				}
			}
			if _, _, ok := sink.get(); ok {
				t.Error("a turned-back revision was recorded")
			}
		})
	}
	var sink revisionSink
	if r := newRevisionTool(&sink, prd, roundFindings("R1-F1"), "", nil).Execute(context.Background(),
		json.RawMessage(`{"body":7}`)); r.OK || r.Error != "invalid_arguments" {
		t.Errorf("arguments that are not a revision: %+v", r)
	}
}

// revise submits one revision of prd answering findings, with the input and
// the repository a contest's counter-evidence is checked against, and returns
// the tool's result and whether anything was recorded.
func revise(t *testing.T, prd PRD, findings []Finding, input string, ws *workspace.Workspace, body string, answers ...any) (core.ToolResult, bool) {
	t.Helper()
	var sink revisionSink
	r := submitRevision(t, newRevisionTool(&sink, prd, findings, input, ws), map[string]any{"body": body, "dispositions": answers})
	_, _, recorded := sink.get()
	return r, recorded
}

// mustRefuse fails the test unless r turned the revision back saying each of
// want, and nothing was recorded.
func mustRefuse(t *testing.T, r core.ToolResult, recorded bool, want ...string) {
	t.Helper()
	if r.OK || recorded {
		t.Fatalf("accepted: %+v; want it turned back", r)
	}
	for _, w := range want {
		if !strings.Contains(r.Detail, w) {
			t.Errorf("the error does not say %q: %s", w, r.Detail)
		}
	}
}

// Each of the four dispositions is an answer; anything else, and a finding
// answered twice, not at all or by an unknown id, is turned back.
func TestRevisionTool_TakesTheFourDispositionsAndNothingElse(t *testing.T) {
	prd := prdUnderRevision()
	const decision = "Widgets are cached for one minute."
	body := revisedIntentBody + "\n## Design Decisions\n\n1. Provisional: " + decision + "\n"
	byDisposition := map[string]any{
		"fixed":     fixed("R1-F1"),
		"contested": contest("R1-F1", "the PRD says so", "cache widgets"),
		"escalated": escalate("R1-F1", "How long?", []string{"One minute.", "One hour."}, decision, "the input is silent"),
		"declined":  decline("R1-F1", "outside this package's scope"),
	}
	for name, submitted := range byDisposition {
		t.Run(name, func(t *testing.T) {
			var sink revisionSink
			r := submitRevision(t, newRevisionTool(&sink, prd, roundFindings("R1-F1"), "", nil),
				map[string]any{"body": body, "dispositions": []any{submitted}})
			_, ds, ok := sink.get()
			if !r.OK || !ok || len(ds) != 1 || ds[0].Disposition != name {
				t.Fatalf("result %+v, recorded %+v", r, ds)
			}
		})
	}
	t.Run("a fifth word", func(t *testing.T) {
		r, rec := revise(t, prd, roundFindings("R1-F1"), "", nil, body, answerOf("R1-F1", "accepted"))
		mustRefuse(t, r, rec, "R1-F1", `"accepted"`, "fixed, contested, escalated or declined")
	})
	t.Run("twice, missing and unknown", func(t *testing.T) {
		r, rec := revise(t, prd, roundFindings("R1-F1", "R1-F2", "R1-F3"), "", nil, body, fixed("R1-F1"), fixed("R1-F1"), fixed("R1-F2"), fixed("R1-F9"))
		mustRefuse(t, r, rec, "no disposition for R1-F3", "answered twice: R1-F1", "R1-F9: not one of")
	})
}

// A fixed answer must change the PRD; a revision that fixes nothing may leave
// it as it was.
func TestRevisionTool_AFixedAnswerMustChangeThePRD(t *testing.T) {
	prd := prdUnderRevision()
	r, rec := revise(t, prd, roundFindings("R1-F1", "R1-F2"), "", nil, prd.Body+"\n",
		fixed("R1-F1"), contest("R1-F2", "it holds", "old intent"))
	mustRefuse(t, r, rec, "fixed", "remedy")

	r, rec = revise(t, prd, roundFindings("R1-F1", "R1-F2"), "", nil, prd.Body,
		contest("R1-F1", "it holds", "old intent"), contest("R1-F2", "it holds", "cache   widgets"))
	if !r.OK || !rec {
		t.Errorf("a revision that fixes nothing and changes nothing was turned back: %+v", r)
	}
}

// A contest stands only on counter-evidence the run can find: a quote in the
// PRD or the input, whitespace aside, or a path that exists in the repository.
func TestRevisionTool_AContestNeedsCounterEvidenceTheRunCanFind(t *testing.T) {
	ws := revisionWorkspace(t)
	prd := prdUnderRevision()
	const input = "Cache every widget the store returns, for one minute."
	for _, tc := range []struct {
		name, evidence string
		says           string // "" when accepted
	}{
		{"a quote from the PRD", `"cache widgets"`, ""},
		{"a quote from the input", "the store returns, for one minute", ""},
		{"a quote spaced differently", "Cache   every\nwidget", ""},
		{"a quote in neither", "widgets are never cached", "quote"},
		{"the path of a file", "a.go: the loader reads widgets", ""},
		{"the path of a directory", "d/ holds the store", ""},
		{"a path that does not exist", "missing/store.go: holds the store", "path"},
		{"a path outside the repository", "../outside.go: holds it", "path"},
		{"no counter-evidence", "  ", "counter-evidence"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, rec := revise(t, prd, roundFindings("R1-F1"), input, ws, prd.Body, contest("R1-F1", "it does not hold", tc.evidence))
			if tc.says == "" {
				if !r.OK || !rec {
					t.Errorf("turned back: %+v", r)
				}
				return
			}
			mustRefuse(t, r, rec, "R1-F1", tc.says)
		})
	}
}

// A decline needs its reason, and a critical or a steering finding cannot be
// declined at all.
func TestRevisionTool_ADeclineNeedsAReasonAndIsNotForCriticalOrSteeringFindings(t *testing.T) {
	prd := prdUnderRevision()
	findings := roundFindings("R1-F1", "R1-F2", "R1-F3")
	findings[0].Severity, findings[0].Rubric = "minor", "prd"
	findings[1].Severity, findings[1].Rubric = "critical", "prd"
	findings[2].Severity, findings[2].Rubric = "major", "steering"
	rest := func(skip string) []any {
		var out []any
		for _, f := range findings {
			if f.ID != skip {
				out = append(out, contest(f.ID, "it holds", "old intent"))
			}
		}
		return out
	}

	r, rec := revise(t, prd, findings, "", nil, prd.Body, append(rest("R1-F1"), decline("R1-F1", " "))...)
	mustRefuse(t, r, rec, "R1-F1", "outside this package's scope", "later package")

	if r, rec = revise(t, prd, findings, "", nil, prd.Body, append(rest("R1-F1"), decline("R1-F1", "spec 18 carries it"))...); !r.OK || !rec {
		t.Errorf("a minor finding declined with a reason was turned back: %+v", r)
	}
	r, rec = revise(t, prd, findings, "", nil, prd.Body, append(rest("R1-F2"), decline("R1-F2", "spec 18 carries it"))...)
	mustRefuse(t, r, rec, "R1-F2", "critical", "fixed, contested or escalated")
	r, rec = revise(t, prd, findings, "", nil, prd.Body, append(rest("R1-F3"), decline("R1-F3", "spec 18 carries it"))...)
	mustRefuse(t, r, rec, "R1-F3", "steering", "fixed, contested or escalated")
}

// The reviser has no field for a finding's severity or rubric: whatever it
// sends there is ignored, and the finding keeps the reviewer's.
func TestRevisionTool_CannotChangeASeverityOrARubric(t *testing.T) {
	prd := prdUnderRevision()
	for _, extra := range []map[string]any{
		{"severity": "minor", "rubric": "prd"}, {"severity": "minor"}, {"rubric": "prd"},
	} {
		findings := roundFindings("R1-F1")
		findings[0].Severity, findings[0].Rubric = "major", "steering"
		submitted := fixed("R1-F1")
		for k, v := range extra {
			submitted[k] = v
		}
		var sink revisionSink
		r := submitRevision(t, newRevisionTool(&sink, prd, findings, "", nil), map[string]any{"body": revisedIntentBody, "dispositions": []any{submitted}})
		_, ds, ok := sink.get()
		if !r.OK || !ok {
			t.Fatalf("%v: turned back: %+v", extra, r)
		}
		answer(findings, ds)
		if findings[0].Severity != "major" || findings[0].Rubric != "steering" {
			t.Errorf("%v: the finding is now %s/%s; want major/steering", extra, findings[0].Severity, findings[0].Rubric)
		}
	}
}

// An escalation carries its question, two or three options, the decision
// taken and why it is uncertain; one that lacks any is turned back.
func TestRevisionTool_AnEscalationNeedsItsQuestionOptionsDecisionAndReason(t *testing.T) {
	prd := prdUnderRevision()
	const decision = "Widgets are cached for one minute."
	body := revisedIntentBody + "\n## Design Decisions\n\n1. Provisional: " + decision + "\n"
	two, three := []string{"One minute.", "One hour."}, []string{"One minute.", "One hour.", "One day."}
	for _, tc := range []struct {
		name     string
		question string
		options  []string
		decision string
		reason   string
		says     []string // nil when accepted
	}{
		{"two options", "How long?", two, decision, "the input is silent", nil},
		{"three options", "How long?", three, decision, "the input is silent", nil},
		{"one option", "How long?", two[:1], decision, "the input is silent", []string{"R1-F1", "two or three options"}},
		{"four options", "How long?", append(three, "Forever."), decision, "the input is silent", []string{"R1-F1", "two or three options"}},
		{"no options", "How long?", nil, decision, "the input is silent", []string{"R1-F1", "options"}},
		{"no question", "", two, decision, "the input is silent", []string{"R1-F1", "question"}},
		{"no decision", "How long?", two, "", "the input is silent", []string{"R1-F1", "decision"}},
		{"no reason", "How long?", two, decision, "", []string{"R1-F1", "reason"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := tc.options
			if options == nil {
				options = []string{}
			}
			r, rec := revise(t, prd, roundFindings("R1-F1"), "", nil, body, escalate("R1-F1", tc.question, options, tc.decision, tc.reason))
			if tc.says == nil {
				if !r.OK || !rec {
					t.Errorf("turned back: %+v", r)
				}
				return
			}
			mustRefuse(t, r, rec, tc.says...)
		})
	}
}

// An escalation's decision is written into the revised body, whitespace
// aside.
func TestRevisionTool_AnEscalationsDecisionMustBeInTheBody(t *testing.T) {
	prd := prdUnderRevision()
	const decision = "Widgets are cached for one minute."
	answer := escalate("R1-F1", "How long?", []string{"One minute.", "One hour."}, decision, "the input is silent")
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"the decision as it is", revisedIntentBody + "\n## Design Decisions\n\n1. Provisional: " + decision + "\n", true},
		{"the decision spaced differently", revisedIntentBody + "\n## Design Decisions\n\n1. Provisional: Widgets are\n   cached for one   minute.\n", true},
		{"no decision", revisedIntentBody, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, rec := revise(t, prd, roundFindings("R1-F1"), "", nil, tc.body, answer)
			if tc.ok {
				if !r.OK || !rec {
					t.Errorf("turned back: %+v", r)
				}
				return
			}
			mustRefuse(t, r, rec, "R1-F1", "## Design Decisions", "Provisional")
		})
	}
}
