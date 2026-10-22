package main

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// warningsOf lists the warnings of the code.
func warningsOf(env envelope.AdminEnvelope, code string) []envelope.Warning {
	var out []envelope.Warning
	for _, w := range env.Warnings {
		if string(w.Code) == code {
			out = append(out, w)
		}
	}
	return out
}

func changeFor(r *envelope.AdminResult, id int) *envelope.Change {
	for i := range r.Changes {
		if r.Changes[i].ID == id {
			return &r.Changes[i]
		}
	}
	return nil
}

// TS-11-27: --force makes a move the machine refuses with the library's
// bypasses and a move it allows with the machine.
func TestForceUsesTheBypassesOnlyForRefusedMoves_TS11_27(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	setTasks(t, dir, planTask(1, "pending", nil), planTask(2, "dropped", nil))
	env, code, _ := run(t, "--dir", root, "09", "--task", "1,2", "--state", "pending", "--force")
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	if got := onDisk(t, dir); got[1] != "pending" || got[2] != "pending" {
		t.Errorf("on disk: %v; want task 2 reset to pending", got)
	}
	if c := changeFor(result(t, env), 2); c == nil || c.From != "dropped" || c.To != "pending" || !c.Forced {
		t.Errorf("change of task 2 = %+v; want dropped to pending, forced", c)
	}

	other := newRepo(t)
	odir := pkgDir(other, "09_agent_mode")
	setTasks(t, odir, planTask(3, "pending", nil), planTask(4, "in_progress", []int{3}))
	env, code, _ = run(t, "--dir", other, "09", "--task", "3,4", "--state", "done", "--force")
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	if got := onDisk(t, odir); got[3] != "done" || got[4] != "done" {
		t.Errorf("on disk: %v", got)
	}
	r := result(t, env)
	if c := changeFor(r, 3); c == nil || !c.Forced || c.From != "pending" {
		t.Errorf("change of task 3 = %+v; want pending to done, through the bypass", c)
	}
	if c := changeFor(r, 4); c == nil || c.Forced || c.From != "in_progress" {
		t.Errorf("change of task 4 = %+v; want in_progress to done, an ordinary move", c)
	}
}

// TS-11-28: each task set through a bypass has its own high warning and a forced
// change; the one the machine allowed or left alone has neither.
func TestEachForcedTaskHasAWarning_TS11_28(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"), planTask(1, "pending", nil), planTask(2, "dropped", nil), planTask(3, "dropped", nil))
	env, code, _ := run(t, "--dir", root, "09", "--task", "1,2,3", "--state", "pending", "--force")
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	ws := warningsOf(env, "task_state_forced")
	if len(ws) != 2 {
		t.Fatalf("warnings = %+v; want one for task 2 and one for task 3", env.Warnings)
	}
	for i, w := range ws {
		task := []string{"task 2", "task 3"}[i]
		if w.Severity != "high" || w.Stage != "task" || !strings.Contains(w.Message, task) ||
			!strings.Contains(w.Message, "dropped") || !strings.Contains(w.Message, "pending") {
			t.Errorf("warning %d = %+v; want a high one at stage task naming %s, dropped and pending", i, w, task)
		}
	}
	r := result(t, env)
	for _, id := range []int{2, 3} {
		if c := changeFor(r, id); c == nil || !c.Forced {
			t.Errorf("change of task %d = %+v; want forced", id, c)
		}
	}
	if changeFor(r, 1) != nil {
		t.Error("task 1 was already pending: it has no change")
	}
}

// TS-11-29: --force on a task already in the target state changes nothing and
// says nothing.
func TestForceOnATaskAlreadyThereIsQuiet_TS11_29(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"), planTask(1, "done", nil))
	before := snapshot(t, root)
	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "done", "--force")
	if code != cli.ExitOK || len(result(t, env).Changes) != 0 || len(warningsOf(env, "task_state_forced")) != 0 {
		t.Errorf("code %d: changes %+v warnings %v", code, result(t, env).Changes, warningCodes(env))
	}
	if !mapsEqual(before, snapshot(t, root)) {
		t.Error("the repository changed")
	}
}

// TS-11-30: --force cannot set in_progress or dropped: the library has no setter
// for them, and the command writes no state by hand.
func TestForceCannotSetInProgressOrDropped_TS11_30(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"), planTask(1, "done", nil), planTask(2, "dropped", nil))
	for _, argv := range [][]string{
		{"--dir", root, "09", "--task", "1", "--state", "in_progress", "--force"},
		{"--dir", root, "09", "--task", "2", "--state", "dropped", "--force"},
	} {
		before := snapshot(t, root)
		env, code, _ := run(t, argv...)
		if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "task" || env.Error.Category != "lifecycle" {
			t.Errorf("%v: code %d: %+v", argv, code, env.Error)
			continue
		}
		if !strings.Contains(env.Error.Message, "cannot set in_progress or dropped") {
			t.Errorf("%v: message %q does not say --force cannot set in_progress or dropped", argv, env.Error.Message)
		}
		if !mapsEqual(before, snapshot(t, root)) {
			t.Errorf("%v: the repository changed", argv)
		}
	}
}

// TS-11-31: --force skips no usage error, no status refusal and no
// legacy-version refusal.
func TestForceSkipsNoOtherRefusal_TS11_31(t *testing.T) {
	root := newRepo(t)
	sealed := addPackage(t, root, "11_sealed", "11", "sealed")
	setStatus(t, sealed, "sealed")
	addLegacyPackage(t, root)
	before := snapshot(t, root)

	for name, tc := range map[string]struct {
		argv  []string
		code  int
		stage string
		says  string
	}{
		"an unknown id":    {[]string{"09", "--task", "99", "--state", "done", "--force"}, cli.ExitUsage, "usage", "has no task 99"},
		"a sealed package": {[]string{"11", "--task", "1", "--state", "done", "--force"}, cli.ExitFailed, "status", "no longer change"},
		"a v1 package":     {[]string{"07", "--task", "1", "--state", "done", "--force"}, cli.ExitUsage, "usage", "writes format version 2 only"},
	} {
		env, code, _ := run(t, append([]string{"--dir", root}, tc.argv...)...)
		if code != tc.code || env.Error == nil || env.Error.Stage != tc.stage || !strings.Contains(env.Error.Message, tc.says) {
			t.Errorf("%s: code %d: %+v; want %d at %s, saying %q", name, code, env.Error, tc.code, tc.stage, tc.says)
		}
	}
	if !mapsEqual(before, snapshot(t, root)) {
		t.Error("the repository changed")
	}
}

// TS-11-32: a claim of a task whose dependencies are not done goes ahead, with
// a low warning that names them. Readiness is judged on the plan before the call.
func TestClaimingATaskThatIsNotReady_TS11_32(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	setTasks(t, dir, planTask(1, "pending", nil), planTask(2, "pending", []int{1}))
	env, code, _ := run(t, "--dir", root, "09", "--task", "2", "--state", "in_progress")
	if code != cli.ExitOK || onDisk(t, dir)[2] != "in_progress" {
		t.Fatalf("code %d: %+v; on disk %v", code, env.Error, onDisk(t, dir))
	}
	ws := warningsOf(env, "task_not_ready")
	if len(ws) != 1 || ws[0].Severity != "low" || ws[0].Stage != "task" ||
		!strings.Contains(ws[0].Message, "task 2") || !strings.Contains(ws[0].Message, "task 1") {
		t.Errorf("warnings = %+v; want one low task_not_ready naming task 2 and its dependency 1", env.Warnings)
	}

	// Without depends_on a task follows the one before it.
	implicit := newRepo(t)
	setTasks(t, pkgDir(implicit, "09_agent_mode"), planTask(1, "pending", nil), planTask(2, "pending", nil))
	env, _, _ = run(t, "--dir", implicit, "09", "--task", "2", "--state", "in_progress")
	if ws := warningsOf(env, "task_not_ready"); len(ws) != 1 || !strings.Contains(ws[0].Message, "task 1") {
		t.Errorf("implicit dependency: warnings = %+v", env.Warnings)
	}

	// Two tasks claimed together: the second depends on the first, and is not ready
	// on the plan as it was before the call.
	together := newRepo(t)
	setTasks(t, pkgDir(together, "09_agent_mode"), planTask(1, "pending", nil), planTask(2, "pending", []int{1}))
	env, _, _ = run(t, "--dir", together, "09", "--task", "1,2", "--state", "in_progress")
	if ws := warningsOf(env, "task_not_ready"); len(ws) != 1 || !strings.Contains(ws[0].Message, "task 2") {
		t.Errorf("claimed together: warnings = %+v; want one, for task 2", env.Warnings)
	}

	// A ready task, and a move to any other state, raise none.
	ready := newRepo(t)
	env, _, _ = run(t, "--dir", ready, "09", "--task", "1", "--state", "in_progress")
	if len(warningsOf(env, "task_not_ready")) != 0 {
		t.Errorf("a ready task: warnings = %+v", env.Warnings)
	}
}

// TS-11-33: a package that does not validate still takes the change, with one
// low warning that names the rules it breaks.
func TestAnInvalidPackageStillTakesTheChange_TS11_33(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	setTasks(t, dir, planTask(1, "pending", nil), planTask(2, "pending", []int{99}))
	errs, _ := ownChecks(t, dir)
	if len(errs) == 0 {
		t.Fatal("the fixture validates")
	}
	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress")
	if code != cli.ExitOK || onDisk(t, dir)[1] != "in_progress" {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	ws := warningsOf(env, "spec_invalid")
	if len(ws) != 1 || ws[0].Severity != "low" || ws[0].Stage != "validate" {
		t.Fatalf("warnings = %+v; want one low spec_invalid at stage validate", env.Warnings)
	}
	for _, rule := range errs {
		if !strings.Contains(ws[0].Message, rule) {
			t.Errorf("warning %q does not name the rule %s", ws[0].Message, rule)
		}
	}
}

// addUnknownKey gives tasks.json a key the schema does not allow, as a leaked
// version 1 traceability or a newer format's field would be.
func addUnknownKey(t *testing.T, dir string) {
	t.Helper()
	editJSON(t, filepath.Join(dir, "tasks.json"), func(m map[string]any) { m["traceability"] = []any{} })
}

// TS-11-34: keys a save would delete refuse the call at stage validate, and the
// package is left as it was.
func TestUnknownKeysRefuseTheWrite_TS11_34(t *testing.T) {
	root := newRepo(t)
	addUnknownKey(t, pkgDir(root, "09_agent_mode"))
	before := snapshot(t, root)
	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress")
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "validate" || env.Error.Category != "invalid_spec" || env.OK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	if env.Error.FixHint != nil {
		t.Errorf("fix_hint = %+v; want none", env.Error.FixHint)
	}
	for _, want := range []string{"tasks.json", "traceability", "delete"} {
		if !strings.Contains(env.Error.Message, want) {
			t.Errorf("message %q does not say %q", env.Error.Message, want)
		}
	}
	if !mapsEqual(before, snapshot(t, root)) {
		t.Error("the repository changed")
	}
}

// TS-11-35: with --force the write goes ahead and says which keys it removed.
func TestForceWritesOverUnknownKeys_TS11_35(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	addUnknownKey(t, dir)
	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress", "--force")
	if code != cli.ExitOK || onDisk(t, dir)[1] != "in_progress" {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	b, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil || strings.Contains(string(b), "traceability") {
		t.Errorf("tasks.json still holds the key (%v)", err)
	}
	ws := warningsOf(env, "spec_invalid")
	if len(ws) != 1 || ws[0].Severity != "high" || ws[0].Stage != "validate" ||
		!strings.Contains(ws[0].Message, "traceability") || !strings.Contains(ws[0].Message, "tasks.json") {
		t.Errorf("warnings = %+v; want one high spec_invalid naming the key", env.Warnings)
	}
	// The package that was written holds no such key: it validates.
	if r := result(t, env); r.Valid == nil || !*r.Valid || len(r.FailingRules) != 0 {
		t.Errorf("valid %v failing_rules %v; want the package as written, which validates", r.Valid, r.FailingRules)
	}
}

// TS-11-36: whatever is wrong with a package, a call raises at most one
// spec_invalid warning.
func TestAtMostOneSpecInvalidWarning_TS11_36(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 36))
	for range 40 {
		root := newRepo(t)
		dir := pkgDir(root, "09_agent_mode")
		if rng.IntN(2) == 0 {
			editFile(t, filepath.Join(dir, "tasks.json"), `"TS-09-6"`, `"TS-09-99"`)
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

// TS-11-41 (the third case): a call refused at stage validate carries the report
// of the package on disk, with no changes and no next.
func TestARefusalAtValidateCarriesTheOnDiskReport_TS11_41(t *testing.T) {
	root := newRepo(t)
	addUnknownKey(t, pkgDir(root, "09_agent_mode"))
	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress")
	r := env.Result
	if code != cli.ExitFailed || r == nil || len(r.Tasks) != 3 || r.Tasks[0].State != "pending" || r.Valid == nil || *r.Valid {
		t.Fatalf("code %d: result %+v; want the report of the package on disk, which does not validate", code, r)
	}
	if len(r.Changes) != 0 || len(env.Next) != 0 || env.Summary != env.Error.Message && !strings.HasPrefix(env.Error.Message, env.Summary) {
		t.Errorf("changes %+v next %+v summary %q", r.Changes, env.Next, env.Summary)
	}
}
