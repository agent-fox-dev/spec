package main

import (
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// claim is the call that claims task id of the package at dir.
func claim(dir string, id int) envelope.Next {
	return envelope.Next{Tool: "speca", Input: dir, Flags: []string{"--task", fmt.Sprint(id), "--state", "in_progress"}}
}

// hasCall reports whether next holds a call to tool with these flags for input.
func hasCall(next []envelope.Next, want envelope.Next) bool {
	return slices.ContainsFunc(next, func(n envelope.Next) bool {
		return n.Tool == want.Tool && n.Input == want.Input && slices.Equal(n.Flags, want.Flags)
	})
}

// TS-11-1: a task form is read as the report is, comma lists are split, and a
// duplicate id is one.
func TestTheTaskFormResolvesLikeTheReport_TS11_1(t *testing.T) {
	root := newRepo(t)
	before := snapshot(t, root)
	env, code, _ := run(t, "--dir", root, "09", "--task", "1,2", "--task", "2", "--state", "in_progress", "--dry-run")
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	r := result(t, env)
	if !slices.Equal(changed(r), []int{1, 2}) || r.SpecDir != filepath.Join(".specs", "09_agent_mode") || r.SpecID != "09" {
		t.Errorf("changes %v, package %q %q; want 1 and 2 once each, in package 09", changed(r), r.SpecDir, r.SpecID)
	}
	if !mapsEqual(before, snapshot(t, root)) {
		t.Error("the repository changed")
	}
}

// TS-11-21: a call applies every named move or none, never a subset. For any
// plan, ids and target, with and without --force: a call that is refused leaves
// the files as they were, and one that goes ahead has put every named task in
// the target state and left the rest alone.
func TestAllOrNothing_TS11_21(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 21))
	states := []string{"pending", "in_progress", "done", "dropped"}
	for range 120 {
		n := 1 + rng.IntN(8)
		var tasks []map[string]any
		before := map[int]string{}
		for id := 1; id <= n; id++ {
			state := states[rng.IntN(len(states))]
			before[id] = state
			var deps []int
			if rng.IntN(2) == 0 && id > 1 {
				deps = []int{1 + rng.IntN(id-1)}
			}
			tasks = append(tasks, planTask(id, state, deps))
		}
		root := newRepo(t)
		dir := pkgDir(root, "09_agent_mode")
		setTasks(t, dir, tasks...)
		files := snapshot(t, root)

		var named []int
		for _, id := range rng.Perm(n)[:1+rng.IntN(n)] {
			named = append(named, id+1)
		}
		target := states[rng.IntN(len(states))]
		ids := make([]string, len(named))
		for i, id := range named {
			ids[i] = fmt.Sprint(id)
		}
		argv := []string{"--dir", root, "09", "--task", strings.Join(ids, ","), "--state", target}
		if rng.IntN(2) == 0 {
			argv = append(argv, "--force")
		}

		env, code, _ := run(t, argv...)
		if code != cli.ExitOK {
			if !mapsEqual(files, snapshot(t, root)) {
				t.Fatalf("%v: refused with exit %d, and the repository changed (plan %v)", argv, code, before)
			}
			continue
		}
		got := onDisk(t, dir)
		for id := 1; id <= n; id++ {
			if slices.Contains(named, id) {
				if got[id] != target {
					t.Fatalf("%v: task %d is %s; want %s, the call having gone ahead (plan %v)", argv, id, got[id], target, before)
				}
			} else if got[id] != before[id] {
				t.Fatalf("%v: task %d, not named, went from %s to %s", argv, id, before[id], got[id])
			}
		}
		_ = env
	}
}

// TS-11-26: a draft takes a task change, and so does an active package whose
// intent matches its hash.
func TestDraftAndActivePackagesTakeChanges_TS11_26(t *testing.T) {
	for name, active := range map[string]bool{"a draft": false, "an active package": true} {
		root := newRepo(t)
		dir := pkgDir(root, "09_agent_mode")
		if active {
			activate(t, dir)
		}
		env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress")
		if code != cli.ExitOK || onDisk(t, dir)[1] != "in_progress" {
			t.Errorf("%s: code %d: %+v; on disk %v", name, code, env.Error, onDisk(t, dir))
		}
		if active && result(t, env).Status != "active" {
			t.Errorf("%s: status %q", name, result(t, env).Status)
		}
	}
}

// TS-11-42: --dry-run runs every check and leaves every file as it was, updated_at too.
func TestADryRunLeavesThePackageAsItWas_TS11_42(t *testing.T) {
	root := newRepo(t)
	before := snapshot(t, root)
	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress", "--dry-run")
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	if after := snapshot(t, root); !mapsEqual(before, after) {
		t.Errorf("the repository changed:\nbefore %v\nafter  %v", before, after)
	}
}

// TS-11-43: a dry run returns the result the call would have produced.
func TestADryRunReturnsTheWouldBeResult_TS11_43(t *testing.T) {
	root := newRepo(t)
	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress", "--dry-run")
	r := result(t, env)
	if code != cli.ExitOK || !r.DryRun || len(r.Changes) != 1 || r.Changes[0].ID != 1 || r.Changes[0].To != "in_progress" {
		t.Fatalf("code %d: dry_run %v, changes %+v", code, r.DryRun, r.Changes)
	}
	if r.UpdatedAt == "2026-09-08T09:00:00Z" || r.UpdatedAt == "" {
		t.Errorf("updated_at = %q; want the time the write would have set", r.UpdatedAt)
	}
	if r.TaskCounts == nil || r.TaskCounts.InProgress != 1 || r.Tasks[0].State != "in_progress" {
		t.Errorf("the result does not show the change: counts %+v", r.TaskCounts)
	}
	if !strings.HasSuffix(env.Summary, " (dry run, nothing written)") {
		t.Errorf("summary = %q", env.Summary)
	}
	// What the dry run said is what the real call then does.
	real, _, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress")
	if !slices.Equal(changed(result(t, real)), changed(r)) {
		t.Errorf("the real call changed %v; the dry run said %v", changed(result(t, real)), changed(r))
	}
}

// TS-11-44: a dry run is refused exactly as the real call is.
func TestADryRunIsRefusedAsTheRealCallIs_TS11_44(t *testing.T) {
	taken := newRepo(t)
	setTasks(t, pkgDir(taken, "09_agent_mode"), planTask(1, "in_progress", nil), planTask(2, "pending", []int{1}))
	sealed := newRepo(t)
	setStatus(t, pkgDir(sealed, "09_agent_mode"), "sealed")
	unknown := newRepo(t)
	addUnknownKey(t, pkgDir(unknown, "09_agent_mode"))

	for name, root := range map[string]string{"a taken task": taken, "a sealed package": sealed, "unknown keys": unknown} {
		base := []string{"--dir", root, "09", "--task", "1", "--state", "in_progress"}
		real, realCode, _ := run(t, base...)
		dry, dryCode, _ := run(t, append(slices.Clone(base), "--dry-run")...)
		if realCode == cli.ExitOK || dryCode != realCode || real.Error == nil || dry.Error == nil {
			t.Errorf("%s: real exit %d, dry exit %d", name, realCode, dryCode)
			continue
		}
		a, b := real.Error, dry.Error
		if a.Stage != b.Stage || a.Category != b.Category || a.Message != b.Message || fmt.Sprint(a.FixHint) != fmt.Sprint(b.FixHint) {
			t.Errorf("%s: the dry run was refused as %+v; the real call as %+v", name, b, a)
		}
	}
}

// TS-11-45: a dry run suggests the call that makes the changes, naming the ids it
// resolved and keeping --force; with nothing to change it suggests nothing.
func TestADryRunSuggestsTheRealCall_TS11_45(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"), planTask(1, "pending", []int{}), planTask(2, "pending", []int{}))
	env, code, _ := run(t, "--dir", root, "09", "--task", "all", "--state", "in_progress", "--force", "--dry-run")
	want := envelope.Next{Tool: "speca", Input: filepath.Join(".specs", "09_agent_mode"),
		Flags: []string{"--task", "1,2", "--state", "in_progress", "--force"}}
	if code != cli.ExitOK || len(env.Next) != 1 || !hasCall(env.Next, want) || env.Next[0].Why == "" {
		t.Errorf("code %d: next = %+v; want one entry, %+v", code, env.Next, want)
	}

	idle := newRepo(t)
	setTasks(t, pkgDir(idle, "09_agent_mode"), planTask(1, "done", nil), planTask(2, "dropped", nil))
	env, _, _ = run(t, "--dir", idle, "09", "--task", "all", "--state", "done", "--dry-run")
	if len(env.Next) != 0 {
		t.Errorf("no changes: next = %+v; want none", env.Next)
	}
}

// TS-11-46: the report suggests the claim of a ready next task on a package that
// can take a write and has nothing in progress, before the call that renders it.
func TestTheReportSuggestsTheClaim_TS11_46(t *testing.T) {
	root := newRepo(t)
	dir := filepath.Join(".specs", "09_agent_mode")
	env, _, _ := run(t, "--dir", root, "09")
	if len(env.Next) != 2 || !hasCall(env.Next[:1], claim(dir, 1)) || env.Next[1].Tool != "specr" {
		t.Errorf("a draft with a ready task: next = %+v; want the claim of task 1, then the render", env.Next)
	}
	if env.Next[0].Why == "" || !strings.Contains(env.Next[0].Why, "task 1") {
		t.Errorf("why = %q", env.Next[0].Why)
	}

	busy := newRepo(t)
	setTasks(t, pkgDir(busy, "09_agent_mode"), planTask(1, "in_progress", nil), planTask(2, "pending", []int{}))
	env, _, _ = run(t, "--dir", busy, "09")
	if len(env.Next) != 1 || env.Next[0].Tool != "specr" || hasCall(env.Next, claim(dir, 2)) {
		t.Errorf("a task in progress: next = %+v; want only the render", env.Next)
	}

	sealed := newRepo(t)
	setStatus(t, pkgDir(sealed, "09_agent_mode"), "sealed")
	env, _, _ = run(t, "--dir", sealed, "09")
	for _, n := range env.Next {
		if n.Tool == "speca" {
			t.Errorf("a sealed package: next holds %+v; it can take no claim", n)
		}
	}

	drifted := newRepo(t)
	activate(t, pkgDir(drifted, "09_agent_mode"))
	editFile(t, filepath.Join(pkgDir(drifted, "09_agent_mode"), "prd.md"), "so that an agent can parse results", "so that anyone can parse results")
	env, _, _ = run(t, "--dir", drifted, "09")
	for _, n := range env.Next {
		if n.Tool == "speca" {
			t.Errorf("a package whose intent drifted: next holds %+v; it can take no claim", n)
		}
	}
}

// TS-11-47: the list suggests the claim for the first active package that can
// take one: nothing in progress, a ready task, and no drift in its intent.
func TestTheListSuggestsTheClaim_TS11_47(t *testing.T) {
	root := t.TempDir()
	busy := addPackage(t, root, "05_busy", "05", "busy")
	activate(t, busy)
	setTasks(t, busy, planTask(1, "in_progress", nil))
	drifted := addPackage(t, root, "07_drifted", "07", "drifted")
	setTasks(t, drifted, planTask(1, "pending", nil))
	activate(t, drifted)
	editFile(t, filepath.Join(drifted, "prd.md"), "so that an agent can parse results", "so that anyone can parse results")
	first := addPackage(t, root, "08_first", "08", "first")
	setTasks(t, first, planTask(1, "pending", nil))
	activate(t, first)
	second := addPackage(t, root, "09_second", "09", "second")
	setTasks(t, second, planTask(1, "pending", nil))
	activate(t, second)

	env, code, _ := run(t, "--dir", root, "--list")
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	if len(env.Next) != 2 || !hasCall(env.Next[:1], claim(filepath.Join(".specs", "08_first"), 1)) ||
		env.Next[1].Tool != "specr" || env.Next[1].Input != filepath.Join(".specs", "08_first") {
		t.Errorf("next = %+v; want the claim of 08's task 1, then its render, and nothing for 07 or 09", env.Next)
	}
}

// TS-11-48: after a move, next is what the report would say of the package now:
// the render of a claimed task, the claim of the next one, and nothing when
// there is no task left.
func TestNextAfterAMove_TS11_48(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"), planTask(1, "pending", nil), planTask(2, "pending", []int{1}))
	dir := filepath.Join(".specs", "09_agent_mode")
	step := func(id, state string) envelope.AdminEnvelope {
		env, code, _ := run(t, "--dir", root, "09", "--task", id, "--state", state)
		if code != cli.ExitOK {
			t.Fatalf("task %s to %s: code %d: %+v", id, state, code, env.Error)
		}
		return env
	}

	env := step("1", "in_progress")
	if len(env.Next) != 1 || env.Next[0].Tool != "specr" || !slices.Equal(env.Next[0].Flags, []string{"--task", "1"}) {
		t.Errorf("after the claim: next = %+v; want the render of task 1", env.Next)
	}
	env = step("1", "done")
	if len(env.Next) == 0 || !hasCall(env.Next[:1], claim(dir, 2)) {
		t.Errorf("after task 1 is done: next = %+v; want the claim of task 2 first", env.Next)
	}
	step("2", "in_progress")
	env = step("2", "done")
	if len(env.Next) != 0 {
		t.Errorf("after the last task: next = %+v; want none", env.Next)
	}
}
