package specgen

import (
	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/authoring"
)

// The generator's own hints on an artifact a phase submits. They are not the
// format's rules: the package's validation matches no words (format §0, rule
// 3). They are sent back to the phase once, beside the validator's warnings
// (ADR 04), because a word or a line number nobody questions is the same word
// in the finished spec. speca --validate reports the same checks on a package
// on disk, beside its verdict (ADR 20).

// generatorHints are the generator's hints on the typed artifact a step
// submitted: vague words in the criteria of a requirements artifact, and
// line references in the steps and touches of a tasks artifact.
func generatorHints(step afspec.GenerationStep, artifact any) []afspec.ValidationEntry {
	switch a := artifact.(type) {
	case *afspec.RequirementsV3Json:
		if step == afspec.StepRequirements {
			return authoring.VagueWords(a)
		}
	case *afspec.TasksV3Json:
		if step == afspec.StepTasks {
			return authoring.LineReferences(a)
		}
	}
	return nil
}

// omitDefaultDependencies removes from a submitted tasks artifact every
// depends_on that only restates the default: one id, the id of the task
// before in the array (format §8.3, "absent means the previous task"). Any
// other depends_on, an empty one among them, and the first task's, is left
// as it was.
func omitDefaultDependencies(content map[string]any) {
	tasks, ok := content["tasks"].([]any)
	if !ok {
		return
	}
	for i := 1; i < len(tasks); i++ {
		task, ok := tasks[i].(map[string]any)
		if !ok {
			continue
		}
		prev, ok := tasks[i-1].(map[string]any)
		if !ok {
			continue
		}
		deps, ok := task["depends_on"].([]any)
		if !ok || len(deps) != 1 {
			continue
		}
		if sameNumber(deps[0], prev["id"]) {
			delete(task, "depends_on")
		}
	}
}

// sameNumber reports whether two decoded JSON values are the same number.
func sameNumber(a, b any) bool {
	x, okA := a.(float64)
	y, okB := b.(float64)
	return okA && okB && x == y
}
