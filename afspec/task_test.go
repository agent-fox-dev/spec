package afspec

import (
	"errors"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// The task state machine of §8.1.
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

// The plan of a full package is its tasks and then the integration task,
// task N+1, with its content derived from the package.
func TestPlan_EndsWithTheDerivedIntegrationTask(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	plan := spec.Plan()
	if len(plan) != 3 {
		t.Fatalf("plan = %d tasks; want 2 and the integration task", len(plan))
	}
	for _, task := range plan[:2] {
		if task.Kind != TaskKindImplement || task.State != TaskStatePending {
			t.Errorf("task %d: kind %s, state %s", task.Id, task.Kind, task.State)
		}
	}
	it := plan[2]
	if it.Id != 3 || it.Kind != TaskKindIntegration || it.Title != IntegrationTaskTitle {
		t.Errorf("integration task = %+v", it)
	}
	if !slices.Equal(it.DependsOn, []int{1, 2}) || len(it.Criteria) != 0 {
		t.Errorf("depends_on %v, criteria %v; want every task and no criterion", it.DependsOn, it.Criteria)
	}
	if !slices.Equal(it.Touches, []string{"afspec/afspec.go", "afspec/prd.go", "afspec/marshal.go", "afspec/validate.go"}) {
		t.Errorf("touches = %v; want the union of the tasks' touches", it.Touches)
	}
	joined := strings.Join(it.Steps, "\n")
	for _, want := range []string{"smoke check of PATH-1", "Trace PATH-1", "REQ-1.2", "stub markers"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the steps lack %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "depends on") {
		t.Error("a package with no dependency got the entry-point step")
	}
	if !slices.Equal(it.DoneWhen, []string{"`go test ./... -count=1` passes", "`go vet ./...` passes"}) {
		t.Errorf("done_when = %v", it.DoneWhen)
	}

	spec.Tasks.Dependencies = []Dependency{{Spec: "00", Reason: "x"}}
	if it, _ := spec.IntegrationTask(); !strings.Contains(strings.Join(it.Steps, "\n"), "depends on (00)") {
		t.Error("a package with a dependency lacks the entry-point step")
	}
	if id, ok := spec.IntegrationTaskID(); !ok || id != 3 {
		t.Errorf("IntegrationTaskID = %d, %v; want 3", id, ok)
	}
	var nilSpec *Spec
	if _, ok := nilSpec.IntegrationTask(); ok || nilSpec.Plan() != nil {
		t.Error("a nil spec has an integration task or a plan")
	}
}

func TestTransitionTask_ReturnsANewProgress(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)

	updated, err := spec.TransitionTask(1, string(TaskStateInProgress))
	if err != nil {
		t.Fatalf("TransitionTask = %v", err)
	}
	if spec.TaskState(1) != TaskStatePending {
		t.Error("TransitionTask mutated the receiver")
	}
	if updated.Tasks[0].Id != 1 || updated.Tasks[0].State != TaskStateInProgress {
		t.Errorf("entry = %+v; want task 1 in_progress", updated.Tasks[0])
	}
	if len(updated.Tasks) != 3 || updated.Tasks[2].Id != 3 {
		t.Errorf("the progress has %d entries; want one per task, the integration task included", len(updated.Tasks))
	}
	spec.Progress = updated
	if spec.TaskState(1) != TaskStateInProgress {
		t.Error("the new progress is not read")
	}

	// The integration task moves like any other.
	spec.Progress = spec.CompleteTaskStates([]int{1, 2})
	done, err := spec.TransitionTask(3, string(TaskStateInProgress))
	if err != nil {
		t.Fatalf("TransitionTask(3) = %v", err)
	}
	if done.Tasks[2].State != TaskStateInProgress {
		t.Errorf("integration task state = %s", done.Tasks[2].State)
	}
}

func TestTransitionTask_RefusesAnIllegalMove(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)

	_, err := spec.TransitionTask(1, string(TaskStateDone))
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

	if _, err := spec.TransitionTask(99, string(TaskStateInProgress)); err == nil {
		t.Error("an unknown task ID was accepted")
	}
	if _, err := spec.TransitionTask(1, "queued"); err == nil {
		t.Error("the removed v1 state \"queued\" was accepted")
	}
}

func TestCompleteAndResetTaskStates_SetTheNamedTasksOnACopy(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)

	done := spec.CompleteTaskStates([]int{1, 2, 99})
	if done.Tasks[0].State != TaskStateDone || done.Tasks[1].State != TaskStateDone {
		t.Error("CompleteTaskStates did not mark the named tasks done")
	}
	if done.Tasks[2].State != TaskStatePending {
		t.Error("CompleteTaskStates touched a task it was not given")
	}
	if spec.TaskState(1) != TaskStatePending {
		t.Error("CompleteTaskStates mutated the receiver")
	}
	for _, e := range done.Tasks {
		if e.Id == 99 {
			t.Error("an unknown id got a progress entry")
		}
	}

	spec.Progress = done
	reset := spec.ResetTaskStates([]int{1})
	if reset.Tasks[0].State != TaskStatePending {
		t.Errorf("state = %q; want pending", reset.Tasks[0].State)
	}
}

func TestGetTask_ReturnsACopy(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)

	task, ok := spec.GetTask(2)
	if !ok {
		t.Fatal("task 2 not found")
	}
	if task.Title == "" || task.Kind != TaskKindImplement {
		t.Errorf("the returned task = %+v", task)
	}
	task.Title = "changed"
	task.Steps[0] = "changed"
	if spec.Tasks.Tasks[1].Title == "changed" || spec.Tasks.Tasks[1].Steps[0] == "changed" {
		t.Error("GetTask returned a view into the artifact")
	}

	if it, ok := spec.GetTask(3); !ok || it.Kind != TaskKindIntegration {
		t.Errorf("GetTask(3) = %+v, %v; want the integration task", it, ok)
	}
	if _, ok := spec.GetTask(99); ok {
		t.Error("GetTask found a task that does not exist")
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

	p.Progress = p.CompleteTaskStates([]int{1})
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

	var err error
	if p.Progress, err = p.TransitionTask(1, string(TaskStatePending)); err != nil {
		t.Fatal(err)
	}
	if p.Progress, err = p.TransitionTask(1, string(TaskStateInProgress)); err != nil {
		t.Fatal(err)
	}
	if p.IsReady(2) {
		t.Error("a task whose previous task is in_progress is ready")
	}
}

// The integration task is ready when every task of the plan is done or
// dropped, and not before.
func TestIsReady_IntegrationTaskWaitsForEveryTask(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	if spec.IsReady(3) {
		t.Error("the integration task is ready with every task pending")
	}
	spec.Progress = spec.CompleteTaskStates([]int{1})
	if spec.IsReady(3) {
		t.Error("the integration task is ready with a task pending")
	}
	spec.Progress = &ProgressV3Json{Tasks: []TaskProgress{{Id: 1, State: TaskStateDone}, {Id: 2, State: TaskStateDropped}}}
	if !spec.IsReady(3) {
		t.Error("the integration task is not ready with every task done or dropped")
	}
	if next, ok := spec.NextTask(); !ok || next.Id != 3 {
		t.Errorf("NextTask = %v, %v; want the integration task", next, ok)
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
	var nilSpec *Spec
	if nilSpec.IsReady(1) {
		t.Error("IsReady on a nil receiver = true")
	}
}

func TestNextTask_ReturnsACopyOfTheFirstInProgressTask(t *testing.T) {
	inProgress := planTask(2, TaskStateInProgress, []int{1})
	inProgress.task.Criteria = []string{"c"}
	inProgress.task.Steps = []string{"s"}
	inProgress.task.Touches = []string{"f"}
	inProgress.task.DoneWhen = []string{"d"}
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
	got.Criteria[0], got.Steps[0], got.Touches[0], got.DoneWhen[0] = "x", "x", "x", "x"
	want := p.Tasks.Tasks[1]
	if want.Title != "task" || want.DependsOn[0] != 1 || want.Criteria[0] != "c" ||
		want.Steps[0] != "s" || want.Touches[0] != "f" || want.DoneWhen[0] != "d" {
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
	var nilSpec *Spec
	for name, p := range map[string]*Spec{
		"nil receiver": nilSpec,
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
		tasks := make([]plannedState, n)
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

		firstInProgress := slices.IndexFunc(tasks, func(task plannedState) bool { return task.state == TaskStateInProgress })
		anyReady := slices.IndexFunc(tasks, func(task plannedState) bool { return p.IsReady(task.task.Id) })

		got, ok := p.NextTask()
		switch {
		case ok && firstInProgress >= 0:
			if got.Id != tasks[firstInProgress].task.Id {
				t.Fatalf("plan %+v: NextTask = %d; want the first in_progress task, %d", tasks, got.Id, tasks[firstInProgress].task.Id)
			}
		case ok:
			if !p.IsReady(got.Id) || anyReady >= 0 && tasks[anyReady].task.Id != got.Id {
				t.Fatalf("plan %+v: NextTask = %d; want the earliest ready task", tasks, got.Id)
			}
		default:
			if firstInProgress >= 0 || anyReady >= 0 {
				t.Fatalf("plan %+v: NextTask found nothing, but a task is in progress (%d) or ready (%d)", tasks, firstInProgress, anyReady)
			}
		}
	}
}
