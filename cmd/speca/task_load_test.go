package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/internal/cli"
)

// 11-REQ-1.8, TS-11-8: a package the library refuses stops the task form as it
// stops the report, with no result, and with nothing said about the format.
func TestTheTaskFormStopsOnAPackageThatDoesNotLoad_TS11_8(t *testing.T) {
	root := newRepo(t)
	if err := os.WriteFile(filepath.Join(pkgDir(root, "09_agent_mode"), "tasks.json"), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, root)
	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress")
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "load" || env.Error.Category != "input" || env.Result != nil || env.OK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	if !strings.Contains(env.Error.Message, "could not be read") || !strings.Contains(env.Error.Message, "tasks.json") {
		t.Errorf("message = %q; want the package and the library's reason", env.Error.Message)
	}
	if len(env.Warnings) != 0 || !mapsEqual(before, snapshot(t, root)) {
		t.Errorf("warnings %v; the repository changed: %v", warningCodes(env), !mapsEqual(before, snapshot(t, root)))
	}
}

// TS-11-9: ids the plan does not have are refused, every one named, with the
// plan's ids as the fix.
func TestUnknownTaskIDsAreRefused_TS11_9(t *testing.T) {
	root := newRepo(t)
	e := refusedAsUsage(t, root, []string{"--dir", root, "09", "--task", "2,7,9", "--state", "done"}, "09_agent_mode", "7", "9")
	if e == nil || e.FixHint == nil || e.FixHint.Flag != "--task" || !slices.Equal(e.FixHint.Valid, []string{"1", "2", "3"}) {
		t.Errorf("fix_hint = %+v; want --task with 1, 2, 3", e)
	}
	if e != nil && !strings.Contains(e.Message, "has no tasks 7, 9;") {
		t.Errorf("message %q does not name exactly 7 and 9 as the unknown ones", e.Message)
	}
}

// TS-11-10: on a plan with no tasks an id is unknown, and the message says the
// plan has none, with no list to offer.
func TestAnIDOnAPlanWithNoTasks_TS11_10(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"))
	e := refusedAsUsage(t, root, []string{"--dir", root, "09", "--task", "1", "--state", "in_progress"}, "09_agent_mode", "no tasks")
	if e != nil && e.FixHint != nil {
		t.Errorf("fix_hint = %+v; want none", e.FixHint)
	}
}

// TS-11-11: --task next with no next task says why, and offers the pending ids
// when they are only blocked.
func TestNextWithNoNextTask_TS11_11(t *testing.T) {
	finished := newRepo(t)
	setTasks(t, pkgDir(finished, "09_agent_mode"), planTask(1, "done", nil), planTask(2, "dropped", []int{1}))
	e := refusedAsUsage(t, finished, []string{"--dir", finished, "09", "--task", "next", "--state", "in_progress"}, "09_agent_mode", "every task is done or dropped")
	if e != nil && e.FixHint != nil {
		t.Errorf("a finished plan: fix_hint = %+v; want none", e.FixHint)
	}

	blocked := newRepo(t)
	setTasks(t, pkgDir(blocked, "09_agent_mode"), planTask(1, "dropped", nil), planTask(2, "pending", []int{1}))
	e = refusedAsUsage(t, blocked, []string{"--dir", blocked, "09", "--task", "next", "--state", "in_progress"}, "09_agent_mode", "no pending task has its dependencies done")
	if e == nil || e.FixHint == nil || e.FixHint.Flag != "--task" || !slices.Equal(e.FixHint.Valid, []string{"2"}) {
		t.Errorf("a blocked plan: fix_hint = %+v; want --task with 2", e)
	}
}

// TS-11-12: --task all on a plan with no tasks has nothing to name.
func TestAllOnAPlanWithNoTasks_TS11_12(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"))
	refusedAsUsage(t, root, []string{"--dir", root, "09", "--task", "all", "--state", "done"}, "09_agent_mode", "no tasks")
}

// TS-11-13: a package in another format version is refused, force or not,
// because a save would replace its plan with an empty one.
func TestAPackageInAnotherFormatIsRefused_TS11_13(t *testing.T) {
	root := newRepo(t)
	addLegacyPackage(t, root)
	for _, extra := range [][]string{nil, {"--force"}, {"--dry-run"}} {
		argv := append([]string{"--dir", root, "07", "--task", "1", "--state", "in_progress"}, extra...)
		before := snapshot(t, root)
		env, code, _ := run(t, argv...)
		if code != cli.ExitUsage || env.Error == nil || env.Error.Stage != "usage" || env.Error.Category != "usage" || env.Result != nil {
			t.Errorf("%v: code %d: %+v", extra, code, env.Error)
			continue
		}
		for _, want := range []string{"07_legacy", "format version 1", "writes format version 2 only", "discard", "v1 plan"} {
			if !strings.Contains(env.Error.Message, want) {
				t.Errorf("%v: message %q does not say %q", extra, env.Error.Message, want)
			}
		}
		if len(env.Warnings) != 0 {
			t.Errorf("%v: warnings %v; a refusal does not also report what loaded", extra, warningCodes(env))
		}
		if !mapsEqual(before, snapshot(t, root)) {
			t.Errorf("%v: the repository changed", extra)
		}
	}
}
