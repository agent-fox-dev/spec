package specgen

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

func roundFindings(ids ...string) []Finding {
	out := make([]Finding, len(ids))
	for i, id := range ids {
		out[i] = Finding{ID: id, Severity: "major", Section: "Goals", Flaw: "f " + id, Evidence: "e", Remedy: "r"}
	}
	return out
}

// revisionWorkspace holds a regular file a.go and a directory d.
func revisionWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	ws := newWorkspace(t)
	if err := os.WriteFile(filepath.Join(ws.Root(), "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(ws.Root(), "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	return ws
}

func accept(id string) map[string]string {
	return map[string]string{"id": id, "disposition": "accepted", "reason": ""}
}
func reject(id, why string) map[string]string {
	return map[string]string{"id": id, "disposition": "rejected", "reason": why}
}

func submitRevision(t *testing.T, tool core.Tool, payload map[string]any) core.ToolResult {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return tool.Execute(context.Background(), raw)
}

// TS-09-22 (unit): the revision schema requires the body and the
// dispositions, offers three optional replacements, and has no field for
// the spec name, the title or the split.
//
// Verifies: 09-REQ-4.3
func TestTS09_22_TheRevisionSchemaAsksForTheBodyAndDispositionsOnly(t *testing.T) {
	s := revisionSchema()
	if !slices.Equal(s.Required, []string{"body", "dispositions"}) {
		t.Errorf("required = %v", s.Required)
	}
	var optional []string
	for _, name := range s.PropertyOrder {
		if !s.IsRequired(name) {
			optional = append(optional, name)
		}
	}
	if !slices.Equal(optional, []string{"open_questions", "relevant_files", "codebase_notes"}) {
		t.Errorf("optional = %v", optional)
	}
	for _, name := range []string{"spec_name", "title", "recommended_split"} {
		if _, ok := s.Properties[name]; ok {
			t.Errorf("the schema has %s", name)
		}
	}
	var sink revisionSink
	tool := newRevisionTool(&sink, prdUnderRevision(), roundFindings("R1-F1"), nil)
	if tool.Name != ToolSubmitPRDRevision || !tool.Terminating {
		t.Errorf("tool = %s, terminating %v", tool.Name, tool.Terminating)
	}
}

// TS-09-23 (unit): an accepted revision keeps the spec name, the title and
// the recommended split of the PRD it revises.
//
// Verifies: 09-REQ-4.4
func TestTS09_23_ARevisionKeepsTheNameTitleAndSplit(t *testing.T) {
	var sink revisionSink
	prd := prdUnderRevision()
	tool := newRevisionTool(&sink, prd, roundFindings("R1-F1"), nil)
	r := submitRevision(t, tool, map[string]any{"body": revisedIntentBody, "dispositions": []any{accept("R1-F1")}})
	if !r.OK || !r.Terminate {
		t.Fatalf("%+v", r)
	}
	out, dispositions, ok := sink.get()
	if !ok || out.Body != revisedIntentBody {
		t.Fatalf("revised = %+v", out)
	}
	if out.SpecName != prd.SpecName || out.Title != prd.Title || !reflect.DeepEqual(out.RecommendedSplit, prd.RecommendedSplit) {
		t.Errorf("revised = %+v", out)
	}
	if len(dispositions) != 1 || dispositions[0].ID != "R1-F1" || dispositions[0].Disposition != "accepted" {
		t.Errorf("dispositions = %+v", dispositions)
	}
}

// TS-09-24 (unit): an omitted optional field keeps the earlier value, a
// supplied one replaces it, an empty list clears it, and empty notes keep
// the earlier notes.
//
// Verifies: 09-REQ-4.5
func TestTS09_24_OptionalFieldsKeepWhenOmittedAndReplaceWhenSupplied(t *testing.T) {
	ws := revisionWorkspace(t)
	prd := prdUnderRevision()
	apply := func(extra map[string]any) PRD {
		t.Helper()
		var sink revisionSink
		tool := newRevisionTool(&sink, prd, roundFindings("R1-F1"), ws)
		payload := map[string]any{"body": revisedIntentBody, "dispositions": []any{accept("R1-F1")}}
		for k, v := range extra {
			payload[k] = v
		}
		if r := submitRevision(t, tool, payload); !r.OK {
			t.Fatalf("%v: %+v", extra, r)
		}
		out, _, _ := sink.get()
		return out
	}

	a := apply(nil)
	if !reflect.DeepEqual(a.OpenQuestions, prd.OpenQuestions) || !reflect.DeepEqual(a.RelevantFiles, prd.RelevantFiles) ||
		a.CodebaseNotes != "OLD" {
		t.Errorf("omitted: %+v", a)
	}
	b := apply(map[string]any{
		"open_questions": []map[string]string{{"question": "nq", "decision": "nd", "why_unsure": "nw"}},
		"relevant_files": []map[string]string{{"path": "a.go", "why": "new"}},
		"codebase_notes": "NEW",
	})
	if len(b.OpenQuestions) != 1 || b.OpenQuestions[0].Question != "nq" || len(b.RelevantFiles) != 1 ||
		b.RelevantFiles[0].Why != "new" || b.CodebaseNotes != "NEW" {
		t.Errorf("supplied: %+v", b)
	}
	c := apply(map[string]any{"open_questions": []any{}, "relevant_files": []any{}})
	if len(c.OpenQuestions) != 0 || len(c.RelevantFiles) != 0 {
		t.Errorf("cleared: %+v", c)
	}
	d := apply(map[string]any{"codebase_notes": ""})
	if d.CodebaseNotes != "OLD" {
		t.Errorf("empty notes: %q", d.CodebaseNotes)
	}
}

// TS-09-25 (unit): replacement notes over the bound are bounded exactly as
// the PRD phase's notes are.
//
// Verifies: 09-REQ-4.6
func TestTS09_25_ReplacementNotesAreBoundedLikeThePRDPhases(t *testing.T) {
	big := strings.Repeat("a line of notes about specgen/prd.go\n", 20000/37+1)
	var sink revisionSink
	tool := newRevisionTool(&sink, prdUnderRevision(), roundFindings("R1-F1"), nil)
	r := submitRevision(t, tool, map[string]any{"body": revisedIntentBody, "dispositions": []any{accept("R1-F1")},
		"codebase_notes": big})
	if !r.OK {
		t.Fatalf("%+v", r)
	}
	out, _, _ := sink.get()
	if len(out.CodebaseNotes) > maxCodebaseNotes+len("\n[…the rest of the notes was cut]") || out.CodebaseNotes != boundNotes(big) {
		t.Errorf("notes are %d bytes, want boundNotes' %d", len(out.CodebaseNotes), len(boundNotes(big)))
	}
}

// TS-09-27 (unit): a missing, a duplicated and an unknown disposition id are
// each named, and nothing is recorded.
//
// Verifies: 09-REQ-5.1
func TestTS09_27_EveryFindingNeedsExactlyOneDisposition(t *testing.T) {
	var sink revisionSink
	tool := newRevisionTool(&sink, prdUnderRevision(), roundFindings("R1-F1", "R1-F2", "R1-F3"), nil)
	r := submitRevision(t, tool, map[string]any{"body": revisedIntentBody, "dispositions": []any{
		accept("R1-F1"), accept("R1-F1"), accept("R1-F2"), accept("R1-F9"),
	}})
	if r.OK || r.Terminate {
		t.Fatalf("%+v", r)
	}
	for _, want := range []string{"R1-F1", "R1-F3", "R1-F9", "twice", "no disposition", "not one of"} {
		if !strings.Contains(r.Detail, want) {
			t.Errorf("the error does not say %q: %s", want, r.Detail)
		}
	}
	if _, _, ok := sink.get(); ok {
		t.Error("a turned-back revision was recorded")
	}
}

// TS-09-28 (unit): a rejection without a reason is turned back naming the
// finding.
//
// Verifies: 09-REQ-5.2
func TestTS09_28_ARejectionNeedsAReason(t *testing.T) {
	var sink revisionSink
	tool := newRevisionTool(&sink, prdUnderRevision(), roundFindings("R1-F1", "R1-F2"), nil)
	r := submitRevision(t, tool, map[string]any{"body": revisedIntentBody,
		"dispositions": []any{accept("R1-F1"), reject("R1-F2", "  ")}})
	if r.OK || !strings.Contains(r.Detail, "R1-F2") || strings.Contains(r.Detail, "R1-F1") {
		t.Errorf("%+v", r)
	}
	if _, _, ok := sink.get(); ok {
		t.Error("a turned-back revision was recorded")
	}
}

// TS-09-29 (unit): accepting a finding and leaving the PRD as it was is
// turned back; rejecting it and leaving the PRD as it was is accepted.
//
// Verifies: 09-REQ-5.3
func TestTS09_29_AnAcceptanceMustChangeThePRD(t *testing.T) {
	prd := prdUnderRevision()
	var sink revisionSink
	tool := newRevisionTool(&sink, prd, roundFindings("R1-F1"), nil)
	r := submitRevision(t, tool, map[string]any{"body": prd.Body, "dispositions": []any{accept("R1-F1")}})
	if r.OK || !strings.Contains(r.Detail, "unchanged") {
		t.Errorf("an acceptance that changed nothing: %+v", r)
	}
	if _, _, ok := sink.get(); ok {
		t.Error("a turned-back revision was recorded")
	}
	r = submitRevision(t, tool, map[string]any{"body": prd.Body, "dispositions": []any{reject("R1-F1", "it holds because Y")}})
	if !r.OK || !r.Terminate {
		t.Errorf("a rejection that changed nothing: %+v", r)
	}
}

// TS-09-30 (unit): a body without ## Intent, or one that starts with
// frontmatter, gets the code the PRD tool gives the same body.
//
// Verifies: 09-REQ-5.4
func TestTS09_30_TheRevisionRefusesABodyThePRDToolRefuses(t *testing.T) {
	for _, body := range []string{
		"## Goals\n\n- no intent here\n",
		"---\ntitle: x\n---\n## Intent\n\nan intent\n",
	} {
		var prdSinkV prdSink
		want := submitPRDTool(&prdSinkV, nil).Execute(context.Background(),
			mustJSON(t, map[string]any{"spec_name": "widget_cache", "title": "Widget cache", "body": body}))
		if want.OK {
			t.Fatalf("the PRD tool accepts %q", body)
		}
		var sink revisionSink
		tool := newRevisionTool(&sink, prdUnderRevision(), roundFindings("R1-F1"), nil)
		got := submitRevision(t, tool, map[string]any{"body": body, "dispositions": []any{accept("R1-F1")}})
		if got.OK || got.Error != want.Error {
			t.Errorf("%q: the revision gives %q, the PRD tool %q", body, got.Error, want.Error)
		}
		if _, _, ok := sink.get(); ok {
			t.Error("a turned-back revision was recorded")
		}
	}
}

// TS-09-31 (unit): a relevant file that is not a regular file in the
// workspace is named.
//
// Verifies: 09-REQ-5.5
func TestTS09_31_ARelevantFileMustBeARegularFile(t *testing.T) {
	var sink revisionSink
	tool := newRevisionTool(&sink, prdUnderRevision(), roundFindings("R1-F1"), revisionWorkspace(t))
	r := submitRevision(t, tool, map[string]any{"body": revisedIntentBody, "dispositions": []any{accept("R1-F1")},
		"relevant_files": []map[string]string{{"path": "missing.go", "why": "w"}, {"path": "d", "why": "w"},
			{"path": "a.go", "why": "w"}}})
	if r.OK || !strings.Contains(r.Detail, "missing.go") || !strings.Contains(r.Detail, "d") || strings.Contains(r.Detail, "a.go") {
		t.Errorf("%+v", r)
	}
	if _, _, ok := sink.get(); ok {
		t.Error("a turned-back revision was recorded")
	}
}

// TS-09-32 (unit): a revision may change the ## Intent text.
//
// Verifies: 09-REQ-5.6
func TestTS09_32_ARevisionMayChangeTheIntent(t *testing.T) {
	prd := prdUnderRevision()
	var sink revisionSink
	tool := newRevisionTool(&sink, prd, roundFindings("R1-F1"), nil)
	body := strings.Replace(prd.Body, "old intent", "new intent", 1)
	if r := submitRevision(t, tool, map[string]any{"body": body, "dispositions": []any{accept("R1-F1")}}); !r.OK || !r.Terminate {
		t.Fatalf("%+v", r)
	}
	if out, _, _ := sink.get(); !strings.Contains(out.Body, "new intent") {
		t.Errorf("body = %q", out.Body)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
