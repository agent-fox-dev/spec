package specgen

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/core"

	"github.com/agent-fox-dev/spec/afspec"
)

func TestApplyPatch(t *testing.T) {
	doc := map[string]any{
		"a":    []any{"x", "y"},
		"b":    map[string]any{"c": 1.0, "d/e": "slash", "f~g": "tilde"},
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
		{name: "escaped tokens", ops: []patchOp{{Op: "replace", Path: "/b/d~1e", Value: `"S"`}, {Op: "remove", Path: "/b/f~0g"}},
			want: map[string]any{"b": map[string]any{"c": 1.0, "d/e": "S"}}},
		{name: "add a member", ops: []patchOp{{Op: "add", Path: "/b/new", Value: `true`}},
			want: map[string]any{"b": map[string]any{"c": 1.0, "d/e": "slash", "f~g": "tilde", "new": true}}},
		{name: "operations compose", ops: []patchOp{{Op: "add", Path: "/a/-", Value: `"q"`}, {Op: "replace", Path: "/a/2", Value: `"r"`}},
			want: map[string]any{"a": []any{"x", "y", "r"}}},
		{name: "replace a missing member", ops: []patchOp{{Op: "replace", Path: "/b/zz", Value: `1`}}, err: `no member "zz"`},
		{name: "index out of range", ops: []patchOp{{Op: "replace", Path: "/a/2", Value: `1`}}, err: "outside the array"},
		{name: "unquoted string", ops: []patchOp{{Op: "replace", Path: "/keep", Value: `other`}}, err: "not JSON text"},
		{name: "the whole artifact", ops: []patchOp{{Op: "replace", Path: "", Value: `{}`}}, err: "whole artifact"},
		{name: "not a pointer", ops: []patchOp{{Op: "remove", Path: "a"}}, err: "not a JSON pointer"},
		{name: "unknown op", ops: []patchOp{{Op: "move", Path: "/a"}}, err: "unknown op"},
		{name: "through a scalar", ops: []patchOp{{Op: "add", Path: "/keep/x", Value: `1`}}, err: "neither an object nor an array"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before, _ := json.Marshal(doc)
			got, err := applyPatch(doc, c.ops)
			after, _ := json.Marshal(doc)
			if string(before) != string(after) {
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
// submission: the patch applies to the submission the step turned back, and
// the result is checked like one.
func TestARejectedArtifactIsPatched(t *testing.T) {
	ws := goWorkspace(t)
	artifacts, _ := loadFixture(t, "01", "test_feature")
	broken := deepCopy(t, artifacts[afspec.StepTestSpec])
	good := broken["tests"].([]any)[0].(map[string]any)["verifies"]
	broken["tests"].([]any)[0].(map[string]any)["verifies"] = []any{"01-REQ-99.1"}
	fix, _ := json.Marshal(good)

	runner, p := fauxRunner(t, ws,
		toolCall("c1", PatchToolName(afspec.StepTestSpec), map[string]any{"operations": []any{}}),
		toolCall("c2", ArtifactToolName(afspec.StepTestSpec), broken),
		toolCall("c3", PatchToolName(afspec.StepTestSpec), map[string]any{"operations": []any{
			map[string]any{"op": "replace", "path": "/tests/0/nope", "value": `1`}}}),
		toolCall("c4", PatchToolName(afspec.StepTestSpec), map[string]any{"operations": []any{
			map[string]any{"op": "replace", "path": "/tests/0/verifies", "value": string(fix)}}}),
	)
	partial := afspec.PartialSpec{SpecID: "01", SpecName: "test_feature"}
	if err := validateArtifactContent(artifacts[afspec.StepRequirements], afspec.StepRequirements, &partial); err != nil {
		t.Fatal(err)
	}
	a := &agentAuthor{runner: runner}
	got, res, err := a.GenerateArtifact(context.Background(), artifactRequest{
		Step: afspec.StepTestSpec, SpecID: "01", SpecName: "test_feature",
		Root: ws.Root(), PRD: "## Intent\n\nx\n", Partial: &partial,
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
	for i, want := range []string{"nothing_to_patch", "patch_test_spec", "patch_failed"} {
		if !strings.Contains(transcript(reqs[i+1]), want) {
			t.Errorf("request %d lacks %q:\n%s", i+2, want, transcript(reqs[i+1]))
		}
	}
}

// A requirements artifact that passes every rule but has vague wording is
// sent back once with the warnings, and the next submission is accepted with
// whatever remains: here a patch with no operations keeps it as it is.
func TestWarningsAreSentBackOnce(t *testing.T) {
	ws := goWorkspace(t)
	artifacts, _ := loadFixture(t, "01", "test_feature")
	vague := deepCopy(t, artifacts[afspec.StepRequirements])
	crit := vague["requirements"].([]any)[0].(map[string]any)["criteria"].([]any)[0].(map[string]any)
	crit["action"] = "handle the input properly"

	runner, p := fauxRunner(t, ws,
		toolCall("c1", ArtifactToolName(afspec.StepRequirements), vague),
		toolCall("c2", PatchToolName(afspec.StepRequirements), map[string]any{"operations": []any{}}),
	)
	partial := afspec.PartialSpec{SpecID: "01", SpecName: "test_feature"}
	a := &agentAuthor{runner: runner}
	if _, _, err := a.GenerateArtifact(context.Background(), artifactRequest{
		Step: afspec.StepRequirements, SpecID: "01", SpecName: "test_feature",
		Root: ws.Root(), PRD: "## Intent\n\nx\n", Partial: &partial,
	}); err != nil {
		t.Fatalf("GenerateArtifact: %v", err)
	}
	if partial.Requirements == nil || partial.Requirements.Requirements[0].Criteria[0].Action != "handle the input properly" {
		t.Fatalf("the artifact kept as it was is not recorded: %+v", partial.Requirements)
	}
	review := transcript(p.Requests()[1])
	for _, want := range []string{codeReviewWarnings, "vague_language", "properly", "with no operations"} {
		if !strings.Contains(review, want) {
			t.Errorf("the review lacks %q:\n%s", want, review)
		}
	}
}

// The patch tool's schema has a strict form and no union parameters, so it
// costs none of the request's small budget of them.
func TestThePatchToolSchemaIsStrictWithoutUnions(t *testing.T) {
	tool := patchArtifactTool(afspec.StepTasks, &afspec.PartialSpec{}, &artifactSink{}, nil)
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "anyOf") || strings.Contains(string(raw), "null") {
		t.Errorf("the patch schema has a union: %s", raw)
	}
	var _ core.Tool = tool
}
