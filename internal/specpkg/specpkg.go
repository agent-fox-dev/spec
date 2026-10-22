// Package specpkg is what the commands that work on a spec package on disk
// share: spec, specr and speca each need to find a package from a reference,
// show where it is, list its tasks and describe how it validates, and none of
// them should carry a copy.
//
// It sits between the library and the shell. It knows afspec's types and the
// public output types of package envelope, and the shell's usage error, and
// nothing of how a command parses its flags or writes its output.
package specpkg

import (
	"path/filepath"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
)

// Display is how a command shows the path of a package: dir relative to root
// when it is inside it, so a suggested next call stays short and runnable from
// the repository, and absolute otherwise.
func Display(dir, root string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return dir
	}
	return rel
}

// Rules names the rules a set of validation entries break: the Check of each,
// or its Category when it has none, each once, in the order it first appears.
func Rules(entries []afspec.ValidationEntry) []string {
	var out []string
	for _, e := range entries {
		name := e.Check
		if name == "" {
			name = e.Category
		}
		if !contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TaskInfos lists the plan's tasks without their bodies, in plan order. A
// package without a plan gives an empty list, not nil.
func TaskInfos(spec *afspec.Spec) []envelope.TaskInfo {
	out := []envelope.TaskInfo{}
	if spec.Tasks == nil {
		return out
	}
	for _, t := range spec.Tasks.Tasks {
		out = append(out, TaskInfo(spec.Tasks, t))
	}
	return out
}

// TaskInfo describes one task of plan, with ready from the plan's own rule
// (afspec's IsReady), so that every command means the same by it.
func TaskInfo(plan *afspec.TasksV2Json, t afspec.Task) envelope.TaskInfo {
	return envelope.TaskInfo{
		ID: t.Id, Title: t.Title, Kind: string(t.Kind), State: string(t.State),
		Optional: t.IsOptional(), DependsOn: append([]int(nil), t.DependsOn...),
		Ready: plan.IsReady(t.Id),
	}
}

// Legacy reports whether the package is in a format version other than the
// one this library reads. Such a package loads, but the v2 types read none of
// its tests or tasks: a v1 plan is task_groups, not tasks.
func Legacy(spec *afspec.Spec) bool {
	return spec.SchemaVersion != 0 && spec.SchemaVersion != afspec.SchemaVersion
}
