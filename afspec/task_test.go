package afspec

import (
	"errors"
	"math/rand/v2"
	"slices"
	"testing"
)

// The task state machine of §8.3.1.
func TestValidTaskTransition(t *testing.T) {
	for _, tc := range []struct {
		from, to TaskState
		want     bool
	}{
		{TaskStatePending, TaskStateInProgress, true},
		{TaskStatePending, TaskStateDropped, true},
		{TaskStateInProgress, TaskStateDone, true},
		{TaskStateInProgress, TaskStatePending, true},
		{TaskStateDone, TaskStatePending, true},
		{TaskStatePending, TaskStateDone, false},
		{TaskStateDone, TaskStateInProgress, false},
		{TaskStateDone, TaskStateDropped, false},
		{TaskStateDropped, TaskStatePending, false},
		{TaskStateDropped, TaskStateInProgress, false},
		{TaskStateInProgress, TaskStateDropped, false},
	} {
		t.Run(string(tc.from)+" to "+string(tc.to), func(t *testing.T) {
			if got := ValidTaskTransition(tc.from, tc.to); got != tc.want {
				t.Errorf("ValidTaskTransition(%s, %s) = %v; want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

func TestTransitionTask_ReturnsANewCopy(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)

	updated, err := spec.Tasks.TransitionTask(1, string(TaskStateInProgress))
	if err != nil {
		t.Fatalf("TransitionTask = %v", err)
	}
	if spec.Tasks.Tasks[0].State != TaskStatePending {
		t.Error("TransitionTask mutated the receiver")
	}
	if updated.Tasks[0].State != TaskStateInProgress {
		t.Errorf("state = %q; want in_progress", updated.Tasks[0].State)
	}

	// The copy's slices must not alias the original's.
	updated.Tasks[0].Steps[0] = "changed"
	if spec.Tasks.Tasks[0].Steps[0] == "changed" {
		t.Error("the copy's Steps slice aliases the original")
	}
}

func TestTransitionTask_RefusesAnIllegalMove(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)

	_, err := spec.Tasks.TransitionTask(1, string(TaskStateDone))
	if err == nil {
		t.Fatal("pending → done was accepted; it must go through in_progress")
	}
	var lifecycleErr *LifecycleError
	if !errors.As(err, &lifecycleErr) {
		t.Errorf("error is %T; want *LifecycleError", err)
	}
}

func TestTransitionTask_RefusesAnUnknownTaskOrState(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)

	if _, err := spec.Tasks.TransitionTask(99, string(TaskStateInProgress)); err == nil {
		t.Error("an unknown task ID was accepted")
	}
	if _, err := spec.Tasks.TransitionTask(1, "queued"); err == nil {
		t.Error("the removed v1 state \"queued\" was accepted")
	}
}

func TestCompleteAndResetTaskStates_SetTheNamedTasksOnACopy(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)

	done := spec.Tasks.CompleteTaskStates([]int{1, 2, 99})
	if done.Tasks[0].State != TaskStateDone || done.Tasks[1].State != TaskStateDone {
		t.Error("CompleteTaskStates did not mark the named tasks done")
	}
	if done.Tasks[2].State != TaskStatePending {
		t.Error("CompleteTaskStates touched a task it was not given")
	}
	if spec.Tasks.Tasks[0].State != TaskStatePending {
		t.Error("CompleteTaskStates mutated the receiver")
	}

	reset := done.ResetTaskStates([]int{1})
	if reset.Tasks[0].State != TaskStatePending {
		t.Errorf("state = %q; want pending", reset.Tasks[0].State)
	}
}

func TestGetTask_ReturnsACopy(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)

	task, ok := spec.Tasks.GetTask(2)
	if !ok {
		t.Fatal("task 2 not found")
	}
	if task.Title == "" {
		t.Error("the returned task has no title")
	}
	task.Title = "changed"
	if spec.Tasks.Tasks[1].Title == "changed" {
		t.Error("GetTask returned a pointer into the artifact")
	}

	if _, ok := spec.Tasks.GetTask(99); ok {
		t.Error("GetTask found a task that does not exist")
	}
}

func TestIsOptional_DefaultsToFalse(t *testing.T) {
	if (Task{}).IsOptional() {
		t.Error("a task with no optional field reported itself optional")
	}
	yes := true
	if !(Task{Optional: &yes}).IsOptional() {
		t.Error("optional: true was not honoured")
	}
}

// IsReady follows depends_on, an explicit empty list, and the previous-task
// default.
func TestIsReady_FollowsDependsOn(t *testing.T) {
	p := plan(
		planTask(1, TaskStatePending, nil),
		planTask(2, TaskStatePending, nil),
		planTask(3, TaskStatePending, []int{}),
		planTask(4, TaskStatePending, []int{1, 2}),
	)
	if !p.IsReady(1) {
		t.Error("a pending first task without depends_on is not ready")
	}

	p = p.CompleteTaskStates([]int{1})
	for id, want := range map[int]bool{
		1: false, // done is not pending
		2: true,  // absent depends_on: the previous task, 1, is done
		3: true,  // an explicit empty list means no dependencies
		4: false, // task 2 is not done
	} {
		if got := p.IsReady(id); got != want {
			t.Errorf("with task 1 done: IsReady(%d) = %v; want %v", id, got, want)
		}
	}

	p, err := p.TransitionTask(1, string(TaskStatePending))
	if err != nil {
		t.Fatal(err)
	}
	if p, err = p.TransitionTask(1, string(TaskStateInProgress)); err != nil {
		t.Fatal(err)
	}
	if p.IsReady(2) {
		t.Error("a task whose previous task is in_progress is ready")
	}
}

// A dependency the plan lacks, or one that is dropped, is not done.
func TestIsReady_TreatsMissingAndDroppedDependenciesAsNotDone(t *testing.T) {
	missing := plan(planTask(1, TaskStateDone, nil), planTask(2, TaskStatePending, []int{99}))
	if missing.IsReady(2) {
		t.Error("a dependency that is not in the plan counts as done")
	}

	droppedExplicit := plan(planTask(1, TaskStateDropped, nil), planTask(3, TaskStatePending, []int{1}))
	if droppedExplicit.IsReady(3) {
		t.Error("an explicit dependency that is dropped counts as done")
	}

	droppedPrevious := plan(planTask(3, TaskStateDropped, nil), planTask(4, TaskStatePending, nil))
	if droppedPrevious.IsReady(4) {
		t.Error("a previous task that is dropped counts as done")
	}
}

// An unknown id or a nil receiver is not ready, and does not panic.
func TestIsReady_IsFalseForAnUnknownIDOrANilPlan(t *testing.T) {
	p := plan(planTask(1, TaskStatePending, nil), planTask(2, TaskStatePending, nil))
	for _, id := range []int{42, 0, -1} {
		if p.IsReady(id) {
			t.Errorf("IsReady(%d) on a plan without that task = true", id)
		}
	}
	var nilPlan *TasksV2Json
	if nilPlan.IsReady(1) {
		t.Error("IsReady on a nil receiver = true")
	}
}

func TestNextTask_ReturnsACopyOfTheFirstInProgressTask(t *testing.T) {
	optional := true
	inProgress := planTask(2, TaskStateInProgress, []int{1})
	inProgress.Optional = &optional
	inProgress.Criteria = []string{"c"}
	inProgress.Tests = []string{"t"}
	inProgress.Steps = []string{"s"}
	inProgress.Touches = []string{"f"}
	inProgress.DoneWhen = []string{"d"}
	p := plan(
		planTask(1, TaskStateDone, nil),
		inProgress,
		planTask(3, TaskStateInProgress, []int{1}),
		planTask(4, TaskStatePending, []int{1}),
	)

	got, ok := p.NextTask()
	if !ok || got.Id != 2 {
		t.Fatalf("NextTask = %+v, %v; want task 2", got, ok)
	}

	got.Title = "changed"
	got.DependsOn[0] = 99
	got.Criteria[0], got.Tests[0], got.Steps[0], got.Touches[0], got.DoneWhen[0] = "x", "x", "x", "x", "x"
	*got.Optional = false
	want := p.Tasks[1]
	if want.Title != "task" || want.DependsOn[0] != 1 || want.Criteria[0] != "c" || want.Tests[0] != "t" ||
		want.Steps[0] != "s" || want.Touches[0] != "f" || want.DoneWhen[0] != "d" || !*want.Optional {
		t.Errorf("mutating the returned task changed the plan: %+v", want)
	}
}

// With no task in progress, NextTask returns the first ready task.
func TestNextTask_ReturnsTheFirstReadyTask(t *testing.T) {
	p := plan(
		planTask(1, TaskStateDone, nil),
		planTask(2, TaskStateDropped, nil),
		planTask(3, TaskStatePending, []int{1}),
		planTask(4, TaskStatePending, []int{3}),
	)
	got, ok := p.NextTask()
	if !ok || got.Id != 3 {
		t.Errorf("NextTask = %+v, %v; want task 3", got, ok)
	}
}

func TestNextTask_ReturnsNothingWhenNoTaskCanRun(t *testing.T) {
	var nilPlan *TasksV2Json
	for name, p := range map[string]*TasksV2Json{
		"nil receiver": nilPlan,
		"empty plan":   plan(),
		"all finished": plan(planTask(1, TaskStateDone, nil), planTask(2, TaskStateDropped, nil)),
		"blocked": plan(
			planTask(1, TaskStatePending, []int{2}),
			planTask(2, TaskStatePending, []int{99}),
		),
	} {
		t.Run(name, func(t *testing.T) {
			if got, ok := p.NextTask(); got != nil || ok {
				t.Errorf("NextTask = %+v, %v; want nil, false", got, ok)
			}
		})
	}
}

// For any plan, NextTask is the first in_progress task, else the
// earliest ready one, and returns nothing only when neither exists. The plans
// are generated from a fixed seed: random states, ids in no particular order,
// and a depends_on that is absent, empty or lists ids that may not be in the
// plan.
func TestNextTask_PicksTheFirstInProgressElseTheEarliestReadyTask(t *testing.T) {
	rng := rand.New(rand.NewPCG(10, 8))
	states := []TaskState{TaskStatePending, TaskStateInProgress, TaskStateDone, TaskStateDropped}
	for range 3000 {
		n := rng.IntN(7)
		ids := rng.Perm(n + 3)[:n]
		tasks := make([]Task, n)
		for i, id := range ids {
			var deps []int
			switch rng.IntN(3) {
			case 1:
				deps = []int{}
			case 2:
				for range 1 + rng.IntN(3) {
					deps = append(deps, rng.IntN(n+3))
				}
			}
			tasks[i] = planTask(id, states[rng.IntN(len(states))], deps)
		}
		p := plan(tasks...)

		firstInProgress := slices.IndexFunc(tasks, func(task Task) bool { return task.State == TaskStateInProgress })
		anyReady := slices.IndexFunc(tasks, func(task Task) bool { return p.IsReady(task.Id) })

		got, ok := p.NextTask()
		switch {
		case ok && firstInProgress >= 0:
			if got.Id != tasks[firstInProgress].Id {
				t.Fatalf("plan %+v: NextTask = %d; want the first in_progress task, %d", tasks, got.Id, tasks[firstInProgress].Id)
			}
		case ok:
			if !p.IsReady(got.Id) || anyReady >= 0 && tasks[anyReady].Id != got.Id {
				t.Fatalf("plan %+v: NextTask = %d; want the earliest ready task", tasks, got.Id)
			}
		default:
			if firstInProgress >= 0 || anyReady >= 0 {
				t.Fatalf("plan %+v: NextTask found nothing, but a task is in progress (%d) or ready (%d)", tasks, firstInProgress, anyReady)
			}
		}
	}
}
