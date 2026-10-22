package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/cli"
)

func plan(tasks ...map[string]any) *afspec.TasksV2Json {
	p := &afspec.TasksV2Json{}
	for _, m := range tasks {
		task := afspec.Task{Id: m["id"].(int), State: afspec.TaskState(m["state"].(string)), Title: m["title"].(string)}
		if d, ok := m["depends_on"]; ok {
			task.DependsOn = d.([]int)
		}
		p.Tasks = append(p.Tasks, task)
	}
	return p
}

func ids(moves []move) []int {
	var out []int
	for _, m := range moves {
		out = append(out, m.id)
	}
	return out
}

// 11-REQ-2.5, 2.6, 2.7, 2.8 and 3.1: next and all are resolved against the plan,
// an explicit id is moved as named, and the ids come out in plan order.
func TestResolvingWhichTasksToMove_11REQ2(t *testing.T) {
	// Plan order is not id order: 4 comes first.
	p := plan(
		planTask(4, "done", nil), planTask(2, "in_progress", []int{4}), planTask(3, "dropped", nil),
		planTask(1, "pending", []int{2}), planTask(5, "in_progress", nil),
	)
	for name, tc := range map[string]struct {
		wanted taskSpec
		target afspec.TaskState
		want   []int
	}{
		"next is NextTask: the first task in progress": {taskSpec{keyword: "next"}, afspec.TaskStateDone, []int{2}},
		// Task 1 is pending, not done and not dropped: all names it, and the machine then refuses it.
		"all skips the target state and dropped":        {taskSpec{keyword: "all"}, afspec.TaskStateDone, []int{2, 1, 5}},
		"all to pending skips only pending and dropped": {taskSpec{keyword: "all"}, afspec.TaskStatePending, []int{4, 2, 5}},
		"explicit ids come out in plan order":           {taskSpec{ids: []int{5, 1, 4}}, afspec.TaskStateInProgress, []int{4, 1, 5}},
		"an explicit id already in the target stays":    {taskSpec{ids: []int{4}}, afspec.TaskStateDone, []int{4}},
		"an explicit dropped id stays":                  {taskSpec{ids: []int{3}}, afspec.TaskStateDone, []int{3}},
	} {
		if got := resolveTasks(p, tc.wanted, tc.target); !slices.Equal(got, tc.want) {
			t.Errorf("%s: resolved %v; want %v", name, got, tc.want)
		}
	}
	finished := plan(planTask(1, "done", nil), planTask(2, "dropped", nil))
	if got := resolveTasks(finished, taskSpec{keyword: "all"}, afspec.TaskStateDone); len(got) != 0 {
		t.Errorf("all on a finished plan resolved %v; want nothing", got)
	}
}

// 11-REQ-3.1: the allowed moves are applied in plan order, each on the copy the
// one before returned, and the plan that was loaded is left as it was.
func TestAppliedMovesAreInPlanOrderOnACopy_11REQ3_1(t *testing.T) {
	p := plan(planTask(1, "pending", nil), planTask(2, "in_progress", nil), planTask(3, "pending", nil))
	got, refusal := planMoves("pkg", p, taskSpec{ids: []int{3, 1}}, afspec.TaskStateInProgress, false)
	if refusal != nil {
		t.Fatalf("refused: %+v", refusal)
	}
	if !slices.Equal(ids(got.moves), []int{1, 3}) {
		t.Errorf("moves %v; want 1 then 3", ids(got.moves))
	}
	for _, m := range got.moves {
		if m.from != afspec.TaskStatePending || m.to != afspec.TaskStateInProgress {
			t.Errorf("move %+v; want pending to in_progress", m)
		}
	}
	if got.after == nil || len(got.after.Tasks) != 3 {
		t.Fatalf("after = %+v; want the plan with the moves applied", got.after)
	}
	for i, want := range []afspec.TaskState{"in_progress", "in_progress", "in_progress"} {
		if got.after.Tasks[i].State != want {
			t.Errorf("after: task %d is %s; want %s", got.after.Tasks[i].Id, got.after.Tasks[i].State, want)
		}
	}
	if p.Tasks[0].State != afspec.TaskStatePending || p.Tasks[2].State != afspec.TaskStatePending {
		t.Error("planning changed the plan that was loaded")
	}
}

// 11-REQ-3.2: a refused move refuses the call; the message names each refused
// task with its state and the target and cites the format; the fix is the first
// refused task's targets.
func TestARefusedMoveRefusesTheCall_11REQ3_2(t *testing.T) {
	p := plan(planTask(1, "pending", nil), planTask(2, "in_progress", nil), planTask(3, "done", nil))
	_, refusal := planMoves("pkg", p, taskSpec{ids: []int{1, 2, 3}}, afspec.TaskStateDone, false)
	if refusal == nil || refusal.Stage != "task" || refusal.Category != "lifecycle" {
		t.Fatalf("refusal = %+v", refusal)
	}
	for _, want := range []string{"pkg", "§8.3.1",
		"task 1 is pending and cannot move to done (it may move to in_progress or dropped)",
		"task 3 is done and cannot move to done (it is already there; it may move to pending)"} {
		if !strings.Contains(refusal.Message, want) {
			t.Errorf("message %q does not say %q", refusal.Message, want)
		}
	}
	if strings.Contains(refusal.Message, "task 2 is") {
		t.Errorf("message %q names task 2, whose move is allowed", refusal.Message)
	}
	if refusal.FixHint == nil || refusal.FixHint.Flag != "--state" || !slices.Equal(refusal.FixHint.Valid, []string{"in_progress", "dropped"}) {
		t.Errorf("fix_hint = %+v", refusal.FixHint)
	}
}

// writable returns the call, run as the task form, against a package whose
// status or intent was changed by edit.
func refusedAtStatus(t *testing.T, root string, argv []string, want ...string) *cli.ErrorInfo {
	t.Helper()
	before := snapshot(t, root)
	env, code, _ := run(t, argv...)
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "status" || env.Error.Category != "lifecycle" || env.OK {
		t.Errorf("%v: code %d: %+v", argv, code, env.Error)
		return env.Error
	}
	if env.Error.FixHint != nil || env.Error.Retryable || env.Error.Resumable {
		t.Errorf("%v: fix_hint %+v retryable %v resumable %v", argv, env.Error.FixHint, env.Error.Retryable, env.Error.Resumable)
	}
	for _, w := range want {
		if !strings.Contains(env.Error.Message, w) {
			t.Errorf("%v: message %q does not say %q", argv, env.Error.Message, w)
		}
	}
	if !mapsEqual(before, snapshot(t, root)) {
		t.Errorf("%v: the repository changed", argv)
	}
	return env.Error
}

// TS-11-17: an explicit id already in the target state is judged by the
// machine, which refuses it: that is how a second agent learns a task is taken.
func TestAClaimOfATakenTaskIsRefused_TS11_17(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"), planTask(1, "in_progress", nil), planTask(2, "pending", []int{1}))
	before := snapshot(t, root)
	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress")
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "task" || env.Error.Category != "lifecycle" || env.OK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	if env.Error.FixHint == nil || !slices.Equal(env.Error.FixHint.Valid, []string{"pending", "done"}) {
		t.Errorf("fix_hint = %+v; want pending and done", env.Error.FixHint)
	}
	if !mapsEqual(before, snapshot(t, root)) {
		t.Error("the repository changed")
	}
}

// TS-11-19: a refused move refuses the whole call: every refused task is named,
// the fix is the first one's targets, and the allowed move is not made either.
func TestARefusedMoveRefusesTheWholeCall_TS11_19(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"), planTask(1, "pending", nil), planTask(2, "in_progress", []int{1}), planTask(3, "done", nil))
	before := snapshot(t, root)
	env, code, _ := run(t, "--dir", root, "09", "--task", "1,2,3", "--state", "done")
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "task" || env.Error.Category != "lifecycle" {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	for _, want := range []string{"09_agent_mode", "§8.3.1",
		"task 1 is pending and cannot move to done (it may move to in_progress or dropped)",
		"task 3 is done and cannot move to done (it is already there; it may move to pending)"} {
		if !strings.Contains(env.Error.Message, want) {
			t.Errorf("message %q does not say %q", env.Error.Message, want)
		}
	}
	if env.Error.FixHint == nil || env.Error.FixHint.Flag != "--state" || !slices.Equal(env.Error.FixHint.Valid, []string{"in_progress", "dropped"}) {
		t.Errorf("fix_hint = %+v", env.Error.FixHint)
	}
	if !mapsEqual(before, snapshot(t, root)) {
		t.Error("the repository changed; task 2 must stay in_progress")
	}
}

// TS-11-20: a dropped task has no move; the message names --force as the way
// out and there is no fix_hint to give.
func TestADroppedTaskHasNoMove_TS11_20(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"), planTask(1, "done", nil), planTask(2, "pending", []int{1}), planTask(3, "dropped", nil))
	env, code, _ := run(t, "--dir", root, "09", "--task", "3", "--state", "in_progress")
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "task" || env.Error.Category != "lifecycle" {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	if env.Error.FixHint != nil {
		t.Errorf("fix_hint = %+v; want none", env.Error.FixHint)
	}
	for _, want := range []string{"task 3 is dropped", "--force", "done or pending"} {
		if !strings.Contains(env.Error.Message, want) {
			t.Errorf("message %q does not say %q", env.Error.Message, want)
		}
	}
}

// TS-11-22: a sealed, superseded or archived package takes no change.
func TestAPackageThatCanNoLongerChangeIsRefused_TS11_22(t *testing.T) {
	for _, status := range []string{"sealed", "superseded", "archived"} {
		root := newRepo(t)
		setStatus(t, pkgDir(root, "09_agent_mode"), status)
		refusedAtStatus(t, root, []string{"--dir", root, "09", "--task", "1", "--state", "in_progress"},
			"09_agent_mode", status, "no longer change", "supersedes")
	}
}

// TS-11-23: an active package whose intent drifted, or whose body lost its
// ## Intent, is refused before any move is looked at, an unknown id included.
func TestAPackageWhoseIntentDriftedIsRefused_TS11_23(t *testing.T) {
	root := newRepo(t)
	edited := pkgDir(root, "09_agent_mode")
	activate(t, edited)
	editFile(t, filepath.Join(edited, "prd.md"), "so that an agent can parse results", "so that anyone can parse results")
	refusedAtStatus(t, root, []string{"--dir", root, "09", "--task", "99", "--state", "done"}, "09_agent_mode", "every write", "intent matches its hash again")

	lost := newRepo(t)
	noSection := pkgDir(lost, "09_agent_mode")
	activate(t, noSection)
	editFile(t, filepath.Join(noSection, "prd.md"), "## Intent", "## Purpose")
	refusedAtStatus(t, lost, []string{"--dir", lost, "09", "--task", "99", "--state", "done"}, "every write", "intent matches its hash again")
}

// TS-11-24: --dry-run and --force change none of these refusals.
func TestStatusRefusalsAreTheSameWithDryRunAndForce_TS11_24(t *testing.T) {
	sealed := newRepo(t)
	setStatus(t, pkgDir(sealed, "09_agent_mode"), "sealed")
	drifted := newRepo(t)
	activate(t, pkgDir(drifted, "09_agent_mode"))
	editFile(t, filepath.Join(pkgDir(drifted, "09_agent_mode"), "prd.md"), "so that an agent can parse results", "so that anyone can parse results")

	for name, root := range map[string]string{"sealed": sealed, "drifted": drifted} {
		base := []string{"--dir", root, "09", "--task", "1", "--state", "in_progress"}
		want := refusedAtStatus(t, root, base)
		for _, extra := range []string{"--dry-run", "--force"} {
			got := refusedAtStatus(t, root, append(slices.Clone(base), extra))
			if got != nil && want != nil && (got.Message != want.Message || got.Stage != want.Stage || got.Category != want.Category) {
				t.Errorf("%s %s: %+v; want the refusal without it, %+v", name, extra, got, want)
			}
		}
	}
}
