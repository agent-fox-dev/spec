package afspec

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GenerationStep names the artifact a generation pipeline has just produced.
// Format v3 §12.1 fixes the order: requirements, then tasks.
type GenerationStep string

const (
	StepRequirements GenerationStep = "requirements"
	StepTasks        GenerationStep = "tasks"
)

// GenerationSteps is the mandatory generation order.
var GenerationSteps = []GenerationStep{StepRequirements, StepTasks}

// SchemaNameForStep returns the bundled schema that validates the artifact a
// step produces.
func SchemaNameForStep(step GenerationStep) (string, bool) {
	switch step {
	case StepRequirements:
		return RequirementsSchemaName, true
	case StepTasks:
		return TasksSchemaName, true
	default:
		return "", false
	}
}

// ArtifactFileName returns the on-disk file name for a step's artifact.
func ArtifactFileName(step GenerationStep) string {
	switch step {
	case StepRequirements:
		return RequirementsFileName
	case StepTasks:
		return TasksFileName
	default:
		return string(step)
	}
}

// PartialSpec holds the artifacts a generation run has produced so far. Any of
// them may be nil while generation is still in progress. Profile is the
// package's profile, ProfileLite or ProfileFull (or "", which is full), which
// rule C7 holds the requirements to.
type PartialSpec struct {
	SpecID       string
	SpecName     string
	Profile      string
	Requirements *RequirementsV3Json
	Tasks        *TasksV3Json
}

// DecodeArtifact turns the tool input a model returned — a decoded JSON object
// — into the typed artifact for the given step.
func DecodeArtifact(step GenerationStep, content map[string]any) (any, error) {
	data, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("cannot re-encode the %s artifact: %w", step, err)
	}
	// A key the schema does not allow would be dropped by the decode below
	// and never reach disk; it is refused instead, by its path, so the model
	// that sent it learns the artifact's real shape.
	refuse := func(out any) error {
		if keys := UnknownFields(content, out); len(keys) > 0 {
			return fmt.Errorf("the %s artifact has key(s) the schema does not allow: %s; remove them",
				step, strings.Join(keys, ", "))
		}
		// An extension key is a team's, and the generator writes none: one
		// in a model's artifact is a mistake, refused by its path as an
		// unknown key is.
		if keys := extensionPaths(content, out); len(keys) > 0 {
			return fmt.Errorf("the %s artifact has extension key(s), which the generator does not write: %s; remove them",
				step, strings.Join(keys, ", "))
		}
		return nil
	}
	switch step {
	case StepRequirements:
		var out RequirementsV3Json
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("cannot decode the requirements artifact: %w", err)
		}
		return &out, refuse(&out)
	case StepTasks:
		var out TasksV3Json
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("cannot decode the tasks artifact: %w", err)
		}
		return &out, refuse(&out)
	default:
		return nil, fmt.Errorf("unknown generation step %q", step)
	}
}

// ValidateGenerationStep runs, on the artifacts produced so far, the schema of
// the artifact just produced plus every cross-file rule of §10.2 that is
// decidable at that point (§12.2):
//
//	after requirements  C1, C2 over requirements, C3, C6, C7
//	after tasks         the above plus C2 over tasks, C4, C5
//
// A pipeline sends the returned errors back to the model for repair and does
// not write an artifact that still fails. Warnings are advisory.
//
// When the schema fails, only C6 and C7 run beside it: the reference rules
// would mostly report consequences of the same defect.
func ValidateGenerationStep(step GenerationStep, partial PartialSpec) ValidationResult {
	return validateGenerationStep(step, partial, false)
}

// ValidateGenerationStepComplete is ValidateGenerationStep without that
// short-circuit: the cross-file rules run even when the schema fails, so a
// repair loop learns of every problem in one pass instead of one class per
// regenerated artifact. The schema errors come first, each with Category
// "schema"; a rule error may restate a consequence of one of them.
func ValidateGenerationStepComplete(step GenerationStep, partial PartialSpec) ValidationResult {
	return validateGenerationStep(step, partial, true)
}

func validateGenerationStep(step GenerationStep, partial PartialSpec, complete bool) ValidationResult {
	spec := &Spec{
		SpecID:       partial.SpecID,
		SpecName:     partial.SpecName,
		Profile:      partial.Profile,
		Requirements: partial.Requirements,
		Tasks:        partial.Tasks,
	}

	var errors []ValidationEntry

	// Schema of the artifact just produced.
	switch step {
	case StepRequirements:
		if partial.Requirements != nil {
			errors = append(errors, ValidateArtifactSchema(partial.Requirements, RequirementsSchemaName, RequirementsFileName)...)
		}
	case StepTasks:
		if partial.Tasks != nil {
			errors = append(errors, ValidateArtifactSchema(partial.Tasks, TasksSchemaName, TasksFileName)...)
		}
	}

	// A schema-invalid artifact makes the reference rules meaningless: they
	// would report consequences of the same defect. Two rules are worth
	// running anyway, because the schema's message does not say which rule
	// was broken, and a repair loop needs to be told.
	if len(errors) > 0 && !complete {
		errors = append(errors, spec.checkC6()...)
		errors = append(errors, spec.checkC7()...)
		return ValidationResult{Valid: false, Errors: errors}
	}

	idx := spec.buildIndex()

	errors = append(errors, spec.checkC1()...)
	errors = append(errors, spec.checkC2()...)
	errors = append(errors, spec.checkC3(idx)...)
	errors = append(errors, spec.checkC6()...)
	errors = append(errors, spec.checkC7()...)

	if step == StepTasks {
		errors = append(errors, spec.checkC4C5(idx)...)
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: spec.crossFileWarnings(),
	}
}

// FormatValidationEntries renders validation entries as the feedback a
// generation pipeline sends back to the model for repair. Each line names the
// rule so the model can act on it.
func FormatValidationEntries(entries []ValidationEntry) string {
	if len(entries) == 0 {
		return ""
	}
	out := ""
	for _, e := range entries {
		line := "- "
		if e.Check != "" {
			line += "[" + e.Check + "] "
		}
		if e.Path != "" {
			line += "at " + e.Path + ": "
		}
		line += e.Message
		out += line + "\n"
	}
	return out
}
