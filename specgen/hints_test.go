package specgen

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
)

// generateTasks runs the tasks phase on the scripted model with the fixture's
// requirements accepted before it, and returns the partial spec and every
// request that reached the model.
func generateTasks(t *testing.T, submission map[string]any) (*afspec.PartialSpec, []string) {
	t.Helper()
	ws := newWorkspace(t)
	artifacts, _ := loadFixture(t, "01", "test_feature")
	partial := acceptedArtifacts(t, artifacts, afspec.StepRequirements)
	runner, p := fauxRunner(t, ws, toolCall("t1", ArtifactToolName(afspec.StepTasks), submission),
		toolCall("t2", PatchToolName(afspec.StepTasks), map[string]any{"operations": []any{}}))
	if _, _, err := (&agentAuthor{runner: runner}).GenerateArtifact(context.Background(), artifactRequest{
		Step: afspec.StepTasks, SpecID: "01", SpecName: "test_feature", Root: ws.Root(), PRD: "## Intent\n\nx\n", Partial: partial,
	}); err != nil {
		t.Fatalf("GenerateArtifact: %v", err)
	}
	var seen []string
	for _, req := range p.Requests() {
		seen = append(seen, transcript(req))
	}
	return partial, seen
}

// The tasks phase drops a depends_on that names only the task before, which
// the format already defaults to, and keeps every other.
func TestSubmitTasks_OmitsTheDefaultDependency(t *testing.T) {
	artifacts, _ := loadFixture(t, "01", "test_feature")
	tasks := deepCopy(t, artifacts[afspec.StepTasks])
	// The fixture has two tasks; a third, owning a criterion again, gives
	// the case of a dependency on two tasks.
	tasks["tasks"] = append(tasks["tasks"].([]any), map[string]any{"id": 3.0, "title": "Render the verdict",
		"criteria": []any{"REQ-2.1"}, "steps": []any{"Render the validation result as Markdown"}})
	list := tasks["tasks"].([]any)
	list[0].(map[string]any)["depends_on"] = []any{}         // the first task: kept
	list[1].(map[string]any)["depends_on"] = []any{1.0}      // the task before: dropped
	list[2].(map[string]any)["depends_on"] = []any{1.0, 2.0} // two ids: kept

	ws := newWorkspace(t)
	partial := acceptedArtifacts(t, artifacts, afspec.StepRequirements)
	runner, _ := fauxRunner(t, ws, toolCall("t1", ArtifactToolName(afspec.StepTasks), tasks))
	if _, _, err := (&agentAuthor{runner: runner}).GenerateArtifact(context.Background(), artifactRequest{
		Step: afspec.StepTasks, SpecID: "01", SpecName: "test_feature", Root: ws.Root(), PRD: "## Intent\n\nx\n", Partial: partial,
	}); err != nil {
		t.Fatalf("GenerateArtifact: %v", err)
	}
	got := partial.Tasks.Tasks
	if got[0].DependsOn == nil || len(got[0].DependsOn) != 0 {
		t.Errorf("the first task's empty depends_on was changed: %v", got[0].DependsOn)
	}
	if got[1].DependsOn != nil {
		t.Errorf("the default dependency of task 2 was kept: %v", got[1].DependsOn)
	}
	if !slices.Equal(got[2].DependsOn, []int{1, 2}) {
		t.Errorf("task 3's dependencies changed: %v", got[2].DependsOn)
	}
}

// A dependency on a task other than the one before, or one that names the
// task before by another position in the array, is not the default.
func TestOmitDefaultDependencies_KeepsWhatIsNotTheDefault(t *testing.T) {
	content := map[string]any{"tasks": []any{
		map[string]any{"id": 1.0},
		map[string]any{"id": 3.0, "depends_on": []any{1.0}},
		map[string]any{"id": 2.0, "depends_on": []any{3.0}},
		map[string]any{"id": 4.0, "depends_on": []any{1.0}},
	}}
	omitDefaultDependencies(content)
	list := content["tasks"].([]any)
	want := []bool{false, false, false, true} // whether depends_on is there
	for i, keep := range want {
		_, has := list[i].(map[string]any)["depends_on"]
		if has != keep {
			t.Errorf("task at %d: depends_on present = %v, want %v", i, has, keep)
		}
	}
}

// A step or a touches entry that names a line of a file is sent back once, as
// line_reference, beside the validator's warnings; the next submission is
// accepted with it.
func TestSubmitTasks_SendsLineReferencesBackOnce(t *testing.T) {
	artifacts, _ := loadFixture(t, "01", "test_feature")
	tasks := deepCopy(t, artifacts[afspec.StepTasks])
	first := tasks["tasks"].([]any)[0].(map[string]any)
	first["steps"] = append(first["steps"].([]any), "Fix the names slice around afspec/afspec_test.go:88")
	first["touches"] = append(first["touches"].([]any), "afspec/render.go:12-30")

	partial, seen := generateTasks(t, tasks)
	if partial.Tasks == nil {
		t.Fatal("the tasks kept as they were are not recorded")
	}
	if len(seen) < 2 {
		t.Fatalf("the submission was not sent back: %d request(s)", len(seen))
	}
	review := seen[1]
	for _, want := range []string{codeReviewWarnings, "line_reference", "afspec/afspec_test.go:88", "afspec/render.go:12-30",
		"name the file in touches"} {
		if !strings.Contains(review, want) {
			t.Errorf("the review lacks %q:\n%s", want, review)
		}
	}
}

func TestLineReferenceRe_FindsALineOfAFile(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"edit afspec/afspec.go:88 to add it", []string{"afspec/afspec.go:88"}},
		{"see render.go:12-30", []string{"render.go:12-30"}},
		{"around line 40 of the file", []string{"line 40"}},
		{"Lines 3 to 9", []string{"Lines 3"}},
		{"add RenderedBytes to afspec/render.go", nil},
		{"listen on localhost:8080", nil},
		{"REQ-1.4 shares the check of REQ-1.2", nil},
		{"a timeout of 30s", nil},
	} {
		if got := lineReferenceRe.FindAllString(tc.text, -1); !slices.Equal(got, tc.want) {
			t.Errorf("%q: found %q, want %q", tc.text, got, tc.want)
		}
	}
}

// The requirements phase still hears about a vague word once, from the
// generator: the package's validation no longer warns on it.
func TestGeneratorHints_NameEachVagueWordOfACriterion(t *testing.T) {
	cond := "the input is handled properly"
	hints := generatorHints(afspec.StepRequirements, &afspec.RequirementsV3Json{Requirements: []afspec.Requirement{{
		Id: "REQ-1", Criteria: []afspec.Criterion{{Id: "REQ-1.1", Condition: &cond,
			Action: "return a reasonable result, etc"}},
	}}})
	var msgs []string
	for _, h := range hints {
		if h.Check != "vague_language" || h.Artifact != "requirements.json" || h.EntityID != "REQ-1.1" {
			t.Errorf("hint %+v", h)
		}
		msgs = append(msgs, h.Message)
	}
	want := []string{
		`vague term "reasonable" in field action of criterion REQ-1.1`,
		`vague term "etc" in field action of criterion REQ-1.1`,
		`vague term "properly" in field condition of criterion REQ-1.1`,
	}
	if !slices.Equal(msgs, want) {
		t.Errorf("hints = %q, want %q", msgs, want)
	}
}
