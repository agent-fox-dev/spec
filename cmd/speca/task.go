package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

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
		return nil, refusal
	}

	// c.opts.tasks was checked with the flags, so it parses.
	wanted, _ := parseTaskSpec(c.opts.tasks)
	if refusal := checkNamedTasks(name, spec.Tasks, wanted); refusal != nil {
		return nil, refusal
	}

	if _, refusal := planMoves(name, spec.Tasks, wanted, afspec.TaskState(c.opts.state)); refusal != nil {
		return nil, refusal
	}
	return nil, &cli.ErrorInfo{Stage: "load", Category: "input", Message: "the task form is not yet implemented"}
}

// unwritable refuses a package that Save would refuse to write: one that is
// sealed, superseded or archived, and an active one whose intent has drifted.
// --force does not bypass it, because the library has no way to write such a
// package. It is a lifecycle refusal at stage status.
func unwritable(name string, spec *afspec.Spec) *cli.ErrorInfo {
	switch spec.Status {
	case string(afspec.PrdFrontmatterV2JsonStatusSealed),
		string(afspec.PrdFrontmatterV2JsonStatusSuperseded),
		string(afspec.PrdFrontmatterV2JsonStatusArchived):
		return &cli.ErrorInfo{Stage: "status", Category: envelope.CategoryLifecycle,
			Message: fmt.Sprintf("%s is %s, so its tasks can no longer change: a change to it is a new package that supersedes it", name, spec.Status)}
	}
	if intentDrift(spec) {
		return &cli.ErrorInfo{Stage: "status", Category: envelope.CategoryLifecycle,
			Message: fmt.Sprintf("%s is active and its ## Intent section no longer matches its intent_hash (or is gone): "+
				"the library refuses every write until the intent matches its hash again", name)}
	}
	return nil
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
