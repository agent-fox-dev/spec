package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// lifecycleRefusal is a call the package's state does not allow, at stage
// status. --force reaches it only where the refusal carries a hint that says so.
func lifecycleRefusal(message string, hint *cli.FixHint) *cli.ErrorInfo {
	return &cli.ErrorInfo{Stage: "status", Category: envelope.CategoryLifecycle, Message: message, FixHint: hint}
}

// forceHint points at --force, for a refusal that it overrides.
var forceHint = &cli.FixHint{Flag: "--force"}

// warn plans a warning for the call. It is raised when the call goes ahead, not
// before: a call that is refused later, or whose first write fails, does not
// say what it did not do.
func (sc *statusCall) warn(code cli.WarnCode, severity, format string, args ...any) {
	sc.warnings = append(sc.warnings, plannedWarning{code, severity, fmt.Sprintf(format, args...)})
}

// raise raises the planned warnings.
func (sc *statusCall) raise() {
	for _, w := range sc.warnings {
		sc.run.Warn(w.code, w.severity, "%s", w.message)
	}
	sc.warnings = nil
}

// unknownKeys names, as "artifact key", each key the package holds that the
// schema does not allow, and that a write of it would drop. The library reports
// those of the three JSON artifacts; the frontmatter of prd.md is looked at
// here, since a write renders it again from the fields it knows.
func (sc *statusCall) unknownKeys() []string {
	return append(unknownKeys(sc.spec.Validate().Errors), frontmatterUnknownKeys(sc.dir)...)
}

// checkTarget makes the checks that belong to the status asked for. Activation
// needs an intent and, unless --force, a package that validates; sealing needs
// an intent that has not drifted and, unless --force, no open task; superseding
// needs a replacing package that can replace.
func checkTarget(sc *statusCall) *cli.ErrorInfo {
	switch sc.target {
	case string(afspec.PrdFrontmatterV2JsonStatusActive):
		return sc.checkActivation()
	case string(afspec.PrdFrontmatterV2JsonStatusSealed):
		return sc.checkSealing()
	case string(afspec.PrdFrontmatterV2JsonStatusSuperseded):
		return sc.checkReplacing()
	}
	return nil
}

// checkActivation refuses a draft that has no ## Intent to hash, which --force
// does not override, and one that does not validate, which it does. The keys the
// schema does not allow are part of that verdict: the write drops them.
func (sc *statusCall) checkActivation() *cli.ErrorInfo {
	if !afspec.HasIntent(sc.spec.PRDBody) {
		return lifecycleRefusal(fmt.Sprintf("%s has no intent to hash: add a `## Intent` section to its prd.md — the heading "+
			"`## Intent`, spelled exactly so and on a line of its own — and describe the intent under it; activation records the hash of that section", sc.name), nil)
	}
	v := sc.spec.Validate()
	keys := sc.unknownKeys()
	if v.Valid && len(keys) == 0 {
		return nil
	}
	rules := specpkg.Rules(v.Errors)
	if len(keys) > 0 && !slices.Contains(rules, "unknown_field") {
		rules = append(rules, "unknown_field")
	}
	dropped := ""
	if len(keys) > 0 {
		dropped = "; the write drops these keys: " + strings.Join(keys, ", ")
	}
	if !sc.opts.force {
		return &cli.ErrorInfo{Stage: "validate", Category: envelope.CategoryInvalidSpec, FixHint: forceHint,
			Message: fmt.Sprintf("%s does not validate (%s), and an active package is one that does: fix it (`speca %s --validate` lists the findings), "+
				"or give --force to activate it anyway%s", sc.name, strings.Join(rules, ", "), sc.name, dropped)}
	}
	sc.warn(cli.WarnSpecInvalid, "high", "%s is activated although it does not validate (%s)%s", sc.name, strings.Join(rules, ", "), dropped)
	return nil
}

// checkSealing refuses a package whose intent has drifted, which --force does
// not override: Save refuses every write of such a package, and sealing it
// would make the drift permanent. A task still open (an optional one too)
// refuses it unless --force is given.
func (sc *statusCall) checkSealing() *cli.ErrorInfo {
	if intentDrift(sc.spec) {
		return lifecycleRefusal(fmt.Sprintf("%s cannot be sealed: its ## Intent has changed since it was activated (or is gone); "+
			"restore the intent that was activated, or write a new package that supersedes this one", sc.name), nil)
	}
	open := openTasks(sc.spec.Tasks)
	if len(open) == 0 {
		return nil
	}
	if !sc.opts.force {
		return lifecycleRefusal(fmt.Sprintf("%s has open tasks: %s; finish or drop each one first, or give --force to seal it anyway",
			sc.name, strings.Join(open, ", ")), forceHint)
	}
	sc.warn(cli.WarnTasksOpen, "high", "%s is sealed with open tasks: %s", sc.name, strings.Join(open, ", "))
	return nil
}

// openTasks lists the tasks that are pending or in progress, optional ones
// included, as "task N (state)", in plan order.
func openTasks(plan *afspec.TasksV2Json) []string {
	if plan == nil {
		return nil
	}
	var out []string
	for _, t := range plan.Tasks {
		if t.State == afspec.TaskStatePending || t.State == afspec.TaskStateInProgress {
			out = append(out, fmt.Sprintf("task %d (%s)", t.Id, t.State))
		}
	}
	return out
}

// checkUnknownKeys refuses a write that would drop keys the schema does not
// allow, unless --force is given, in which case it warns. It applies to the
// moves that write the package's files and are not activation, which asks the
// same as part of its validation: sealing, archiving and superseding. A call
// that only moves the directory writes no file.
func checkUnknownKeys(sc *statusCall) *cli.ErrorInfo {
	if sc.moveOnly || sc.target == string(afspec.PrdFrontmatterV2JsonStatusActive) {
		return nil
	}
	keys := sc.unknownKeys()
	if len(keys) == 0 {
		return nil
	}
	if !sc.opts.force {
		return lifecycleRefusal(fmt.Sprintf("%s holds keys the schema does not allow, and a write would drop them: %s; "+
			"remove them yourself, or give --force to write over them", sc.name, strings.Join(keys, ", ")), forceHint)
	}
	sc.warn(cli.WarnSpecInvalid, "high", "%s holds keys the schema does not allow, and this write drops them: %s", sc.name, strings.Join(keys, ", "))
	return nil
}
