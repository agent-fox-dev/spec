package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// move is one task's move from the state it is in to the one --state names.
type move struct {
	id       int
	from, to afspec.TaskState
}

// movePlan is what a call would do: the moves, in plan order, and the plan with
// all of them applied. The plan that was loaded is not changed.
type movePlan struct {
	moves []move
	after *afspec.TasksV2Json
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
// target state: the state machine judges that move, and refuses it.
func resolveTasks(plan *afspec.TasksV2Json, wanted taskSpec, target afspec.TaskState) []int {
	if plan == nil {
		return nil
	}
	var out []int
	switch wanted.keyword {
	case "next":
		if t, ok := plan.NextTask(); ok {
			out = append(out, t.Id)
		}
	case "all":
		for _, t := range plan.Tasks {
			if t.State != target && t.State != afspec.TaskStateDropped {
				out = append(out, t.Id)
			}
		}
	default:
		for _, t := range plan.Tasks {
			if slices.Contains(wanted.ids, t.Id) {
				out = append(out, t.Id)
			}
		}
	}
	return out
}

// planMoves checks every move before it applies any. If the state machine
// refuses one, the whole call is refused and no move is applied. Otherwise the
// moves are applied in plan order with TransitionTask, each on the copy the
// previous one returned.
func planMoves(name string, plan *afspec.TasksV2Json, wanted taskSpec, target afspec.TaskState) (movePlan, *cli.ErrorInfo) {
	ids := resolveTasks(plan, wanted, target)
	moves := make([]move, len(ids))
	var refused []move
	for i, id := range ids {
		t, _ := plan.GetTask(id)
		moves[i] = move{id: id, from: t.State, to: target}
		if !afspec.ValidTaskTransition(t.State, target) {
			refused = append(refused, moves[i])
		}
	}
	if len(refused) > 0 {
		return movePlan{}, refuseMoves(name, refused)
	}

	after := plan
	for _, m := range moves {
		next, err := after.TransitionTask(m.id, string(m.to))
		if err != nil {
			// Every move passed ValidTaskTransition, which is TransitionTask's test.
			return movePlan{}, &cli.ErrorInfo{Stage: "task", Category: envelope.CategoryLifecycle, Message: fmt.Sprintf("%s: %v", name, err)}
		}
		after = next
	}
	return movePlan{moves: moves, after: after}, nil
}

// refuseMoves is the refusal of a call whose moves the state machine does not
// allow. It names every refused task, with its state, the target and where the
// task may go, and its fix_hint holds the targets of the first one.
func refuseMoves(name string, refused []move) *cli.ErrorInfo {
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
	msg := fmt.Sprintf("%s: the task state machine (format v2 §8.3.1) does not allow %s, so no task was changed: %s",
		name, noun, strings.Join(parts, "; "))

	var hint *cli.FixHint
	if allowed := allowedFrom(refused[0].from); len(allowed) > 0 {
		hint = &cli.FixHint{Flag: "--state", Valid: allowed}
	} else {
		msg += ". --force with done or pending is the only way out for a dropped task"
	}
	return &cli.ErrorInfo{Stage: "task", Category: envelope.CategoryLifecycle, Message: msg, FixHint: hint}
}
