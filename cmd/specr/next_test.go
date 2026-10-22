package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

const example = "09_agent_mode"

// editTasks rewrites the plan of the package in dir: edit is given each task as
// a JSON object.
func editTasks(t *testing.T, dir string, edit func(tasks []map[string]any)) {
	t.Helper()
	path := filepath.Join(dir, "tasks.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	var tasks []map[string]any
	for _, task := range m["tasks"].([]any) {
		tasks = append(tasks, task.(map[string]any))
	}
	edit(tasks)
	if b, err = json.MarshalIndent(m, "", "  "); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// plan gives the tasks of the example the states, and their depends_on: nil
// leaves the key out, so the task depends on the one before it; an empty list
// says it depends on none.
func plan(t *testing.T, dir string, states []string, deps [][]int) {
	t.Helper()
	editTasks(t, dir, func(tasks []map[string]any) {
		for i, task := range tasks {
			task["state"] = states[i]
			switch {
			case deps == nil:
			case deps[i] == nil:
				delete(task, "depends_on")
			default:
				task["depends_on"] = deps[i]
			}
		}
	})
}

// setStatus gives the package a lifecycle status by the text of its frontmatter.
func setStatus(t *testing.T, dir, status string) {
	t.Helper()
	path := filepath.Join(dir, "prd.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`status: "draft"`)) {
		t.Fatalf("prd.md of %s is not a draft", dir)
	}
	if err := os.WriteFile(path, bytes.Replace(b, []byte(`status: "draft"`), []byte(`status: "`+status+`"`), 1), 0o644); err != nil {
		t.Fatal(err)
	}
}

// activate moves the package to active through the library, which records the
// hash of its intent, and, if drift, changes the intent after it.
func activate(t *testing.T, dir string, drift bool) {
	t.Helper()
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spec.Transition("active", dir); err != nil {
		t.Fatal(err)
	}
	if drift {
		path := filepath.Join(dir, "prd.md")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, bytes.Replace(b, []byte("Coding agents drive"), []byte("Coding agents might drive"), 1), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// bySpeca returns the entries of next that call speca, and by specr those that
// call specr.
func byTool(next []envelope.Next, tool string) []envelope.Next {
	var out []envelope.Next
	for _, n := range next {
		if n.Tool == tool {
			out = append(out, n)
		}
	}
	return out
}

// claims lists the tasks next claims, as the flags of the entries that do.
func claims(next []envelope.Next) [][]string {
	var out [][]string
	for _, n := range byTool(next, "speca") {
		if len(n.Flags) == 4 && n.Flags[2] == "--state" && n.Flags[3] == "in_progress" {
			out = append(out, n.Flags)
		}
	}
	return out
}

// TS-13-15: specr keeps whether the package can take a write in a field of its
// result that is not written, and the schema does not describe it.
func TestWritabilityIsKeptButNotWritten_TS13_15(t *testing.T) {
	for name, tc := range map[string]struct {
		prepare func(t *testing.T, dir string)
		want    bool
	}{
		"a draft":              {func(*testing.T, string) {}, true},
		"an active package":    {func(t *testing.T, dir string) { activate(t, dir, false) }, true},
		"a drifted package":    {func(t *testing.T, dir string) { activate(t, dir, true) }, false},
		"a sealed package":     {func(t *testing.T, dir string) { setStatus(t, dir, "sealed") }, false},
		"a superseded package": {func(t *testing.T, dir string) { setStatus(t, dir, "superseded") }, false},
		"an archived package":  {func(t *testing.T, dir string) { setStatus(t, dir, "archived") }, false},
	} {
		repo := newRepo(t)
		tc.prepare(t, filepath.Join(repo, ".specs", example))
		code, res, failure := execute("09", &options{dir: repo}, cli.NewRun(tool, "test"))
		if code != cli.ExitOK || failure != nil || res.writable != tc.want {
			t.Errorf("%s: code %d, failure %+v, writable %v; want %v", name, code, failure, res != nil && res.writable, tc.want)
		}
		var stdout, stderr bytes.Buffer
		Main([]string{"--dir", repo, "09"}, &stdout, &stderr)
		for _, key := range []string{"writable", "intent_drift"} {
			if strings.Contains(stdout.String(), key) {
				t.Errorf("%s: the result has a %q key", name, key)
			}
		}
	}
	var stdout, stderr bytes.Buffer
	Main([]string{"--schema"}, &stdout, &stderr)
	for _, key := range []string{"writable", "intent_drift"} {
		if strings.Contains(stdout.String(), key) {
			t.Errorf("--schema describes a %q field", key)
		}
	}
}

// TS-13-16: a whole render of a package that can take a write, with nothing in
// progress, offers the claim of its first task, and then the render of it.
func TestAWholeRenderOffersTheClaimOfTheFirstTask_TS13_16(t *testing.T) {
	root := newRepo(t)
	env, code, _ := run(t, "--dir", root, "09")
	if code != cli.ExitOK {
		t.Fatalf("code %d", code)
	}
	title := env.Result.Tasks[0].Title
	if len(env.Next) != 2 {
		t.Fatalf("next = %+v; want the claim and the render", env.Next)
	}
	claim, render := env.Next[0], env.Next[1]
	if claim.Tool != "speca" || claim.Input != env.Result.SpecDir || !slices.Equal(claim.Flags, []string{"--task", "1", "--state", "in_progress"}) {
		t.Errorf("next[0] = %+v; want the claim of task 1 against %s", claim, env.Result.SpecDir)
	}
	for _, want := range []string{"task 1", title, "next ready task", "claim", "no other agent"} {
		if !strings.Contains(claim.Why, want) {
			t.Errorf("why = %q; want it to say %q", claim.Why, want)
		}
	}
	if render.Tool != "specr" || render.Input != claim.Input || !slices.Equal(render.Flags, []string{"--task", "1"}) || len(claim.Flags) != 4 {
		t.Errorf("next[1] = %+v", render)
	}
}

// TS-13-17: a whole render of any shape offers the claim of the first task that
// is not finished, when that task is ready.
func TestAWholeRenderWithSectionsOrABudgetOffersTheClaim_TS13_17(t *testing.T) {
	root := newRepo(t)
	plan(t, filepath.Join(root, ".specs", example), []string{"done", "pending", "pending"}, nil)
	for _, extra := range [][]string{{"--section", "tasks"}, {"--max-tokens", "2000"}, nil} {
		env, code, _ := run(t, append([]string{"--dir", root, "09"}, extra...)...)
		if code != cli.ExitOK || len(env.Next) != 2 ||
			!slices.Equal(env.Next[0].Flags, []string{"--task", "2", "--state", "in_progress"}) || !slices.Equal(env.Next[1].Flags, []string{"--task", "2"}) {
			t.Errorf("%v: code %d, next = %+v; want the claim of task 2, then its render", extra, code, env.Next)
		}
	}
}

// TS-13-18: with a task in progress, a whole render offers no claim, and keeps
// its render suggestion.
func TestAWholeRenderWithATaskInProgressOffersNoClaim_TS13_18(t *testing.T) {
	root := newRepo(t)
	plan(t, filepath.Join(root, ".specs", example), []string{"pending", "in_progress", "pending"}, [][]int{nil, {}, {}})
	env, _, _ := run(t, "--dir", root, "09")
	if len(byTool(env.Next, "speca")) != 0 || len(env.Next) != 1 || !slices.Equal(env.Next[0].Flags, []string{"--task", "1"}) {
		t.Errorf("next = %+v; want the render of task 1 alone", env.Next)
	}
}

// TS-13-19: when the first unfinished task is not ready, a whole render offers no
// claim.
func TestAWholeRenderWhoseFirstTaskIsNotReadyOffersNoClaim_TS13_19(t *testing.T) {
	root := newRepo(t)
	plan(t, filepath.Join(root, ".specs", example), []string{"pending", "pending", "pending"}, [][]int{{2}, nil, nil})
	env, _, _ := run(t, "--dir", root, "09")
	if len(byTool(env.Next, "speca")) != 0 || len(env.Next) != 1 || env.Next[0].Tool != "specr" || !slices.Equal(env.Next[0].Flags, []string{"--task", "1"}) {
		t.Errorf("next = %+v; want the render of task 1 alone", env.Next)
	}
}

// TS-13-20: a package that cannot take a write is offered no claim, and keeps its
// render suggestion.
func TestAWholeRenderOfAPackageThatCannotTakeAWriteOffersNoClaim_TS13_20(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, dir string){
		"sealed":     func(t *testing.T, dir string) { setStatus(t, dir, "sealed") },
		"superseded": func(t *testing.T, dir string) { setStatus(t, dir, "superseded") },
		"archived":   func(t *testing.T, dir string) { setStatus(t, dir, "archived") },
		"drifted":    func(t *testing.T, dir string) { activate(t, dir, true) },
	} {
		root := newRepo(t)
		prepare(t, filepath.Join(root, ".specs", example))
		env, code, _ := run(t, "--dir", root, "09")
		if code != cli.ExitOK || len(byTool(env.Next, "speca")) != 0 || len(env.Next) != 1 || env.Next[0].Tool != "specr" {
			t.Errorf("%s: code %d, next = %+v; want the render alone", name, code, env.Next)
		}
	}
}

// TS-13-21: with every task finished there is nothing to suggest.
func TestAWholeRenderOfAFinishedPlanOffersNothing_TS13_21(t *testing.T) {
	root := newRepo(t)
	plan(t, filepath.Join(root, ".specs", example), []string{"done", "dropped", "done"}, nil)
	env, code, _ := run(t, "--dir", root, "09")
	if code != cli.ExitOK || len(env.Next) != 0 {
		t.Errorf("code %d, next = %+v; want none", code, env.Next)
	}
}

// The real speca command is built once for the tests that need it. Its Main is
// in a package main, which a test cannot import.
var specaBuild struct {
	once sync.Once
	dir  string
	err  string
}

func TestMain(m *testing.M) {
	code := m.Run()
	if specaBuild.dir != "" {
		_ = os.RemoveAll(specaBuild.dir)
	}
	os.Exit(code)
}

// buildSpeca returns the path of the real speca command, built on first use.
// Building takes seconds, so -short skips what needs it.
func buildSpeca(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("-short: building speca takes seconds")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go tool is not on the path, so speca cannot be built")
	}
	specaBuild.once.Do(func() {
		dir, err := os.MkdirTemp("", "speca-build")
		if err != nil {
			specaBuild.err = err.Error()
			return
		}
		specaBuild.dir = dir
		if out, err := exec.Command(goTool, "build", "-o", filepath.Join(dir, "speca"), "github.com/agent-fox-dev/spec/cmd/speca").CombinedOutput(); err != nil {
			specaBuild.err = fmt.Sprintf("%v\n%s", err, out)
		}
	})
	if specaBuild.err != "" {
		t.Fatalf("building speca: %s", specaBuild.err)
	}
	return filepath.Join(specaBuild.dir, "speca")
}

// speca runs the built command and decodes its envelope.
func speca(t *testing.T, bin string, argv ...string) envelope.AdminEnvelope {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, argv...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	_ = cmd.Run() // a refusal exits non-zero, and says so in its envelope
	env, err := envelope.DecodeSpeca(stdout.Bytes())
	if err != nil {
		t.Fatalf("speca %v: stdout is not one envelope (%v):\nstdout %s\nstderr %s", argv, err, stdout.String(), stderr.String())
	}
	return env
}

// TS-13-22: for a package of any state, specr and speca claim the same task, or
// neither does, and the task is the one specr's render suggestion names. The
// states are the tasks' states and their dependencies on earlier tasks; a
// dependency on a later one is a package that does not validate, and has its own
// test below.
func TestSpecrAndSpecaClaimTheSameTask_TS13_22(t *testing.T) {
	bin := buildSpeca(t)
	taskStates := []string{"pending", "in_progress", "done", "dropped"}
	depVariants := [][][]int{
		{nil, nil, nil},    // each depends on the one before it
		{{}, {}, {}},       // none depends on any
		{nil, {1}, {1, 2}}, // as the example is written
		{{}, {1}, nil},     // mixed
	}
	type state struct {
		status string
		states []string
		deps   [][]int
	}
	// Every combination of the three tasks' states, each with a shape of the
	// dependencies drawn from a fixed seed, and a few of the same for each of the
	// other statuses.
	rng := rand.New(rand.NewPCG(13, 22))
	var all []state
	for _, a := range taskStates {
		for _, b := range taskStates {
			for _, c := range taskStates {
				all = append(all, state{"draft", []string{a, b, c}, depVariants[rng.IntN(len(depVariants))]})
			}
		}
	}
	for _, status := range []string{"active", "drifted", "sealed", "superseded", "archived"} {
		for i := 0; i < 6; i++ {
			s := all[rng.IntN(64)]
			all = append(all, state{status, s.states, depVariants[rng.IntN(len(depVariants))]})
		}
	}

	roots := map[string]string{}
	rootFor := func(status string) string {
		if root, ok := roots[status]; ok {
			return root
		}
		root := newRepo(t)
		dir := filepath.Join(root, ".specs", example)
		switch status {
		case "active":
			activate(t, dir, false)
		case "drifted":
			activate(t, dir, true)
		case "sealed", "superseded", "archived":
			setStatus(t, dir, status)
		}
		roots[status] = root
		return root
	}

	claimed, checked := 0, 0
	for _, s := range all {
		root := rootFor(s.status)
		plan(t, filepath.Join(root, ".specs", example), s.states, s.deps)
		env, _, _ := run(t, "--dir", root, "09")
		a := claims(env.Next)
		b := claims(speca(t, bin, "--dir", root, "09").Next)
		checked++
		if !equalFlags(a, b) {
			t.Errorf("%s %v deps %v: specr claims %v, speca %v", s.status, s.states, s.deps, a, b)
			continue
		}
		if len(a) > 0 {
			claimed++
			renders := byTool(env.Next, "specr")
			if len(renders) != 1 || renders[0].Flags[1] != a[0][1] {
				t.Errorf("%s %v deps %v: the claim names task %s, the render suggestion %+v", s.status, s.states, s.deps, a[0][1], renders)
			}
		}
	}
	if claimed == 0 || claimed == checked {
		t.Errorf("%d of %d states were claimed; the property was checked of one kind only", claimed, checked)
	}
	t.Logf("%d states, %d of them claimed", checked, claimed)
}

func equalFlags(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !slices.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

// D-3: with a task that depends on a later one, which makes the package fail
// validation, the first unfinished task is not ready while a later one is. specr
// offers no claim, as 13-REQ-5.4 says, where speca claims the later ready task;
// specr never claims what speca would not.
func TestAWholeRenderWithAForwardDependencyClaimsNothingSpecaWouldNot(t *testing.T) {
	bin := buildSpeca(t)
	root := newRepo(t)
	plan(t, filepath.Join(root, ".specs", example), []string{"done", "pending", "pending"}, [][]int{nil, {3}, {1}})

	env, _, _ := run(t, "--dir", root, "09")
	if len(claims(env.Next)) != 0 || len(env.Next) != 1 || !slices.Equal(env.Next[0].Flags, []string{"--task", "2"}) {
		t.Errorf("specr: next = %+v; want the render of task 2 alone", env.Next)
	}
	theirs := speca(t, bin, "--dir", root, "09")
	if got := claims(theirs.Next); len(got) != 1 || got[0][1] != "3" {
		t.Errorf("speca: claims %v; want the claim of the ready task 3", got)
	}
	if theirs.Result == nil || theirs.Result.Valid == nil || *theirs.Result.Valid {
		t.Error("a package that depends on a later task validates")
	}
}
