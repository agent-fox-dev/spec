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

// saveProgress writes the package's progress file, the one file a task move
// writes. It is a variable so that a test can make the write fail.
var saveProgress = (*afspec.Spec).SaveProgress

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

	// The version 3 types read none of an older plan's states: a v2 task
	// carries its state in tasks.json, which has no progress.json to write.
	if specpkg.Legacy(spec) {
		return nil, usageInfo(fmt.Errorf("%s is format version %d, and speca writes format version %d only: "+
			"none of its task states can be read; convert it to version %d first "+
			"(speca %s --migrate converts a version 2 package)",
			name, spec.SchemaVersion, afspec.SchemaVersion, afspec.SchemaVersion, name), nil)
	}

	// A package Save would refuse is refused here, before any move is looked at,
	// so that --dry-run reports the refusal the real call would meet.
	if refusal := unwritable(name, spec); refusal != nil {
		return c.refused(dir, spec, refusal)
	}

	// c.opts.tasks was checked with the flags, so it parses.
	wanted, _ := parseTaskSpec(c.opts.tasks)
	if refusal := checkNamedTasks(name, spec, wanted); refusal != nil {
		return nil, refusal
	}

	moves, refusal := planMoves(name, spec, wanted, afspec.TaskState(c.opts.state), c.opts.force)
	if refusal != nil {
		return c.refused(dir, spec, refusal)
	}
	evidenceLog, refusal := c.closeOnEvidence(name, spec, &moves)
	if refusal != nil {
		return c.refused(dir, spec, refusal)
	}

	warnings := moves.warnings
	validity := spec.Validate()
	if len(moves.moves) > 0 {
		// The write re-encodes progress.json from the Go types, and the decode
		// dropped the keys they lack: writing a package whose progress holds
		// any deletes them. That is data loss, and a refusal that --force
		// overrides. The other artifacts are not written by a task move, so
		// their keys are safe.
		v := validity
		switch unknown := progressUnknownKeys(v.Errors); {
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

		spec.Progress = moves.after
		spec.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		spec.Progress.UpdatedAt = spec.UpdatedAt
		// The report is on the package as written, which holds none of the keys
		// the write deletes.
		validity = validateWritten(spec, afspec.ProgressFileName)
		if !c.opts.dryRun {
			if failure := writeProgress(spec, name); failure != nil {
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
	r.EvidenceLog = evidenceLog
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

// writeProgress writes the package's progress file. What the library refuses
// on lifecycle or intent grounds is the status refusal it is; a failure to
// write is a disk failure, and says what the caller may find.
func writeProgress(spec *afspec.Spec, name string) *cli.ErrorInfo {
	err := saveProgress(spec, spec.Dir)
	if err == nil {
		return nil
	}
	var lifecycle *afspec.LifecycleError
	var intent *afspec.IntentError
	if errors.As(err, &lifecycle) || errors.As(err, &intent) {
		return &cli.ErrorInfo{Stage: "status", Category: envelope.CategoryLifecycle, Message: fmt.Sprintf("%s cannot be written: %v", name, err)}
	}
	return &cli.ErrorInfo{Stage: "write", Category: envelope.CategoryDisk,
		Message: fmt.Sprintf("%s could not be written: %v. A failure while writing the temporary file leaves the package unchanged; "+
			"a failure during the final rename may have replaced progress.json, so run `speca %s` to see the state", name, err, name)}
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
	case len(specpkg.ContractDrift(spec)) > 0:
		return contractDriftRefusal(name, specpkg.ContractDrift(spec), "its tasks can change")
	}
	switch spec.Status {
	case string(afspec.PrdFrontmatterV3JsonStatusSealed),
		string(afspec.PrdFrontmatterV3JsonStatusSuperseded),
		string(afspec.PrdFrontmatterV3JsonStatusArchived):
		return &cli.ErrorInfo{Stage: "status", Category: envelope.CategoryLifecycle,
			Message: fmt.Sprintf("%s is %s, so its tasks can no longer change: a change to it is a new package that supersedes it", name, spec.Status)}
	}
	return &cli.ErrorInfo{Stage: "status", Category: envelope.CategoryLifecycle,
		Message: fmt.Sprintf("%s has the status %q, which the format does not define: only a draft, or an active package whose intent has not drifted, takes a change", name, spec.Status)}
}

// contractDriftRefusal is the refusal of a call on an active package whose
// contract files no longer match the contract_hash it was activated with,
// naming the files that differ. There is no call that accepts a changed
// contract, so it has no fix_hint, and --force does not override it (ADR 26).
func contractDriftRefusal(name string, files []string, what string) *cli.ErrorInfo {
	verb := "no longer matches"
	if len(files) > 1 {
		verb = "no longer match"
	}
	list := strings.Join(files, " and ")
	return &cli.ErrorInfo{Stage: "status", Category: envelope.CategoryLifecycle,
		Message: fmt.Sprintf("%s is active and its contract has changed since it was activated: %s %s its contract_hash; "+
			"restore %s to the contract the package was activated with before %s, or write a package that supersedes this one",
			name, list, verb, list, what)}
}

// checkNamedTasks refuses a --task that names what the plan cannot supply: an
// id it does not have, next when there is no next task, or all when there is
// no task. It is a usage error: the call cannot hold for this package. The
// plan is the package's, the derived integration task included.
func checkNamedTasks(name string, spec *afspec.Spec, wanted taskSpec) *cli.ErrorInfo {
	tasks := spec.Plan()
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = strconv.Itoa(t.Id)
	}

	switch wanted.keyword {
	case "next":
		if _, ok := spec.NextTask(); ok {
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
	return unknownKeysOf(entries, "")
}

// progressUnknownKeys is unknownKeys for progress.json alone, the one file a
// task move writes.
func progressUnknownKeys(entries []afspec.ValidationEntry) []string {
	return unknownKeysOf(entries, afspec.ProgressFileName)
}

// unknownKeysOf is unknownKeys for the artifact named, or for all of them when
// artifact is "".
func unknownKeysOf(entries []afspec.ValidationEntry, artifact string) []string {
	var out []string
	for _, e := range entries {
		if e.Check != "unknown_field" || (artifact != "" && e.Artifact != artifact) {
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
	for _, key := range afspec.UnknownFields(raw, &afspec.PrdFrontmatterV3Json{}) {
		out = append(out, "prd.md "+key)
	}
	return out
}

// validateWritten is the verdict on the package as a write leaves it:
// Validate without the unknown keys of the artifacts the write re-encodes,
// which it leaves out. The task form writes progress.json alone, so only
// its keys go; the status form saves every artifact, so all of them do. It
// runs the library's two stages as Validate does, the cross-file rules only
// when the schema holds.
func validateWritten(spec *afspec.Spec, written ...string) afspec.ValidationResult {
	schema := spec.ValidateSchema()
	errs := slices.DeleteFunc(slices.Clone(schema.Errors), func(e afspec.ValidationEntry) bool {
		return e.Check == "unknown_field" && (len(written) == 0 || slices.Contains(written, e.Artifact))
	})
	if len(errs) > 0 {
		return afspec.ValidationResult{Valid: false, Errors: errs, Warnings: schema.Warnings}
	}
	cross := spec.ValidateCrossFile()
	return afspec.ValidationResult{Valid: len(cross.Errors) == 0, Errors: cross.Errors, Warnings: append(schema.Warnings, cross.Warnings...)}
}
