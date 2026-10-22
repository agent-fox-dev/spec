package main

import (
	"slices"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// report is the form of speca <spec>: the state of the package in dir, which
// it reads and changes nothing of.
func report(c *call, dir string) (*Result, *cli.ErrorInfo) {
	spec, failure := c.loadPackage(dir)
	if failure != nil {
		return nil, failure
	}
	r := c.reportOf(dir, spec)
	r.lifecycle = statusSuggestion(r, spec)
	return r, nil
}

// statusSuggestion is the status call the report suggests for the package: to
// activate a draft that validates and has an intent, and to seal an active
// package that has no task open. The conditions are the checks of the status
// form, the refusals that --force does not override and the ones it does, so that
// a suggestion never comes back as a refusal; a package for which any of them
// fails is not told to make the call. A package in another format version is
// told nothing: the status form does not change it.
func statusSuggestion(r *Result, spec *afspec.Spec) []cli.Next {
	if specpkg.Legacy(spec) {
		return nil
	}
	holdsKeys := slices.Contains(r.FailingRules, "unknown_field") || len(frontmatterUnknownKeys(spec.Dir)) > 0
	switch spec.Status {
	case string(afspec.PrdFrontmatterV2JsonStatusDraft):
		if r.Valid != nil && *r.Valid && afspec.HasIntent(spec.PRDBody) && !holdsKeys {
			return []cli.Next{{Tool: tool, Input: r.SpecDir, Flags: []string{"--status", "active"},
				Why: "the package validates and has an intent; activating it records the hash of its ## Intent"}}
		}
	case string(afspec.PrdFrontmatterV2JsonStatusActive):
		if len(openTasks(spec.Tasks)) == 0 && !r.IntentDrift && !holdsKeys {
			return []cli.Next{{Tool: tool, Input: r.SpecDir, Flags: []string{"--status", "sealed"},
				Why: "no task is open and the intent is as it was activated; sealing the package ends its work"}}
		}
	}
	return nil
}

// reportOf is the report on spec, which was loaded from dir. The task form
// calls it too, on the package after a change, which may not be on disk (under
// --dry-run it never is), so it reads nothing of the package from dir.
func (c *call) reportOf(dir string, spec *afspec.Spec) *Result {
	return c.reportWith(dir, spec, spec.Validate())
}

// reportWith is the report on spec with v as its verdict. The task form gives
// the verdict on the package as written, which is not Validate's when a write
// deletes keys the library reported.
func (c *call) reportWith(dir string, spec *afspec.Spec, v afspec.ValidationResult) *Result {
	r := &Result{AdminResult: c.identityOf(dir, spec)}

	// The package's own verdict. A version 2 package that does not validate
	// raises no warning: valid and failing_rules are the answer.
	valid := v.Valid
	r.Valid = &valid
	if !valid {
		r.FailingRules = specpkg.Rules(v.Errors)
	}

	r.Tasks = specpkg.TaskInfos(spec)
	r.TaskCounts = countTasks(spec.Tasks)
	if next, ok := spec.Tasks.NextTask(); ok {
		info := specpkg.TaskInfo(spec.Tasks, *next)
		r.NextTask = &info
	}
	r.AllowedStatus = allowedStatus(spec.Status)
	r.DependsOn = dependsOn(spec)
	if metas, ok := c.discover("dependents is left out"); ok {
		r.Dependents = dependentsOf(metas, c.specsDir, spec.SpecID)
	}
	return r
}

// countTasks counts the plan's tasks by state. Total is the number of tasks, so
// a task whose state is none of the four (which the package's validation
// reports) is in the total and in no count.
func countTasks(plan *afspec.TasksV2Json) *envelope.TaskCounts {
	c := &envelope.TaskCounts{}
	if plan == nil {
		return c
	}
	for _, t := range plan.Tasks {
		switch t.State {
		case afspec.TaskStatePending:
			c.Pending++
		case afspec.TaskStateInProgress:
			c.InProgress++
		case afspec.TaskStateDone:
			c.Done++
		case afspec.TaskStateDropped:
			c.Dropped++
		}
	}
	c.Total = len(plan.Tasks)
	return c
}

// allowedStatus lists the lifecycle states a package can move to from status,
// in the order of the lifecycle; none for a terminal status.
func allowedStatus(status string) []string {
	out := []string{}
	for _, target := range lifecycle {
		if afspec.ValidTransition(status, target) {
			out = append(out, target)
		}
	}
	return out
}

// dependsOn lists the spec ids the package's own tasks.json depends on, in
// file order, each once. It does not need the spec root, so it is known when
// the root cannot be read.
func dependsOn(spec *afspec.Spec) []string {
	out := []string{}
	if spec.Tasks == nil {
		return out
	}
	for _, d := range spec.Tasks.Dependencies {
		if !slices.Contains(out, d.Spec) {
			out = append(out, d.Spec)
		}
	}
	return out
}

// dependentsOf lists the spec ids of the other packages in the spec root that
// depend on the package, sorted and each once.
func dependentsOf(metas []afspec.SpecMeta, specsDir, id string) []string {
	// The graph's error says a package depends on one that is not in the root;
	// the graph it returns alongside holds every edge that was read.
	graph, _ := afspec.BuildDependencyGraph(metas, specsDir)
	out := []string{}
	for _, e := range graph.Dependents(id) {
		if e.FromSpec != id {
			out = append(out, e.FromSpec)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
