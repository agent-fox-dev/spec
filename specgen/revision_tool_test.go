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
// no field to change; it may change the Intent; and a revision that rejects
// every finding may leave the PRD as it was.
func TestRevisionTool_AcceptsAnAnsweredRevision(t *testing.T) {
	prd := prdUnderRevision()
	for _, tc := range []struct {
		name         string
		body         string
		dispositions []any
	}{
		{"an accepted finding", revisedIntentBody, []any{accept("R1-F1")}},
		{"a changed Intent", strings.Replace(prd.Body, "old intent", "new intent", 1), []any{accept("R1-F1")}},
		{"a rejected finding and the PRD as it was", prd.Body, []any{reject("R1-F1", "it holds because Y")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sink revisionSink
			tool := newRevisionTool(&sink, prd, roundFindings("R1-F1"), nil)
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
			tool := newRevisionTool(&sink, prd, roundFindings("R1-F1"), ws)
			payload := map[string]any{"body": revisedIntentBody, "dispositions": []any{accept("R1-F1")}}
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
// with a reason for each rejection, or whose PRD could not be written, is
// turned back naming every offender, and records nothing.
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
			map[string]any{"body": revisedIntentBody, "dispositions": []any{accept("R1-F1"), accept("R1-F1"), accept("R1-F2"), accept("R1-F9")}},
			"invalid_dispositions", []string{"no disposition for R1-F3", "answered twice: R1-F1", "R1-F9: not one of"}, nil},
		{"a rejection without a reason", []string{"R1-F1", "R1-F2"},
			map[string]any{"body": revisedIntentBody, "dispositions": []any{accept("R1-F1"), reject("R1-F2", "  ")}},
			"invalid_dispositions", []string{"rejected without a reason: R1-F2"}, []string{"R1-F1"}},
		{"a disposition that is not one", []string{"R1-F1"},
			map[string]any{"body": revisedIntentBody, "dispositions": []any{map[string]string{"id": "R1-F1", "disposition": "deferred"}}},
			"invalid_dispositions", []string{`R1-F1 has disposition "deferred"`}, nil},
		{"an acceptance that changed nothing", []string{"R1-F1"},
			map[string]any{"body": prd.Body + "\n", "dispositions": []any{accept("R1-F1")}},
			"unchanged_prd", []string{"unchanged"}, nil},
		{"a body with no Intent", []string{"R1-F1"},
			map[string]any{"body": noIntent, "dispositions": []any{accept("R1-F1")}}, prdToolCode(noIntent), nil, nil},
		{"a body with frontmatter", []string{"R1-F1"},
			map[string]any{"body": frontmatter, "dispositions": []any{accept("R1-F1")}}, prdToolCode(frontmatter), nil, nil},
		{"relevant files that are not regular files", []string{"R1-F1"},
			map[string]any{"body": revisedIntentBody, "dispositions": []any{accept("R1-F1")},
				"relevant_files": []map[string]string{{"path": "missing.go", "why": "w"}, {"path": "d", "why": "w"}, {"path": "a.go", "why": "w"}}},
			"invalid_relevant_files", []string{"missing.go", ", d"}, []string{"a.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sink revisionSink
			r := submitRevision(t, newRevisionTool(&sink, prd, roundFindings(tc.findings...), ws), tc.payload)
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
	if r := newRevisionTool(&sink, prd, roundFindings("R1-F1"), nil).Execute(context.Background(),
		json.RawMessage(`{"body":7}`)); r.OK || r.Error != "invalid_arguments" {
		t.Errorf("arguments that are not a revision: %+v", r)
	}
}
