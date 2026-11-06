package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// move is one task's move from the state it is in to the one --state names.
// forced says --force made it, through the library's bypass, because the state
// machine refuses it.
type move struct {
	id       int
	from, to afspec.TaskState
	forced   bool
}

// plannedWarning is a warning the call raises if it goes ahead. Planning only
// decides them: a call that is refused, or whose write fails, raises none, since
// they speak of moves that were not made.
type plannedWarning struct {
	code     cli.WarnCode
	severity string
	message  string
}

// movePlan is what a call would do: the moves, in plan order, and the progress
// with all of them applied. The package that was loaded is not changed.
type movePlan struct {
	moves    []move
	after    *afspec.ProgressV3Json
	warnings []plannedWarning
}

// allowedFrom lists the states a task in state from may move to, in the order
// of taskStates. The library exports the test of a move, not its table, so
// each of the four states is tried.
func allowedFrom(from afspec.TaskState) []string {
	var out []string
	for _, s := range taskStates {
		if afspec.ValidTaskTransition(from, afspec.TaskState(s)) {
			out = append(out, s)
		}
	}
	return out
}

// resolveTasks turns what --task named into the ids of the tasks to move, in
// plan order whatever order they were named in. next is the task NextTask
// names, the one the report calls next_task. all is every task except those
// already in the target state and those dropped, which only a named id moves.
// An id that is named is always moved, even when the task is already in the
// target state: the state machine judges that move, and refuses it. Each id is
// in the result once: a plan that has a task id twice does not validate, but
// the library addresses a task by its id and finds the first. The plan is the
// package's, the derived integration task included.
func resolveTasks(spec *afspec.Spec, wanted taskSpec, target afspec.TaskState) []int {
	plan := spec.Plan()
	var out []int
	switch wanted.keyword {
	case "next":
		if t, ok := spec.NextTask(); ok {
			out = append(out, t.Id)
		}
	case "all":
		for _, t := range plan {
			if t.State != target && t.State != afspec.TaskStateDropped {
				out = append(out, t.Id)
			}
		}
	default:
		for _, t := range plan {
			if slices.Contains(wanted.ids, t.Id) {
				out = append(out, t.Id)
			}
		}
	}
	var once []int
	for _, id := range out {
		if !slices.Contains(once, id) {
			once = append(once, id)
		}
	}
	return once
}

// planMoves checks every move before it applies any. If the state machine
// refuses one, and --force does not cover it, the whole call is refused and no
// move is applied. Otherwise the moves are applied in plan order, each on the
// progress the previous one returned: with TransitionTask where the machine
// allows the move, and where it does not and --force is given with the target
// done or pending, with the library's own bypass, CompleteTaskStates or
// ResetTaskStates. The library has no setter for in_progress or dropped, so
// --force covers nothing there. A task already in the target state is no move
// under --force with done or pending, and is a move the machine refuses
// otherwise.
func planMoves(name string, spec *afspec.Spec, wanted taskSpec, target afspec.TaskState, force bool) (movePlan, *cli.ErrorInfo) {
	bypass := force && (target == afspec.TaskStateDone || target == afspec.TaskStatePending)
	var moves, refused []move
	for _, id := range resolveTasks(spec, wanted, target) {
		t, _ := spec.GetTask(id)
		m := move{id: id, from: t.State, to: target}
		switch {
		case afspec.ValidTaskTransition(t.State, target):
			moves = append(moves, m)
		case bypass && t.State == target:
			// Already there: nothing to set, and nothing to say.
		case bypass:
			m.forced = true
			moves = append(moves, m)
		default:
			refused = append(refused, m)
		}
	}
	if len(refused) > 0 {
		return movePlan{}, refuseMoves(name, refused, force && !bypass)
	}

	// The library's moves return the progress to write and leave the package
	// alone, so each move is made on a copy that holds the progress of the
	// one before it.
	work := *spec
	var warnings []plannedWarning
	for _, m := range moves {
		switch {
		case m.forced && m.to == afspec.TaskStateDone:
			work.Progress = work.CompleteTaskStates([]int{m.id})
		case m.forced:
			work.Progress = work.ResetTaskStates([]int{m.id})
		default:
			next, err := work.TransitionTask(m.id, string(m.to))
			if err != nil {
				// The move passed ValidTaskTransition, which is TransitionTask's test.
				return movePlan{}, &cli.ErrorInfo{Stage: "task", Category: envelope.CategoryLifecycle, Message: fmt.Sprintf("%s: %v", name, err)}
			}
			work.Progress = next
		}
		switch {
		case m.forced:
			warnings = append(warnings, plannedWarning{cli.WarnTaskStateForced, "high", fmt.Sprintf(
				"task %d was set from %s to %s through the library's bypass: the task state machine (format v3 §8.1) does not allow that move",
				m.id, m.from, m.to)})
		case m.to == afspec.TaskStateInProgress && !spec.IsReady(m.id):
			warnings = append(warnings, plannedWarning{cli.WarnTaskNotReady, "low", fmt.Sprintf(
				"task %d moves to in_progress though it is not ready: %s", m.id, openDependencies(spec, m.id))})
		}
	}
	return movePlan{moves: moves, after: work.Progress, warnings: warnings}, nil
}

// openDependencies says which of a task's dependencies are not done, judged on
// the package's plan: the ids in its depends_on, or, where it has none stated,
// the task before it; for the integration task, every task not done.
func openDependencies(spec *afspec.Spec, id int) string {
	plan := spec.Plan()
	index := func(id int) int { return slices.IndexFunc(plan, func(t afspec.PlannedTask) bool { return t.Id == id }) }
	i := index(id)
	var open []int
	switch deps := plan[i].DependsOn; {
	case plan[i].Kind == afspec.TaskKindIntegration:
		for _, t := range plan[:i] {
			if t.State != afspec.TaskStateDone && t.State != afspec.TaskStateDropped {
				open = append(open, t.Id)
			}
		}
	case deps == nil:
		if i > 0 && plan[i-1].State != afspec.TaskStateDone {
			open = append(open, plan[i-1].Id)
		}
	default:
		for _, d := range deps {
			if j := index(d); j < 0 || plan[j].State != afspec.TaskStateDone {
				open = append(open, d)
			}
		}
	}
	ids := make([]string, len(open))
	for k, d := range open {
		ids[k] = strconv.Itoa(d)
	}
	if len(open) == 1 {
		return "task " + ids[0] + " is not done"
	}
	return "tasks " + strings.Join(ids, ", ") + " are not done"
}

// refuseMoves is the refusal of a call whose moves the state machine does not
// allow. It names every refused task, with its state, the target and where the
// task may go, and its fix_hint holds the targets of the first one.
func refuseMoves(name string, refused []move, forceInert bool) *cli.ErrorInfo {
	parts := make([]string, len(refused))
	for i, m := range refused {
		allowed := allowedFrom(m.from)
		s := fmt.Sprintf("task %d is %s and cannot move to %s", m.id, m.from, m.to)
		switch {
		case len(allowed) == 0:
			s += " (a dropped task has no move)"
		case m.from == m.to:
			s += " (it is already there; it may move to " + strings.Join(allowed, " or ") + ")"
		default:
			s += " (it may move to " + strings.Join(allowed, " or ") + ")"
		}
		parts[i] = s
	}
	noun := "this move"
	if len(refused) > 1 {
		noun = "these moves"
	}
	msg := fmt.Sprintf("%s: the task state machine (format v3 §8.1) does not allow %s, so no task was changed: %s",
		name, noun, strings.Join(parts, "; "))

	if forceInert {
		msg += ". --force cannot set in_progress or dropped: the library has no setter for those states, so it covers none of these moves"
	}
	var hint *cli.FixHint
	if allowed := allowedFrom(refused[0].from); len(allowed) > 0 {
		hint = &cli.FixHint{Flag: "--state", Valid: allowed}
	} else {
		msg += ". --force with done or pending is the only way out for a dropped task"
	}
	return &cli.ErrorInfo{Stage: "task", Category: envelope.CategoryLifecycle, Message: msg, FixHint: hint}
}
