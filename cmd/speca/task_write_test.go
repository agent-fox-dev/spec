package main

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// onDisk loads the package and returns the state of each of its tasks, by id.
func onDisk(t *testing.T, dir string) map[int]string {
	t.Helper()
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int]string{}
	for _, task := range spec.Tasks.Tasks {
		out[task.Id] = string(task.State)
	}
	return out
}

// changed lists the ids of a result's changes.
func changed(r *envelope.AdminResult) []int {
	var out []int
	for _, c := range r.Changes {
		out = append(out, c.ID)
	}
	return out
}

// TS-11-14: --task next is the id NextTask names, the report's next_task.
func TestTaskNextIsTheReportsNextTask_TS11_14(t *testing.T) {
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

// TS-11-15: --task all leaves out the tasks already in the target state and the
// dropped ones, and moves the rest in plan order.
func TestTaskAllSkipsTheTargetStateAndDropped_TS11_15(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	setTasks(t, dir, planTask(1, "done", nil), planTask(2, "in_progress", []int{1}), planTask(3, "dropped", nil), planTask(4, "in_progress", []int{}))
	env, code, _ := run(t, "--dir", root, "09", "--task", "all", "--state", "done")
	if code != cli.ExitOK || !slices.Equal(changed(result(t, env)), []int{2, 4}) {
		t.Fatalf("code %d: changes %+v, error %+v", code, result(t, env).Changes, env.Error)
	}
	for _, c := range result(t, env).Changes {
		if c.From != "in_progress" || c.To != "done" {
			t.Errorf("change = %+v; want in_progress to done", c)
		}
	}
	if got := onDisk(t, dir); !maps_eq(got, map[int]string{1: "done", 2: "done", 3: "dropped", 4: "done"}) {
		t.Errorf("on disk: %v", got)
	}
}

func maps_eq(a, b map[int]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TS-11-16: --task all that resolves to nothing succeeds, changes nothing and
// writes nothing: not a byte, not updated_at.
func TestNothingToMoveWritesNothing_TS11_16(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"), planTask(1, "done", nil), planTask(2, "dropped", nil))
	before := snapshot(t, root)
	env, raw := rawRun(t, "--dir", root, "09", "--task", "all", "--state", "done")
	if env.ExitCode != cli.ExitOK || !env.OK {
		t.Fatalf("exit %d: %+v", env.ExitCode, env.Error)
	}
	if _, ok := raw["changes"]; ok {
		t.Errorf("changes = %s; want none", raw["changes"])
	}
	if env.Summary != "speca: 09_agent_mode no task changed" {
		t.Errorf("summary = %q", env.Summary)
	}
	if after := snapshot(t, root); !mapsEqual(before, after) {
		t.Errorf("the repository changed:\nbefore %v\nafter  %v", before, after)
	}
	if got := result(t, env).UpdatedAt; got != "2026-09-08T09:00:00Z" {
		t.Errorf("updated_at = %q; want the fixture's", got)
	}
}

// TS-11-18: the allowed moves of several tasks are made together, in plan order.
func TestSeveralMovesAreMadeTogether_TS11_18(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	setTasks(t, dir, planTask(1, "pending", nil), planTask(2, "in_progress", []int{1}), planTask(3, "pending", []int{}))
	env, code, _ := run(t, "--dir", root, "09", "--task", "3,1", "--state", "in_progress")
	if code != cli.ExitOK || !slices.Equal(changed(result(t, env)), []int{1, 3}) {
		t.Fatalf("code %d: changes %+v, error %+v", code, result(t, env).Changes, env.Error)
	}
	if got := onDisk(t, dir); got[1] != "in_progress" || got[3] != "in_progress" || got[2] != "in_progress" {
		t.Errorf("on disk: %v", got)
	}
}

// TS-11-25: what Save returns is reported by what it says: a lifecycle or
// intent error as the status refusal it is, a save error as a write failure
// that tells the caller what may be left.
func TestSaveErrorsAreMapped_TS11_25(t *testing.T) {
	was := saveSpec
	t.Cleanup(func() { saveSpec = was })
	for name, tc := range map[string]struct {
		err             error
		stage, category string
	}{
		"lifecycle": {&afspec.LifecycleError{Msg: "cannot save spec in sealed state"}, "status", "lifecycle"},
		"intent":    {&afspec.IntentError{Msg: "intent drift detected"}, "status", "lifecycle"},
		"save":      {&afspec.SaveError{Msg: "cannot rename tasks.json"}, "write", "disk"},
	} {
		saveSpec = func(*afspec.Spec, string) error { return tc.err }
		root := newRepo(t)
		before := snapshot(t, root)
		env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress")
		if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != tc.stage || env.Error.Category != tc.category || env.OK {
			t.Errorf("%s: code %d: %+v", name, code, env.Error)
			continue
		}
		if env.Error.Retryable || env.Error.Resumable {
			t.Errorf("%s: retryable %v resumable %v", name, env.Error.Retryable, env.Error.Resumable)
		}
		if !strings.Contains(env.Error.Message, tc.err.Error()) {
			t.Errorf("%s: message %q does not carry the library's words %q", name, env.Error.Message, tc.err)
		}
		if name == "save" {
			for _, want := range []string{"temporary files", "unchanged", "final renames", "some files updated", "`speca .specs/09_agent_mode`"} {
				if !strings.Contains(env.Error.Message, want) {
					t.Errorf("save: message %q does not say %q", env.Error.Message, want)
				}
			}
		}
		if env.Result == nil || len(env.Result.Changes) != 0 || len(env.Next) != 0 {
			t.Errorf("%s: want the report of the package on disk, without changes or next: %+v", name, env.Result)
		}
		if !mapsEqual(before, snapshot(t, root)) {
			t.Errorf("%s: the repository changed", name)
		}
	}
}

var updatedAt = regexp.MustCompile(`(?m)^updated_at: "([^"]*)"`)

// TS-11-37: a change sets updated_at to now, in UTC, in RFC 3339, and the
// package is saved with it.
func TestAChangeSetsUpdatedAtAndSaves_TS11_37(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	t0 := time.Now().UTC().Truncate(time.Second)
	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress")
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	if got := onDisk(t, dir); got[1] != "in_progress" {
		t.Errorf("tasks.json: %v", got)
	}
	prd, err := os.ReadFile(filepath.Join(dir, "prd.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := updatedAt.FindStringSubmatch(string(prd))
	if m == nil {
		t.Fatalf("prd.md has no updated_at:\n%s", prd)
	}
	ts, err := time.Parse(time.RFC3339, m[1])
	if err != nil || !strings.HasSuffix(m[1], "Z") || ts.Before(t0) || ts.After(time.Now().UTC()) {
		t.Errorf("updated_at = %q (%v); want now, in UTC, between %s and the end of the call", m[1], err, t0.Format(time.RFC3339))
	}
	if result(t, env).UpdatedAt != m[1] {
		t.Errorf("result.updated_at = %q; the file says %q", result(t, env).UpdatedAt, m[1])
	}
}

// TS-11-38: a success is the report of the package as it was written, and the
// changes, in plan order.
func TestASuccessReportsThePackageAfterTheChange_TS11_38(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"), planTask(1, "pending", nil), planTask(2, "done", nil), planTask(3, "pending", []int{}))
	env, raw := rawRun(t, "--dir", root, "09", "--task", "3,1", "--state", "in_progress")
	r := result(t, env)
	if env.ExitCode != cli.ExitOK || !slices.Equal(changed(r), []int{1, 3}) {
		t.Fatalf("exit %d: changes %+v, error %+v", env.ExitCode, r.Changes, env.Error)
	}
	if r.TaskCounts == nil || r.TaskCounts.InProgress != 2 || r.TaskCounts.Done != 1 || r.TaskCounts.Pending != 0 || r.TaskCounts.Total != 3 {
		t.Errorf("task_counts = %+v; want the counts after the change", r.TaskCounts)
	}
	if len(r.Tasks) != 3 || r.Tasks[0].State != "in_progress" || r.Tasks[2].State != "in_progress" {
		t.Errorf("tasks = %+v", r.Tasks)
	}
	for _, k := range []string{"spec_dir", "spec_id", "status", "valid", "tasks", "task_counts", "next_task", "allowed_status", "depends_on", "dependents", "changes"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("the result lacks %s", k)
		}
	}
	for i, c := range r.Changes {
		if c.Kind != "task" || c.From != "pending" || c.To != "in_progress" || c.ID != []int{1, 3}[i] {
			t.Errorf("change %d = %+v", i, c)
		}
	}
}

// TS-11-41: a call that is refused carries the report of the package as it is on
// disk, with no changes and no next, and its summary is the refusal.
func TestARefusalCarriesTheOnDiskReport_TS11_41(t *testing.T) {
	taken := newRepo(t)
	setTasks(t, pkgDir(taken, "09_agent_mode"), planTask(1, "in_progress", nil), planTask(2, "pending", []int{1}))
	sealed := newRepo(t)
	setStatus(t, pkgDir(sealed, "09_agent_mode"), "sealed")

	for name, root := range map[string]string{"a taken task": taken, "a sealed package": sealed} {
		before := snapshot(t, root)
		env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress")
		if code != cli.ExitFailed || env.Error == nil {
			t.Errorf("%s: code %d", name, code)
			continue
		}
		r := env.Result
		if r == nil || len(r.Tasks) == 0 || r.Tasks[0].ID != 1 || r.Valid == nil || r.TaskCounts == nil {
			t.Errorf("%s: result = %+v; want the report of the package on disk", name, r)
			continue
		}
		if name == "a taken task" && r.Tasks[0].State != "in_progress" {
			t.Errorf("%s: task 1 is %s in the result; it is in_progress on disk", name, r.Tasks[0].State)
		}
		if len(r.Changes) != 0 || len(env.Next) != 0 {
			t.Errorf("%s: changes %+v next %+v; want neither", name, r.Changes, env.Next)
		}
		if env.Summary != env.Error.Message && !strings.HasPrefix(env.Error.Message, env.Summary) {
			t.Errorf("%s: summary %q is not the refusal %q", name, env.Summary, env.Error.Message)
		}
		if !mapsEqual(before, snapshot(t, root)) {
			t.Errorf("%s: the repository changed", name)
		}
	}
}

// TS-11-39: the summary names one change, several or none, and the dry run.
func TestTheTaskSummary_TS11_39(t *testing.T) {
	task := func(id int, from, to string) envelope.Change {
		return envelope.Change{Kind: "task", ID: id, From: from, To: to}
	}
	for name, tc := range map[string]struct {
		changes []envelope.Change
		want    string
	}{
		"one":     {[]envelope.Change{task(2, "pending", "in_progress")}, "speca: 11_x task 2 pending → in_progress"},
		"several": {[]envelope.Change{task(2, "in_progress", "done"), task(3, "in_progress", "done")}, "speca: 11_x tasks 2, 3 → done"},
		"none":    {nil, "speca: 11_x no task changed"},
	} {
		r := &Result{AdminResult: envelope.AdminResult{SpecDir: ".specs/11_x", Changes: tc.changes}, taskForm: true}
		if got := r.Summary(); got != tc.want {
			t.Errorf("%s: Summary = %q; want %q", name, got, tc.want)
		}
		r.DryRun = true
		if got := r.Summary(); got != tc.want+" (dry run, nothing written)" {
			t.Errorf("%s, dry run: Summary = %q", name, got)
		}
	}
}

// TS-11-40: for any list of changes the summary begins with the prefix and the
// package, ends with the dry-run suffix exactly under a dry run, and says no task
// changed exactly when the list is empty.
func TestTheTaskSummaryProperty_TS11_40(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 40))
	states := []string{"pending", "in_progress", "done", "dropped"}
	const suffix = " (dry run, nothing written)"
	for range 400 {
		n := rng.IntN(7)
		to := states[rng.IntN(len(states))]
		var changes []envelope.Change
		for _, id := range rng.Perm(20)[:n] {
			changes = append(changes, envelope.Change{Kind: "task", ID: id + 1, From: states[rng.IntN(len(states))], To: to})
		}
		dry := rng.IntN(2) == 0
		r := &Result{AdminResult: envelope.AdminResult{SpecDir: ".specs/12_dir", Changes: changes, DryRun: dry}, taskForm: true}
		s := r.Summary()
		if !strings.HasPrefix(s, "speca: 12_dir ") || strings.HasSuffix(s, suffix) != dry || strings.Contains(s, "no task changed") != (n == 0) {
			t.Fatalf("%d changes, dry run %v: Summary = %q", n, dry, s)
		}
	}
}

// A dry run writes nothing: the guard on the write, which task 7's dry-run
// behaviour (the result, the next call) builds on.
func TestADryRunWritesNothing(t *testing.T) {
	root := newRepo(t)
	before := snapshot(t, root)
	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress", "--dry-run")
	if code != cli.ExitOK || !slices.Equal(changed(result(t, env)), []int{1}) {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	if after := snapshot(t, root); !mapsEqual(before, after) {
		t.Errorf("a dry run changed the repository:\nbefore %v\nafter  %v", before, after)
	}
}
