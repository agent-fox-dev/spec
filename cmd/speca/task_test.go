package main

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// A save re-encodes every artifact from the version 2 types, which read none of
// a version 1 plan, so a package in another format version is refused before
// anything, --force or not.
func TestTask_RefusesAPackageInAnotherFormatVersion(t *testing.T) {
	root := newRepo(t)
	addLegacyPackage(t, root)
	for _, extra := range [][]string{nil, {"--force"}, {"--dry-run"}} {
		argv := append([]string{"--dir", root, "07", "--task", "1", "--state", "in_progress"}, extra...)
		refusedAsUsage(t, root, argv, "07_legacy", "format version 1", "writes format version 2 only", "discard", "v1 plan")
		if env, _, _ := run(t, argv...); len(env.Warnings) != 0 {
			t.Errorf("%v: warnings %v; a refusal does not also report what loaded", extra, warningCodes(env))
		}
	}
}

// A --task that names what the plan cannot supply is a usage error, whatever
// --force says: an id it does not have, next when there is no next task, all
// when there is no task.
func TestTask_RefusesTasksThePlanDoesNotHave(t *testing.T) {
	for name, tc := range map[string]struct {
		tasks  []map[string]any // nil keeps the example's three
		argv   []string
		want   []string
		notSay string
		hint   *cli.FixHint
	}{
		"unknown ids": {nil, []string{"--task", "2,7,9", "--state", "done"}, []string{"09_agent_mode", "has no tasks 7, 9;"}, "",
			&cli.FixHint{Flag: "--task", Valid: []string{"1", "2", "3"}}},
		"an unknown id under --force": {nil, []string{"--task", "99", "--state", "done", "--force"}, []string{"has no task 99;"}, "",
			&cli.FixHint{Flag: "--task", Valid: []string{"1", "2", "3"}}},
		"an id on a plan with no tasks": {[]map[string]any{}, []string{"--task", "1", "--state", "in_progress"}, []string{"09_agent_mode", "no tasks"}, "", nil},
		"next with every task done or dropped": {[]map[string]any{planTask(1, "done", nil), planTask(2, "dropped", []int{1})},
			[]string{"--task", "next", "--state", "in_progress"}, []string{"09_agent_mode", "every task is done or dropped"}, "", nil},
		"next with the pending tasks blocked": {[]map[string]any{planTask(1, "dropped", nil), planTask(2, "pending", []int{1})},
			[]string{"--task", "next", "--state", "in_progress"}, []string{"09_agent_mode", "no pending task has its dependencies done"}, "",
			&cli.FixHint{Flag: "--task", Valid: []string{"2"}}},
		// A plan with no tasks has none, not all done.
		"next on a plan with no tasks": {[]map[string]any{}, []string{"--task", "next", "--state", "in_progress"}, []string{"09_agent_mode", "no tasks"}, "done or dropped", nil},
		"all on a plan with no tasks":  {[]map[string]any{}, []string{"--task", "all", "--state", "done"}, []string{"09_agent_mode", "no tasks"}, "", nil},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			if tc.tasks != nil {
				setTasks(t, pkgDir(root, "09_agent_mode"), tc.tasks...)
			}
			e := refusedAsUsage(t, root, append([]string{"--dir", root, "09"}, tc.argv...), tc.want...)
			if e == nil {
				return
			}
			if tc.notSay != "" && strings.Contains(e.Message, tc.notSay) {
				t.Errorf("message %q says %q", e.Message, tc.notSay)
			}
			if !reflect.DeepEqual(e.FixHint, tc.hint) {
				t.Errorf("fix_hint = %+v; want %+v", e.FixHint, tc.hint)
			}
		})
	}
}

// A package that cannot take a write is refused at stage status before any move
// is looked at, an unknown id included, and --dry-run and --force change
// nothing of the refusal: the library has no way to write such a package.
func TestTask_RefusesAPackageThatCannotTakeAWrite(t *testing.T) {
	drifted := []string{"09_agent_mode", "every write", "intent matches its hash again"}
	for name, tc := range map[string]struct {
		setup  func(t *testing.T, dir string)
		want   []string
		notSay string
	}{
		"sealed":     {func(t *testing.T, dir string) { setStatus(t, dir, "sealed") }, []string{"09_agent_mode", "sealed", "no longer change", "supersedes"}, ""},
		"superseded": {func(t *testing.T, dir string) { setStatus(t, dir, "superseded") }, []string{"superseded", "no longer change", "supersedes"}, ""},
		"archived":   {func(t *testing.T, dir string) { setStatus(t, dir, "archived") }, []string{"archived", "no longer change", "supersedes"}, ""},
		"active, its intent edited": {func(t *testing.T, dir string) {
			activate(t, dir)
			driftIntent(t, dir)
		}, drifted, ""},
		"active, its intent gone": {func(t *testing.T, dir string) {
			activate(t, dir)
			removeIntent(t, dir)
		}, drifted, ""},
		// An empty hash is set, and is not the hash of the intent.
		"active, with an empty hash": {func(t *testing.T, dir string) {
			activate(t, dir)
			editFile(t, filepath.Join(dir, "prd.md"), `intent_hash: "`+intentHashOf(t, dir)+`"`, `intent_hash: ""`)
		}, drifted, ""},
		// A status the format does not define is refused by name: the package is
		// not sealed, superseded or archived, and nothing supersedes it.
		"a status the format does not define": {func(t *testing.T, dir string) { setStatus(t, dir, "weird") }, []string{`"weird"`}, "supersedes it"},
		"a status in the wrong case":          {func(t *testing.T, dir string) { setStatus(t, dir, "Draft") }, []string{`"Draft"`}, "supersedes it"},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			tc.setup(t, pkgDir(root, "09_agent_mode"))
			base := []string{"09", "--task", "99", "--state", "done"}
			want := refusedWithReport(t, root, "status", "lifecycle", base...).Error
			saysAll(t, base, want.Message, tc.want...)
			if tc.notSay != "" && strings.Contains(want.Message, tc.notSay) {
				t.Errorf("message %q says %q", want.Message, tc.notSay)
			}
			if want.FixHint != nil {
				t.Errorf("fix_hint %+v; want none", want.FixHint)
			}
			for _, extra := range []string{"--dry-run", "--force"} {
				got := refusedWithReport(t, root, "status", "lifecycle", append(slices.Clone(base), extra)...).Error
				if got.Message != want.Message {
					t.Errorf("%s: %q; want the refusal without it, %q", extra, got.Message, want.Message)
				}
			}
		})
	}
}

// A draft takes a task change, and so does an active package whose intent
// matches its hash.
func TestTask_TakesAChangeOnADraftOrAnActivePackage(t *testing.T) {
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

// Claiming a task that is in progress is refused by the machine, which is how a
// second agent learns the task is taken: with the states it may move to, the
// package as it is on disk, and nothing touched.
func TestTask_RefusesAClaimOfATakenTask(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"), planTask(1, "in_progress", nil), planTask(2, "pending", []int{1}))
	env := refusedWithReport(t, root, "task", "lifecycle", "09", "--task", "1", "--state", "in_progress")
	if h := env.Error.FixHint; h == nil || h.Flag != "--state" || !slices.Equal(h.Valid, []string{"pending", "done"}) {
		t.Errorf("fix_hint = %+v; want --state with pending and done", h)
	}
	if r := env.Result; len(r.Tasks) != 2 || r.Tasks[0].State != "in_progress" || r.NextTask == nil || r.NextTask.ID != 1 {
		t.Errorf("result = %+v; want task 1 in progress, as on disk", r)
	}
}

// --task next is the task the report calls next_task.
func TestTask_NextIsTheReportsNextTask(t *testing.T) {
	root := newRepo(t)
	if env, _, _ := run(t, "--dir", root, "09"); result(t, env).NextTask == nil || result(t, env).NextTask.ID != 1 {
		t.Fatalf("the report's next_task = %+v; want task 1", result(t, env).NextTask)
	}
	env, code, _ := run(t, "--dir", root, "09", "--task", "next", "--state", "in_progress")
	if code != cli.ExitOK || !slices.Equal(changed(result(t, env)), []int{1}) {
		t.Fatalf("code %d: changes %+v, error %+v", code, result(t, env).Changes, env.Error)
	}
	if c := result(t, env).Changes[0]; c.Kind != "task" || c.From != "pending" || c.To != "in_progress" || c.Forced {
		t.Errorf("change = %+v", c)
	}
	if got := onDisk(t, pkgDir(root, "09_agent_mode")); got[1] != "in_progress" || got[2] != "pending" {
		t.Errorf("on disk: %v", got)
	}
}

// A success is the report of the package as it was written, and the changes, in
// plan order, each task once however often --task named it.
func TestTask_ReportsThePackageAfterTheChange(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	setTasks(t, dir, planTask(1, "pending", nil), planTask(2, "done", nil), planTask(3, "pending", []int{}))
	env, raw := rawRun(t, "--dir", root, "09", "--task", "3", "--task", "1,3", "--state", "in_progress")
	r := result(t, env)
	if env.ExitCode != cli.ExitOK || !slices.Equal(changed(r), []int{1, 3}) {
		t.Fatalf("exit %d: changes %+v, error %+v", env.ExitCode, r.Changes, env.Error)
	}
	for _, c := range r.Changes {
		if c.Kind != "task" || c.From != "pending" || c.To != "in_progress" || c.Forced {
			t.Errorf("change = %+v", c)
		}
	}
	if r.SpecDir != filepath.Join(".specs", "09_agent_mode") || r.SpecID != "09" {
		t.Errorf("package %q %q", r.SpecDir, r.SpecID)
	}
	if want := (envelope.TaskCounts{InProgress: 2, Done: 1, Total: 3}); r.TaskCounts == nil || *r.TaskCounts != want {
		t.Errorf("task_counts = %+v; want %+v, the counts after the change", r.TaskCounts, want)
	}
	if len(r.Tasks) != 3 || r.Tasks[0].State != "in_progress" || r.Tasks[2].State != "in_progress" {
		t.Errorf("tasks = %+v", r.Tasks)
	}
	if got := onDisk(t, dir); !maps.Equal(got, map[int]string{1: "in_progress", 2: "done", 3: "in_progress"}) {
		t.Errorf("on disk: %v", got)
	}
	for _, k := range []string{"spec_dir", "spec_id", "status", "valid", "tasks", "task_counts", "next_task", "allowed_status", "depends_on", "dependents", "changes"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("the result lacks %s", k)
		}
	}
}

// A call that resolves to no move succeeds, changes nothing and writes nothing:
// not a byte, not updated_at. Since it writes nothing it deletes no key either,
// so the keys the schema does not allow neither refuse it nor warn.
func TestTask_NothingToMoveWritesNothing(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	setTasks(t, dir, planTask(1, "done", nil), planTask(2, "dropped", nil))
	addUnknownKey(t, dir)
	before := snapshot(t, root)
	env, raw := rawRun(t, "--dir", root, "09", "--task", "all", "--state", "done")
	if env.ExitCode != cli.ExitOK || !env.OK || len(env.Warnings) != 0 {
		t.Fatalf("exit %d: %+v, warnings %v", env.ExitCode, env.Error, warningCodes(env))
	}
	if _, ok := raw["changes"]; ok {
		t.Errorf("changes = %s; want none", raw["changes"])
	}
	if env.Summary != "speca: 09_agent_mode no task changed" {
		t.Errorf("summary = %q", env.Summary)
	}
	if got := result(t, env).UpdatedAt; got != "2026-09-08T09:00:00Z" {
		t.Errorf("updated_at = %q; want the fixture's", got)
	}
	if after := snapshot(t, root); !maps.Equal(before, after) {
		t.Errorf("the repository changed:\nbefore %v\nafter  %v", before, after)
	}
}

// A call applies every named move or none, never a subset. For any plan, ids and
// target, with and without --force: a call that is refused leaves the files as
// they were, and one that goes ahead has put every named task in the target
// state and left the rest alone.
func TestTask_AppliesEveryMoveOrNone(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 21))
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	for range 120 {
		n := 1 + rng.IntN(8)
		var tasks []map[string]any
		before := map[int]string{}
		for id := 1; id <= n; id++ {
			state := wantTaskStates[rng.IntN(len(wantTaskStates))]
			before[id] = state
			var deps []int
			if rng.IntN(2) == 0 && id > 1 {
				deps = []int{1 + rng.IntN(id-1)}
			}
			tasks = append(tasks, planTask(id, state, deps))
		}
		setTasks(t, dir, tasks...)
		files := snapshot(t, root)

		var named []int
		for _, id := range rng.Perm(n)[:1+rng.IntN(n)] {
			named = append(named, id+1)
		}
		target := wantTaskStates[rng.IntN(len(wantTaskStates))]
		taskIDs := make([]string, len(named))
		for i, id := range named {
			taskIDs[i] = fmt.Sprint(id)
		}
		argv := []string{"--dir", root, "09", "--task", strings.Join(taskIDs, ","), "--state", target}
		if rng.IntN(2) == 0 {
			argv = append(argv, "--force")
		}

		if _, code, _ := run(t, argv...); code != cli.ExitOK {
			if !maps.Equal(files, snapshot(t, root)) {
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
	}
}

// A plan with a task id twice is invalid, and still takes a change: the library
// addresses a task by id and finds the first, so the id is moved once.
func TestTask_ADuplicateTaskIDIsMovedOnce(t *testing.T) {
	duplicated := []map[string]any{planTask(1, "pending", nil), planTask(2, "pending", []int{}), planTask(1, "pending", []int{})}
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	setTasks(t, dir, duplicated...)

	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress")
	if code != cli.ExitOK || !slices.Equal(changed(result(t, env)), []int{1}) {
		t.Fatalf("code %d: changes %+v, error %+v", code, result(t, env).Changes, env.Error)
	}
	// onDisk maps by id, so the second task 1 would hide the first: read the plan.
	var states []string
	for _, task := range loadSpec(t, dir).Tasks.Tasks {
		states = append(states, string(task.State))
	}
	if want := []string{"in_progress", "pending", "pending"}; !slices.Equal(states, want) {
		t.Errorf("states on disk %v; want %v: the first task 1 moves, the other does not", states, want)
	}

	other := newRepo(t)
	setTasks(t, pkgDir(other, "09_agent_mode"), duplicated...)
	env, code, _ = run(t, "--dir", other, "09", "--task", "all", "--state", "in_progress")
	if code != cli.ExitOK || !slices.Equal(changed(result(t, env)), []int{1, 2}) {
		t.Errorf("all: code %d: changes %+v, error %+v", code, result(t, env).Changes, env.Error)
	}
}

// After a move, next is what the report would say of the package now: the
// render of a claimed task, the claim of the next one, and nothing when there
// is no task left.
func TestTask_NextAfterAMove(t *testing.T) {
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
	if len(env.Next) != 1 || !sameCall(env.Next[0], envelope.Next{Tool: "specr", Input: dir, Flags: []string{"--task", "1"}}) {
		t.Errorf("after the claim: next = %+v; want the render of task 1", env.Next)
	}
	env = step("1", "done")
	if len(env.Next) == 0 || !sameCall(env.Next[0], claim(dir, 2)) {
		t.Errorf("after task 1 is done: next = %+v; want the claim of task 2 first", env.Next)
	}
	step("2", "in_progress")
	if env = step("2", "done"); len(env.Next) != 0 {
		t.Errorf("after the last task: next = %+v; want none", env.Next)
	}
}

// A save re-encodes every artifact from the Go types, which drops the keys they
// lack: a write that would delete keys the schema does not allow, in the JSON
// artifacts or in the frontmatter of prd.md (which the library does not report),
// is refused at stage validate. --force writes over them, with a high warning
// naming them, and the package it writes holds none and validates.
func TestTask_UnknownKeys(t *testing.T) {
	for name, tc := range map[string]struct {
		add   func(t *testing.T, dir string)
		file  string
		key   string
		valid bool
	}{
		"in tasks.json":      {addUnknownKey, "tasks.json", "traceability", false},
		"in the frontmatter": {withFrontmatterKey, "prd.md", "badkey", true},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			dir := pkgDir(root, "09_agent_mode")
			tc.add(t, dir)
			argv := []string{"09", "--task", "1", "--state", "in_progress"}

			refused := refusedWithReport(t, root, "validate", "invalid_spec", argv...)
			saysAll(t, argv, refused.Error.Message, tc.file, tc.key, "delete", "--force")
			if refused.Error.FixHint != nil || len(refused.Warnings) != 0 {
				t.Errorf("fix_hint %+v, warnings %v; want neither", refused.Error.FixHint, warningCodes(refused))
			}
			// The library reports a key of the JSON artifacts, not one of the
			// frontmatter, so only the first leaves the package invalid.
			if r := refused.Result; r.Tasks[0].State != "pending" || *r.Valid != tc.valid {
				t.Errorf("result %+v; want the package on disk, valid %v", r, tc.valid)
			}

			env, r := succeeds(t, root, append(argv, "--force")...)
			if onDisk(t, dir)[1] != "in_progress" {
				t.Errorf("on disk %v; want task 1 in progress", onDisk(t, dir))
			}
			if w := oneWarning(t, env, "spec_invalid", "high"); w.Stage != "validate" || !strings.Contains(w.Message, tc.file) || !strings.Contains(w.Message, tc.key) {
				t.Errorf("warning %+v; want one at stage validate naming %s %s", w, tc.file, tc.key)
			}
			if b, _ := os.ReadFile(filepath.Join(dir, tc.file)); strings.Contains(string(b), tc.key) {
				t.Errorf("%s still holds %s", tc.file, tc.key)
			}
			if r.Valid == nil || !*r.Valid || len(r.FailingRules) != 0 {
				t.Errorf("valid %v failing_rules %v; want the package as written, which validates", r.Valid, r.FailingRules)
			}
		})
	}
}

// The report a forced write returns is of the package as it was written: it
// validates as a fresh report of the package does, and the rules that the
// unknown keys had hidden (the library checks the cross-file rules only after
// the schema) are reported. A dry run says the same.
func TestTask_TheResultOfAForcedWriteValidatesAsTheWrittenPackageDoes(t *testing.T) {
	for name, damage := range map[string]func(t *testing.T, dir string){
		"only the unknown key":         func(*testing.T, string) {},
		"and a broken cross-file rule": breakValidity,
	} {
		for _, dry := range []bool{false, true} {
			root := newRepo(t)
			dir := pkgDir(root, "09_agent_mode")
			addUnknownKey(t, dir)
			damage(t, dir)
			argv := []string{"--dir", root, "09", "--task", "1", "--state", "in_progress", "--force"}
			if dry {
				argv = append(argv, "--dry-run")
			}
			env, code, _ := run(t, argv...)
			r := result(t, env)
			if code != cli.ExitOK || r.Valid == nil {
				t.Fatalf("%s, dry run %v: code %d: %+v", name, dry, code, env.Error)
			}
			if dry {
				// Write it for real, to see what the dry run should have said.
				if _, code, _ = run(t, argv[:len(argv)-1]...); code != cli.ExitOK {
					t.Fatalf("%s: the real call: code %d", name, code)
				}
			}
			fresh, _, _ := run(t, "--dir", root, "09")
			f := result(t, fresh)
			if *r.Valid != *f.Valid || !slices.Equal(r.FailingRules, f.FailingRules) {
				t.Errorf("%s, dry run %v: the call said valid %v %v; the package, read again, is valid %v %v",
					name, dry, *r.Valid, r.FailingRules, *f.Valid, f.FailingRules)
			}
			if slices.Contains(r.FailingRules, "unknown_field") {
				t.Errorf("%s, dry run %v: failing_rules %v names keys the write removed", name, dry, r.FailingRules)
			}
		}
	}
}

// A package that does not validate still takes the change, with one low
// warning that names the rules it breaks.
func TestTask_AnInvalidPackageStillTakesTheChange(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	setTasks(t, dir, planTask(1, "pending", nil), planTask(2, "pending", []int{99}))
	errs, _ := ownChecks(t, dir)
	if len(errs) == 0 {
		t.Fatal("the fixture validates")
	}
	env, _ := succeeds(t, root, "09", "--task", "1", "--state", "in_progress")
	if onDisk(t, dir)[1] != "in_progress" {
		t.Errorf("on disk %v", onDisk(t, dir))
	}
	w := oneWarning(t, env, "spec_invalid", "low")
	if w.Stage != "validate" {
		t.Errorf("warning stage %q; want validate", w.Stage)
	}
	saysAll(t, nil, w.Message, errs...)
}

// Whatever is wrong with a package, a call raises at most one spec_invalid
// warning.
func TestTask_RaisesAtMostOneSpecInvalidWarning(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 36))
	for range 40 {
		root := newRepo(t)
		dir := pkgDir(root, "09_agent_mode")
		if rng.IntN(2) == 0 {
			breakValidity(t, dir)
		}
		if rng.IntN(2) == 0 {
			editJSON(t, filepath.Join(dir, "tasks.json"), func(m map[string]any) {
				m["tasks"].([]any)[1].(map[string]any)["depends_on"] = []any{99}
			})
		}
		if rng.IntN(2) == 0 {
			addUnknownKey(t, dir)
		}
		if rng.IntN(3) == 0 {
			editJSON(t, filepath.Join(dir, "requirements.json"), func(m map[string]any) { m["leaked"] = 1 })
		}
		argv := []string{"--dir", root, "09", "--task", []string{"1", "next", "all"}[rng.IntN(3)], "--state", []string{"in_progress", "done"}[rng.IntN(2)]}
		if rng.IntN(2) == 0 {
			argv = append(argv, "--force")
		}
		env, _, _ := run(t, argv...)
		if n := len(warningsOf(env, "spec_invalid")); n > 1 {
			t.Fatalf("%v: %d spec_invalid warnings: %+v", argv, n, env.Warnings)
		}
	}
}

// A call that is refused, or whose write fails, raises none of the warnings of
// the moves it did not make.
func TestTask_ARefusedCallRaisesNoWarning(t *testing.T) {
	// A claim of a task that is not ready, beside a claim of one that is taken.
	taken := newRepo(t)
	setTasks(t, pkgDir(taken, "09_agent_mode"), planTask(1, "pending", nil), planTask(2, "pending", []int{1}), planTask(3, "in_progress", []int{}))
	env, code, _ := run(t, "--dir", taken, "09", "--task", "2,3", "--state", "in_progress")
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "task" || len(env.Warnings) != 0 {
		t.Errorf("a refused move: code %d, error %+v, warnings %v; want none", code, env.Error, warningCodes(env))
	}

	// The same claim, refused for the unknown keys the write would delete.
	keys := newRepo(t)
	setTasks(t, pkgDir(keys, "09_agent_mode"), planTask(1, "pending", nil), planTask(2, "pending", []int{1}))
	addUnknownKey(t, pkgDir(keys, "09_agent_mode"))
	env, code, _ = run(t, "--dir", keys, "09", "--task", "2", "--state", "in_progress")
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "validate" || len(env.Warnings) != 0 {
		t.Errorf("unknown keys: code %d, error %+v, warnings %v; want none", code, env.Error, warningCodes(env))
	}

	// A move set through a bypass, whose write then fails.
	failWith(t, &saveSpec, func(*afspec.Spec, string) error { return &afspec.SaveError{Msg: "cannot rename tasks.json"} })
	failed := newRepo(t)
	setTasks(t, pkgDir(failed, "09_agent_mode"), planTask(1, "dropped", nil))
	env, code, _ = run(t, "--dir", failed, "09", "--task", "1", "--state", "pending", "--force")
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "write" || len(env.Warnings) != 0 {
		t.Errorf("a failed write: code %d, error %+v, warnings %v; want none", code, env.Error, warningCodes(env))
	}
}

// What Save returns is reported by what it says: a lifecycle or intent error as
// the status refusal it is, a save error as a write failure that tells the
// caller what may be left.
func TestTask_SaveErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		err             error
		stage, category string
		want            []string
	}{
		"lifecycle": {&afspec.LifecycleError{Msg: "cannot save spec in sealed state"}, "status", "lifecycle", nil},
		"intent":    {&afspec.IntentError{Msg: "intent drift detected"}, "status", "lifecycle", nil},
		"save": {&afspec.SaveError{Msg: "cannot rename tasks.json"}, "write", "disk",
			[]string{"temporary files", "unchanged", "final renames", "some files updated", "`speca .specs/09_agent_mode`"}},
	} {
		t.Run(name, func(t *testing.T) {
			failWith(t, &saveSpec, func(*afspec.Spec, string) error { return tc.err })
			root := newRepo(t)
			env := refusedWithReport(t, root, tc.stage, tc.category, "09", "--task", "1", "--state", "in_progress")
			saysAll(t, nil, env.Error.Message, append([]string{tc.err.Error()}, tc.want...)...)
		})
	}
}

// A change sets updated_at to now, in UTC, in RFC 3339, every time, and the
// package is saved with it.
func TestTask_SetsUpdatedAtOnEveryWrite(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	for _, state := range []string{"in_progress", "done"} {
		// The second call finds an old updated_at, and must leave a new one.
		editFile(t, filepath.Join(dir, "prd.md"), `updated_at: "`+loadSpec(t, dir).UpdatedAt+`"`, `updated_at: "2000-01-01T00:00:00Z"`)
		start := time.Now().UTC().Truncate(time.Second)
		env, r := succeeds(t, root, "09", "--task", "1", "--state", state)
		got := updatedAtIn(t, dir)
		if got.Before(start) || got.After(time.Now().UTC()) {
			t.Errorf("to %s: updated_at %s; want the time of the call, from %s", state, got.Format(time.RFC3339), start.Format(time.RFC3339))
		}
		if onDisk(t, dir)[1] != state || r.UpdatedAt != loadSpec(t, dir).UpdatedAt {
			t.Errorf("to %s: on disk %v, result updated_at %q; want the state and the file's time (%+v)", state, onDisk(t, dir), r.UpdatedAt, env.Error)
		}
	}
}

// A write goes through Spec.Save, which writes every artifact again: each file
// is replaced by a temporary one renamed over it, so it has that one's mode, and
// the content of the artifacts the call did not change is the content it had.
func TestTask_ASaveRewritesEveryArtifactAtMode0600(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	files := []string{"prd.md", "requirements.json", "test_spec.json", "tasks.json"}
	var before []os.FileInfo
	for _, f := range files {
		if err := os.Chmod(filepath.Join(dir, f), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		before = append(before, info)
	}
	was := loadSpec(t, dir)

	succeeds(t, root, "09", "--task", "1", "--state", "in_progress")

	for i, f := range files {
		info, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		if os.SameFile(before[i], info) {
			t.Errorf("%s was not replaced", f)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Errorf("%s has mode %v; want 0600, the temporary file's", f, info.Mode().Perm())
		}
	}
	now := loadSpec(t, dir)
	if !reflect.DeepEqual(was.Requirements, now.Requirements) || !reflect.DeepEqual(was.TestSpec, now.TestSpec) {
		t.Error("requirements.json or test_spec.json holds other content than before the write")
	}
}

// --dry-run runs every check and writes nothing, updated_at included, and
// returns the result the call would have produced, which the real call then
// produces.
func TestTask_ADryRunWritesNothingAndReportsTheChange(t *testing.T) {
	root := newRepo(t)
	before := snapshot(t, root)
	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress", "--dry-run")
	r := result(t, env)
	if code != cli.ExitOK || !r.DryRun || len(r.Changes) != 1 || r.Changes[0].ID != 1 || r.Changes[0].To != "in_progress" {
		t.Fatalf("code %d: dry_run %v, changes %+v", code, r.DryRun, r.Changes)
	}
	if after := snapshot(t, root); !maps.Equal(before, after) {
		t.Errorf("the repository changed:\nbefore %v\nafter  %v", before, after)
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
	real, _, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress")
	if !slices.Equal(result(t, real).Changes, r.Changes) || result(t, real).DryRun {
		t.Errorf("the real call changed %+v; the dry run said %+v", result(t, real).Changes, r.Changes)
	}
}

// A dry run is refused exactly as the real call is.
func TestTask_ADryRunIsRefusedAsTheRealCallIs(t *testing.T) {
	taken := newRepo(t)
	setTasks(t, pkgDir(taken, "09_agent_mode"), planTask(1, "in_progress", nil), planTask(2, "pending", []int{1}))
	unknown := newRepo(t)
	addUnknownKey(t, pkgDir(unknown, "09_agent_mode"))

	for name, root := range map[string]string{"a taken task": taken, "unknown keys": unknown} {
		base := []string{"--dir", root, "09", "--task", "1", "--state", "in_progress"}
		real, realCode, _ := run(t, base...)
		dry, dryCode, _ := run(t, append(slices.Clone(base), "--dry-run")...)
		if realCode == cli.ExitOK || dryCode != realCode || real.Error == nil || dry.Error == nil {
			t.Errorf("%s: real exit %d, dry exit %d", name, realCode, dryCode)
			continue
		}
		if a, b := real.Error, dry.Error; !reflect.DeepEqual(a, b) {
			t.Errorf("%s: the dry run was refused as %+v; the real call as %+v", name, b, a)
		}
	}
}

// A dry run suggests the one call that makes the changes, naming the ids it
// resolved and keeping --force, and the call it suggests does what it said;
// with nothing to change it suggests nothing.
func TestTask_ADryRunSuggestsTheRealCall(t *testing.T) {
	const dir = ".specs/09_agent_mode"
	for name, tc := range map[string]struct {
		tasks []map[string]any
		argv  []string
		want  envelope.Next
	}{
		"all, resolved to ids": {[]map[string]any{planTask(1, "pending", []int{}), planTask(2, "pending", []int{})},
			[]string{"--task", "all", "--state", "in_progress", "--force"},
			envelope.Next{Tool: "speca", Input: dir, Flags: []string{"--task", "1,2", "--state", "in_progress", "--force"}}},
		"a forced recovery of a dropped task": {[]map[string]any{planTask(1, "done", nil), planTask(2, "done", []int{1}), planTask(3, "dropped", nil)},
			[]string{"--task", "3", "--state", "pending", "--force"},
			envelope.Next{Tool: "speca", Input: dir, Flags: []string{"--task", "3", "--state", "pending", "--force"}}},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			setTasks(t, pkgDir(root, "09_agent_mode"), tc.tasks...)
			dry, r := succeeds(t, root, append([]string{"09", "--dry-run"}, tc.argv...)...)
			if len(dry.Next) != 1 || !sameCall(dry.Next[0], tc.want) || dry.Next[0].Why == "" {
				t.Fatalf("next = %+v; want one entry, %+v", dry.Next, tc.want)
			}
			real, code := followed(t, root, dry.Next[0])
			if code != cli.ExitOK || result(t, real).DryRun || !slices.Equal(result(t, real).Changes, r.Changes) ||
				!slices.Equal(warningCodes(real), warningCodes(dry)) {
				t.Errorf("the suggested call: code %d, changes %+v, warnings %v; the dry run said %+v, %v",
					code, result(t, real).Changes, warningCodes(real), r.Changes, warningCodes(dry))
			}
			for _, c := range r.Changes {
				if onDisk(t, pkgDir(root, "09_agent_mode"))[c.ID] != c.To {
					t.Errorf("task %d is not %s on disk", c.ID, c.To)
				}
			}
		})
	}

	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"), planTask(1, "done", nil), planTask(2, "dropped", nil))
	if env, _ := succeeds(t, root, "09", "--task", "all", "--state", "done", "--dry-run"); len(env.Next) != 0 {
		t.Errorf("no changes: next = %+v; want none", env.Next)
	}
}

// unknownKeys reads the key out of the library's message, and falls back to the
// whole message when it cannot.
func TestUnknownKeys_NamesTheKeysThatWouldBeLost(t *testing.T) {
	entries := []afspec.ValidationEntry{
		{Check: "unknown_field", Artifact: "tasks.json", Message: "tasks.json: the key /traceability is not allowed by the schema (additionalProperties: false); remove it"},
		{Check: "unknown_field", Artifact: "tasks.json", Message: "tasks.json: the key /tasks/0/a b is not allowed by the schema (additionalProperties: false); remove it"},
		{Check: "unknown_field", Artifact: "test_spec.json", Message: "a message in other words"},
		{Check: "C7", Artifact: "test_spec.json", Message: "the key /x is not allowed by the schema"},
	}
	want := []string{"tasks.json /traceability", "tasks.json /tasks/0/a b", "a message in other words"}
	if got := unknownKeys(entries); !slices.Equal(got, want) {
		t.Errorf("unknownKeys = %q; want %q", got, want)
	}
}
