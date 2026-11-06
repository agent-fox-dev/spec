package afspec

import (
	"fmt"
	"slices"
	"strings"
)

// TaskState is the state of a task, kept in progress.json (format v3 §8.3).
type TaskState = TaskProgressState

// The four task states.
const (
	TaskStatePending    = TaskProgressStatePending
	TaskStateInProgress = TaskProgressStateInProgress
	TaskStateDone       = TaskProgressStateDone
	TaskStateDropped    = TaskProgressStateDropped
)

// TaskKind tells a task of the plan from the derived integration task.
type TaskKind string

const (
	// TaskKindImplement is every task stored in tasks.json.
	TaskKindImplement TaskKind = "implement"
	// TaskKindIntegration is the derived last task of a full package
	// (format v3 §8.4).
	TaskKindIntegration TaskKind = "integration"
)

// IntegrationTaskTitle is the title of the derived integration task.
const IntegrationTaskTitle = "Verify every execution path end to end"

// PlannedTask is one task as the package's plan has it: a task of tasks.json,
// or the derived integration task, with its kind and its state from the
// progress.
type PlannedTask struct {
	Task
	Kind  TaskKind
	State TaskState
}

// taskTransitions is the task state machine of format v3 §8.1:
//
//	pending ──→ in_progress ──→ done
//	   │             │            │
//	   │             └──→ pending ┘   (released, or reopened upstream)
//	   └──→ dropped
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

// IntegrationTaskID is the id of the package's integration task: one past
// the highest task id of the plan. It is false for a lite package and for a
// package with no task (format v3 §8.4).
func (s *Spec) IntegrationTaskID() (int, bool) {
	if s == nil || s.IsLite() || s.Tasks == nil || len(s.Tasks.Tasks) == 0 {
		return 0, false
	}
	return NextTaskID(*s.Tasks), true
}

// IntegrationTask is the derived integration task of a full package: task
// N+1, depending on every task of the plan, touching what every task
// touches, with the steps of format v3 §8.4 computed from the paths, the
// contracts, the touches and the dependencies. It is false for a lite package
// and for a package with no task.
func (s *Spec) IntegrationTask() (PlannedTask, bool) {
	id, ok := s.IntegrationTaskID()
	if !ok {
		return PlannedTask{}, false
	}
	t := Task{Id: id, Title: IntegrationTaskTitle, Criteria: []string{}, DependsOn: []int{}}
	for _, task := range s.Tasks.Tasks {
		t.DependsOn = append(t.DependsOn, task.Id)
		for _, p := range task.Touches {
			if !slices.Contains(t.Touches, p) {
				t.Touches = append(t.Touches, p)
			}
		}
	}
	var paths []ExecutionPath
	var contracts []string
	if s.Requirements != nil {
		paths = s.Requirements.ExecutionPaths
		for _, r := range s.Requirements.Requirements {
			for _, c := range r.Criteria {
				if c.ContractText() != "" {
					contracts = append(contracts, c.Id)
				}
			}
		}
	}
	for _, p := range paths {
		t.Steps = append(t.Steps, fmt.Sprintf("Run the smoke check of %s (%s) with %s unmocked and confirm its end state: %s",
			p.Id, p.Title, strings.Join(p.Smoke.RealComponents, ", "), strings.Join(p.Smoke.EndState, "; ")))
		t.Steps = append(t.Steps, fmt.Sprintf("Trace %s through the real code; confirm each step calls the next and no stub remains", p.Id))
	}
	if len(contracts) > 0 {
		t.Steps = append(t.Steps, fmt.Sprintf("For every criterion with a contract (%s), confirm the producer's result is consumed by a caller in production code, not only by tests",
			strings.Join(contracts, ", ")))
	}
	if len(t.Touches) > 0 {
		t.Steps = append(t.Steps, "Search every path in touches for stub markers (the project's own, then TODO, NotImplementedError, todo!(), panic(\"not implemented\"), a bare return of nothing on a non-trivial path) and remove any that remain")
	}
	if s.Tasks != nil && len(s.Tasks.Dependencies) > 0 {
		var deps []string
		for _, d := range s.Tasks.Dependencies {
			deps = append(deps, d.Spec)
		}
		t.Steps = append(t.Steps, fmt.Sprintf("For every path whose entry point belongs to a spec this one depends on (%s), confirm the entry point is called from production code",
			strings.Join(deps, ", ")))
	}
	if s.Tasks != nil {
		for _, cmd := range []string{s.Tasks.TestCommands.AllTests, s.Tasks.TestCommands.Linter} {
			if cmd != "" {
				t.DoneWhen = append(t.DoneWhen, fmt.Sprintf("`%s` passes", cmd))
			}
		}
	}
	return PlannedTask{Task: t, Kind: TaskKindIntegration, State: s.TaskState(id)}, true
}

// Plan is every task of the package in id order: the tasks of tasks.json,
// each with kind implement, then the integration task when the package has
// one, each with its state from the progress. A nil receiver and a package
// with no tasks have an empty plan.
func (s *Spec) Plan() []PlannedTask {
	if s == nil || s.Tasks == nil {
		return nil
	}
	out := make([]PlannedTask, 0, len(s.Tasks.Tasks)+1)
	for _, t := range s.Tasks.Tasks {
		out = append(out, PlannedTask{Task: t.clone(), Kind: TaskKindImplement, State: s.TaskState(t.Id)})
	}
	if integration, ok := s.IntegrationTask(); ok {
		out = append(out, integration)
	}
	return out
}

// TaskState is the state of the task with the given id: its entry in the
// progress, or pending when it has none (format v3 §8.3). The id is not
// checked against the plan.
func (s *Spec) TaskState(id int) TaskState {
	if s == nil || s.Progress == nil {
		return TaskStatePending
	}
	for _, e := range s.Progress.Tasks {
		if e.Id == id {
			return e.State
		}
	}
	return TaskStatePending
}

// GetTask returns a copy of the planned task with the given id, the
// integration task included.
func (s *Spec) GetTask(id int) (*PlannedTask, bool) {
	for _, t := range s.Plan() {
		if t.Id == id {
			return &t, true
		}
	}
	return nil, false
}

// IsReady reports whether the task with the given id can be started now: it
// is in the plan, it is pending, and everything it depends on is done. A
// task's dependencies are its depends_on when the field is present, where an
// empty list means none; when the field is absent, the task before it in plan
// order, and the first task then has none (format v3 §8.2). The integration
// task is ready when every task of the plan is done or dropped. A dependency
// the plan does not have, or one that is dropped, is not done. An unknown id
// and a nil receiver are not ready.
func (s *Spec) IsReady(id int) bool {
	plan := s.Plan()
	for i := range plan {
		if plan[i].Id == id {
			return s.ready(plan, i)
		}
	}
	return false
}

// NextTask returns a copy of the task to work on now: the first in_progress
// task in plan order, else the first task for which IsReady is true. It
// returns nil and false when there is none, which a nil receiver and an empty
// plan are.
func (s *Spec) NextTask() (*PlannedTask, bool) {
	plan := s.Plan()
	for i := range plan {
		if plan[i].State == TaskStateInProgress {
			return &plan[i], true
		}
	}
	for i := range plan {
		if s.ready(plan, i) {
			return &plan[i], true
		}
	}
	return nil, false
}

// ready is IsReady for the task at position i of plan.
func (s *Spec) ready(plan []PlannedTask, i int) bool {
	task := plan[i]
	if task.State != TaskStatePending {
		return false
	}
	if task.Kind == TaskKindIntegration {
		for _, other := range plan[:i] {
			if other.State != TaskStateDone && other.State != TaskStateDropped {
				return false
			}
		}
		return true
	}
	if task.DependsOn == nil {
		return i == 0 || plan[i-1].State == TaskStateDone
	}
	for _, dep := range task.DependsOn {
		j := slices.IndexFunc(plan, func(other PlannedTask) bool { return other.Id == dep })
		if j < 0 || plan[j].State != TaskStateDone {
			return false
		}
	}
	return true
}

// TransitionTask moves one task to the target state and returns the progress
// to write; the receiver is not modified. It returns a LifecycleError when
// the task is not in the plan or the transition is not allowed by §8.1.
func (s *Spec) TransitionTask(taskID int, target string) (*ProgressV3Json, error) {
	targetState := TaskState(target)

	if _, ok := taskTransitions[targetState]; !ok {
		msg := fmt.Sprintf("unknown task state %q", target)
		return nil, &LifecycleError{Msg: msg, Err: &SpecError{Msg: msg}}
	}

	task, found := s.GetTask(taskID)
	if !found {
		msg := fmt.Sprintf("task %d not found", taskID)
		return nil, &LifecycleError{Msg: msg, Err: &SpecError{Msg: msg}}
	}

	if !ValidTaskTransition(task.State, targetState) {
		msg := fmt.Sprintf("transition from %s to %s is not allowed for task %d", task.State, target, taskID)
		return nil, &LifecycleError{
			Msg: msg,
			Err: &SpecError{Msg: fmt.Sprintf("transition from %s to %s is not allowed", task.State, target)},
		}
	}
	return s.setStates([]int{taskID}, targetState), nil
}

// CompleteTaskStates sets the named tasks to done, bypassing the state
// machine, and returns the progress to write. Unknown ids are skipped.
func (s *Spec) CompleteTaskStates(taskIDs []int) *ProgressV3Json {
	return s.setStates(taskIDs, TaskStateDone)
}

// ResetTaskStates sets the named tasks to pending, bypassing the state
// machine, and returns the progress to write. Unknown ids are skipped.
func (s *Spec) ResetTaskStates(taskIDs []int) *ProgressV3Json {
	return s.setStates(taskIDs, TaskStatePending)
}

// setStates is the progress of the package with the named tasks in state: a
// new artifact holding every task of the plan, so the receiver's progress is
// not touched.
func (s *Spec) setStates(taskIDs []int, state TaskState) *ProgressV3Json {
	p := s.progressArtifact()
	for i := range p.Tasks {
		if slices.Contains(taskIDs, p.Tasks[i].Id) {
			p.Tasks[i].State = state
		}
	}
	return p
}

// clone returns a copy of the task whose slices are independent of the
// receiver's, preserving the distinction between a nil slice (field absent)
// and an empty one (field present but empty).
func (t Task) clone() Task {
	t.Criteria = copyStrings(t.Criteria)
	t.Steps = copyStrings(t.Steps)
	t.Touches = copyStrings(t.Touches)
	t.DoneWhen = copyStrings(t.DoneWhen)
	if t.DependsOn != nil {
		deps := make([]int, len(t.DependsOn))
		copy(deps, t.DependsOn)
		t.DependsOn = deps
	}
	return t
}

// deepCopy returns a copy of the artifact whose slices are independent of the
// receiver's, so that mutations on the copy cannot alias the original.
func (t *TasksV3Json) deepCopy() *TasksV3Json {
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
