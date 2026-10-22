package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
)

// copyFixture copies the worked example of the format into a spec directory
// of its own, so the test can write tasks.json.
func copyFixture(t *testing.T) string {
	t.Helper()
	src := "../../afspec/testdata/v2_example"
	dir := filepath.Join(t.TempDir(), "09_agent_mode")
	if err := os.CopyFS(dir, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	return dir
}

func states(t *testing.T, dir string) []afspec.TaskState {
	t.Helper()
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []afspec.TaskState
	for _, task := range spec.Tasks.Tasks {
		out = append(out, task.State)
	}
	return out
}

func TestTasksCompletesOneTaskPerRunInDependencyOrder(t *testing.T) {
	dir := copyFixture(t)

	var markdown strings.Builder
	if err := run([]string{dir}, &markdown, io.Discard); err != nil {
		t.Fatalf("run = %v", err)
	}
	if !strings.Contains(markdown.String(), "Detect agent mode and keep stdout JSON-only") {
		t.Errorf("the render does not show task 1:\n%s", markdown.String())
	}
	if got := states(t, dir); got[0] != afspec.TaskStateDone || got[1] != afspec.TaskStatePending {
		t.Fatalf("states after one run = %v; want task 1 done, task 2 pending", got)
	}

	for range 2 {
		if err := run([]string{dir}, io.Discard, io.Discard); err != nil {
			t.Fatalf("run = %v", err)
		}
	}
	for i, s := range states(t, dir) {
		if s != afspec.TaskStateDone {
			t.Errorf("task %d is %s; want done", i+1, s)
		}
	}

	if err := run([]string{dir}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "every task is done") {
		t.Errorf("run on a finished plan = %v; want every task done", err)
	}
}

func TestTasksRefusesATaskThatIsDone(t *testing.T) {
	dir := copyFixture(t)
	if err := run([]string{"-task", "1", dir}, io.Discard, io.Discard); err != nil {
		t.Fatalf("run = %v", err)
	}
	if err := run([]string{"-task", "1", dir}, io.Discard, io.Discard); err == nil {
		t.Error("run on a done task = nil; want an error")
	}
}

// TS-10-7: pick with id 0 takes NextTask's choice and keeps its two messages.
func TestPickWithoutAnIDUsesNextTask_TS10_7(t *testing.T) {
	task := func(id int, state afspec.TaskState, deps []int) afspec.Task {
		return afspec.Task{Id: id, State: state, DependsOn: deps}
	}
	plan := func(tasks ...afspec.Task) *afspec.TasksV2Json { return &afspec.TasksV2Json{Tasks: tasks} }

	_, err := pick(plan(task(1, afspec.TaskStateDone, nil), task(2, afspec.TaskStateDropped, nil)), 0)
	if err == nil || !strings.Contains(err.Error(), "every task is done or dropped") {
		t.Errorf("a finished plan: err = %v", err)
	}

	_, err = pick(plan(task(1, afspec.TaskStateDropped, nil), task(2, afspec.TaskStatePending, []int{1})), 0)
	if err == nil || !strings.Contains(err.Error(), "no pending task has all its dependencies done") {
		t.Errorf("a blocked plan: err = %v", err)
	}

	got, err := pick(plan(
		task(1, afspec.TaskStateDone, nil),
		task(2, afspec.TaskStatePending, nil),
		task(3, afspec.TaskStateInProgress, []int{1}),
	), 0)
	if err != nil || got.Id != 3 {
		t.Errorf("in_progress and ready tasks: pick = %+v, %v; want the in_progress task 3", got, err)
	}

	got, err = pick(plan(
		task(1, afspec.TaskStateDone, nil),
		task(2, afspec.TaskStatePending, []int{1}),
		task(3, afspec.TaskStatePending, nil),
	), 0)
	if err != nil || got.Id != 2 {
		t.Errorf("ready tasks: pick = %+v, %v; want task 2", got, err)
	}

	// A task without depends_on follows the task before it (format v2 §8.3);
	// the old pick treated it as having no dependencies at all.
	got, err = pick(plan(
		task(1, afspec.TaskStateInProgress, nil),
		task(2, afspec.TaskStatePending, nil),
	), 0)
	if err != nil || got.Id != 1 {
		t.Errorf("pick = %+v, %v; want the in_progress task 1", got, err)
	}
	_, err = pick(plan(task(1, afspec.TaskStatePending, []int{2}), task(2, afspec.TaskStatePending, nil)), 0)
	if err == nil {
		t.Error("task 2 follows task 1, which is pending, so no task is ready")
	}
}
