package main

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/internal/cli"
)

// taskState is the state of task id in the example, read from disk.
func taskState(t *testing.T, root string, id int) string {
	t.Helper()
	for _, task := range load(t, root, example).Tasks.Tasks {
		if task.Id == id {
			return string(task.State)
		}
	}
	t.Fatalf("no task %d", id)
	return ""
}

// TS-13-32 (13-PATH-1): an agent renders a package, runs the claim that next
// suggests through the real speca, and tasks.json records the task in progress.
func TestAnAgentRendersAndClaimsWhatNextSuggests_TS13_32(t *testing.T) {
	bin := buildSpeca(t)
	root := newRepo(t)
	dir := filepath.Join(root, ".specs", example)
	before := treeOf(t, dir)

	env, code, _ := run(t, "--dir", root, "09")
	if code != cli.ExitOK || len(env.Next) == 0 || env.Next[0].Tool != "speca" ||
		!slices.Equal(env.Next[0].Flags, []string{"--task", "1", "--state", "in_progress"}) {
		t.Fatalf("code %d, next = %+v; want the claim of task 1 first", code, env.Next)
	}
	after := treeOf(t, dir)
	for name, was := range before {
		if after[name] != was {
			t.Errorf("specr changed %s", name)
		}
	}
	if taskState(t, root, 1) != "pending" {
		t.Fatal("task 1 was not pending before the claim")
	}

	claim := env.Next[0]
	out, status := speca(t, bin, append([]string{"--dir", root, claim.Input}, claim.Flags...)...)
	if status != cli.ExitOK || !out.OK || out.Result == nil || len(out.Result.Changes) != 1 {
		t.Fatalf("speca %s: exit %d, error %+v", strings.Join(claim.Flags, " "), status, out.Error)
	}
	if got := taskState(t, root, 1); got != "in_progress" {
		t.Errorf("tasks.json says task 1 is %s; want in_progress", got)
	}
}

// TS-13-33 (13-PATH-2): an agent renders its task in progress, and records it done
// through the real speca with the call that next suggests.
func TestAnAgentRendersItsTaskAndRecordsItDone_TS13_33(t *testing.T) {
	bin := buildSpeca(t)
	root := exampleWith(t, []string{"in_progress", "pending", "pending"}, nil)

	env, code, _ := run(t, "--dir", root, "09", "--task", "1")
	if code != cli.ExitOK || len(env.Next) != 2 || !slices.Equal(env.Next[0].Flags, []string{"--task", "1", "--state", "done"}) ||
		env.Next[1].Tool != "specr" || !slices.Equal(env.Next[1].Flags, []string{"--task", "2"}) {
		t.Fatalf("code %d, next = %+v; want the done call of task 1, then the render of task 2", code, env.Next)
	}

	done := env.Next[0]
	out, status := speca(t, bin, append([]string{"--dir", root, done.Input}, done.Flags...)...)
	if status != cli.ExitOK || !out.OK {
		t.Fatalf("speca %s: exit %d, error %+v", strings.Join(done.Flags, " "), status, out.Error)
	}
	if got := taskState(t, root, 1); got != "done" {
		t.Errorf("tasks.json says task 1 is %s; want done", got)
	}
}

// TS-13-34 (13-PATH-3): the help of each of the three commands, and the schema of
// two of them, name the other commands with the sentence cli.SiblingCommands.
func TestEachCommandNamesItsSiblings_TS13_34(t *testing.T) {
	for _, name := range []string{"spec", "specr", "speca"} {
		bin := buildCommand(t, name)
		stdout, stderr, code := runBinary(t, bin, "--help")
		if code != cli.ExitOK || stdout != "" {
			t.Errorf("%s --help: exit %d, stdout %q", name, code, stdout)
		}
		usage := strings.Index(stderr, "Usage:")
		if usage < 0 || !strings.Contains(stderr[:usage], "\n\n"+cli.SiblingCommands+"\n\n") {
			t.Errorf("%s --help does not carry the sentence before Usage::\n%s", name, stderr)
		}
	}
	for _, name := range []string{"specr", "speca"} {
		stdout, _, code := runBinary(t, buildCommand(t, name), "--schema")
		var doc struct {
			Description string `json:"description"`
		}
		if err := json.Unmarshal([]byte(stdout), &doc); err != nil || code != cli.ExitOK || !strings.HasSuffix(doc.Description, " "+cli.SiblingCommands) {
			t.Errorf("%s --schema: exit %d (%v), description %q; want it to end with the sentence", name, code, err, doc.Description)
		}
	}
}
