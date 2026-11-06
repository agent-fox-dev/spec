package afspec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each step accepts the canonical fixture's artifacts produced so far. The
// requirements step passes only because a rule is applied once the artifact
// it needs exists: after the requirements step nothing complains that no task
// owns a criterion.
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

// A shared check that resolves nowhere is caught at the requirements step,
// before the tasks are written against it.
func TestValidateGenerationStep_CatchesAnUnresolvedSharedCheckAtTheRequirementsStep(t *testing.T) {
	partial := partialFrom(t, StepRequirements)
	reqs := deepCopyRequirements(*partial.Requirements)
	reqs.Requirements[1].Criteria[0].Check = SameAs("REQ-7.7")
	partial.Requirements = &reqs

	result := ValidateGenerationStep(StepRequirements, partial)
	if result.Valid {
		t.Fatal("a shared check naming no criterion passed the requirements step")
	}
	if !hasCheck(result, "C3") {
		t.Errorf("checks = %v; want C3", errorChecks(result))
	}
}

// The profile is held at the requirements step: a lite package with a path,
// or a full one without, is sent back before the tasks are written.
func TestValidateGenerationStep_HoldsTheRequirementsToTheProfile(t *testing.T) {
	partial := partialFrom(t, StepRequirements)
	partial.Profile = ProfileLite
	if result := ValidateGenerationStep(StepRequirements, partial); result.Valid || !hasCheck(result, "C7") {
		t.Errorf("a lite package with a path: valid %v, checks %v; want C7", result.Valid, errorChecks(result))
	}
	reqs := deepCopyRequirements(*partial.Requirements)
	reqs.ExecutionPaths = nil
	partial.Requirements = &reqs
	if result := ValidateGenerationStep(StepRequirements, partial); !result.Valid {
		t.Errorf("a lite package without a path: %v", result.Errors)
	}
	partial.Profile = ""
	if result := ValidateGenerationStep(StepRequirements, partial); result.Valid || !hasCheck(result, "C7") {
		t.Errorf("a full package without a path: valid %v, checks %v; want C7", result.Valid, errorChecks(result))
	}
}

// The pipeline-side guard on the defect that motivated v2: a criterion no
// task owns.
func TestValidateGenerationStep_CatchesAnUnownedCriterionAtTheTasksStep(t *testing.T) {
	partial := partialFrom(t, StepRequirements, StepTasks)
	tasks := *partial.Tasks
	tasks.Tasks = append([]Task{}, tasks.Tasks...)
	tasks.Tasks[1].Criteria = []string{"REQ-1.1"}
	partial.Tasks = &tasks

	result := ValidateGenerationStep(StepTasks, partial)
	if result.Valid {
		t.Fatal("a plan that leaves a criterion unowned passed the tasks step")
	}
	if !hasCheck(result, "C5") {
		t.Errorf("checks = %v; want C5", errorChecks(result))
	}
}

// A schema-invalid artifact must not produce a cascade of reference errors
// that are all consequences of the same defect.
func TestValidateGenerationStep_SkipsReferenceRulesOnASchemaFailure(t *testing.T) {
	partial := partialFrom(t, StepRequirements)
	reqs := deepCopyRequirements(*partial.Requirements)
	reqs.Requirements[0].Title = "" // violates minLength
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

// Two rules the schema expresses as conditionals or that it cannot say. The
// schema's own message for the first ("missing property") does not say which
// rule broke, so C6 and C7 run alongside it to give the repair loop something
// to act on.
func TestValidateGenerationStep_NamesTheRuleBehindASchemaConditional(t *testing.T) {
	t.Run("unwanted criterion without a contract", func(t *testing.T) {
		partial := partialFrom(t, StepRequirements)
		reqs := deepCopyRequirements(*partial.Requirements)
		reqs.Requirements[0].Criteria[1].Contract = nil
		partial.Requirements = &reqs

		result := ValidateGenerationStep(StepRequirements, partial)
		if !hasCheck(result, "C6") {
			t.Errorf("checks = %v; want C6 alongside the schema error", errorChecks(result))
		}
	})
	t.Run("a lite package with a path and a schema error", func(t *testing.T) {
		partial := partialFrom(t, StepRequirements)
		partial.Profile = ProfileLite
		reqs := deepCopyRequirements(*partial.Requirements)
		reqs.Requirements[0].Title = ""
		partial.Requirements = &reqs

		result := ValidateGenerationStep(StepRequirements, partial)
		if !hasCheck(result, "C7") {
			t.Errorf("checks = %v; want C7 alongside the schema error", errorChecks(result))
		}
	})
}

// A tasks artifact that breaks the schema and leaves a criterion unowned is
// told about both at once, schema errors first, instead of learning of the
// gap only after regenerating the whole file to fix the schema.
func TestValidateGenerationStepComplete_ReportsEveryProblemInOnePass(t *testing.T) {
	partial := partialFrom(t, StepRequirements, StepTasks)
	tasks := *partial.Tasks
	tasks.Tasks = append([]Task{}, tasks.Tasks...)
	tasks.Tasks[0].Title = ""                  // violates minLength
	tasks.Tasks[1].Criteria = []string{"REQ-1"} // leaves REQ-2.1 unowned
	partial.Tasks = &tasks

	short := ValidateGenerationStep(StepTasks, partial)
	if hasCheck(short, "C5") {
		t.Fatalf("ValidateGenerationStep should still stop at the schema: %v", errorChecks(short))
	}

	result := ValidateGenerationStepComplete(StepTasks, partial)
	if result.Valid {
		t.Fatal("an invalid plan was accepted")
	}
	if !hasCheck(result, "C5") || !hasCheck(result, "json_schema") {
		t.Fatalf("checks = %v; want the schema error and C5", errorChecks(result))
	}
	if result.Errors[0].Category != "schema" {
		t.Errorf("the schema errors should come first: %+v", result.Errors[0])
	}
}

// The reference rules run on whatever decoded, so an artifact missing most of
// what the schema requires must not panic them.
func TestValidateGenerationStepComplete_SurvivesAStrippedArtifact(t *testing.T) {
	partial := partialFrom(t, StepRequirements)
	partial.Tasks = &TasksV3Json{Tasks: []Task{{}, {Criteria: []string{"REQ-99.1"}, DependsOn: []int{7}}}}
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
	reqs, ok := decoded.(*RequirementsV3Json)
	if !ok {
		t.Fatalf("DecodeArtifact returned %T; want *RequirementsV3Json", decoded)
	}
	if len(reqs.Requirements) != 2 {
		t.Errorf("requirements = %d; want 2", len(reqs.Requirements))
	}
	if _, err := DecodeArtifact(StepTasks, artifactMap(t, source.Tasks)); err != nil {
		t.Errorf("DecodeArtifact(tasks) = %v", err)
	}
}

func TestDecodeArtifact_RefusesAnUnknownStepOrAMistypedArtifact(t *testing.T) {
	if _, err := DecodeArtifact("nonsense", map[string]any{}); err == nil {
		t.Error("an unknown step was accepted")
	}
	if _, err := DecodeArtifact("test_spec", map[string]any{"tests": []any{}}); err == nil {
		t.Error("the version 2 test_spec step was accepted")
	}
	if _, err := DecodeArtifact(StepTasks, map[string]any{"tasks": "not an array"}); err == nil {
		t.Error("a type-mismatched artifact decoded without error")
	}
}

// A key the schema does not allow is refused when the model submits it, by its
// path, rather than silently dropped. A version 2 field is such a key.
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
	first := content["tasks"].([]any)[0].(map[string]any)
	first["done-when"] = []any{"x"}
	first["tests"] = []any{"TS-01-1"}
	first["state"] = "pending"
	_, err = DecodeArtifact(StepTasks, content)
	if err == nil {
		t.Fatal("unknown keys were accepted")
	}
	for _, want := range []string{"/traceability", "/tasks/0/done-when", "/tasks/0/tests", "/tasks/0/state"} {
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
	if len(GenerationSteps) != 2 {
		t.Errorf("GenerationSteps = %v; want requirements and tasks", GenerationSteps)
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
		{Check: "C5", Message: "criterion REQ-1.3 is not owned by any task"},
		{Check: "json_schema", Path: "/requirements/0/criteria/0/check", Message: "missing property 'kind'"},
	})
	for _, want := range []string{"[C5]", "REQ-1.3", "at /requirements/0/criteria/0/check"} {
		if !strings.Contains(out, want) {
			t.Errorf("the feedback is missing %q:\n%s", want, out)
		}
	}
}
