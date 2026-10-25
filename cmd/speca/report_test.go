package main

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

func TestReport_CarriesThePackagesIdentity(t *testing.T) {
	root := newRepo(t)
	rich := pkgDir(root, "09_agent_mode")
	activate(t, rich)
	editFile(t, filepath.Join(rich, "prd.md"), `schema_version: 2`, "schema_version: 2\nsupersedes: [\"07\"]\ntags: [\"agent\", \"cli\"]")
	addPackage(t, root, "08_plain", "08", "plain")

	env, code, _ := run(t, "--dir", root, "09")
	r := result(t, env)
	if code != cli.ExitOK || env.Error != nil {
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

// valid is always written, failing_rules only when it is false, and a package
// that does not validate raises no warning: the answer is in valid.
func TestReport_SaysWhetherThePackageValidates(t *testing.T) {
	root := newRepo(t)
	_, raw := rawRun(t, "--dir", root, "09")
	if string(raw["valid"]) != "true" {
		t.Errorf("valid = %s; want true", raw["valid"])
	}
	if _, ok := raw["failing_rules"]; ok {
		t.Errorf("a valid package writes failing_rules: %s", raw["failing_rules"])
	}

	breakValidity(t, pkgDir(root, "09_agent_mode"))
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

// intent_drift is written, as true, only for an active package whose ## Intent
// section no longer hashes to its intent_hash, or is gone; an empty hash is a
// hash that is set and wrong.
func TestReport_IntentDrift(t *testing.T) {
	root := newRepo(t)
	activePackage(t, root, "11_matching", "11", "matching")
	driftIntent(t, activePackage(t, root, "12_edited", "12", "edited"))
	removeIntent(t, activePackage(t, root, "13_nosection", "13", "nosection"))
	draft := addPackage(t, root, "14_draft", "14", "draft")
	editFile(t, filepath.Join(draft, "prd.md"), "intent_hash: null", `intent_hash: "0000"`)
	setStatus(t, addPackage(t, root, "15_nohash", "15", "nohash"), "active")
	emptyHash := activePackage(t, root, "16_emptyhash", "16", "emptyhash")
	editFile(t, filepath.Join(emptyHash, "prd.md"), `intent_hash: "`+intentHashOf(t, emptyHash)+`"`, `intent_hash: ""`)

	for ref, want := range map[string]bool{"11": false, "12": true, "13": true, "14": false, "15": false, "16": true} {
		_, raw := rawRun(t, "--dir", root, ref)
		switch got := raw["intent_drift"]; {
		case want && string(got) != "true":
			t.Errorf("package %s: intent_drift = %s; want true", ref, got)
		case !want && got != nil:
			t.Errorf("package %s: intent_drift = %s; want it absent", ref, got)
		}
	}
}

// The report lists every task with its readiness, counts them by state, and
// names the task to work on: one in progress before a ready one.
func TestReport_ListsAndCountsTheTasks(t *testing.T) {
	root := newRepo(t)
	setTasks(t, pkgDir(root, "09_agent_mode"),
		planTask(1, "done", nil),
		planTask(2, "in_progress", []int{1}),
		planTask(3, "pending", []int{1}), // its dependency is done: ready
		planTask(4, "pending", []int{2}), // its dependency is under way: blocked
		planTask(5, "dropped", []int{1}),
	)
	setTasks(t, addPackage(t, root, "11_empty", "11", "empty"))
	setTasks(t, addPackage(t, root, "12_finished", "12", "finished"), planTask(1, "done", nil), planTask(2, "done", []int{1}), planTask(3, "dropped", []int{2}))

	env, code, _ := run(t, "--dir", root, "09")
	r := result(t, env)
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	var taskIDs []int
	var ready []bool
	for _, task := range r.Tasks {
		taskIDs = append(taskIDs, task.ID)
		ready = append(ready, task.Ready)
	}
	if !slices.Equal(taskIDs, []int{1, 2, 3, 4, 5}) || !slices.Equal(ready, []bool{false, false, true, false, false}) {
		t.Errorf("tasks %v ready %v", taskIDs, ready)
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

// For any plan, the counts add up to the tasks listed.
func TestReport_TaskCountsAddUp(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	rng := rand.New(rand.NewPCG(10, 30))
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
			tasks = append(tasks, planTask(id, wantTaskStates[rng.IntN(len(wantTaskStates))], deps))
		}
		setTasks(t, dir, tasks...)

		env, code, _ := run(t, "--dir", root, "09")
		r := result(t, env)
		if code != cli.ExitOK || r.TaskCounts == nil {
			t.Fatalf("code %d: %+v", code, env.Error)
		}
		c := r.TaskCounts
		if c.Total != len(r.Tasks) || c.Total != n || c.Total != c.Pending+c.InProgress+c.Done+c.Dropped {
			t.Fatalf("%d tasks: counts %+v, %d listed", n, *c, len(r.Tasks))
		}
	}
}

// allowed_status lists the moves the lifecycle allows from the package's status,
// in the order of the lifecycle, and is written as [] for a terminal one.
func TestReport_AllowedStatus(t *testing.T) {
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
		t.Run(tc.status, func(t *testing.T) {
			id := fmt.Sprintf("%02d", 20+i)
			packageWith(t, root, id+"_"+tc.status, id, tc.status, tc.status)
			env, raw := rawRun(t, "--dir", root, id)
			if got := result(t, env).AllowedStatus; !slices.Equal(got, tc.want) {
				t.Errorf("allowed_status = %v; want %v", got, tc.want)
			}
			if len(tc.want) == 0 && string(raw["allowed_status"]) != "[]" {
				t.Errorf("a terminal status writes allowed_status = %s; want []", raw["allowed_status"])
			}
		})
	}
}

// depends_on is the package's own dependencies, each once; dependents are the
// other packages of the spec root that name it, sorted and each once. A
// dependency on a package that is not there stops neither.
func TestReport_DependsOnAndDependents(t *testing.T) {
	root := t.TempDir()
	for _, p := range []struct{ id, name string }{{"01", "a"}, {"02", "b"}, {"03", "c"}, {"04", "d"}, {"05", "e"}, {"06", "f"}} {
		addPackage(t, root, p.id+"_"+p.name, p.id, p.name)
	}
	setDependencies(t, pkgDir(root, "03_c"), "01", "01", "02")
	setDependencies(t, pkgDir(root, "04_d"), "03")
	setDependencies(t, pkgDir(root, "05_e"), "03", "03")
	setDependencies(t, pkgDir(root, "06_f"), "99", "05") // 99 is not in the root

	decode := func(raw json.RawMessage) []string {
		t.Helper()
		var out []string
		if raw == nil || json.Unmarshal(raw, &out) != nil {
			t.Fatalf("%s is not a list", raw)
		}
		return out
	}
	for ref, want := range map[string]struct{ dependsOn, dependents []string }{
		"01": {nil, []string{"03"}},
		"03": {[]string{"01", "02"}, []string{"04", "05"}},
		"05": {[]string{"03"}, []string{"06"}},
		"06": {[]string{"99", "05"}, nil},
	} {
		env, raw := rawRun(t, "--dir", root, ref)
		if got := decode(raw["depends_on"]); !slices.Equal(got, want.dependsOn) {
			t.Errorf("%s: depends_on %v; want %v", ref, got, want.dependsOn)
		}
		if got := decode(raw["dependents"]); !slices.Equal(got, want.dependents) {
			t.Errorf("%s: dependents %v; want %v", ref, got, want.dependents)
		}
		if len(env.Warnings) != 0 {
			t.Errorf("%s: warnings %v; a dangling dependency is the verdict's to report", ref, warningCodes(env))
		}
	}
}

// A spec root that cannot be read costs the report its dependents and nothing
// else, with a low warning that names it.
func TestReport_WithAnUnreadableSpecRoot(t *testing.T) {
	root := newRepo(t)
	pkg := pkgDir(root, "09_agent_mode")
	file := filepath.Join(t.TempDir(), "not-a-directory")
	writeFile(t, file, "")
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

// The report suggests the status call first, where there is one, then the
// claim of a ready next task on a package that can take a write and has nothing
// in progress, then the render of the next task.
func TestReport_SuggestsTheStatusCallTheClaimAndTheRender(t *testing.T) {
	const dir = ".specs/09_agent_mode"
	activateIt := envelope.Next{Tool: "speca", Input: dir, Flags: []string{"--status", "active"}}
	render := func(id int) envelope.Next {
		return envelope.Next{Tool: "specr", Input: dir, Flags: []string{"--task", fmt.Sprint(id)}}
	}
	for name, tc := range map[string]struct {
		setup func(t *testing.T, dir string)
		want  []envelope.Next
	}{
		"a draft that validates, with a ready task": {func(*testing.T, string) {}, []envelope.Next{activateIt, claim(dir, 1), render(1)}},
		"a draft with no intent":                    {removeIntent, []envelope.Next{claim(dir, 1), render(1)}},
		"a draft that does not validate":            {breakValidity, []envelope.Next{claim(dir, 1), render(1)}},
		"a draft that holds a key":                  {withFrontmatterKey, []envelope.Next{claim(dir, 1), render(1)}},
		"a task in progress": {func(t *testing.T, dir string) {
			setTasks(t, dir, planTask(1, "in_progress", nil), planTask(2, "pending", []int{}))
		}, []envelope.Next{render(1)}},
		"a sealed package": {func(t *testing.T, dir string) { setStatus(t, dir, "sealed") }, []envelope.Next{render(1)}},
		"an active package whose intent drifted": {func(t *testing.T, dir string) {
			activate(t, dir)
			driftIntent(t, dir)
		}, []envelope.Next{render(1)}},
		"an active package with no task open": {func(t *testing.T, dir string) {
			activate(t, dir)
			setTasks(t, dir, planTask(1, "done", nil), planTask(2, "dropped", []int{}))
		}, []envelope.Next{{Tool: "speca", Input: dir, Flags: []string{"--status", "sealed"}}}},
		"an active package with a task open": {func(t *testing.T, dir string) {
			activate(t, dir)
			setTasks(t, dir, planTask(1, "done", nil), planTask(2, "pending", nil))
		}, []envelope.Next{claim(dir, 2), render(2)}},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			tc.setup(t, pkgDir(root, "09_agent_mode"))
			env, _, _ := run(t, "--dir", root, "09")
			if len(env.Next) != len(tc.want) {
				t.Fatalf("next = %+v; want %+v", env.Next, tc.want)
			}
			for i, n := range env.Next {
				if !sameCall(n, tc.want[i]) || n.Why == "" {
					t.Errorf("next[%d] = %+v; want %+v, with a reason", i, n, tc.want[i])
				}
			}
			if c := slices.IndexFunc(env.Next, func(n envelope.Next) bool { return n.Tool == "speca" && slices.Contains(n.Flags, "--task") }); c >= 0 &&
				!strings.Contains(env.Next[c].Why, "task "+env.Next[c].Flags[1]) {
				t.Errorf("the claim's reason %q does not name its task", env.Next[c].Why)
			}
		})
	}
}

// The list suggests no status call, whatever the packages it lists.
func TestReport_TheListSuggestsNoStatusCall(t *testing.T) {
	root := newRepo(t)
	activePackage(t, root, "03_done", "03", "done", planTask(1, "done", nil))
	env, _, _ := run(t, "--dir", root, "--list")
	if got := statusCalls(env.Next); len(got) != 0 {
		t.Errorf("the list suggests %+v", got)
	}
}

// For any package, a status call the report suggests is a call that goes
// through. Packages are made in every status, and in the states that make the
// checks refuse: no intent, an invalid package, tasks open, a drifted intent,
// keys the schema does not allow. Each is the one package of its own
// repository, under the name the example validates under.
func TestReport_ASuggestedStatusCallIsNeverRefused(t *testing.T) {
	keys := map[string]func(t *testing.T, dir string){
		"no keys":                  func(*testing.T, string) {},
		"a key in tasks.json":      addUnknownKey,
		"a key in the frontmatter": withFrontmatterKey,
	}
	drifts := map[string]func(t *testing.T, dir string){
		"no drift":           func(*testing.T, string) {},
		"the intent edited":  driftIntent,
		"the intent removed": removeIntent,
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
						removeIntent(t, dir)
					}
					if !valid {
						breakValidity(t, dir)
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
		status := statusOnDisk(t, dir)
		for _, call := range statusCalls(env.Next) {
			suggested[strings.Join(call.Flags, " ")]++
			if status != "draft" && status != "active" {
				t.Errorf("%s: the package is %s, and the report suggests %v", name, status, call.Flags)
			}
			if out, code := followed(t, root, call); code != cli.ExitOK {
				t.Errorf("%s: the suggested call %v exits %d: %+v", name, call.Flags, code, out.Error)
			}
		}
	}
	if suggested["--status active"] == 0 || suggested["--status sealed"] == 0 {
		t.Errorf("suggestions made: %v; the property was checked of none of one kind", suggested)
	}
}
