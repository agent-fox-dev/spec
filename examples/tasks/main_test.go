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
