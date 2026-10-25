package main

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// next and all are resolved against the plan, an explicit id is moved as named,
// and the ids come out in plan order.
func TestResolveTasks(t *testing.T) {
	// Plan order is not id order: 4 comes first.
	p := plan(
		planTask(4, "done", nil), planTask(2, "in_progress", []int{4}), planTask(3, "dropped", nil),
		planTask(1, "pending", []int{2}), planTask(5, "in_progress", nil),
	)
	finished := plan(planTask(1, "done", nil), planTask(2, "dropped", nil))
	for name, tc := range map[string]struct {
		plan   *afspec.TasksV2Json
		wanted taskSpec
		target afspec.TaskState
		want   []int
	}{
		"next is NextTask: the first task in progress": {p, taskSpec{keyword: "next"}, afspec.TaskStateDone, []int{2}},
		// Task 1 is pending, not done and not dropped: all names it, and the machine then refuses it.
		"all skips the target state and dropped":        {p, taskSpec{keyword: "all"}, afspec.TaskStateDone, []int{2, 1, 5}},
		"all to pending skips only pending and dropped": {p, taskSpec{keyword: "all"}, afspec.TaskStatePending, []int{4, 2, 5}},
		"explicit ids come out in plan order":           {p, taskSpec{ids: []int{5, 1, 4}}, afspec.TaskStateInProgress, []int{4, 1, 5}},
		"an explicit id already in the target stays":    {p, taskSpec{ids: []int{4}}, afspec.TaskStateDone, []int{4}},
		"an explicit dropped id stays":                  {p, taskSpec{ids: []int{3}}, afspec.TaskStateDone, []int{3}},
		"all on a finished plan":                        {finished, taskSpec{keyword: "all"}, afspec.TaskStateDone, nil},
		"no plan":                                       {nil, taskSpec{keyword: "all"}, afspec.TaskStateDone, nil},
	} {
		if got := resolveTasks(tc.plan, tc.wanted, tc.target); !slices.Equal(got, tc.want) {
			t.Errorf("%s: resolved %v; want %v", name, got, tc.want)
		}
	}
}

// The allowed moves are applied in plan order, each on the copy the one before
// returned, and the plan that was loaded is left as it was.
func TestPlanMoves_AppliesTheMovesInPlanOrderOnACopy(t *testing.T) {
	p := plan(planTask(1, "pending", nil), planTask(2, "in_progress", nil), planTask(3, "pending", nil))
	got, refusal := planMoves("pkg", p, taskSpec{ids: []int{3, 1}}, afspec.TaskStateInProgress, false)
	if refusal != nil {
		t.Fatalf("refused: %+v", refusal)
	}
	if !slices.Equal(ids(got.moves), []int{1, 3}) {
		t.Errorf("moves %v; want 1 then 3", ids(got.moves))
	}
	for _, m := range got.moves {
		if m.from != afspec.TaskStatePending || m.to != afspec.TaskStateInProgress || m.forced {
			t.Errorf("move %+v; want pending to in_progress", m)
		}
	}
	if got.after == nil || len(got.after.Tasks) != 3 {
		t.Fatalf("after = %+v; want the plan with the moves applied", got.after)
	}
	for _, task := range got.after.Tasks {
		if task.State != afspec.TaskStateInProgress {
			t.Errorf("after: task %d is %s; want in_progress", task.Id, task.State)
		}
	}
	if p.Tasks[0].State != afspec.TaskStatePending || p.Tasks[2].State != afspec.TaskStatePending {
		t.Error("planning changed the plan that was loaded")
	}
}

// A move the machine refuses refuses the call. The message names each refused
// task with its state, the target and where it may go, and cites the format;
// the fix is the first refused task's targets, and a dropped task, which has
// none, is told --force is its way out. --force does not cover in_progress or
// dropped, and the message says so.
func TestPlanMoves_RefusesAMoveTheMachineRefuses(t *testing.T) {
	for name, tc := range map[string]struct {
		plan   *afspec.TasksV2Json
		ids    []int
		target afspec.TaskState
		force  bool
		want   []string
		notSay []string
		hint   []string
	}{
		"several, one allowed": {plan(planTask(1, "pending", nil), planTask(2, "in_progress", nil), planTask(3, "done", nil)), []int{1, 2, 3}, afspec.TaskStateDone, false,
			[]string{"pkg", "§8.3.1", "these moves",
				"task 1 is pending and cannot move to done (it may move to in_progress or dropped)",
				"task 3 is done and cannot move to done (it is already there; it may move to pending)"},
			[]string{"task 2 is", "--force"}, []string{"in_progress", "dropped"}},
		"a claim of a task that is taken": {plan(planTask(1, "in_progress", nil)), []int{1}, afspec.TaskStateInProgress, false,
			[]string{"this move", "task 1 is in_progress and cannot move to in_progress (it is already there"}, nil, []string{"pending", "done"}},
		"a dropped task": {plan(planTask(1, "done", nil), planTask(3, "dropped", nil)), []int{3}, afspec.TaskStateInProgress, false,
			[]string{"task 3 is dropped", "(a dropped task has no move)", "--force with done or pending is the only way out"}, nil, nil},
		"--force to in_progress": {plan(planTask(1, "done", nil)), []int{1}, afspec.TaskStateInProgress, true,
			[]string{"--force cannot set in_progress or dropped"}, nil, []string{"pending"}},
		"--force to dropped": {plan(planTask(2, "dropped", nil)), []int{2}, afspec.TaskStateDropped, true,
			[]string{"--force cannot set in_progress or dropped", "the only way out"}, nil, nil},
	} {
		t.Run(name, func(t *testing.T) {
			got, refusal := planMoves("pkg", tc.plan, taskSpec{ids: tc.ids}, tc.target, tc.force)
			if refusal == nil || refusal.Stage != "task" || refusal.Category != "lifecycle" || got.after != nil {
				t.Fatalf("refusal = %+v, plan %+v", refusal, got)
			}
			saysAll(t, nil, refusal.Message, tc.want...)
			for _, w := range tc.notSay {
				if strings.Contains(refusal.Message, w) {
					t.Errorf("message %q says %q", refusal.Message, w)
				}
			}
			switch h := refusal.FixHint; {
			case tc.hint == nil && h != nil:
				t.Errorf("fix_hint = %+v; want none", h)
			case tc.hint != nil && (h == nil || h.Flag != "--state" || !slices.Equal(h.Valid, tc.hint)):
				t.Errorf("fix_hint = %+v; want --state with %v", h, tc.hint)
			}
		})
	}
}

// A claim of a task that is not ready goes ahead, with a low warning naming the
// dependencies that are not done, judged on the plan before the call: the ids
// in depends_on, or the task before it where it states none.
func TestPlanMoves_WarnsOfAClaimOfATaskThatIsNotReady(t *testing.T) {
	for name, tc := range map[string]struct {
		plan *afspec.TasksV2Json
		ids  []int
		want []string
	}{
		"a stated dependency":  {plan(planTask(1, "pending", nil), planTask(2, "pending", []int{1})), []int{2}, []string{"task 2 moves to in_progress though it is not ready: task 1 is not done"}},
		"the task before":      {plan(planTask(1, "pending", nil), planTask(2, "pending", nil)), []int{2}, []string{"task 2 moves to in_progress though it is not ready: task 1 is not done"}},
		"claimed together":     {plan(planTask(1, "pending", nil), planTask(2, "pending", []int{1})), []int{1, 2}, []string{"task 2 moves to in_progress though it is not ready: task 1 is not done"}},
		"several dependencies": {plan(planTask(1, "pending", nil), planTask(2, "pending", []int{}), planTask(3, "pending", []int{1, 2})), []int{3}, []string{"task 3 moves to in_progress though it is not ready: tasks 1, 2 are not done"}},
		"one not in the plan":  {plan(planTask(1, "done", nil), planTask(2, "pending", []int{1, 99})), []int{2}, []string{"task 2 moves to in_progress though it is not ready: task 99 is not done"}},
		"a ready task":         {plan(planTask(1, "pending", nil), planTask(2, "pending", []int{1})), []int{1}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			got, refusal := planMoves("pkg", tc.plan, taskSpec{ids: tc.ids}, afspec.TaskStateInProgress, false)
			if refusal != nil {
				t.Fatalf("refused: %+v", refusal)
			}
			var messages []string
			for _, w := range got.warnings {
				if w.code != cli.WarnTaskNotReady || w.severity != "low" {
					t.Errorf("warning %+v; want a low task_not_ready", w)
				}
				messages = append(messages, w.message)
			}
			if !slices.Equal(messages, tc.want) {
				t.Errorf("warnings %q; want %q", messages, tc.want)
			}
		})
	}
}

// The warning is raised by a call that goes ahead, and the claim is written.
func TestTask_ClaimsATaskThatIsNotReadyWithAWarning(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	setTasks(t, dir, planTask(1, "pending", nil), planTask(2, "pending", []int{1}))
	env, code, _ := run(t, "--dir", root, "09", "--task", "2", "--state", "in_progress")
	if code != cli.ExitOK || onDisk(t, dir)[2] != "in_progress" {
		t.Fatalf("code %d: %+v; on disk %v", code, env.Error, onDisk(t, dir))
	}
	if w := oneWarning(t, env, "task_not_ready", "low"); w.Stage != "task" || !strings.Contains(w.Message, "task 1") {
		t.Errorf("warning %+v; want one at stage task naming task 1", w)
	}
}

// --force makes a move the machine refuses with the library's bypass, and a
// move it allows with the machine. Each task set through a bypass has a forced
// change and a high warning of its own, which the summary counts; a task already
// in the target state has neither.
func TestTask_ForceUsesTheBypassesOnlyForRefusedMoves(t *testing.T) {
	for name, tc := range map[string]struct {
		tasks   []map[string]any
		ids     string
		target  string
		forced  []int
		plain   []int
		summary string
	}{
		"reset to pending": {[]map[string]any{planTask(1, "pending", nil), planTask(2, "dropped", nil), planTask(3, "dropped", nil)}, "1,2,3", "pending",
			[]int{2, 3}, nil, "speca: 09_agent_mode tasks 2, 3 → pending; 2 high-severity warnings"},
		"complete": {[]map[string]any{planTask(3, "pending", nil), planTask(4, "in_progress", []int{3})}, "3,4", "done",
			[]int{3}, []int{4}, "speca: 09_agent_mode tasks 3, 4 → done; 1 high-severity warning"},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			dir := pkgDir(root, "09_agent_mode")
			setTasks(t, dir, tc.tasks...)
			env, code, _ := run(t, "--dir", root, "09", "--task", tc.ids, "--state", tc.target, "--force")
			r := result(t, env)
			if code != cli.ExitOK {
				t.Fatalf("code %d: %+v", code, env.Error)
			}
			for id, state := range onDisk(t, dir) {
				if state != tc.target {
					t.Errorf("task %d is %s on disk; want %s", id, state, tc.target)
				}
			}
			if want := append(slices.Clone(tc.forced), tc.plain...); !slices.Equal(changed(r), want) {
				t.Errorf("changes %+v; want %v, in plan order", r.Changes, want)
			}
			ws := warningsOf(env, "task_state_forced")
			if len(ws) != len(tc.forced) {
				t.Fatalf("warnings %+v; want one for each of %v", env.Warnings, tc.forced)
			}
			for i, id := range tc.forced {
				c := changeFor(r, id)
				if c == nil || !c.Forced || c.To != tc.target {
					t.Errorf("change of task %d = %+v; want forced", id, c)
					continue
				}
				if w := ws[i]; w.Severity != "high" || w.Stage != "task" || !strings.Contains(w.Message, "task "+strconv.Itoa(id)) ||
					!strings.Contains(w.Message, c.From) || !strings.Contains(w.Message, tc.target) {
					t.Errorf("warning %+v; want a high one at stage task naming task %d, %s and %s", w, id, c.From, tc.target)
				}
			}
			for _, id := range tc.plain {
				if c := changeFor(r, id); c == nil || c.Forced {
					t.Errorf("change of task %d = %+v; want an ordinary move", id, c)
				}
			}
			if env.Summary != tc.summary {
				t.Errorf("summary %q; want %q", env.Summary, tc.summary)
			}
		})
	}
}

// --force on a task already in the target state changes nothing, writes
// nothing and says nothing.
func TestTask_ForceOnATaskAlreadyThereIsQuiet(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"), planTask(1, "done", nil))
	before := snapshot(t, root)
	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "done", "--force")
	if code != cli.ExitOK || len(result(t, env).Changes) != 0 || len(env.Warnings) != 0 {
		t.Errorf("code %d: changes %+v warnings %v", code, result(t, env).Changes, warningCodes(env))
	}
	if !maps.Equal(before, snapshot(t, root)) {
		t.Error("the repository changed")
	}
}
