package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// rawRun runs speca and returns the members of the envelope's result as they
// were written, so that a test can tell a field that is absent from one that is
// empty, along with the decoded envelope.
func rawRun(t *testing.T, argv ...string) (envelope.AdminEnvelope, map[string]json.RawMessage) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Main(argv, &stdout, &stderr)
	env, err := envelope.DecodeSpeca(stdout.Bytes())
	if err != nil {
		t.Fatalf("stdout is not one envelope (%v):\n%s", err, stdout.String())
	}
	if code != env.ExitCode {
		t.Fatalf("exit code %d, envelope says %d", code, env.ExitCode)
	}
	var top struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &top); err != nil {
		t.Fatal(err)
	}
	return env, top.Result
}

// editFile replaces the one occurrence of old in path with new.
func editFile(t *testing.T, path, old, new string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), old) {
		t.Fatalf("%s does not contain %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(b), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// editJSON reads the JSON object in path, applies edit to it and writes it back.
func editJSON(t *testing.T, path string, edit func(m map[string]any)) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	if b, err = json.MarshalIndent(m, "", "  "); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func pkgDir(root, name string) string { return filepath.Join(root, ".specs", name) }

// addPackage copies the v2 example into .specs/dirName under the given id and
// name. The copy loads, and validates only when it keeps the example's id
// and name; the tests that add siblings care how they load, not that they
// validate.
func addPackage(t *testing.T, root, dirName, id, name string) string {
	t.Helper()
	dir := pkgDir(root, dirName)
	copyDir(t, filepath.Join("..", "..", "afspec", "testdata", "v2_example"), dir)
	editFile(t, filepath.Join(dir, "prd.md"), `spec_id: "09"`, `spec_id: "`+id+`"`)
	editFile(t, filepath.Join(dir, "prd.md"), `spec_name: "agent_mode"`, `spec_name: "`+name+`"`)
	editJSON(t, filepath.Join(dir, "tasks.json"), func(m map[string]any) {
		m["spec_id"], m["spec_name"] = id, name
		m["dependencies"] = []any{}
	})
	return dir
}

// setDependencies makes the package depend on the given spec ids, in order.
func setDependencies(t *testing.T, dir string, ids ...string) {
	t.Helper()
	editJSON(t, filepath.Join(dir, "tasks.json"), func(m map[string]any) {
		deps := []any{}
		for _, id := range ids {
			deps = append(deps, map[string]any{"spec": id, "reason": "needs " + id})
		}
		m["dependencies"] = deps
	})
}

// planTask is one task of a plan written by setTasks. A nil deps leaves
// depends_on out, which format v2 §8.3 reads as the previous task.
func planTask(id int, state string, deps []int) map[string]any {
	task := map[string]any{"id": id, "kind": "implement", "title": fmt.Sprintf("Task %d", id),
		"criteria": []any{}, "tests": []any{}, "steps": []any{}, "state": state}
	if deps != nil {
		task["depends_on"] = deps
	}
	return task
}

// setTasks replaces the package's plan.
func setTasks(t *testing.T, dir string, tasks ...map[string]any) {
	t.Helper()
	editJSON(t, filepath.Join(dir, "tasks.json"), func(m map[string]any) {
		plan := []any{}
		for _, task := range tasks {
			plan = append(plan, task)
		}
		m["tasks"] = plan
	})
}

func setStatus(t *testing.T, dir, status string) {
	t.Helper()
	editFile(t, filepath.Join(dir, "prd.md"), `status: "draft"`, `status: "`+status+`"`)
}

// activate moves the package to active through the library, which records the
// hash of its Intent section.
func activate(t *testing.T, dir string) {
	t.Helper()
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spec.Transition("active", dir); err != nil {
		t.Fatal(err)
	}
}

// result is the envelope's result, or a stop for the test that finds none.
func result(t *testing.T, env envelope.AdminEnvelope) *envelope.AdminResult {
	t.Helper()
	if env.Result == nil {
		t.Fatalf("the call has no result; error: %+v", env.Error)
	}
	return env.Result
}

func warningCodes(env envelope.AdminEnvelope) []string {
	var out []string
	for _, w := range env.Warnings {
		out = append(out, string(w.Code))
	}
	return out
}

// TS-10-24: the report carries the identity of the package.
func TestReportIdentity_TS10_24(t *testing.T) {
	root := newRepo(t)
	rich := pkgDir(root, "09_agent_mode")
	activate(t, rich)
	editFile(t, filepath.Join(rich, "prd.md"), `schema_version: 2`, "schema_version: 2\nsupersedes: [\"07\"]\ntags: [\"agent\", \"cli\"]")
	addPackage(t, root, "08_plain", "08", "plain")

	env, code, _ := run(t, "--dir", root, "09")
	r := result(t, env)
	if code != cli.ExitOK || env.Error != nil || r == nil {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	if r.SpecDir != filepath.Join(".specs", "09_agent_mode") || r.SpecID != "09" || r.SpecName != "agent_mode" ||
		r.Title != "Agent mode for the spec CLI" || r.Status != "active" ||
		r.CreatedAt != "2026-09-08T09:00:00Z" || r.UpdatedAt == "" {
		t.Errorf("identity = %+v", r)
	}
	if len(r.IntentHash) != 64 || !slices.Equal(r.Supersedes, []string{"07"}) || !slices.Equal(r.Tags, []string{"agent", "cli"}) {
		t.Errorf("intent_hash %q supersedes %v tags %v", r.IntentHash, r.Supersedes, r.Tags)
	}

	_, raw := rawRun(t, "--dir", root, "08")
	for _, absent := range []string{"intent_hash", "supersedes", "tags"} {
		if _, ok := raw[absent]; ok {
			t.Errorf("a package with no %s writes it: %s", absent, raw[absent])
		}
	}
	for _, present := range []string{"spec_dir", "spec_id", "spec_name", "title", "status", "created_at", "updated_at", "valid"} {
		if _, ok := raw[present]; !ok {
			t.Errorf("the report lacks %s", present)
		}
	}
}

// TS-10-25: valid is always written; failing_rules only when it is false; a
// package that does not validate raises no warning, the answer being in valid.
func TestReportValidity_TS10_25(t *testing.T) {
	root := newRepo(t)
	_, raw := rawRun(t, "--dir", root, "09")
	if string(raw["valid"]) != "true" {
		t.Errorf("valid = %s; want true", raw["valid"])
	}
	if _, ok := raw["failing_rules"]; ok {
		t.Errorf("a valid package writes failing_rules: %s", raw["failing_rules"])
	}

	// Naming a test that does not exist breaks cross-file rules C6, C7 and C9.
	editFile(t, filepath.Join(pkgDir(root, "09_agent_mode"), "tasks.json"), `"TS-09-6"`, `"TS-09-99"`)
	env, raw := rawRun(t, "--dir", root, "09")
	if env.ExitCode != cli.ExitOK || string(raw["valid"]) != "false" {
		t.Fatalf("exit %d, valid = %s; want 0 and false", env.ExitCode, raw["valid"])
	}
	if got := result(t, env).FailingRules; !slices.Equal(got, []string{"C6", "C7", "C9"}) {
		t.Errorf("failing_rules = %v; want C6 C7 C9", got)
	}
	if len(env.Warnings) != 0 {
		t.Errorf("a package that does not validate raises %v; want no warning", warningCodes(env))
	}

	// A schema error is a rule too: tasks.json with a member the schema does not allow.
	editJSON(t, filepath.Join(pkgDir(root, "01_test_feature"), "tasks.json"), func(m map[string]any) { m["bogus"] = 1 })
	env, _, _ = run(t, "--dir", root, "01")
	if r := result(t, env); r.Valid == nil || *r.Valid || !slices.Contains(r.FailingRules, "unknown_field") || len(env.Warnings) != 0 {
		t.Errorf("an unknown member of tasks.json: valid %v rules %v warnings %v", r.Valid, r.FailingRules, warningCodes(env))
	}
}

// TS-10-26 (the report): intent_drift is true only for an active package whose
// ## Intent section no longer hashes to its intent_hash, or is gone.
func TestReportIntentDrift_TS10_26(t *testing.T) {
	root := newRepo(t)
	drift := func(name string) bool {
		t.Helper()
		_, raw := rawRun(t, "--dir", root, name)
		return string(raw["intent_drift"]) == "true"
	}

	matching := addPackage(t, root, "11_matching", "11", "matching")
	activate(t, matching)

	edited := addPackage(t, root, "12_edited", "12", "edited")
	activate(t, edited)
	editFile(t, filepath.Join(edited, "prd.md"), "so that an agent can parse results", "so that anyone can parse results")

	noSection := addPackage(t, root, "13_nosection", "13", "nosection")
	activate(t, noSection)
	editFile(t, filepath.Join(noSection, "prd.md"), "## Intent", "## Purpose")

	draft := addPackage(t, root, "14_draft", "14", "draft")
	editFile(t, filepath.Join(draft, "prd.md"), "intent_hash: null", `intent_hash: "0000"`)

	noHash := addPackage(t, root, "15_nohash", "15", "nohash")
	setStatus(t, noHash, "active")

	for name, want := range map[string]bool{"11": false, "12": true, "13": true, "14": false, "15": false} {
		if got := drift(name); got != want {
			t.Errorf("package %s: intent_drift = %v; want %v", name, got, want)
		}
	}
	if _, raw := rawRun(t, "--dir", root, "11"); raw["intent_drift"] != nil {
		t.Errorf("intent_drift is written for a package that has not drifted: %s", raw["intent_drift"])
	}
}

// TS-10-27: the report lists every task with its readiness, counts them by
// state, and names the task to work on.
func TestReportTasks_TS10_27(t *testing.T) {
	root := newRepo(t)
	mixed := pkgDir(root, "09_agent_mode")
	setTasks(t, mixed,
		planTask(1, "done", nil),
		planTask(2, "in_progress", []int{1}),
		planTask(3, "pending", []int{1}), // its dependency is done: ready
		planTask(4, "pending", []int{2}), // its dependency is under way: blocked
		planTask(5, "dropped", []int{1}),
	)
	empty := addPackage(t, root, "11_empty", "11", "empty")
	setTasks(t, empty)
	finished := addPackage(t, root, "12_finished", "12", "finished")
	setTasks(t, finished, planTask(1, "done", nil), planTask(2, "done", []int{1}), planTask(3, "dropped", []int{2}))

	env, code, _ := run(t, "--dir", root, "09")
	r := result(t, env)
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	var ids []int
	var ready []bool
	for _, task := range r.Tasks {
		ids = append(ids, task.ID)
		ready = append(ready, task.Ready)
	}
	if !slices.Equal(ids, []int{1, 2, 3, 4, 5}) || !slices.Equal(ready, []bool{false, false, true, false, false}) {
		t.Errorf("tasks %v ready %v", ids, ready)
	}
	if want := (envelope.TaskCounts{Pending: 2, InProgress: 1, Done: 1, Dropped: 1, Total: 5}); r.TaskCounts == nil || *r.TaskCounts != want {
		t.Errorf("task_counts = %+v; want %+v", r.TaskCounts, want)
	}
	if r.NextTask == nil || r.NextTask.ID != 2 || r.NextTask.State != "in_progress" {
		t.Errorf("next_task = %+v; want the task in progress", r.NextTask)
	}

	_, raw := rawRun(t, "--dir", root, "11")
	if string(raw["tasks"]) != "[]" {
		t.Errorf("a plan without tasks writes tasks = %s; want []", raw["tasks"])
	}
	var counts envelope.TaskCounts
	if err := json.Unmarshal(raw["task_counts"], &counts); err != nil || counts != (envelope.TaskCounts{}) {
		t.Errorf("task_counts = %s", raw["task_counts"])
	}
	if _, ok := raw["next_task"]; ok {
		t.Errorf("a plan without tasks writes next_task: %s", raw["next_task"])
	}

	env, _, _ = run(t, "--dir", root, "12")
	if result(t, env).NextTask != nil || result(t, env).TaskCounts.Done != 2 {
		t.Errorf("a finished plan: next_task %+v counts %+v", result(t, env).NextTask, result(t, env).TaskCounts)
	}
}

// TS-10-28: allowed_status lists the moves the lifecycle allows from the
// package's status, in the order of the lifecycle.
func TestReportAllowedStatus_TS10_28(t *testing.T) {
	root := newRepo(t)
	for i, tc := range []struct {
		status string
		want   []string
	}{
		{"draft", []string{"active", "archived"}},
		{"active", []string{"sealed"}},
		{"sealed", []string{"superseded", "archived"}},
		{"superseded", []string{"archived"}},
		{"archived", []string{}},
	} {
		id := fmt.Sprintf("%02d", 20+i)
		dir := addPackage(t, root, id+"_"+tc.status, id, tc.status)
		setStatus(t, dir, tc.status)

		env, raw := rawRun(t, "--dir", root, id)
		if got := result(t, env).AllowedStatus; !slices.Equal(got, tc.want) {
			t.Errorf("%s: allowed_status = %v; want %v", tc.status, got, tc.want)
		}
		if tc.status == "archived" && string(raw["allowed_status"]) != "[]" {
			t.Errorf("a terminal status writes allowed_status = %s; want []", raw["allowed_status"])
		}
	}
}

// TS-10-29: depends_on is the package's own dependencies; dependents are the
// other packages of the spec root that name it.
func TestReportDependsOnAndDependents_TS10_29(t *testing.T) {
	root := t.TempDir()
	for _, p := range []struct{ id, name string }{{"01", "a"}, {"02", "b"}, {"03", "c"}, {"04", "d"}, {"05", "e"}} {
		addPackage(t, root, p.id+"_"+p.name, p.id, p.name)
	}
	setDependencies(t, pkgDir(root, "03_c"), "01", "01", "02")
	setDependencies(t, pkgDir(root, "04_d"), "03")
	setDependencies(t, pkgDir(root, "05_e"), "03", "03")

	env, _, _ := run(t, "--dir", root, "03")
	if r := result(t, env); !slices.Equal(r.DependsOn, []string{"01", "02"}) || !slices.Equal(r.Dependents, []string{"04", "05"}) {
		t.Errorf("03: depends_on %v dependents %v; want [01 02] and [04 05]", r.DependsOn, r.Dependents)
	}

	env, raw := rawRun(t, "--dir", root, "01")
	if string(raw["depends_on"]) != "[]" {
		t.Errorf("01: depends_on = %s; want []", raw["depends_on"])
	}
	if !slices.Equal(result(t, env).Dependents, []string{"03"}) {
		t.Errorf("01: dependents %v; want [03]", result(t, env).Dependents)
	}
	_, raw = rawRun(t, "--dir", root, "05")
	if string(raw["dependents"]) != "[]" {
		t.Errorf("05: dependents = %s; want [] (the root was read and nothing depends on it)", raw["dependents"])
	}
}

// TS-10-30: for any plan, the counts add up to the tasks listed.
func TestReportCountsAddUp_TS10_30(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	rng := rand.New(rand.NewPCG(10, 30))
	states := []string{"pending", "in_progress", "done", "dropped"}
	for range 80 {
		n := rng.IntN(9)
		var tasks []map[string]any
		for id := 1; id <= n; id++ {
			var deps []int
			if rng.IntN(2) == 0 {
				for range rng.IntN(3) {
					deps = append(deps, 1+rng.IntN(n+1))
				}
				if deps == nil {
					deps = []int{}
				}
			}
			tasks = append(tasks, planTask(id, states[rng.IntN(len(states))], deps))
		}
		setTasks(t, dir, tasks...)

		env, code, _ := run(t, "--dir", root, "09")
		r := result(t, env)
		if code != cli.ExitOK || r == nil || r.TaskCounts == nil {
			t.Fatalf("code %d: %+v", code, env.Error)
		}
		c := r.TaskCounts
		if c.Total != len(r.Tasks) || c.Total != n || c.Total != c.Pending+c.InProgress+c.Done+c.Dropped {
			t.Fatalf("%d tasks: counts %+v, %d listed", n, *c, len(r.Tasks))
		}
	}
}

// TS-10-31: a spec root that cannot be read costs the report its dependents and
// nothing else.
func TestReportWithAnUnreadableSpecRoot_TS10_31(t *testing.T) {
	root := newRepo(t)
	pkg := pkgDir(root, "09_agent_mode")
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing")

	for name, specsDir := range map[string]string{"missing": missing, "a file": file} {
		env, raw := rawRun(t, "--specs-dir", specsDir, pkg)
		if env.ExitCode != cli.ExitOK || !env.OK {
			t.Fatalf("%s: exit %d: %+v", name, env.ExitCode, env.Error)
		}
		if _, ok := raw["dependents"]; ok {
			t.Errorf("%s: dependents is written: %s", name, raw["dependents"])
		}
		if len(env.Warnings) != 1 {
			t.Fatalf("%s: warnings = %+v", name, env.Warnings)
		}
		w := env.Warnings[0]
		if w.Code != cli.WarnSpecsDirUnreadable || w.Severity != "low" || w.Stage != "preflight" || !strings.Contains(w.Message, specsDir) {
			t.Errorf("%s: warning = %+v", name, w)
		}
		r := result(t, env)
		if r.SpecID != "09" || r.Valid == nil || !*r.Valid || len(r.Tasks) != 3 || r.NextTask == nil || len(r.DependsOn) != 1 || len(r.AllowedStatus) == 0 {
			t.Errorf("%s: the rest of the report is missing: %+v", name, r)
		}
	}
}

// TS-10-32: a dependency on a package that is not there does not stop the
// dependents from being found.
func TestReportWithADanglingDependency_TS10_32(t *testing.T) {
	root := t.TempDir()
	addPackage(t, root, "03_c", "03", "c")
	addPackage(t, root, "04_d", "04", "d")
	setDependencies(t, pkgDir(root, "03_c"), "99")
	setDependencies(t, pkgDir(root, "04_d"), "03")

	env, code, _ := run(t, "--dir", root, "03")
	if code != cli.ExitOK || !slices.Equal(result(t, env).Dependents, []string{"04"}) || !slices.Equal(result(t, env).DependsOn, []string{"99"}) {
		t.Errorf("code %d: dependents %v depends_on %v", code, result(t, env).Dependents, result(t, env).DependsOn)
	}
	if len(env.Warnings) != 0 {
		t.Errorf("warnings = %v; a dangling dependency is the verdict's to report", warningCodes(env))
	}
}

// statusCalls lists the suggestions in next that change a package's status.
func statusCalls(next []envelope.Next) []envelope.Next {
	var out []envelope.Next
	for _, n := range next {
		if slices.Contains(n.Flags, "--status") {
			out = append(out, n)
		}
	}
	return out
}

// TS-12-52: a draft that validates and has an intent is told to activate, before
// anything about its tasks; one that does not is not.
func TestTheReportSuggestsActivatingAValidDraftFirst_TS12_52(t *testing.T) {
	root := newRepo(t)
	env, _, _ := run(t, "--dir", root, "09")
	want := envelope.Next{Tool: "speca", Input: ".specs/09_agent_mode", Flags: []string{"--status", "active"}}
	if len(env.Next) < 2 || env.Next[0].Tool != want.Tool || env.Next[0].Input != want.Input || !slices.Equal(env.Next[0].Flags, want.Flags) {
		t.Fatalf("next %+v; want the activation first, and the task after it", env.Next)
	}
	if len(statusCalls(env.Next)) != 1 || !slices.Equal(env.Next[1].Flags, []string{"--task", "1", "--state", "in_progress"}) {
		t.Errorf("next %+v; want one activation, then the claim of task 1", env.Next)
	}

	for name, damage := range map[string]func(t *testing.T, dir string){
		"it has no intent": func(t *testing.T, dir string) { editFile(t, filepath.Join(dir, "prd.md"), "## Intent", "## Purpose") },
		"it does not validate": func(t *testing.T, dir string) {
			editFile(t, filepath.Join(dir, "tasks.json"), `"TS-09-6"`, `"TS-09-99"`)
		},
		"it holds a key": withFrontmatterKey,
	} {
		root := newRepo(t)
		damage(t, pkgDir(root, "09_agent_mode"))
		if env, _, _ := run(t, "--dir", root, "09"); len(statusCalls(env.Next)) != 0 {
			t.Errorf("%s: next %+v; want no activation", name, env.Next)
		}
	}
}

// TS-12-53: an active package with no task open is told to seal; one with a task
// open is not; and the list suggests no status call.
func TestTheReportSuggestsSealingAPackageWithNoOpenTask_TS12_53(t *testing.T) {
	root := newRepo(t)
	activePackage(t, root, "03_done", "03", "done", planTask(1, "done", nil), planTask(2, "dropped", []int{}))
	activePackage(t, root, "04_open", "04", "open", planTask(1, "pending", nil))

	env, _, _ := run(t, "--dir", root, "03_done")
	if got := statusCalls(env.Next); len(got) != 1 || !slices.Equal(got[0].Flags, []string{"--status", "sealed"}) || got[0].Input != ".specs/03_done" {
		t.Errorf("next %+v; want the sealing of .specs/03_done", env.Next)
	}
	env, _, _ = run(t, "--dir", root, "04_open")
	if got := statusCalls(env.Next); len(got) != 0 {
		t.Errorf("next %+v; a package with a task open is not sealed", env.Next)
	}
	env, _, _ = run(t, "--dir", root, "--list")
	if got := statusCalls(env.Next); len(got) != 0 {
		t.Errorf("the list suggests %+v; it suggests no status call", got)
	}
}

// TS-12-54: for any package, a status call the report suggests is a call that
// goes through. Packages are made in every status, and in the states that make
// the checks refuse: no intent, an invalid package, tasks open, a drifted intent,
// keys the schema does not allow. Each is the one package of its own repository,
// under the name the example validates under.
func TestASuggestedStatusCallIsNeverRefused_TS12_54(t *testing.T) {
	keys := map[string]func(t *testing.T, dir string){
		"no keys":                  func(*testing.T, string) {},
		"a key in tasks.json":      addUnknownKey,
		"a key in the frontmatter": withFrontmatterKey,
	}
	drifts := map[string]func(t *testing.T, dir string){
		"no drift": func(*testing.T, string) {},
		"the intent edited": func(t *testing.T, dir string) {
			editFile(t, filepath.Join(dir, "prd.md"), "Coding agents drive", "Coding agents might drive")
		},
		"the intent removed": func(t *testing.T, dir string) { editFile(t, filepath.Join(dir, "prd.md"), "## Intent", "## Purpose") },
	}
	plans := map[string][]map[string]any{
		"no tasks": {}, "all done": {planTask(1, "done", nil)}, "one pending": {planTask(1, "pending", nil)},
		"one in progress": {planTask(1, "in_progress", nil)}, "done and an optional open one": {planTask(1, "done", nil), planWithOpenTasks()[1]},
	}

	recipes := map[string]func(t *testing.T, dir string){}
	for _, status := range []string{"sealed", "superseded", "archived"} {
		recipes[status] = func(t *testing.T, dir string) { setStatus(t, dir, status) }
	}
	for _, hasIntent := range []bool{true, false} {
		for _, valid := range []bool{true, false} {
			for keyName, addKey := range keys {
				recipes[fmt.Sprintf("a draft; intent %v; valid %v; %s", hasIntent, valid, keyName)] = func(t *testing.T, dir string) {
					if !hasIntent {
						editFile(t, filepath.Join(dir, "prd.md"), "## Intent", "## Purpose")
					}
					if !valid {
						editFile(t, filepath.Join(dir, "tasks.json"), `"TS-09-6"`, `"TS-09-99"`)
					}
					addKey(t, dir)
				}
			}
		}
	}
	for planName, plan := range plans {
		for driftName, drift := range drifts {
			for keyName, addKey := range keys {
				recipes[fmt.Sprintf("active; %s; %s; %s", planName, driftName, keyName)] = func(t *testing.T, dir string) {
					activate(t, dir)
					setTasks(t, dir, plan...)
					drift(t, dir)
					addKey(t, dir)
				}
			}
		}
	}

	suggested := map[string]int{}
	for name, recipe := range recipes {
		root := newRepo(t)
		dir := pkgDir(root, "09_agent_mode")
		recipe(t, dir)
		env, _, _ := run(t, "--dir", root, "09")
		spec, err := afspec.LoadSpec(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, call := range statusCalls(env.Next) {
			suggested[strings.Join(call.Flags, " ")]++
			if spec.Status != "draft" && spec.Status != "active" {
				t.Errorf("%s: the package is %s, and the report suggests %v", name, spec.Status, call.Flags)
			}
			if out, code, _ := run(t, append([]string{"--dir", root, call.Input}, call.Flags...)...); code != cli.ExitOK {
				t.Errorf("%s: the suggested call %v exits %d: %+v", name, call.Flags, code, out.Error)
			}
		}
	}
	t.Logf("%d packages; suggestions made and run: %v", len(recipes), suggested)
	if suggested["--status active"] == 0 || suggested["--status sealed"] == 0 {
		t.Errorf("suggestions made: %v; the property was checked of none of one kind", suggested)
	}
}
