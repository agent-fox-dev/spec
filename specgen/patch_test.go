package specgen

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/provider/faux"

	"github.com/agent-fox-dev/spec/afspec"
)

func TestApplyPatch(t *testing.T) {
	doc := map[string]any{
		"a":    []any{"x", "y"},
		"b":    map[string]any{"c": 1.0, "d/e": "slash", "f~g": "tilde"},
		"rows": []any{map[string]any{"n": 1.0}},
		"keep": "same",
	}
	cases := []struct {
		name string
		ops  []patchOp
		want map[string]any
		err  string
	}{
		{name: "replace in an array", ops: []patchOp{{Op: "replace", Path: "/a/1", Value: `"z"`}},
			want: map[string]any{"a": []any{"x", "z"}}},
		{name: "insert into an array", ops: []patchOp{{Op: "add", Path: "/a/0", Value: `"w"`}},
			want: map[string]any{"a": []any{"w", "x", "y"}}},
		{name: "append to an array", ops: []patchOp{{Op: "add", Path: "/a/-", Value: `{"n":2}`}},
			want: map[string]any{"a": []any{"x", "y", map[string]any{"n": 2.0}}}},
		{name: "remove from an array", ops: []patchOp{{Op: "remove", Path: "/a/0"}},
			want: map[string]any{"a": []any{"y"}}},
		{name: "a member inside an array element", ops: []patchOp{{Op: "replace", Path: "/rows/0/n", Value: `5`}},
			want: map[string]any{"rows": []any{map[string]any{"n": 5.0}}}},
		{name: "escaped tokens", ops: []patchOp{{Op: "replace", Path: "/b/d~1e", Value: `"S"`}, {Op: "remove", Path: "/b/f~0g"}},
			want: map[string]any{"b": map[string]any{"c": 1.0, "d/e": "S"}}},
		{name: "add a member", ops: []patchOp{{Op: "add", Path: "/b/new", Value: `true`}},
			want: map[string]any{"b": map[string]any{"c": 1.0, "d/e": "slash", "f~g": "tilde", "new": true}}},
		{name: "operations compose", ops: []patchOp{{Op: "add", Path: "/a/-", Value: `"q"`}, {Op: "replace", Path: "/a/2", Value: `"r"`}},
			want: map[string]any{"a": []any{"x", "y", "r"}}},
		{name: "replace a missing member", ops: []patchOp{{Op: "replace", Path: "/b/zz", Value: `1`}}, err: `no member "zz"`},
		{name: "through a missing member", ops: []patchOp{{Op: "add", Path: "/zz/x", Value: `1`}}, err: `no member "zz" at the artifact`},
		{name: "index out of range", ops: []patchOp{{Op: "replace", Path: "/a/2", Value: `1`}}, err: "outside the array"},
		{name: "an index with a leading zero", ops: []patchOp{{Op: "remove", Path: "/a/01"}}, err: "outside the array"},
		{name: "through an index out of range", ops: []patchOp{{Op: "add", Path: "/rows/3/n", Value: `1`}}, err: "at /rows"},
		{name: "unquoted string", ops: []patchOp{{Op: "replace", Path: "/keep", Value: `other`}}, err: "not JSON text"},
		{name: "the whole artifact", ops: []patchOp{{Op: "replace", Path: "", Value: `{}`}}, err: "whole artifact"},
		{name: "not a pointer", ops: []patchOp{{Op: "remove", Path: "a"}}, err: "not a JSON pointer"},
		{name: "unknown op", ops: []patchOp{{Op: "move", Path: "/a"}}, err: "unknown op"},
		{name: "into a scalar", ops: []patchOp{{Op: "add", Path: "/keep/x", Value: `1`}}, err: "/keep is neither an object nor an array"},
		{name: "through a scalar", ops: []patchOp{{Op: "add", Path: "/keep/x/y", Value: `1`}}, err: "/keep is neither an object nor an array"},
		{name: "a failure names its operation", ops: []patchOp{{Op: "add", Path: "/a/-", Value: `1`}, {Op: "remove", Path: "/b/zz"}},
			err: "operation 2 (remove /b/zz)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before, _ := json.Marshal(doc)
			got, err := applyPatch(doc, c.ops)
			if after, _ := json.Marshal(doc); string(before) != string(after) {
				t.Fatalf("the original was changed: %s", after)
			}
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err = %v, want one containing %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range c.want {
				if !reflect.DeepEqual(got[k], v) {
					t.Errorf("%s = %#v, want %#v", k, got[k], v)
				}
			}
			if got["keep"] != "same" {
				t.Errorf("an untouched member changed: %v", got["keep"])
			}
		})
	}
}

// A rejected submission is corrected with a patch instead of a second full
// submission: the patch applies to the submission the step turned back, a
// patch that does not apply changes nothing, and the result is checked like
// a submission.
func TestPatchArtifact_CorrectsTheLastRejectedSubmission(t *testing.T) {
	ws := newWorkspace(t)
	artifacts, _ := loadFixture(t, "01", "test_feature")
	broken := deepCopy(t, artifacts[afspec.StepTestSpec])
	good := broken["tests"].([]any)[0].(map[string]any)["verifies"]
	broken["tests"].([]any)[0].(map[string]any)["verifies"] = []any{"01-REQ-99.1"}
	fix, _ := json.Marshal(good)
	patch := func(id string, ops ...map[string]any) faux.Turn {
		if ops == nil {
			ops = []map[string]any{}
		}
		return toolCall(id, PatchToolName(afspec.StepTestSpec), map[string]any{"operations": ops})
	}

	runner, p := fauxRunner(t, ws,
		patch("c1"),
		toolCall("c2", ArtifactToolName(afspec.StepTestSpec), broken),
		patch("c3", map[string]any{"op": "replace", "path": "/tests/0/nope", "value": `1`}),
		patch("c4", map[string]any{"op": "replace", "path": "/tests/0/verifies", "value": string(fix)}),
	)
	partial := acceptedArtifacts(t, artifacts, afspec.StepRequirements)
	got, res, err := (&agentAuthor{runner: runner}).GenerateArtifact(context.Background(), artifactRequest{
		Step: afspec.StepTestSpec, SpecID: "01", SpecName: "test_feature", Root: ws.Root(), PRD: "## Intent\n\nx\n", Partial: partial,
	})
	if err != nil {
		t.Fatalf("GenerateArtifact: %v", err)
	}
	if got == nil || partial.TestSpec == nil || partial.TestSpec.Tests[0].Verifies[0] == "01-REQ-99.1" {
		t.Fatalf("the patched artifact was not recorded: %+v", partial.TestSpec)
	}
	if res.Turns != 4 {
		t.Errorf("Turns = %d, want 4", res.Turns)
	}
	reqs := p.Requests()
	for i, want := range []string{"nothing_to_patch", PatchToolName(afspec.StepTestSpec), "patch_failed"} {
		if !strings.Contains(transcript(reqs[i+1]), want) {
			t.Errorf("request %d lacks %q:\n%s", i+2, want, transcript(reqs[i+1]))
		}
	}
}
