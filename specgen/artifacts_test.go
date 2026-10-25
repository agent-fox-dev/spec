package specgen

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/schema"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/project"
)

// The repair loop is the agent loop: a violation is a tool error appended to
// the transcript the model is already holding, and the model corrects itself
// on the next turn with the system prompt byte-identical, so the provider's
// cache prefix survives the repair. The tasks step adds the project's own
// check: a plan that names another ecosystem's runner hears the real one.
func TestSubmitArtifact_ARejectionIsRepairedInTheLoop(t *testing.T) {
	for _, tc := range []struct {
		name   string
		step   afspec.GenerationStep
		before []afspec.GenerationStep
		breakf func(map[string]any)
		want   []string
	}{
		{"a criterion that does not exist", afspec.StepTestSpec, []afspec.GenerationStep{afspec.StepRequirements},
			func(c map[string]any) { c["tests"].([]any)[0].(map[string]any)["verifies"] = []any{"01-REQ-99.1"} },
			[]string{"C3", PatchToolName(afspec.StepTestSpec)}},
		{"another ecosystem's runner", afspec.StepTasks, []afspec.GenerationStep{afspec.StepRequirements, afspec.StepTestSpec},
			func(c map[string]any) {
				c["test_commands"] = map[string]any{"all_tests": "pytest -q", "linter": "ruff check ."}
			},
			[]string{"project_mismatch", "pytest", "go test ./... -count=1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := newWorkspace(t)
			artifacts, _ := loadFixture(t, "01", "test_feature")
			broken := deepCopy(t, artifacts[tc.step])
			tc.breakf(broken)
			runner, p := fauxRunner(t, ws,
				toolCall("c1", ArtifactToolName(tc.step), broken),
				toolCall("c2", ArtifactToolName(tc.step), artifacts[tc.step]))
			partial := acceptedArtifacts(t, artifacts, tc.before...)
			a := &agentAuthor{runner: runner}
			got, res, err := a.GenerateArtifact(context.Background(), artifactRequest{
				Step: tc.step, SpecID: "01", SpecName: "test_feature", Root: ws.Root(), PRD: "## Intent\n\nx\n",
				Profile: project.DetectProfile(ws.Root()), Partial: partial,
			})
			if err != nil || got == nil {
				t.Fatalf("GenerateArtifact: %v", err)
			}
			if res.Turns != 2 {
				t.Errorf("Turns = %d, want the rejection and the correction", res.Turns)
			}
			reqs := p.Requests()
			for _, want := range tc.want {
				if !strings.Contains(transcript(reqs[1]), want) {
					t.Errorf("the repair instruction lacks %q:\n%s", want, transcript(reqs[1]))
				}
			}
			if blocksText(reqs[0].System) != blocksText(reqs[1].System) {
				t.Error("the system prompt changed between the attempt and the repair")
			}
			switch tc.step {
			case afspec.StepTestSpec:
				if partial.TestSpec == nil || partial.TestSpec.Tests[0].Verifies[0] == "01-REQ-99.1" {
					t.Errorf("the rejected artifact was recorded: %+v", partial.TestSpec)
				}
			case afspec.StepTasks:
				if partial.Tasks == nil || partial.Tasks.TestCommands.AllTests != "go test ./... -count=1" {
					t.Errorf("the accepted plan = %+v", partial.Tasks)
				}
			}
		})
	}
}

// The artifact's $schema, the format version and the spec's identity are the
// pipeline's to supply: the tool does not ask for them, a submission without
// them is accepted, and one that sends them out of habit is stored with the
// right values all the same.
func TestSubmitArtifact_WritesTheFieldsThePipelineKnows(t *testing.T) {
	programSupplied := []string{"$schema", "spec_id", "spec_name", "schema_version"}
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"left out", func(c map[string]any) {
			for _, f := range programSupplied {
				delete(c, f)
			}
		}},
		{"sent wrong", func(c map[string]any) {
			c["$schema"], c["spec_id"], c["spec_name"], c["schema_version"] = "https://example.com/x.json", "99", "other", "0.1"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := newWorkspace(t)
			artifacts, _ := loadFixture(t, "01", "test_feature")
			submitted := deepCopy(t, artifacts[afspec.StepRequirements])
			tc.edit(submitted)
			runner, _ := fauxRunner(t, ws, toolCall("c1", ArtifactToolName(afspec.StepRequirements), submitted))
			partial := &afspec.PartialSpec{SpecID: "01", SpecName: "test_feature"}
			got, _, err := (&agentAuthor{runner: runner}).GenerateArtifact(context.Background(), artifactRequest{
				Step: afspec.StepRequirements, SpecID: "01", SpecName: "test_feature", Root: ws.Root(), PRD: "## Intent\n\nx\n", Partial: partial,
			})
			if err != nil {
				t.Fatalf("GenerateArtifact: %v", err)
			}
			const uri = "https://agent-fox.dev/schemas/requirements.v2.json"
			if got["$schema"] != uri {
				t.Errorf("the content carries $schema = %v, want %q", got["$schema"], uri)
			}
			r := partial.Requirements
			if r == nil || r.Schema != uri || r.SpecId != "01" || r.SpecName != "test_feature" || r.SchemaVersion != afspec.SchemaVersion {
				t.Errorf("the recorded artifact lacks the program-supplied fields: %+v", r)
			}
		})
	}
}

// Every artifact's tool schema has a strict form, the API's guarantee that the
// first submission has the artifact's shape, and declares none of the fields
// the handler writes. The $schema it writes is the schema document's own $id.
func TestArtifactSchema_IsStrictAndLeavesOutWhatTheHandlerWrites(t *testing.T) {
	for _, step := range afspec.GenerationSteps {
		t.Run(string(step), func(t *testing.T) {
			s, err := ArtifactSchema(step)
			if err != nil {
				t.Fatal(err)
			}
			if !schema.StrictSubsetOK(s) {
				t.Error("the tool schema has no strict form")
			}
			for _, f := range append([]string{schemaProperty}, programFields...) {
				if s.Properties[f] != nil || slices.Contains(s.Required, f) || slices.Contains(s.PropertyOrder, f) {
					t.Errorf("the tool schema declares %s, which the handler writes", f)
				}
			}
			if uri := artifactSchemaURI(step); !strings.HasPrefix(uri, "https://") || !strings.Contains(uri, string(step)) {
				t.Errorf("$schema would be filled in with %q", uri)
			}
		})
	}
	if _, err := ArtifactSchema(afspec.GenerationStep("nonsense")); err == nil || artifactSchemaURI("nonsense") != "" {
		t.Errorf("an unknown step: err %v", err)
	}
}

// The conversion is pure and runs once per step per process: two calls
// return the same value.
func TestArtifactSchema_IsConvertedOnce(t *testing.T) {
	for _, step := range afspec.GenerationSteps {
		a, _ := ArtifactSchema(step)
		b, _ := ArtifactSchema(step)
		if a == nil || a != b {
			t.Errorf("%s: two calls returned different schema values", step)
		}
	}
}

// The glossary is asked for as a list of entries, which strict mode can
// declare, and stored as the object the format defines. A submission in the
// stored form, which a tool sent without the strict flag can receive, is
// accepted too.
func TestSubmitArtifact_TakesTheGlossaryAsEntriesOrAnObject(t *testing.T) {
	for name, glossary := range map[string]any{
		"entries": []any{map[string]any{"term": "widget", "definition": "A counted thing."}},
		"object":  map[string]any{"widget": "A counted thing."},
	} {
		t.Run(name, func(t *testing.T) {
			ws := newWorkspace(t)
			artifacts, _ := loadFixture(t, "01", "test_feature")
			submitted := deepCopy(t, artifacts[afspec.StepRequirements])
			submitted["glossary"] = glossary
			runner, _ := fauxRunner(t, ws, toolCall("c1", ArtifactToolName(afspec.StepRequirements), submitted))
			got, _, err := (&agentAuthor{runner: runner}).GenerateArtifact(context.Background(), artifactRequest{
				Step: afspec.StepRequirements, SpecID: "01", SpecName: "test_feature", Root: ws.Root(), PRD: "## Intent\n\nx\n",
				Partial: &afspec.PartialSpec{SpecID: "01", SpecName: "test_feature"},
			})
			if err != nil {
				t.Fatalf("GenerateArtifact: %v", err)
			}
			if g, ok := got["glossary"].(map[string]any); !ok || g["widget"] != "A counted thing." || len(g) != 1 {
				t.Errorf("glossary = %#v, want the object form", got["glossary"])
			}
		})
	}
}

// An artifact that passes every rule but carries the validator's warnings is
// sent back once with them, and the next submission is accepted with
// whatever remains: here a patch with no operations keeps it as it is.
func TestSubmitArtifact_SendsItsWarningsBackOnce(t *testing.T) {
	ws := newWorkspace(t)
	artifacts, _ := loadFixture(t, "01", "test_feature")
	vague := deepCopy(t, artifacts[afspec.StepRequirements])
	crit := vague["requirements"].([]any)[0].(map[string]any)["criteria"].([]any)[0].(map[string]any)
	crit["action"] = "handle the input properly"
	runner, p := fauxRunner(t, ws,
		toolCall("c1", ArtifactToolName(afspec.StepRequirements), vague),
		toolCall("c2", PatchToolName(afspec.StepRequirements), map[string]any{"operations": []any{}}))
	partial := &afspec.PartialSpec{SpecID: "01", SpecName: "test_feature"}
	if _, _, err := (&agentAuthor{runner: runner}).GenerateArtifact(context.Background(), artifactRequest{
		Step: afspec.StepRequirements, SpecID: "01", SpecName: "test_feature", Root: ws.Root(), PRD: "## Intent\n\nx\n", Partial: partial,
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

// A rejection costs the model a regeneration, so it names every problem at
// once — the schema's first, then the rules across files, then the project's
// tooling — and records nothing: the next attempt is checked against the same
// upstream artifacts as this one.
func TestCheckArtifact_NamesEveryProblemAtOnceAndKeepsNone(t *testing.T) {
	ws := newWorkspace(t)
	artifacts, _ := loadFixture(t, "01", "test_feature")
	partial := acceptedArtifacts(t, artifacts, afspec.StepRequirements, afspec.StepTestSpec)
	before := *partial
	audit := project.DetectProfile(ws.Root()).AuditTasks

	bad := deepCopy(t, artifacts[afspec.StepTasks])
	bad["test_commands"] = map[string]any{"all_tests": "pytest -q", "linter": "ruff check ."}
	first := bad["tasks"].([]any)[0].(map[string]any)
	first["title"] = ""
	first["tests"] = []any{"TS-01-1"}
	code, err := checkArtifact(bad, afspec.StepTasks, partial, audit, false)
	if err == nil || code != "validation_failed" {
		t.Fatalf("code %q, err %v", code, err)
	}
	msg := err.Error()
	for _, want := range []string{"[json_schema]", "[C7]", "pytest", "go test ./... -count=1", "Cross-file rules", "The project's own tooling"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the rejection lacks %q:\n%s", want, msg)
		}
	}
	if strings.Index(msg, "[json_schema]") > strings.Index(msg, "[C7]") {
		t.Errorf("schema errors should come first:\n%s", msg)
	}
	if *partial != before {
		t.Error("a rejected artifact changed the accumulated state")
	}

	// The audit alone is a project mismatch, not a validation failure.
	pythonic := deepCopy(t, artifacts[afspec.StepTasks])
	pythonic["test_commands"] = map[string]any{"all_tests": "pytest -q", "linter": "ruff check ."}
	if code, err := checkArtifact(pythonic, afspec.StepTasks, partial, audit, false); err == nil || code != "project_mismatch" {
		t.Errorf("audit only: code %q, err %v", code, err)
	}
	// An artifact that cannot be decoded is one problem among the rest.
	if _, err := checkArtifact(map[string]any{"tasks": "not a list"}, afspec.StepTasks, partial, nil, false); err == nil {
		t.Error("an undecodable artifact was accepted")
	}
}

// A phase that ends without an accepted artifact says how many submissions
// were turned back and the first problem of the last one.
func TestArtifactSink_SaysWhatThePhaseWasStuckOn(t *testing.T) {
	var sink artifactSink
	if sink.rejectionDetail() != "" || withRejections(errors.New("no result"), &sink).Error() != "no result" {
		t.Error("nothing was rejected, so there is no detail")
	}
	sink.reject(nil, "the tasks artifact has 2 problem(s):\n[C7] test TS-01-3 is not owned by any task\n[C8] criterion X")
	sink.reject(nil, "the tasks artifact has 1 problem(s):\nCross-file rules:\n[C7] test TS-01-4 is not owned by any task\n[C8] y")
	got := sink.rejectionDetail()
	want := "2 submission(s) were rejected; the last failure: the tasks artifact has 1 problem(s): [C7] test TS-01-4 is not owned by any task"
	if got != want {
		t.Errorf("detail = %q, want %q", got, want)
	}
	err := withRejections(errors.New("no result"), &sink)
	if errors.Unwrap(err) == nil || !strings.HasPrefix(err.Error(), "no result (2 submission(s)") {
		t.Errorf("err = %v", err)
	}
}
