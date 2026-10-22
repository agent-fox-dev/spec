package afspec

import (
	"fmt"
	"slices"
)

// taskTransitions is the task state machine of format v2 §8.3.1:
//
//	pending ──→ in_progress ──→ done
//	   │             │            │
//	   │             └──→ pending ┘   (released, or reopened upstream)
//	   └──→ dropped
//
// The v1 states `queued` and `pending_reevaluation` are gone; a runtime that
// needs them keeps that state in its own store.
var taskTransitions = map[TaskState][]TaskState{
	TaskStatePending:    {TaskStateInProgress, TaskStateDropped},
	TaskStateInProgress: {TaskStateDone, TaskStatePending},
	TaskStateDone:       {TaskStatePending},
	TaskStateDropped:    {},
}

// ValidTaskTransition reports whether current → target is allowed.
func ValidTaskTransition(current, target TaskState) bool {
	for _, allowed := range taskTransitions[current] {
		if allowed == target {
			return true
		}
	}
	return false
}

// GetTask returns a pointer to a copy of the task with the given ID.
func (t *TasksV2Json) GetTask(id int) (*Task, bool) {
	for i := range t.Tasks {
		if t.Tasks[i].Id == id {
			task := t.Tasks[i]
			return &task, true
		}
	}
	return nil, false
}

// IsReady reports whether the task with the given id can be started now: it
// is in the plan, it is pending, and everything it depends on is done. A
// task's dependencies are its depends_on when the field is present, where an
// empty list means none; when the field is absent, the task before it in plan
// order, and the first task then has none (format v2 §8.3). A dependency the
// plan does not have, or one that is dropped, is not done. An unknown id and a
// nil receiver are not ready.
func (t *TasksV2Json) IsReady(id int) bool {
	if t == nil {
		return false
	}
	for i := range t.Tasks {
		if t.Tasks[i].Id == id {
			return t.ready(i)
		}
	}
	return false
}

// NextTask returns a copy of the task to work on now: the first in_progress
// task in plan order, else the first task for which IsReady is true. It
// returns nil and false when there is none, which a nil receiver and an empty
// plan are.
func (t *TasksV2Json) NextTask() (*Task, bool) {
	if t == nil {
		return nil, false
	}
	for i := range t.Tasks {
		if t.Tasks[i].State == TaskStateInProgress {
			task := t.Tasks[i].clone()
			return &task, true
		}
	}
	for i := range t.Tasks {
		if t.ready(i) {
			task := t.Tasks[i].clone()
			return &task, true
		}
	}
	return nil, false
}

// ready is IsReady for the task at position i of the plan.
func (t *TasksV2Json) ready(i int) bool {
	task := t.Tasks[i]
	if task.State != TaskStatePending {
		return false
	}
	if task.DependsOn == nil {
		return i == 0 || t.Tasks[i-1].State == TaskStateDone
	}
	for _, dep := range task.DependsOn {
		j := slices.IndexFunc(t.Tasks, func(other Task) bool { return other.Id == dep })
		if j < 0 || t.Tasks[j].State != TaskStateDone {
			return false
		}
	}
	return true
}

// TransitionTask moves one task to the target state and returns a new Tasks
// copy; the receiver is not modified. It returns a LifecycleError when the
// task does not exist or the transition is not allowed by §8.3.1.
func (t *TasksV2Json) TransitionTask(taskID int, target string) (*TasksV2Json, error) {
	targetState := TaskState(target)

	if _, ok := taskTransitions[targetState]; !ok {
		msg := fmt.Sprintf("unknown task state %q", target)
		return nil, &LifecycleError{Msg: msg, Err: &SpecError{Msg: msg}}
	}

	current, found := TaskState(""), false
	for _, task := range t.Tasks {
		if task.Id == taskID {
			current, found = task.State, true
			break
		}
	}
	if !found {
		msg := fmt.Sprintf("task %d not found", taskID)
		return nil, &LifecycleError{Msg: msg, Err: &SpecError{Msg: msg}}
	}

	if !ValidTaskTransition(current, targetState) {
		msg := fmt.Sprintf("transition from %s to %s is not allowed for task %d", current, target, taskID)
		return nil, &LifecycleError{
			Msg: msg,
			Err: &SpecError{Msg: fmt.Sprintf("transition from %s to %s is not allowed", current, target)},
		}
	}

	newTasks := t.deepCopy()
	for i := range newTasks.Tasks {
		if newTasks.Tasks[i].Id == taskID {
			newTasks.Tasks[i].State = targetState
			break
		}
	}
	return newTasks, nil
}

// CompleteTaskStates sets the named tasks to done, bypassing the state
// machine. Unknown IDs are skipped. Returns a new copy.
func (t *TasksV2Json) CompleteTaskStates(taskIDs []int) *TasksV2Json {
	return t.setStates(taskIDs, TaskStateDone)
}

// ResetTaskStates sets the named tasks to pending, bypassing the state
// machine. Unknown IDs are skipped. Returns a new copy.
func (t *TasksV2Json) ResetTaskStates(taskIDs []int) *TasksV2Json {
	return t.setStates(taskIDs, TaskStatePending)
}

func (t *TasksV2Json) setStates(taskIDs []int, state TaskState) *TasksV2Json {
	wanted := make(map[int]bool, len(taskIDs))
	for _, id := range taskIDs {
		wanted[id] = true
	}
	newTasks := t.deepCopy()
	for i := range newTasks.Tasks {
		if wanted[newTasks.Tasks[i].Id] {
			newTasks.Tasks[i].State = state
		}
	}
	return newTasks
}

// IsOptional reports whether the task is marked optional (default false).
func (t Task) IsOptional() bool {
	return t.Optional != nil && *t.Optional
}

// clone returns a copy of the task whose slices and pointer are independent of
// the receiver's, preserving the distinction between a nil slice (field
// absent) and an empty one (field present but empty).
func (t Task) clone() Task {
	t.Criteria = copyStrings(t.Criteria)
	t.Tests = copyStrings(t.Tests)
	t.Steps = copyStrings(t.Steps)
	t.Touches = copyStrings(t.Touches)
	t.DoneWhen = copyStrings(t.DoneWhen)
	if t.DependsOn != nil {
		deps := make([]int, len(t.DependsOn))
		copy(deps, t.DependsOn)
		t.DependsOn = deps
	}
	if t.Optional != nil {
		opt := *t.Optional
		t.Optional = &opt
	}
	return t
}

// deepCopy returns a copy of the artifact whose slices are independent of the
// receiver's, so that mutations on the copy cannot alias the original.
func (t *TasksV2Json) deepCopy() *TasksV2Json {
	newTasks := *t

	newTasks.Tasks = make([]Task, len(t.Tasks))
	for i, task := range t.Tasks {
		newTasks.Tasks[i] = task.clone()
	}

	if t.Dependencies != nil {
		newTasks.Dependencies = make([]Dependency, len(t.Dependencies))
		copy(newTasks.Dependencies, t.Dependencies)
	}

	if t.TestCommands.SpecTests != nil {
		st := *t.TestCommands.SpecTests
		newTasks.TestCommands.SpecTests = &st
	}

	return &newTasks
}

// copyStrings returns an independent copy of a string slice, preserving the
// distinction between nil (field absent) and empty (field present but empty).
func copyStrings(src []string) []string {
	if src == nil {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}
