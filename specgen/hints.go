package specgen

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
)

// The generator's own hints on an artifact a phase submits. They are not the
// format's rules: the package's validation matches no words (format §0, rule
// 3), and a package on disk is never checked for them. They are sent back to
// the phase once, beside the validator's warnings (ADR 04), because a word or
// a line number nobody questions is the same word in the finished spec.

// vagueWordsRe matches the words the format asks a criterion to avoid
// (§6.5.4). The PRD review and the plan review rubrics list the same words.
var vagueWordsRe = regexp.MustCompile(`(?i)\b(appropriate|properly|correctly|reasonable|relevant|adequate|suitable|as needed|if necessary|etc)\b`)

// lineReferenceRe matches a reference to a line of a file: a path with an
// extension followed by a colon and a line or a range (`afspec/afspec.go:88`,
// `render.go:12-30`), or the word line or lines followed by a number.
var lineReferenceRe = regexp.MustCompile(`(?i)(\b[\w./-]+\.[A-Za-z0-9]+:\d+(?:-\d+)?\b|\blines? \d+\b)`)

// generatorHints are the generator's hints on the typed artifact a step
// submitted: vague words in the criteria of a requirements artifact, and
// line references in the steps and touches of a tasks artifact.
func generatorHints(step afspec.GenerationStep, artifact any) []afspec.ValidationEntry {
	switch a := artifact.(type) {
	case *afspec.RequirementsV3Json:
		if step == afspec.StepRequirements {
			return vagueWordHints(a)
		}
	case *afspec.TasksV3Json:
		if step == afspec.StepTasks {
			return lineReferenceHints(a)
		}
	}
	return nil
}

func hint(check, artifact, entity, msg string) afspec.ValidationEntry {
	return afspec.ValidationEntry{Category: "warning", Check: check, Artifact: artifact, EntityID: entity, Message: msg}
}

// vagueWordHints names each vague word in a criterion's action and
// condition, in that order of fields.
func vagueWordHints(r *afspec.RequirementsV3Json) []afspec.ValidationEntry {
	var out []afspec.ValidationEntry
	for _, req := range r.Requirements {
		for _, c := range req.Criteria {
			fields := []struct{ name, text string }{{"action", c.Action}}
			if c.Condition != nil {
				fields = append(fields, struct{ name, text string }{"condition", *c.Condition})
			}
			for _, f := range fields {
				for _, match := range vagueWordsRe.FindAllString(f.text, -1) {
					out = append(out, hint("vague_language", "requirements.json", c.Id,
						fmt.Sprintf("vague term %q in field %s of criterion %s", strings.ToLower(match), f.name, c.Id)))
				}
			}
		}
	}
	return out
}

// lineReferenceHints names each step and touches entry of a task that points
// at a line of a file, which is wrong after the first commit that moves it.
func lineReferenceHints(t *afspec.TasksV3Json) []afspec.ValidationEntry {
	var out []afspec.ValidationEntry
	for _, task := range t.Tasks {
		id := fmt.Sprint(task.Id)
		for i, step := range task.Steps {
			for _, ref := range lineReferenceRe.FindAllString(step, -1) {
				out = append(out, hint("line_reference", "tasks.json", id,
					fmt.Sprintf("step %d.%d names a line of a file, %q: say the behaviour or the decision, and name the file in touches", task.Id, i+1, ref)))
			}
		}
		for _, path := range task.Touches {
			for _, ref := range lineReferenceRe.FindAllString(path, -1) {
				out = append(out, hint("line_reference", "tasks.json", id,
					fmt.Sprintf("touches entry %q of task %d names a line of a file, %q: name the file alone", path, task.Id, ref)))
			}
		}
	}
	return out
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
