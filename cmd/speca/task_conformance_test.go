package main

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// These tests close what a review of the finished form found.

// The report a write returns is the package as it was written. A --force that
// deletes unknown keys leaves a package without them, so it validates as a fresh
// report of the package does; and the rules that the unknown keys had hidden (the
// library checks the cross-file rules only after the schema) are reported.
func TestTheResultOfAForcedWriteValidatesAsTheWrittenPackageDoes(t *testing.T) {
	for name, damage := range map[string]func(t *testing.T, dir string){
		"only the unknown key": func(t *testing.T, dir string) {},
		"and a broken cross-file rule": func(t *testing.T, dir string) {
			editFile(t, filepath.Join(dir, "tasks.json"), `"TS-09-6"`, `"TS-09-99"`)
		},
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

// The frontmatter of prd.md is re-encoded from its fields by Save, so a key it
// has no field for is lost with the write. The library does not report such a
// key, so the guard of 11-REQ-6 looks for it itself.
func TestUnknownFrontmatterKeysRefuseTheWrite(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	editFile(t, filepath.Join(dir, "prd.md"), "schema_version: 2", "schema_version: 2\nmy_custom_key: \"keepme\"")
	before := snapshot(t, root)

	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress")
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "validate" || env.Error.Category != "invalid_spec" {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	for _, want := range []string{"prd.md", "my_custom_key"} {
		if !strings.Contains(env.Error.Message, want) {
			t.Errorf("message %q does not say %q", env.Error.Message, want)
		}
	}
	if !mapsEqual(before, snapshot(t, root)) {
		t.Error("the repository changed")
	}

	env, code, _ = run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress", "--force")
	ws := warningsOf(env, "spec_invalid")
	if code != cli.ExitOK || len(ws) != 1 || ws[0].Severity != "high" || !strings.Contains(ws[0].Message, "my_custom_key") {
		t.Errorf("--force: code %d, warnings %+v; want one high spec_invalid naming the key", code, env.Warnings)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "prd.md")); strings.Contains(string(b), "my_custom_key") {
		t.Error("the key is still in prd.md")
	}
}

// unknownKeys reads the key out of the library's message, and falls back to the
// whole message when it cannot.
func TestUnknownKeysNamesTheKeysThatWouldBeLost(t *testing.T) {
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

// A plan with a task id twice is invalid, and still takes a change: the library
// addresses a task by id and finds the first, so the id is moved once.
func TestADuplicateTaskIDIsMovedOnce(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	setTasks(t, dir, planTask(1, "pending", nil), planTask(2, "pending", []int{}), planTask(1, "pending", []int{}))

	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress")
	if code != cli.ExitOK || !slices.Equal(changed(result(t, env)), []int{1}) {
		t.Fatalf("code %d: changes %+v, error %+v", code, result(t, env).Changes, env.Error)
	}
	// onDisk maps by id, so the second task 1 would hide the first: read the plan.
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	var states []string
	for _, task := range spec.Tasks.Tasks {
		states = append(states, string(task.State))
	}
	if want := []string{"in_progress", "pending", "pending"}; !slices.Equal(states, want) {
		t.Errorf("states on disk %v; want %v: the first task 1 moves, the other does not", states, want)
	}

	other := newRepo(t)
	setTasks(t, pkgDir(other, "09_agent_mode"), planTask(1, "pending", nil), planTask(2, "pending", []int{}), planTask(1, "pending", []int{}))
	env, code, _ = run(t, "--dir", other, "09", "--task", "all", "--state", "in_progress")
	if code != cli.ExitOK || !slices.Equal(changed(result(t, env)), []int{1, 2}) {
		t.Errorf("all: code %d: changes %+v, error %+v", code, result(t, env).Changes, env.Error)
	}
}

// A call that is refused, or whose write fails, raises none of the warnings of
// the moves it did not make.
func TestARefusedCallRaisesNoWarning(t *testing.T) {
	// A claim of a task that is not ready, beside a claim of one that is taken.
	taken := newRepo(t)
	setTasks(t, pkgDir(taken, "09_agent_mode"), planTask(1, "pending", nil), planTask(2, "pending", []int{1}), planTask(3, "in_progress", []int{}))
	env, code, _ := run(t, "--dir", taken, "09", "--task", "2,3", "--state", "in_progress")
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "task" || len(warningsOf(env, "task_not_ready")) != 0 {
		t.Errorf("a refused move: code %d, error %+v, warnings %v; want no task_not_ready", code, env.Error, warningCodes(env))
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
	was := saveSpec
	t.Cleanup(func() { saveSpec = was })
	saveSpec = func(*afspec.Spec, string) error { return &afspec.SaveError{Msg: "cannot rename tasks.json"} }
	failed := newRepo(t)
	setTasks(t, pkgDir(failed, "09_agent_mode"), planTask(1, "dropped", nil))
	env, code, _ = run(t, "--dir", failed, "09", "--task", "1", "--state", "pending", "--force")
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "write" || len(warningsOf(env, "task_state_forced")) != 0 {
		t.Errorf("a failed write: code %d, error %+v, warnings %v; want no task_state_forced", code, env.Error, warningCodes(env))
	}
}

// The unknown-key guard is about a write. A call that resolves to no move writes
// nothing, deletes no key, and so neither refuses nor warns.
func TestNothingToMoveIgnoresUnknownKeys(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	setTasks(t, dir, planTask(1, "done", nil), planTask(2, "dropped", nil))
	addUnknownKey(t, dir)
	before := snapshot(t, root)
	env, code, _ := run(t, "--dir", root, "09", "--task", "all", "--state", "done")
	if code != cli.ExitOK || len(result(t, env).Changes) != 0 || len(env.Warnings) != 0 {
		t.Errorf("code %d: changes %+v, warnings %v, error %+v", code, result(t, env).Changes, warningCodes(env), env.Error)
	}
	if !mapsEqual(before, snapshot(t, root)) {
		t.Error("the repository changed")
	}
}

// The shell adds a clause to the sentence of any success that raised a high
// warning; the sentence of the task form is Result.Summary().
func TestTheSummaryOfAForcedMoveCarriesTheShellsClause(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"), planTask(1, "done", nil), planTask(2, "dropped", nil))
	env, code, _ := run(t, "--dir", root, "09", "--task", "2", "--state", "pending", "--force")
	if want := "speca: 09_agent_mode task 2 dropped → pending; 1 high-severity warning"; code != cli.ExitOK || env.Summary != want {
		t.Errorf("code %d: summary %q; want %q", code, env.Summary, want)
	}
}

// --task next on a plan with no tasks says there are none, not that all are done.
func TestNextOnAPlanWithNoTasks(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"))
	e := refusedAsUsage(t, root, []string{"--dir", root, "09", "--task", "next", "--state", "in_progress"}, "09_agent_mode", "no tasks")
	if e != nil && strings.Contains(e.Message, "done or dropped") {
		t.Errorf("message %q says every task is done or dropped, of a plan that has none", e.Message)
	}
}

// The usage errors of one call come in the same order every time.
func TestUsageMessagesAreInAFixedOrder(t *testing.T) {
	for _, argv := range [][]string{
		{"09", "--force", "--dry-run"},
		{"09", "--validate", "--list", "--task", "1", "--state", "done"},
	} {
		var first string
		for i := range 12 {
			env, _, _ := run(t, argv...)
			if env.Error == nil {
				t.Fatalf("%v: no error", argv)
			}
			if i == 0 {
				first = env.Error.Message
			} else if env.Error.Message != first {
				t.Fatalf("%v: the message changed between runs:\n%q\n%q", argv, first, env.Error.Message)
			}
		}
		if argv[1] == "--force" && strings.Index(first, "--force") > strings.Index(first, "--dry-run") {
			t.Errorf("message %q names --dry-run before --force", first)
		}
	}
}

// The write sets updated_at every time, not only the first: the second call finds
// an old one, and leaves a new one.
func TestEveryWriteSetsUpdatedAt(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	if _, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress"); code != cli.ExitOK {
		t.Fatal("the claim failed")
	}
	editFile(t, filepath.Join(dir, "prd.md"), "updated_at: \""+updatedAtOf(t, dir)+"\"", `updated_at: "2000-01-01T00:00:00Z"`)
	t0 := time.Now().UTC().Truncate(time.Second)
	if _, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "done"); code != cli.ExitOK {
		t.Fatal("the move to done failed")
	}
	ts, err := time.Parse(time.RFC3339, updatedAtOf(t, dir))
	if err != nil || ts.Before(t0) {
		t.Errorf("updated_at = %v (%v); want a time from this call, after %s", ts, err, t0.Format(time.RFC3339))
	}
}

func updatedAtOf(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "prd.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := updatedAt.FindStringSubmatch(string(b))
	if m == nil {
		t.Fatalf("prd.md has no updated_at:\n%s", b)
	}
	return m[1]
}

// A write goes through Spec.Save, which writes every artifact again: each file is
// replaced by a temporary one renamed over it, so it has that one's mode, and the
// content of the artifacts the call did not change is the content it had. The
// documentation says this, and so does this test.
func TestASaveRewritesEveryArtifactAtMode0600(t *testing.T) {
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
	was, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}

	if _, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress"); code != cli.ExitOK {
		t.Fatalf("code %d", code)
	}

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
	now, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(was.Requirements, now.Requirements) || !reflect.DeepEqual(was.TestSpec, now.TestSpec) {
		t.Error("requirements.json or test_spec.json holds other content than before the write")
	}
}
