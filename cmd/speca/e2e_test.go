package main

import (
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// An agent reads a package's state, renders the task it is pointed at with the
// real specr, claims it as suggested, has it rendered again, and finishes it.
// Only the built specr proves that the calls speca suggests are calls specr
// takes, and that the two agree on the tasks. Reading and rendering change
// nothing; the claim and the finish are on disk.
func TestAnAgentFollowsNextFromTheReportToTheFinishedTask(t *testing.T) {
	bin := specr(t)
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	t.Chdir(root)
	before := snapshot(t, root)

	// The report: a draft that validates is told to activate, then to claim task
	// 1, then to render it.
	report, code, _ := run(t, "09")
	r := result(t, report)
	if code != cli.ExitOK || r.SpecID != "09" || len(r.Tasks) != 3 || r.NextTask == nil || r.NextTask.ID != 1 || !r.Tasks[0].Ready || r.Tasks[1].Ready ||
		r.Tasks[2].Kind != "integration" || r.Tasks[2].Ready {
		t.Fatalf("code %d: %+v", code, r)
	}
	const input = ".specs/09_agent_mode"
	want := []envelope.Next{
		{Tool: "speca", Input: input, Flags: []string{"--status", "active"}},
		claim(input, 1),
		{Tool: "specr", Input: input, Flags: []string{"--task", "1"}},
	}
	if len(report.Next) != 3 || !sameCall(report.Next[0], want[0]) || !sameCall(report.Next[1], want[1]) || !sameCall(report.Next[2], want[2]) {
		t.Fatalf("next = %+v; want %+v", report.Next, want)
	}

	rendered := runSpecr(t, bin, root, report.Next[2])
	if task := rendered.Result.Task; task == nil || task.ID != 1 || !task.Ready || rendered.Result.Scope != "task" {
		t.Errorf("specr rendered %+v; want task 1, ready", task)
	}
	for i, task := range rendered.Result.Tasks {
		if task.Ready != r.Tasks[i].Ready {
			t.Errorf("task %d: speca says ready %v, specr %v", task.ID, r.Tasks[i].Ready, task.Ready)
		}
	}
	if !maps.Equal(before, snapshot(t, root)) {
		t.Error("reading and rendering changed the repository")
	}

	claimed, code, _ := run(t, append([]string{report.Next[1].Input}, report.Next[1].Flags...)...)
	if code != cli.ExitOK || onDisk(t, dir)[1] != "in_progress" ||
		!slices.Equal(result(t, claimed).Changes, []envelope.Change{{Kind: "task", ID: 1, From: "pending", To: "in_progress"}}) {
		t.Fatalf("the claim: code %d, %+v, on disk %v", code, claimed.Error, onDisk(t, dir))
	}
	if len(claimed.Next) != 1 || !sameCall(claimed.Next[0], want[2]) {
		t.Fatalf("after the claim: next = %+v; want the render of task 1", claimed.Next)
	}
	if task := runSpecr(t, bin, root, claimed.Next[0]).Result.Task; task == nil || task.ID != 1 || task.State != "in_progress" {
		t.Errorf("specr rendered %+v; want task 1, in progress", task)
	}

	done, code, _ := run(t, "09", "--task", "1", "--state", "done")
	if code != cli.ExitOK || onDisk(t, dir)[1] != "done" {
		t.Fatalf("code %d: %+v; on disk %v", code, done.Error, onDisk(t, dir))
	}
	if len(done.Next) == 0 || !sameCall(done.Next[0], claim(input, 2)) {
		t.Errorf("after task 1 is done: next = %+v; want the claim of task 2", done.Next)
	}
}

// An agent activates a draft whose render is current, and the activation writes
// the render of the activated package: its new status and its intent hash.
func TestAnAgentActivatesAPackageAndItsRenderIsWrittenWithIt(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	succeeds(t, root, "09", "--render")

	_, r := succeeds(t, root, "09", "--status", "active")
	if !slices.Equal(r.Changes, []envelope.Change{{Kind: "status", From: "draft", To: "active"}}) {
		t.Fatalf("changes %+v; want draft → active", r.Changes)
	}
	got := renderedFile(t, dir)
	if got != renderOf(t, dir) {
		t.Error("spec.md is not the render of the activated package")
	}
	if !strings.Contains(got, "**Status:** active") || r.IntentHash == "" || !strings.Contains(got, r.IntentHash) {
		t.Errorf("spec.md does not carry the new status and the intent hash %q", r.IntentHash)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "*.tmp.*")); len(matches) > 0 {
		t.Errorf("temp files were left behind: %v", matches)
	}
}

// An agent edits a package by hand, is told by the verdict that its render is
// stale and how to repair it, repairs it with one call, and the verdict is
// clean again.
func TestAnAgentIsToldItsRenderIsStaleAndRepairsIt(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	setDependencies(t, dir) // the example depends on a spec 08 the root does not hold
	succeeds(t, root, "09", "--render")
	editFile(t, filepath.Join(dir, "requirements.json"), `"JSON-only stdout in agent mode"`, `"JSON-only stdout in agent mode, edited by hand"`)

	_, v := succeeds(t, root, "09", "--validate")
	var finding *envelope.ValidationItem
	for i, e := range v.Validation.Errors {
		if e.Check == "rendered" {
			finding = &v.Validation.Errors[i]
		}
	}
	if *v.Valid || finding == nil {
		t.Fatalf("valid %v, errors %+v; want a rendered error", *v.Valid, v.Validation.Errors)
	}
	if !regexp.MustCompile(`line \d+`).MatchString(finding.Message) || !strings.Contains(finding.Message, "speca 09_agent_mode --render") {
		t.Errorf("message %q does not name the line and the call that repairs it", finding.Message)
	}

	before := withoutRender(treeBytes(t, dir))
	_, r := succeeds(t, root, "09", "--render")
	if got := outcomes(r.Rendered); !slices.Equal(got, []string{".specs/09_agent_mode written"}) {
		t.Errorf("rendered %v", got)
	}
	if !maps.Equal(before, withoutRender(treeBytes(t, dir))) {
		t.Error("the render changed a file other than spec.md")
	}

	_, v = succeeds(t, root, "09", "--validate")
	if !*v.Valid {
		t.Errorf("the repaired package does not validate: %+v", v.Validation.Errors)
	}
	for _, e := range append(v.Validation.Errors, v.Validation.Warnings...) {
		if e.Check == "rendered" {
			t.Errorf("a rendered finding is left: %+v", e)
		}
	}
}

// A maintainer sees which renders a renderer change made stale, brings every
// one of them up to date, archive included, and finds only spec.md changed.
func TestAMaintainerBringsEveryRenderUpToDate(t *testing.T) {
	root := t.TempDir()
	dirs := []string{
		addPackage(t, root, "01_a", "01", "a"), addPackage(t, root, "02_b", "02", "b"),
		addPackage(t, root, "03_c", "03", "c"), addPackage(t, root, "archive/04_d", "04", "d"),
	}
	for _, dir := range dirs {
		writeFile(t, filepath.Join(dir, afspec.RenderedFileName), "<!-- an earlier renderer -->\n\n# PRD\n")
	}
	specs := filepath.Join(root, ".specs")
	before := snapshot(t, specs)
	contents := treeBytes(t, specs)

	_, dry := succeeds(t, root, "--render", "--all", "--dry-run")
	want := []string{".specs/01_a written", ".specs/02_b written", ".specs/03_c written", ".specs/archive/04_d written"}
	if got := outcomes(dry.Rendered); !dry.DryRun || !slices.Equal(got, want) {
		t.Errorf("the dry run: dry_run %v, rendered %v; want %v", dry.DryRun, got, want)
	}
	if !maps.Equal(before, snapshot(t, specs)) {
		t.Fatal("the dry run wrote to the spec root")
	}

	env, r := succeeds(t, root, "--render", "--all")
	if got := outcomes(r.Rendered); !slices.Equal(got, want) {
		t.Errorf("rendered %v; want %v", got, want)
	}
	if env.Summary != "speca: 4 written, 0 unchanged, 0 failed" {
		t.Errorf("summary %q", env.Summary)
	}
	for _, dir := range dirs {
		if renderedFile(t, dir) != renderOf(t, dir) {
			t.Errorf("%s holds no current spec.md", dir)
		}
	}
	after := treeBytes(t, specs)
	for name := range after {
		if after[name] != contents[name] && filepath.Base(name) != afspec.RenderedFileName {
			t.Errorf("%s changed, and it is not a spec.md", name)
		}
	}
}

// A maintainer amends the steering, is told which amendment an active package
// was written before, checks the package, and clears the warning by moving the
// package's steering_version.
func TestAMaintainerAmendsTheSteeringAndClearsWhatAPackageMissed(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	setDependencies(t, dir) // the example depends on a spec 08 the root does not hold
	editFile(t, filepath.Join(dir, "prd.md"), "schema_version: 3\n", "schema_version: 3\nsteering_version: \"1.0.0\"\n")
	activate(t, dir)
	steering := filepath.Join(root, ".specs", "steering.md")
	const baseline = "  - version: 1.0.0\n    date: 2026-10-15\n    change: The baseline.\n"
	writeFile(t, steering, "---\nversion: 1.0.0\namendments:\n"+baseline+"---\n# Steering\n\nUse make check.\n")

	// The amendment: a directive, and a record of it at 1.1.0.
	writeFile(t, steering, "---\nversion: 1.1.0\namendments:\n"+
		"  - version: 1.1.0\n    date: 2026-11-06\n    change: Every command writes one JSON object.\n"+baseline+
		"---\n# Steering\n\nUse make check.\n\nEvery command writes one JSON object.\n")
	before := withoutPRD(treeBytes(t, dir))

	_, v := succeeds(t, root, "09", "--validate")
	var found []envelope.ValidationItem
	for _, w := range v.Validation.Warnings {
		if w.Check == "steering_version" {
			found = append(found, w)
		}
	}
	if !*v.Valid || len(found) != 1 {
		t.Fatalf("valid %v, steering_version findings %+v; want valid and one warning", *v.Valid, found)
	}
	for _, s := range []string{"1.0.0", "1.1.0", "2026-11-06", "Every command writes one JSON object."} {
		if !strings.Contains(found[0].Message, s) {
			t.Errorf("message %q lacks %q", found[0].Message, s)
		}
	}

	// A hand edit of prd.md leaves spec.md behind it, so the edit is rendered.
	editFile(t, filepath.Join(dir, "prd.md"), `steering_version: "1.0.0"`, `steering_version: "1.1.0"`)
	succeeds(t, root, "09", "--render")
	_, v = succeeds(t, root, "09", "--validate")
	if !*v.Valid {
		t.Errorf("the package does not validate: %+v", v.Validation.Errors)
	}
	for _, w := range append(v.Validation.Errors, v.Validation.Warnings...) {
		if w.Check == "steering_version" {
			t.Errorf("a finding is left: %+v", w)
		}
	}
	if !maps.Equal(before, withoutPRD(treeBytes(t, dir))) {
		t.Error("a file of the package other than prd.md and its render changed")
	}
}

// withoutPRD is the files of a tree, prd.md and its render left out.
func withoutPRD(tree map[string]string) map[string]string {
	out := withoutRender(tree)
	delete(out, "prd.md")
	return out
}
