package main

import (
	"maps"
	"slices"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// An agent reads a package's state, renders the task it is pointed at with the
// real specr, claims it as suggested, has it rendered again, and finishes it.
// Only the built specr proves that the calls speca suggests are calls specr
// takes, and that the two agree on the tasks. Reading and rendering change
// nothing; the claim and the finish are on disk.
func TestAnAgentFollowsNextFromTheReportToTheFinishedTask(t *testing.T) {
	bin := specr(t)
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	t.Chdir(root)
	before := snapshot(t, root)

	// The report: a draft that validates is told to activate, then to claim task
	// 1, then to render it.
	report, code, _ := run(t, "09")
	r := result(t, report)
	if code != cli.ExitOK || r.SpecID != "09" || len(r.Tasks) != 3 || r.NextTask == nil || r.NextTask.ID != 1 || !r.Tasks[0].Ready || r.Tasks[1].Ready {
		t.Fatalf("code %d: %+v", code, r)
	}
	const input = ".specs/09_agent_mode"
	want := []envelope.Next{
		{Tool: "speca", Input: input, Flags: []string{"--status", "active"}},
		claim(input, 1),
		{Tool: "specr", Input: input, Flags: []string{"--task", "1"}},
	}
	if len(report.Next) != 3 || !sameCall(report.Next[0], want[0]) || !sameCall(report.Next[1], want[1]) || !sameCall(report.Next[2], want[2]) {
		t.Fatalf("next = %+v; want %+v", report.Next, want)
	}

	rendered := runSpecr(t, bin, root, report.Next[2])
	if task := rendered.Result.Task; task == nil || task.ID != 1 || !task.Ready || rendered.Result.Scope != "task" {
		t.Errorf("specr rendered %+v; want task 1, ready", task)
	}
	for i, task := range rendered.Result.Tasks {
		if task.Ready != r.Tasks[i].Ready {
			t.Errorf("task %d: speca says ready %v, specr %v", task.ID, r.Tasks[i].Ready, task.Ready)
		}
	}
	if !maps.Equal(before, snapshot(t, root)) {
		t.Error("reading and rendering changed the repository")
	}

	claimed, code, _ := run(t, append([]string{report.Next[1].Input}, report.Next[1].Flags...)...)
	if code != cli.ExitOK || onDisk(t, dir)[1] != "in_progress" ||
		!slices.Equal(result(t, claimed).Changes, []envelope.Change{{Kind: "task", ID: 1, From: "pending", To: "in_progress"}}) {
		t.Fatalf("the claim: code %d, %+v, on disk %v", code, claimed.Error, onDisk(t, dir))
	}
	if len(claimed.Next) != 1 || !sameCall(claimed.Next[0], want[2]) {
		t.Fatalf("after the claim: next = %+v; want the render of task 1", claimed.Next)
	}
	if task := runSpecr(t, bin, root, claimed.Next[0]).Result.Task; task == nil || task.ID != 1 || task.State != "in_progress" {
		t.Errorf("specr rendered %+v; want task 1, in progress", task)
	}

	done, code, _ := run(t, "09", "--task", "1", "--state", "done")
	if code != cli.ExitOK || onDisk(t, dir)[1] != "done" {
		t.Fatalf("code %d: %+v; on disk %v", code, done.Error, onDisk(t, dir))
	}
	if len(done.Next) == 0 || !sameCall(done.Next[0], claim(input, 2)) {
		t.Errorf("after task 1 is done: next = %+v; want the claim of task 2", done.Next)
	}
}
