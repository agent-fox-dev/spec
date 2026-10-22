package main

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// specr builds the real specr command and returns its path. Its Main is in a
// package main, which a test cannot import, and the smoke tests must not stand
// a double in its place. Building takes seconds, so -short skips what needs it.
func specr(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("-short: building specr takes seconds")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go tool is not on the path, so specr cannot be built")
	}
	bin := filepath.Join(t.TempDir(), "specr")
	if out, err := exec.Command(goTool, "build", "-o", bin, "github.com/agent-fox-dev/spec/cmd/specr").CombinedOutput(); err != nil {
		t.Fatalf("building specr: %v\n%s", err, out)
	}
	return bin
}

// TS-10-57: an agent reads a package's state and renders the task that was
// suggested, with the real specr, and nothing in the repository changes.
func TestAnAgentReadsAPackageAndFollowsNext_TS10_57(t *testing.T) {
	bin := specr(t)
	root := newRepo(t)
	t.Chdir(root)
	before := snapshot(t, root)

	first, code, _ := run(t, "09")
	r := result(t, first)
	if code != cli.ExitOK || r.SpecID != "09" || len(r.Tasks) != 3 || r.NextTask == nil || len(r.AllowedStatus) == 0 {
		t.Fatalf("code %d: %+v", code, r)
	}
	if !r.Tasks[0].Ready || r.Tasks[1].Ready || r.NextTask.ID != 1 {
		t.Fatalf("tasks %+v next %+v", r.Tasks, r.NextTask)
	}
	// A draft that validates is told to activate (spec 12); then comes the claim
	// of task 1 (spec 11), and the render of it after.
	if len(first.Next) != 3 || !slices.Equal(first.Next[0].Flags, []string{"--status", "active"}) ||
		first.Next[1].Tool != "speca" || !slices.Equal(first.Next[1].Flags, []string{"--task", "1", "--state", "in_progress"}) ||
		first.Next[2].Tool != "specr" || first.Next[2].Input != filepath.Join(".specs", "09_agent_mode") ||
		!slices.Equal(first.Next[2].Flags, []string{"--task", "1"}) {
		t.Fatalf("next = %+v", first.Next)
	}

	n := first.Next[2]
	cmd := exec.Command(bin, append([]string{n.Input}, n.Flags...)...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("specr %s %s: %v\n%s", n.Input, strings.Join(n.Flags, " "), err, stderr.String())
	}
	second, err := envelope.DecodeSpecr(stdout.Bytes())
	if err != nil || second.Result == nil || second.Result.Task == nil {
		t.Fatalf("specr's output: %v\n%s", err, stdout.String())
	}
	if second.Result.Task.ID != r.NextTask.ID || !second.Result.Task.Ready || second.Result.Scope != "task" {
		t.Errorf("specr rendered %+v; want task %d, ready", second.Result.Task, r.NextTask.ID)
	}
	for i, task := range second.Result.Tasks {
		if task.Ready != r.Tasks[i].Ready {
			t.Errorf("task %d: speca says ready %v, specr %v", task.ID, r.Tasks[i].Ready, task.Ready)
		}
	}

	if after := snapshot(t, root); !mapsEqual(before, after) {
		t.Errorf("the repository changed:\nbefore %v\nafter  %v", before, after)
	}
}

// TS-10-58: an agent asks for the verdict on a package that depends on another
// and shares no actor with it, and is told which rules fail.
func TestAnAgentAsksForTheFullVerdict_TS10_58(t *testing.T) {
	root := t.TempDir()
	downstream := addPackage(t, root, "02_down", "02", "down")
	upstream := addPackage(t, root, "01_up", "01", "up")
	addPackage(t, root, "03_other", "03", "other")
	setDependencies(t, downstream, "01")
	setActors(t, upstream, "someone else entirely")
	before := snapshot(t, root)

	env, raw := rawRun(t, "--dir", root, "02", "--validate")
	r := result(t, env)
	if env.ExitCode != cli.ExitOK || !env.OK || r.Valid == nil || *r.Valid || r.Validation == nil || r.Traceability == nil {
		t.Fatalf("exit %d: %+v", env.ExitCode, r)
	}
	if !slices.Contains(checks(r.Validation.Errors), "cross_spec_actor") {
		t.Errorf("errors %v; want the actor error kept", checks(r.Validation.Errors))
	}
	for _, k := range []string{"tasks", "task_counts", "next_task"} {
		if _, ok := raw[k]; ok {
			t.Errorf("the verdict writes %s", k)
		}
	}
	if !strings.Contains(env.Summary, "does not validate") || !strings.Contains(env.Summary, "cross_spec_actor") ||
		!strings.Contains(env.Summary, "errors") && !strings.Contains(env.Summary, "error:") {
		t.Errorf("summary = %q; want the error count and the rules", env.Summary)
	}
	if len(env.Next) != 0 {
		t.Errorf("next = %+v", env.Next)
	}
	if after := snapshot(t, root); !mapsEqual(before, after) {
		t.Errorf("the repository changed")
	}
}

// TS-10-59: an agent lists every package, archived ones too, and is told which
// active package has a task ready.
func TestAnAgentListsEveryPackageWithTheArchive_TS10_59(t *testing.T) {
	root := t.TempDir()
	one := addPackage(t, root, "01_a", "01", "a")
	setTasks(t, one, planTask(1, "pending", nil))
	two := addPackage(t, root, "02_b", "02", "b")
	setStatus(t, two, "active")
	setTasks(t, two, planTask(1, "in_progress", nil))
	three := addPackage(t, root, "03_c", "03", "c")
	setStatus(t, three, "active")
	setTasks(t, three, planTask(1, "done", nil), planTask(2, "pending", []int{1}))
	four := addPackage(t, root, "04_d", "04", "d")
	setStatus(t, four, "sealed")
	setTasks(t, four, planTask(1, "done", nil))
	broken := addPackage(t, root, "05_bad", "05", "bad")
	if err := os.WriteFile(filepath.Join(broken, "prd.md"), []byte("---\nspec_id: [unclosed\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := addPackage(t, root, "archive/06_old", "06", "old")
	setStatus(t, old, "superseded")
	before := snapshot(t, root)

	env, packages := rawPackages(t, "--dir", root, "--list", "--all")
	if env.ExitCode != cli.ExitOK || !env.OK {
		t.Fatalf("exit %d: %+v", env.ExitCode, env.Error)
	}
	got := env.Result.Packages
	if want := []string{".specs/01_a", ".specs/02_b", ".specs/03_c", ".specs/04_d", ".specs/05_bad", ".specs/archive/06_old"}; !slices.Equal(dirs(got), want) {
		t.Fatalf("packages %v; want %v", dirs(got), want)
	}
	if last := got[5]; !last.Archived || last.Status != "superseded" {
		t.Errorf("the archived package: %+v", last)
	}
	if len(packages[4]) != 2 || got[4].LoadError == "" {
		t.Errorf("the malformed package holds %v; want spec_dir and load_error only", packages[4])
	}
	if want := "speca: 6 packages (1 draft, 2 active, 1 sealed, 1 superseded, 1 unreadable)"; env.Summary != want {
		t.Errorf("summary = %q; want %q", env.Summary, want)
	}
	if len(env.Next) != 2 || env.Next[0].Tool != "speca" || !slices.Equal(env.Next[0].Flags, []string{"--task", "2", "--state", "in_progress"}) ||
		env.Next[1].Tool != "specr" || env.Next[1].Input != filepath.Join(".specs", "03_c") || !slices.Equal(env.Next[1].Flags, []string{"--task", "2"}) {
		t.Errorf("next = %+v; want the claim of .specs/03_c's task 2, then specr .specs/03_c --task 2", env.Next)
	}
	if after := snapshot(t, root); !mapsEqual(before, after) {
		t.Errorf("the repository changed")
	}
}

// TS-10-60: a malformed call is refused before anything is read.
func TestAMalformedCallIsRefusedBeforeAnythingIsRead_TS10_60(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)
	before := snapshot(t, root)

	var stdout, stderr bytes.Buffer
	code := Main([]string{"--list", "09"}, &stdout, &stderr)
	env, err := envelope.DecodeSpeca(stdout.Bytes())
	if err != nil {
		t.Fatalf("stdout is not one envelope: %v\n%s", err, stdout.String())
	}
	if code != cli.ExitUsage || env.ExitCode != cli.ExitUsage || env.Error == nil || env.Error.Stage != "usage" || env.Error.Category != "usage" || env.Result != nil {
		t.Errorf("code %d: %+v", code, env.Error)
	}
	if !strings.Contains(stderr.String(), env.Error.Message) || env.Error.Message == "" {
		t.Errorf("stderr %q does not hold the message", stderr.String())
	}
	if after := snapshot(t, root); !mapsEqual(before, after) {
		t.Errorf("the repository changed")
	}
}

// readFiles returns the bytes of the named files of the package.
func readFiles(t *testing.T, dir string, names ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		out[name] = string(b)
	}
	return out
}

// TS-11-54 (11-PATH-1): an agent reads the report, takes the claim it suggests,
// has the task rendered, and finishes it; the package on disk follows.
func TestAnAgentClaimsTheNextTaskAndFinishesIt_TS11_54(t *testing.T) {
	bin := specr(t)
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	t.Chdir(root)

	report, code, _ := run(t, "09")
	// The activation of a draft comes before the tasks (spec 12), then the claim.
	if code != cli.ExitOK || len(report.Next) < 2 || !slices.Equal(report.Next[0].Flags, []string{"--status", "active"}) ||
		report.Next[1].Tool != "speca" || !slices.Equal(report.Next[1].Flags, []string{"--task", "1", "--state", "in_progress"}) {
		t.Fatalf("code %d: next = %+v; want the claim of task 1 after the activation", code, report.Next)
	}

	claim, code, _ := run(t, "09", "--task", "next", "--state", "in_progress")
	if code != cli.ExitOK || !claim.OK || len(result(t, claim).Changes) != 1 {
		t.Fatalf("code %d: %+v", code, claim.Error)
	}
	if c := result(t, claim).Changes[0]; c.Kind != "task" || c.ID != 1 || c.From != "pending" || c.To != "in_progress" {
		t.Errorf("change = %+v", c)
	}
	if len(claim.Next) != 1 || claim.Next[0].Tool != "specr" || !slices.Equal(claim.Next[0].Flags, []string{"--task", "1"}) {
		t.Fatalf("next = %+v; want specr --task 1", claim.Next)
	}
	if got := onDisk(t, dir); got[1] != "in_progress" {
		t.Errorf("tasks.json: %v", got)
	}
	afterClaim := result(t, claim).UpdatedAt
	if afterClaim == "2026-09-08T09:00:00Z" {
		t.Errorf("updated_at = %q; want it set by the write", afterClaim)
	}

	// The call it suggested renders the task that was claimed.
	n := claim.Next[0]
	cmd := exec.Command(bin, append([]string{n.Input}, n.Flags...)...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("specr: %v\n%s", err, stderr.String())
	}
	rendered, err := envelope.DecodeSpecr(stdout.Bytes())
	if err != nil || rendered.Result == nil || rendered.Result.Task == nil || rendered.Result.Task.ID != 1 || rendered.Result.Task.State != "in_progress" {
		t.Errorf("specr rendered %+v (%v); want task 1, in progress", rendered.Result, err)
	}

	done, code, _ := run(t, "09", "--task", "1", "--state", "done")
	if code != cli.ExitOK || onDisk(t, dir)[1] != "done" {
		t.Fatalf("code %d: %+v; on disk %v", code, done.Error, onDisk(t, dir))
	}
	if len(done.Next) == 0 || !slices.Equal(done.Next[0].Flags, []string{"--task", "2", "--state", "in_progress"}) {
		t.Errorf("after task 1 is done: next = %+v; want the claim of task 2", done.Next)
	}
	if result(t, done).UpdatedAt < afterClaim {
		t.Errorf("updated_at went from %q back to %q", afterClaim, result(t, done).UpdatedAt)
	}
}

// TS-11-55 (11-PATH-2): a second agent's claim of a task that is taken is
// refused with the states it may move to and the package as it is on disk, and
// the package is not touched.
func TestASecondAgentsClaimIsRefused_TS11_55(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	setTasks(t, dir, planTask(1, "in_progress", nil), planTask(2, "pending", []int{1}))
	before := readFiles(t, dir, "tasks.json", "prd.md")

	env, code, _ := run(t, "--dir", root, "09", "--task", "1", "--state", "in_progress")
	if code != cli.ExitFailed || env.OK || env.Error == nil || env.Error.Stage != "task" || env.Error.Category != "lifecycle" {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	if env.Error.FixHint == nil || env.Error.FixHint.Flag != "--state" || !slices.Equal(env.Error.FixHint.Valid, []string{"pending", "done"}) {
		t.Errorf("fix_hint = %+v; want --state with pending and done", env.Error.FixHint)
	}
	r := env.Result
	if r == nil || len(r.Tasks) != 2 || r.Tasks[0].State != "in_progress" || r.NextTask == nil || r.NextTask.ID != 1 {
		t.Errorf("result = %+v; want the package as it is on disk", r)
	}
	if r != nil && len(r.Changes) != 0 {
		t.Errorf("changes %+v; want none", r.Changes)
	}
	if len(env.Next) != 0 {
		t.Errorf("next %+v; want none", env.Next)
	}
	if after := readFiles(t, dir, "tasks.json", "prd.md"); !maps.Equal(before, after) {
		t.Error("tasks.json or prd.md changed")
	}
}

// TS-11-56 (11-PATH-3): a dry run of a forced recovery of a dropped task says
// what it would do and writes nothing; the call it suggests then does it.
func TestADryRunOfAForcedRecoveryThenTheRealCall_TS11_56(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	setTasks(t, dir, planTask(1, "done", nil), planTask(2, "done", []int{1}), planTask(3, "dropped", nil))
	before := snapshot(t, root)

	dry, code, _ := run(t, "--dir", root, "09", "--task", "3", "--state", "pending", "--force", "--dry-run")
	r := result(t, dry)
	if code != cli.ExitOK || !r.DryRun || len(r.Changes) != 1 {
		t.Fatalf("code %d: dry_run %v, changes %+v, error %+v", code, r.DryRun, r.Changes, dry.Error)
	}
	if c := r.Changes[0]; c.ID != 3 || c.From != "dropped" || c.To != "pending" || !c.Forced {
		t.Errorf("change = %+v", c)
	}
	if ws := warningsOf(dry, "task_state_forced"); len(ws) != 1 || ws[0].Severity != "high" || ws[0].Stage != "task" {
		t.Errorf("warnings = %+v; want one high task_state_forced", dry.Warnings)
	}
	if !mapsEqual(before, snapshot(t, root)) {
		t.Error("the dry run changed the repository")
	}
	want := envelope.Next{Tool: "speca", Input: filepath.Join(".specs", "09_agent_mode"), Flags: []string{"--task", "3", "--state", "pending", "--force"}}
	if len(dry.Next) != 1 || !hasCall(dry.Next, want) {
		t.Fatalf("next = %+v; want %+v", dry.Next, want)
	}

	n := dry.Next[0]
	real, code, _ := run(t, append([]string{"--dir", root, n.Input}, n.Flags...)...)
	if code != cli.ExitOK || real.Result == nil || real.Result.DryRun || onDisk(t, dir)[3] != "pending" {
		t.Errorf("code %d: %+v; on disk %v", code, real.Error, onDisk(t, dir))
	}
	if len(warningsOf(real, "task_state_forced")) != 1 || !real.Result.Changes[0].Forced {
		t.Errorf("the real call's warnings %+v, changes %+v", real.Warnings, real.Result.Changes)
	}
}
