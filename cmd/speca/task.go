package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// saveSpec writes the package. It is a variable so that a test can make the
// write fail.
var saveSpec = (*afspec.Spec).Save

// executeTask is the task form: speca <spec> --task T --state S.
//
// It loads the package and refuses what cannot be done with it before anything
// is moved. A call that is refused as a usage error, or whose package does not
// load, carries no result.
func executeTask(c *call, dir string) (*Result, *cli.ErrorInfo) {
	spec, failure := load(dir)
	if failure != nil {
		return nil, failure
	}
	name := specpkg.Display(dir, c.root)

	// A save re-encodes every artifact from the version 2 types, which read
	// none of a version 1 plan: it would replace the plan with an empty one.
	if specpkg.Legacy(spec) {
		return nil, usageInfo(fmt.Errorf("%s is format version %d, and speca writes format version %d only: "+
			"none of its tasks can be read, and a save would discard the v1 plan; convert it to version %d first "+
			"(format v2 §13 maps each v1 construct to its v2 form)",
			name, spec.SchemaVersion, afspec.SchemaVersion, afspec.SchemaVersion), nil)
	}

	// A package Save would refuse is refused here, before any move is looked at,
	// so that --dry-run reports the refusal the real call would meet.
	if refusal := unwritable(name, spec); refusal != nil {
		return c.refused(dir, spec, refusal)
	}

	// c.opts.tasks was checked with the flags, so it parses.
	wanted, _ := parseTaskSpec(c.opts.tasks)
	if refusal := checkNamedTasks(name, spec.Tasks, wanted); refusal != nil {
		return nil, refusal
	}

	moves, refusal := planMoves(name, spec.Tasks, wanted, afspec.TaskState(c.opts.state), c.opts.force)
	if refusal != nil {
		return c.refused(dir, spec, refusal)
	}

	warnings := moves.warnings
	validity := spec.Validate()
	if len(moves.moves) > 0 {
		// A save re-encodes every artifact from the Go types, and the decode
		// dropped the keys they lack: writing a package that holds any deletes them.
		// That is data loss, and a refusal that --force overrides. The library
		// reports the keys of the three JSON artifacts; the frontmatter of prd.md
		// is looked at here.
		v := validity
		switch unknown := append(unknownKeys(v.Errors), frontmatterUnknownKeys(spec.Dir)...); {
		case len(unknown) > 0 && !c.opts.force:
			return c.refused(dir, spec, &cli.ErrorInfo{Stage: "validate", Category: envelope.CategoryInvalidSpec,
				Message: fmt.Sprintf("%s holds keys the schema does not allow, and a save would delete them: %s; "+
					"remove them yourself, or give --force to write over them", name, strings.Join(unknown, ", "))})
		case len(unknown) > 0:
			warnings = append(warnings, plannedWarning{cli.WarnSpecInvalid, "high", fmt.Sprintf(
				"the package holds keys the schema does not allow, and this write deletes them: %s", strings.Join(unknown, ", "))})
		case !v.Valid:
			warnings = append(warnings, plannedWarning{cli.WarnSpecInvalid, "low", fmt.Sprintf(
				"the package does not validate (%d errors: %s); the change went ahead", len(v.Errors), strings.Join(specpkg.Rules(v.Errors), ", "))})
		}

		spec.Tasks = moves.after
		spec.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		// The report is on the package as written, which holds none of the keys
		// the write deletes.
		validity = validateWritten(spec)
		if !c.opts.dryRun {
			if failure := writeSpec(spec, name); failure != nil {
				return c.onDisk(dir, failure)
			}
		}
	}
	// The call goes ahead: its warnings are true now.
	for _, w := range warnings {
		c.run.Warn(w.code, w.severity, "%s", w.message)
	}

	r := c.reportWith(dir, spec, validity)
	r.taskForm = true
	r.DryRun = c.opts.dryRun
	for _, m := range moves.moves {
		r.Changes = append(r.Changes, envelope.Change{Kind: "task", ID: m.id, From: string(m.from), To: string(m.to), Forced: m.forced})
	}
	if c.opts.dryRun {
		r.nextSet = true
		if len(moves.moves) > 0 {
			r.callNext = []cli.Next{realCall(r.SpecDir, moves.moves, c.opts)}
		}
	}
	return r, nil
}

// realCall is the call that makes what a dry run reported: the same call without
// --dry-run, naming the ids it resolved rather than next or all, and keeping
// --force when it was given.
func realCall(dir string, moves []move, o *options) cli.Next {
	ids := make([]string, len(moves))
	for i, m := range moves {
		ids[i] = strconv.Itoa(m.id)
	}
	flags := []string{"--task", strings.Join(ids, ","), "--state", o.state}
	if o.force {
		flags = append(flags, "--force")
	}
	return cli.Next{Tool: tool, Input: dir, Flags: flags, Why: "the same call without --dry-run, which makes these changes"}
}

// refused returns failure with the report of spec, the package as it is on disk:
// nothing was changed, so the package as it was loaded is that.
func (c *call) refused(dir string, spec *afspec.Spec, failure *cli.ErrorInfo) (*Result, *cli.ErrorInfo) {
	return c.reportOf(dir, spec), failure
}

// onDisk returns failure with the report of the package as it is on disk, read
// again, since a write that failed may have left it other than it was loaded.
// If it cannot be read the failure goes without one.
func (c *call) onDisk(dir string, failure *cli.ErrorInfo) (*Result, *cli.ErrorInfo) {
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		return nil, failure
	}
	return c.reportOf(dir, spec), failure
}

// writeSpec saves the package. What Save refuses on lifecycle or intent grounds
// is the status refusal it is; a failure to write is a disk failure, and says
// what the caller may find.
func writeSpec(spec *afspec.Spec, name string) *cli.ErrorInfo {
	err := saveSpec(spec, spec.Dir)
	if err == nil {
		return nil
	}
	var lifecycle *afspec.LifecycleError
	var intent *afspec.IntentError
	if errors.As(err, &lifecycle) || errors.As(err, &intent) {
		return &cli.ErrorInfo{Stage: "status", Category: envelope.CategoryLifecycle, Message: fmt.Sprintf("%s cannot be written: %v", name, err)}
	}
	return &cli.ErrorInfo{Stage: "write", Category: envelope.CategoryDisk,
		Message: fmt.Sprintf("%s could not be written: %v. A failure while writing the temporary files leaves the package unchanged; "+
			"a failure during the final renames may leave some files updated, so run `speca %s` to see the state", name, err, name)}
}

// unwritable refuses a package that cannot take a write: one that is sealed,
// superseded or archived, which Save would refuse, and an active one whose intent
// has drifted. So is one whose status the format does not define, which is
// neither a draft nor an active package, and is refused by name. --force does
// not bypass it, because the library has no way to write such a package. It is a
// lifecycle refusal at stage status.
func unwritable(name string, spec *afspec.Spec) *cli.ErrorInfo {
	switch {
	case specpkg.Writable(spec):
		return nil
	case specpkg.IntentDrift(spec):
		return &cli.ErrorInfo{Stage: "status", Category: envelope.CategoryLifecycle,
			Message: fmt.Sprintf("%s is active and its ## Intent section no longer matches its intent_hash (or is gone): "+
				"the library refuses every write until the intent matches its hash again", name)}
	}
	switch spec.Status {
	case string(afspec.PrdFrontmatterV2JsonStatusSealed),
		string(afspec.PrdFrontmatterV2JsonStatusSuperseded),
		string(afspec.PrdFrontmatterV2JsonStatusArchived):
		return &cli.ErrorInfo{Stage: "status", Category: envelope.CategoryLifecycle,
			Message: fmt.Sprintf("%s is %s, so its tasks can no longer change: a change to it is a new package that supersedes it", name, spec.Status)}
	}
	return &cli.ErrorInfo{Stage: "status", Category: envelope.CategoryLifecycle,
		Message: fmt.Sprintf("%s has the status %q, which the format does not define: only a draft, or an active package whose intent has not drifted, takes a change", name, spec.Status)}
}

// checkNamedTasks refuses a --task that names what the plan cannot supply: an
// id it does not have, next when there is no next task, or all when there is
// no task. It is a usage error: the call cannot hold for this package.
func checkNamedTasks(name string, plan *afspec.TasksV2Json, wanted taskSpec) *cli.ErrorInfo {
	var tasks []afspec.Task
	if plan != nil {
		tasks = plan.Tasks
	}
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = strconv.Itoa(t.Id)
	}

	switch wanted.keyword {
	case "next":
		if _, ok := plan.NextTask(); ok {
			return nil
		}
		if len(tasks) == 0 {
			return usageInfo(fmt.Errorf("%s has no tasks, so --task next names none", name), nil)
		}
		var pending []string
		for _, t := range tasks {
			if t.State == afspec.TaskStatePending {
				pending = append(pending, strconv.Itoa(t.Id))
			}
		}
		if len(pending) == 0 {
			return usageInfo(fmt.Errorf("%s has no next task: every task is done or dropped", name), nil)
		}
		return usageInfo(fmt.Errorf("%s has no next task: no pending task has its dependencies done (the pending tasks are %s)",
			name, strings.Join(pending, ", ")), &cli.FixHint{Flag: "--task", Valid: pending})
	case "all":
		if len(tasks) == 0 {
			return usageInfo(fmt.Errorf("%s has no tasks, so --task all names none", name), nil)
		}
		return nil
	}

	var unknown []string
	for _, id := range wanted.ids {
		if !slices.Contains(ids, strconv.Itoa(id)) {
			unknown = append(unknown, strconv.Itoa(id))
		}
	}
	switch {
	case len(unknown) == 0:
		return nil
	case len(tasks) == 0:
		return usageInfo(fmt.Errorf("%s has no task %s; it has no tasks", name, strings.Join(unknown, ", ")), nil)
	}
	noun := "task"
	if len(unknown) > 1 {
		noun = "tasks"
	}
	return usageInfo(fmt.Errorf("%s has no %s %s; its tasks are %s", name, noun, strings.Join(unknown, ", "), strings.Join(ids, ", ")),
		&cli.FixHint{Flag: "--task", Valid: ids})
}

// unknownKey finds the key in the library's message about one it dropped.
var unknownKey = regexp.MustCompile(`the key (.+?) is not allowed by the schema`)

// unknownKeys names, as "artifact key", each key the validation says the schema
// does not allow. The entries carry the key only in their message, so it is cut
// out of it, and the whole message stands in if the library words it otherwise.
func unknownKeys(entries []afspec.ValidationEntry) []string {
	var out []string
	for _, e := range entries {
		if e.Check != "unknown_field" {
			continue
		}
		if m := unknownKey.FindStringSubmatch(e.Message); m != nil {
			out = append(out, e.Artifact+" "+m[1])
		} else {
			out = append(out, e.Message)
		}
	}
	return out
}

// frontmatterUnknownKeys names, as "prd.md /key", each key in the frontmatter of
// the prd.md in dir that the schema does not allow. A save writes the
// frontmatter again from the fields it knows, so such a key is deleted as the
// ones in the other artifacts are; the library does not report it, so it is
// looked for here, in the frontmatter as parsePRD finds it. A prd.md that cannot
// be read or parsed has none to report: the package would not have loaded.
func frontmatterUnknownKeys(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "prd.md"))
	if err != nil {
		return nil
	}
	rest, ok := strings.CutPrefix(string(data), "---")
	if !ok {
		return nil
	}
	rest = strings.TrimPrefix(rest, "\n")
	block, _, found := strings.Cut(rest, "\n---\n")
	if !found {
		if block, found = strings.CutSuffix(rest, "\n---"); !found {
			return nil
		}
	}
	var raw map[string]any
	if yaml.Unmarshal([]byte(block), &raw) != nil {
		return nil
	}
	var out []string
	for _, key := range afspec.UnknownFields(raw, &afspec.PrdFrontmatterV2Json{}) {
		out = append(out, "prd.md "+key)
	}
	return out
}

// validateWritten is the verdict on the package as a save writes it: Validate
// without the keys the decode dropped, which the save leaves out. It runs the
// library's two stages as Validate does, the cross-file rules only when the
// schema holds.
func validateWritten(spec *afspec.Spec) afspec.ValidationResult {
	schema := spec.ValidateSchema()
	errs := slices.DeleteFunc(slices.Clone(schema.Errors), func(e afspec.ValidationEntry) bool { return e.Check == "unknown_field" })
	if len(errs) > 0 {
		return afspec.ValidationResult{Valid: false, Errors: errs, Warnings: schema.Warnings}
	}
	cross := spec.ValidateCrossFile()
	return afspec.ValidationResult{Valid: len(cross.Errors) == 0, Errors: cross.Errors, Warnings: append(schema.Warnings, cross.Warnings...)}
}
