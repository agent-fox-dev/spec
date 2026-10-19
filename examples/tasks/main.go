// Command tasks shows how a coding agent works through a spec package with
// the afspec library: pick the next task, mark it in progress, render the
// package scoped to it, do the work, then record the task as done in
// tasks.json.
//
//	go run ./examples/tasks [-task N] DIR
//
// DIR is a spec package directory, e.g. .specs/01_greet_command. Without
// -task, the next task that is ready is taken: one already in progress, else
// the first pending task whose dependencies are all done. The scoped
// Markdown is written to stdout, progress to stderr.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "tasks:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("tasks", flag.ContinueOnError)
	fs.SetOutput(stderr)
	taskID := fs.Int("task", 0, "the task to work on (default: the next one that is ready)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: tasks [-task N] DIR")
	}
	dir := fs.Arg(0)

	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		return err
	}
	// A coder relies on the plan's references resolving, so an invalid
	// package is refused before any task is touched.
	if result := spec.Validate(); !result.Valid {
		return fmt.Errorf("%s is not valid (%d errors); run the validate example on it", dir, len(result.Errors))
	}

	task, err := pick(spec.Tasks, *taskID)
	if err != nil {
		return err
	}

	// Claim the task: pending → in_progress, persisted before the work
	// starts, so that anyone reading tasks.json sees it is taken.
	if task.State == afspec.TaskStatePending {
		if err := setState(spec, task.Id, afspec.TaskStateInProgress); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "task %d: %s → in_progress\n", task.Id, task.Title)
	}

	// The scoped render is what the coder reads instead of the files: the
	// task, its criteria, tests and paths in full, everything else it does
	// not own as one line by id and title. WithMaxTokens caps its size.
	markdown := renderScoped(spec, task.Id, afspec.WithMaxTokens(20000))

	if err := implement(*task, markdown, stdout); err != nil {
		// The work failed: release the task (in_progress → pending) so it
		// can be picked up again, and report the failure.
		if releaseErr := setState(spec, task.Id, afspec.TaskStatePending); releaseErr != nil {
			return errors.Join(err, releaseErr)
		}
		fmt.Fprintf(stderr, "task %d: released → pending\n", task.Id)
		return err
	}

	// The work is done: in_progress → done, written back to tasks.json.
	if err := setState(spec, task.Id, afspec.TaskStateDone); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "task %d: %s → done\n", task.Id, task.Title)

	if next, err := pick(spec.Tasks, 0); err == nil {
		fmt.Fprintf(stderr, "next: task %d (%s)\n", next.Id, next.Title)
	} else {
		fmt.Fprintf(stderr, "next: %s\n", err)
	}
	return nil
}

// pick returns the task with the given ID, or with id 0 the next one that is
// ready: a task already in progress, else the first pending task whose
// dependencies (depends_on) are all done.
func pick(tasks *afspec.TasksV2Json, id int) (*afspec.Task, error) {
	if id != 0 {
		task, ok := tasks.GetTask(id)
		if !ok {
			return nil, fmt.Errorf("task %d not found", id)
		}
		if task.State != afspec.TaskStatePending && task.State != afspec.TaskStateInProgress {
			return nil, fmt.Errorf("task %d is %s", id, task.State)
		}
		return task, nil
	}

	done := map[int]bool{}
	for _, t := range tasks.Tasks {
		done[t.Id] = t.State == afspec.TaskStateDone
	}
	for _, t := range tasks.Tasks {
		if t.State == afspec.TaskStateInProgress {
			return &t, nil
		}
	}
	for _, t := range tasks.Tasks {
		if t.State != afspec.TaskStatePending {
			continue
		}
		if !slices.ContainsFunc(t.DependsOn, func(dep int) bool { return !done[dep] }) {
			return &t, nil
		}
	}
	if !slices.ContainsFunc(tasks.Tasks, func(t afspec.Task) bool { return t.State == afspec.TaskStatePending }) {
		return nil, errors.New("every task is done or dropped")
	}
	return nil, errors.New("no pending task has all its dependencies done")
}

// setState moves one task through the task state machine and saves the
// package. TransitionTask refuses a move the state machine does not allow
// (pending → done, say) and returns a new TasksV2Json instead of changing
// the one it is called on.
func setState(spec *afspec.Spec, id int, target afspec.TaskState) error {
	tasks, err := spec.Tasks.TransitionTask(id, string(target))
	if err != nil {
		return err
	}
	spec.Tasks = tasks
	spec.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	// Save writes every artifact atomically. On an active spec it refuses a
	// changed ## Intent section or identity; a task's state is neither.
	return spec.Save(spec.Dir)
}

// renderScoped joins the per-artifact parts of the scoped render into one
// document, in the order of a whole render.
func renderScoped(spec *afspec.Spec, taskID int, opts ...afspec.RenderOption) string {
	parts := spec.RenderIndividualScoped(taskID, opts...)
	var out string
	for _, part := range []struct{ key, heading string }{
		{"prd", "PRD"},
		{"architecture", "Architecture"},
		{"requirements", "Requirements"},
		{"test_spec", "Test Specification"},
		{"tasks", "Tasks"},
	} {
		if text, ok := parts[part.key]; ok {
			out += "# " + part.heading + "\n\n" + text + "\n"
		}
	}
	return out
}

// implement stands in for the coder. A real agent would hand the Markdown to
// a model, write the tests the task owns, make them pass and run the plan's
// test commands; here the Markdown is only printed.
func implement(task afspec.Task, markdown string, w io.Writer) error {
	_, err := io.WriteString(w, markdown)
	return err
}
