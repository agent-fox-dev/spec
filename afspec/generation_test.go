package afspec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each step accepts the canonical fixture's artifacts produced so far. The
// earlier steps pass only because a rule is applied once the artifact it needs
// exists: after the requirements step nothing complains that no test verifies
// a criterion.
func TestValidateGenerationStep_AcceptsTheCanonicalArtifacts(t *testing.T) {
	for _, step := range GenerationSteps {
		t.Run(string(step), func(t *testing.T) {
			partial := partialFrom(t, stepsThrough(step)...)
			if result := ValidateGenerationStep(step, partial); !result.Valid {
				t.Errorf("ValidateGenerationStep rejected valid artifacts: %v", result.Errors)
			}
			if result := ValidateGenerationStepComplete(step, partial); !result.Valid {
				t.Errorf("ValidateGenerationStepComplete rejected valid artifacts: %v", result.Errors)
			}
		})
	}
}

func TestValidateGenerationStep_CatchesAnUnverifiedCriterionAtTheTestStep(t *testing.T) {
	partial := partialFrom(t, StepRequirements, StepTestSpec)
	// Drop TS-01-4, the test that verifies 01-REQ-2.1.
	ts := *partial.TestSpec
	ts.Tests = append(append([]Test{}, ts.Tests[:3]...), ts.Tests[4])
	partial.TestSpec = &ts

	result := ValidateGenerationStep(StepTestSpec, partial)
	if result.Valid {
		t.Fatal("a criterion verified by no test passed the test step")
	}
	if !hasCheck(result, "C4") {
		t.Errorf("checks = %v; want C4", errorChecks(result))
	}
}

// The pipeline-side guard on the defect that motivated v2: under v1 the task
// generator never saw the test IDs and orphaned every edge-case and property
// test.
func TestValidateGenerationStep_CatchesOrphanedTestsAtTheTasksStep(t *testing.T) {
	partial := partialFrom(t, StepRequirements, StepTestSpec, StepTasks)
	tasks := *partial.Tasks
	tasks.Tasks = append([]Task{}, tasks.Tasks...)
	tasks.Tasks[0].Tests = []string{"TS-01-1"}
	partial.Tasks = &tasks

	result := ValidateGenerationStep(StepTasks, partial)
	if result.Valid {
		t.Fatal("a plan that orphans two tests passed the tasks step")
	}
	if !hasCheck(result, "C7") {
		t.Errorf("checks = %v; want C7", errorChecks(result))
	}
}

// A schema-invalid artifact must not produce a cascade of reference errors
// that are all consequences of the same defect.
func TestValidateGenerationStep_SkipsReferenceRulesOnASchemaFailure(t *testing.T) {
	partial := partialFrom(t, StepRequirements)
	reqs := *partial.Requirements
	reqs.Introduction = "" // violates minLength
	partial.Requirements = &reqs

	result := ValidateGenerationStep(StepRequirements, partial)
	if result.Valid {
		t.Fatal("a schema-invalid artifact was accepted")
	}
	for _, e := range result.Errors {
		if e.Category != "schema" {
			t.Errorf("a reference rule ran on a schema-invalid artifact: %+v", e)
		}
	}
}

// Two rules the schema expresses as conditionals. The schema's own message for
// those ("missing property", "'not' failed") does not say which rule broke, so
// C10 and C11 run alongside it to give the repair loop something to act on.
func TestValidateGenerationStep_NamesTheRuleBehindASchemaConditional(t *testing.T) {
	t.Run("unwanted criterion without a contract", func(t *testing.T) {
		partial := partialFrom(t, StepRequirements)
		reqs := *partial.Requirements
		reqs.Requirements = append([]Requirement{}, reqs.Requirements...)
		reqs.Requirements[0].Criteria = append([]Criterion{}, reqs.Requirements[0].Criteria...)
		reqs.Requirements[0].Criteria[1].Contract = nil
		partial.Requirements = &reqs

		result := ValidateGenerationStep(StepRequirements, partial)
		if !hasCheck(result, "C10") {
			t.Errorf("checks = %v; want C10 alongside the schema error", errorChecks(result))
		}
	})

	t.Run("unit test carrying real components", func(t *testing.T) {
		partial := partialFrom(t, StepRequirements, StepTestSpec)
		ts := *partial.TestSpec
		ts.Tests = append([]Test{}, ts.Tests...)
		ts.Tests[0].RealComponents = []string{"a database"}
		partial.TestSpec = &ts

		result := ValidateGenerationStep(StepTestSpec, partial)
		if !hasCheck(result, "C11") {
			t.Errorf("checks = %v; want C11 alongside the schema error", errorChecks(result))
		}
	})
}

// A tasks artifact that breaks the schema and orphans tests is told about both
// at once, schema errors first, instead of learning of the orphans only after
// regenerating the whole file to fix the schema.
func TestValidateGenerationStepComplete_ReportsEveryProblemInOnePass(t *testing.T) {
	partial := partialFrom(t, StepRequirements, StepTestSpec, StepTasks)
	tasks := *partial.Tasks
	tasks.Tasks = append([]Task{}, tasks.Tasks...)
	tasks.Tasks[0].Title = ""                  // violates minLength
	tasks.Tasks[0].Tests = []string{"TS-01-1"} // orphans the others
	partial.Tasks = &tasks

	short := ValidateGenerationStep(StepTasks, partial)
	if hasCheck(short, "C7") {
		t.Fatalf("ValidateGenerationStep should still stop at the schema: %v", errorChecks(short))
	}

	result := ValidateGenerationStepComplete(StepTasks, partial)
	if result.Valid {
		t.Fatal("an invalid plan was accepted")
	}
	if !hasCheck(result, "C7") || !hasCheck(result, "json_schema") {
		t.Fatalf("checks = %v; want the schema error and C7", errorChecks(result))
	}
	if result.Errors[0].Category != "schema" {
		t.Errorf("the schema errors should come first: %+v", result.Errors[0])
	}
}

// The reference rules run on whatever decoded, so an artifact missing most of
// what the schema requires must not panic them.
func TestValidateGenerationStepComplete_SurvivesAStrippedArtifact(t *testing.T) {
	partial := partialFrom(t, StepRequirements, StepTestSpec)
	partial.Tasks = &TasksV2Json{Tasks: []Task{{}, {Tests: []string{"TS-99-1"}, DependsOn: []int{7}}}}
	reqs := *partial.Requirements
	reqs.Requirements = append([]Requirement{{}}, reqs.Requirements...)
	partial.Requirements = &reqs

	for _, step := range GenerationSteps {
		if r := ValidateGenerationStepComplete(step, partial); r.Valid {
			t.Errorf("%s: a stripped artifact was accepted", step)
		}
	}
}

func TestDecodeArtifact_DecodesEachStepsArtifact(t *testing.T) {
	source := loadFixture(t, fixtureValidSpec)

	decoded, err := DecodeArtifact(StepRequirements, artifactMap(t, source.Requirements))
	if err != nil {
		t.Fatalf("DecodeArtifact = %v", err)
	}
	reqs, ok := decoded.(*RequirementsV2Json)
	if !ok {
		t.Fatalf("DecodeArtifact returned %T; want *RequirementsV2Json", decoded)
	}
	if len(reqs.Requirements) != 2 {
		t.Errorf("requirements = %d; want 2", len(reqs.Requirements))
	}
	if _, err := DecodeArtifact(StepTestSpec, artifactMap(t, source.TestSpec)); err != nil {
		t.Errorf("DecodeArtifact(test_spec) = %v", err)
	}
	if _, err := DecodeArtifact(StepTasks, artifactMap(t, source.Tasks)); err != nil {
		t.Errorf("DecodeArtifact(tasks) = %v", err)
	}
}

func TestDecodeArtifact_RefusesAnUnknownStepOrAMistypedArtifact(t *testing.T) {
	if _, err := DecodeArtifact("nonsense", map[string]any{}); err == nil {
		t.Error("an unknown step was accepted")
	}
	if _, err := DecodeArtifact(StepTasks, map[string]any{"tasks": "not an array"}); err == nil {
		t.Error("a type-mismatched artifact decoded without error")
	}
}

// A key the schema does not allow is refused when the model submits it, by its
// path, rather than silently dropped.
func TestDecodeArtifact_RefusesUnknownKeysByPath(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(fixtureValidSpec, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var content map[string]any
	if err := json.Unmarshal(raw, &content); err != nil {
		t.Fatal(err)
	}
	content["traceability"] = map[string]any{"x": 1}
	content["tasks"].([]any)[0].(map[string]any)["done-when"] = []any{"x"}
	_, err = DecodeArtifact(StepTasks, content)
	if err == nil {
		t.Fatal("unknown keys were accepted")
	}
	for _, want := range []string{"/traceability", "/tasks/0/done-when"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %s: %v", want, err)
		}
	}
}

func TestSchemaNameForStepAndArtifactFileName(t *testing.T) {
	for _, tc := range []struct {
		step         GenerationStep
		schema, file string
	}{
		{StepRequirements, RequirementsSchemaName, "requirements.json"},
		{StepTestSpec, TestSpecSchemaName, "test_spec.json"},
		{StepTasks, TasksSchemaName, "tasks.json"},
	} {
		t.Run(string(tc.step), func(t *testing.T) {
			if name, ok := SchemaNameForStep(tc.step); !ok || name != tc.schema {
				t.Errorf("SchemaNameForStep = %q, %v; want %q", name, ok, tc.schema)
			}
			if file := ArtifactFileName(tc.step); file != tc.file {
				t.Errorf("ArtifactFileName = %q; want %q", file, tc.file)
			}
		})
	}
	if _, ok := SchemaNameForStep("nonsense"); ok {
		t.Error("an unknown step resolved to a schema")
	}
	if file := ArtifactFileName("nonsense"); file != "nonsense" {
		t.Errorf("ArtifactFileName(nonsense) = %q; want the step name itself", file)
	}
}

func TestFormatValidationEntries_NamesTheRuleAndPath(t *testing.T) {
	if got := FormatValidationEntries(nil); got != "" {
		t.Errorf("= %q; want empty for no entries", got)
	}
	out := FormatValidationEntries([]ValidationEntry{
		{Check: "C7", Message: "test TS-01-3 is not owned by any task"},
		{Check: "json_schema", Path: "/tests/0/kind", Message: "value must be one of …"},
	})
	for _, want := range []string{"[C7]", "TS-01-3", "at /tests/0/kind"} {
		if !strings.Contains(out, want) {
			t.Errorf("the feedback is missing %q:\n%s", want, out)
		}
	}
}
