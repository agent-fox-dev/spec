// Package authoring holds the checks on how a package is written that are not
// the format's rules: a vague word in a criterion, a step or touches entry
// that names a line of a file, and a test command from an ecosystem the
// project does not have.
//
// The format's validation matches no words (format §0, rule 3; ADR 18), so
// none of these is part of a package's verdict and none makes it invalid.
// spec sends them back to a generation phase once, while it can still act on
// them (ADR 04); speca --validate reports them beside the verdict as hints,
// for whoever writes a package by hand or in a session (ADR 20).
package authoring

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/project"
)

// The checks, as each hint names its own.
const (
	CheckVagueLanguage      = "vague_language"
	CheckLineReference      = "line_reference"
	CheckForeignTestCommand = "foreign_test_command"
)

// vagueWordsRe matches the words the format asks a criterion to avoid
// (§6.5.4). The PRD review and the plan review rubrics list the same words.
var vagueWordsRe = regexp.MustCompile(`(?i)\b(appropriate|properly|correctly|reasonable|relevant|adequate|suitable|as needed|if necessary|etc)\b`)

// lineReferenceRe matches a reference to a line of a file: a path with an
// extension followed by a colon and a line or a range (`afspec/afspec.go:88`,
// `render.go:12-30`), or the word line or lines followed by a number.
var lineReferenceRe = regexp.MustCompile(`(?i)(\b[\w./-]+\.[A-Za-z0-9]+:\d+(?:-\d+)?\b|\blines? \d+\b)`)

// Hints are every hint on a loaded package: the vague words of its criteria,
// the line references of its tasks, and its foreign test commands under
// profile, in that order. A package missing an artifact gets the hints of the
// ones it has.
func Hints(spec *afspec.Spec, profile project.Profile) []afspec.ValidationEntry {
	out := []afspec.ValidationEntry{}
	if spec == nil {
		return out
	}
	out = append(out, VagueWords(spec.Requirements)...)
	out = append(out, LineReferences(spec.Tasks)...)
	out = append(out, ForeignTestCommands(spec.Tasks, profile)...)
	return out
}

func hint(check, artifact, entity, msg string) afspec.ValidationEntry {
	return afspec.ValidationEntry{Category: "warning", Check: check, Artifact: artifact, EntityID: entity, Message: msg}
}

// VagueWords names each vague word in a criterion's action and condition, in
// that order of fields.
func VagueWords(r *afspec.RequirementsV3Json) []afspec.ValidationEntry {
	if r == nil {
		return nil
	}
	var out []afspec.ValidationEntry
	for _, req := range r.Requirements {
		for _, c := range req.Criteria {
			fields := []struct{ name, text string }{{"action", c.Action}}
			if c.Condition != nil {
				fields = append(fields, struct{ name, text string }{"condition", *c.Condition})
			}
			for _, f := range fields {
				for _, match := range vagueWordsRe.FindAllString(f.text, -1) {
					out = append(out, hint(CheckVagueLanguage, "requirements.json", c.Id,
						fmt.Sprintf("vague term %q in field %s of criterion %s", strings.ToLower(match), f.name, c.Id)))
				}
			}
		}
	}
	return out
}

// LineReferences names each step and touches entry of a task that points at
// a line of a file, which is wrong after the first commit that moves it.
func LineReferences(t *afspec.TasksV3Json) []afspec.ValidationEntry {
	if t == nil {
		return nil
	}
	var out []afspec.ValidationEntry
	for _, task := range t.Tasks {
		id := fmt.Sprint(task.Id)
		for i, step := range task.Steps {
			for _, ref := range lineReferenceRe.FindAllString(step, -1) {
				out = append(out, hint(CheckLineReference, "tasks.json", id,
					fmt.Sprintf("step %d.%d names a line of a file, %q: say the behaviour or the decision, and name the file in touches", task.Id, i+1, ref)))
			}
		}
		for _, path := range task.Touches {
			for _, ref := range lineReferenceRe.FindAllString(path, -1) {
				out = append(out, hint(CheckLineReference, "tasks.json", id,
					fmt.Sprintf("touches entry %q of task %d names a line of a file, %q: name the file alone", path, task.Id, ref)))
			}
		}
	}
	return out
}

// ForeignTestCommands names each test command of the plan whose program
// belongs to a known ecosystem the project does not have, with the project's
// own command where it has one. It is the check spec refuses a tasks
// artifact on (project.Profile.AuditTasks), reported instead of refused.
func ForeignTestCommands(t *afspec.TasksV3Json, profile project.Profile) []afspec.ValidationEntry {
	if t == nil {
		return nil
	}
	commands := map[string]string{"all_tests": t.TestCommands.AllTests, "linter": t.TestCommands.Linter}
	if t.TestCommands.SpecTests != nil {
		commands["spec_tests"] = *t.TestCommands.SpecTests
	}
	if t.TestCommands.AffectedTests != nil {
		commands["affected_tests"] = *t.TestCommands.AffectedTests
	}
	var out []afspec.ValidationEntry
	for _, f := range profile.ForeignCommands(commands) {
		msg := fmt.Sprintf("%s, but this project is %s (detected from %s)", f, profile.Language, profile.Manifest)
		if own := ownCommand(profile, f.Field); own != "" {
			msg += fmt.Sprintf("; the project's own is %q", own)
		}
		out = append(out, hint(CheckForeignTestCommand, "tasks.json", "test_commands."+f.Field, msg))
	}
	return out
}

// ownCommand is the project's command for a test_commands field, or "" when
// detection found none or the field has no project-wide equivalent.
func ownCommand(p project.Profile, field string) string {
	switch field {
	case "all_tests":
		return p.AllTests
	case "linter":
		return p.Linter
	}
	return ""
}
