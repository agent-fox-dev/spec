package main

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
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

// An implementer gates the base branch, gates a change on a branch, merges it
// with --no-ff and gates the merged tree before committing the merge, and
// reads the report for the merge commit. The merged tree is the branch tip's,
// so its gate is answered from the tip's pass and runs nothing; the report
// counts what ran and what was reused; the repository holds nothing the gates
// wrote.
func TestAnImplementerGatesABranchThroughItsMergeAndReadsTheReport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the gate's test commands are shell scripts")
	}
	g := newGateDir(t)
	g.setCommands(t, g.script(t, "linter", ""), g.script(t, "all_tests",
		`printf 'ok  \tpkg/slow\t0.200s\nok  \tpkg/fast\t0.100s\n'`))
	gitInit(t, g.root)
	base := git(t, g.root, "rev-parse", "main")

	baseline := g.gateOf(t, "05", "full").Result.Gate
	if baseline.Verdict != "pass" {
		t.Fatalf("the baseline: %q", baseline.Verdict)
	}
	git(t, g.root, "switch", "-q", "-c", "feature")
	writeFile(t, filepath.Join(g.root, "feature.txt"), "feature")
	tip := g.commit(t, "the feature")
	g.gateOf(t, "05", "task")
	g.gateOf(t, "05", "full")
	if n := g.allTestsRuns(t); n != 3 {
		t.Fatalf("all_tests ran %d times before the merge; want 3", n)
	}

	git(t, g.root, "switch", "-q", "main")
	git(t, g.root, "merge", "-q", "--no-ff", "--no-commit", "feature")
	merged := g.gateOf(t, "05", "full").Result.Gate
	if merged.Verdict != "pass" || merged.Reused == nil || merged.Reused.Commit != tip {
		t.Fatalf("the merged tree's gate %+v; want a pass reused from %s", merged, tip)
	}
	if n := g.allTestsRuns(t); n != 3 {
		t.Errorf("all_tests ran for the merged tree (%d runs)", n)
	}

	env, _ := g.report(t, "05", "--since", base)
	r := env.Result.GateReport
	if r.Full.Runs != 3 || r.Full.Reused != 1 || r.Task.Runs != 1 || len(r.Slowest) != 2 ||
		r.BaselineFullSeconds == nil || *r.BaselineFullSeconds != baseline.Seconds {
		t.Errorf("report %+v; want 3 full runs with one reused, one task run, two slowest units and the baseline's %v seconds", r, baseline.Seconds)
	}
	if status := git(t, g.root, "status", "--porcelain"); status != "A  feature.txt" {
		t.Errorf("status %q; want the merge's own change alone", status)
	}
}

// A conformance pass runs the gate in a clean environment on a branch whose
// tests pass only with this machine's git identity: the clean gate is not
// answered by the normal pass, fails, and carries the last 40 lines of the
// failure; it runs again when called again, and its HOME is gone.
func TestAConformancePassFindsATestThatNeedsThisMachinesConfiguration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the gate's test commands are shell scripts")
	}
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = Gate Probe\n")
	t.Setenv("HOME", home)
	homeGitConfig(t)
	g := newGateDir(t)
	seen := filepath.Join(g.bin, "home")
	g.setCommands(t, g.script(t, "linter", ""), g.script(t, "all_tests", `echo "$HOME" > '`+seen+`'
if [ -n "$(git config --global user.name)" ]; then exit 0; fi
`+numbered(60)+`; exit 1`))
	gitInit(t, g.root)

	if v := g.gateOf(t, "05", "full").Result.Gate.Verdict; v != "pass" {
		t.Fatalf("the gate on this machine: %q", v)
	}
	clean := g.gateOf(t, "05", "full", "--clean-env").Result.Gate
	if clean.Reused != nil || clean.Verdict != "fail" || !clean.CleanEnv || len(clean.Commands) != 2 || clean.Commands[1].Tail == nil {
		t.Fatalf("the clean gate %+v; want a failure that ran, with a tail", clean)
	}
	var want []string
	for i := 21; i <= 60; i++ {
		want = append(want, "line "+strconv.Itoa(i))
	}
	if got := strings.Split(*clean.Commands[1].Tail, "\n"); !slices.Equal(got, want) {
		t.Errorf("tail %q; want lines 21 to 60", got)
	}
	again := g.gateOf(t, "05", "full", "--clean-env").Result.Gate
	if again.Reused != nil || g.allTestsRuns(t) != 3 {
		t.Errorf("the second clean gate: reused %+v, all_tests ran %d times; want it run again", again.Reused, g.allTestsRuns(t))
	}
	b, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	if h := strings.TrimSpace(string(b)); h == home || exists(h) {
		t.Errorf("the clean HOME %q is still there, or was the test's", h)
	}
}

// An implementer gates a task on the tests its change can affect, from the
// commit it started from, and learns from the full gate and the report that
// the suite is over the steering's budget.
func TestAnImplementerGatesATaskOnItsAffectedTestsAndFindsTheSuiteOverBudget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the gate's test commands are shell scripts")
	}
	g := newGateDir(t)
	g.setCommands(t, g.script(t, "linter", ""), g.script(t, "all_tests", "sleep 1.2"))
	g.setAffected(t, "BASE={base}")
	g.writeSteering(t, "gate_budget_seconds: 1\n")
	gitInit(t, g.root)
	base := git(t, g.root, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(g.root, "change.txt"), "the task's change")
	g.commit(t, "the task")

	task := g.gateOf(t, "05", "task", "--base", base).Result.Gate
	if got := commandNames(task.Commands); !slices.Equal(got, []string{"linter", "affected_tests"}) || task.Verdict != "pass" {
		t.Fatalf("the task gate ran %v, %s; want linter, then affected_tests, passing", got, task.Verdict)
	}
	if g.affectedArgs(t) != "BASE="+base || g.allTestsRuns(t) != 0 {
		t.Errorf("the affected tests got %q and all_tests ran %d times; want BASE=%s and none", g.affectedArgs(t), g.allTestsRuns(t), base)
	}

	full := g.gateOf(t, "05", "full").Result.Gate
	if full.Budget == nil || !full.Budget.Exceeded || full.Budget.Seconds != 1 {
		t.Fatalf("the full gate's budget %+v; want 1 s, exceeded", full.Budget)
	}
	env, _ := g.report(t, "05", "--since", base)
	if c := env.Result.GateReport.TestCost; c == nil || !c.Finding || !c.Exceeded {
		t.Errorf("test_cost %+v; want a finding, the budget exceeded", c)
	}
	if n := g.allTestsRuns(t); n != 1 {
		t.Errorf("all_tests ran %d times; want once, in the full gate", n)
	}
	if status := git(t, g.root, "status", "--porcelain"); status != "" {
		t.Errorf("the gates wrote into the repository: %q", status)
	}
}

// A spec author writes a handoff the wrong way, is told so, moves it to the
// package that offers the hook, makes the receiver take it, and the
// receiver's coder sees with the built specr what to wire.
func TestASpecAuthorDeclaresAHandoffAndTheReceiversCoderSeesWhatToWire(t *testing.T) {
	bin := specr(t)
	root, dirs := handoffRoot(t)
	setHandoffs(t, dirs["02"], [4]string{"H-1", "the after-landing hooks", "01", "REQ-1.1"})
	if v := validation(t, root, dirs["02"]); v.Valid || !slices.Equal(checks(v.Errors), []string{"cross_spec_handoff_direction"}) {
		t.Fatalf("02 with a handoff to 01: valid %v, errors %+v; want one cross_spec_handoff_direction", v.Valid, v.Errors)
	}

	setHandoffs(t, dirs["02"])
	setHandoffs(t, dirs["01"], [4]string{"H-1", "the after-landing hooks", "02", "REQ-1.1"})
	if v := validation(t, root, dirs["01"]); !v.Valid {
		t.Fatalf("01 after the move: %+v", v.Errors)
	}
	v := validation(t, root, dirs["02"])
	if !v.Valid || len(itemsOf(v.Warnings, "handoff_not_taken")) != 1 {
		t.Fatalf("02 after the move: valid %v, warnings %+v; want valid with one handoff_not_taken", v.Valid, v.Warnings)
	}

	setTakes(t, dirs["02"], "01:H-1")
	if v := validation(t, root, dirs["02"], "--strict-handoffs"); !v.Valid {
		t.Fatalf("02 taking the handoff, strict: %+v", v.Errors)
	}
	md := runSpecr(t, bin, root, envelope.Next{Tool: "specr", Input: dirs["02"], Flags: []string{"--task", "1"}}).Result.Markdown
	sender, err := afspec.LoadSpec(dirs["01"])
	if err != nil {
		t.Fatal(err)
	}
	sentence := sender.Requirements.Requirements[0].Resolve(sender.Requirements.Requirements[0].Criteria[0]).RenderEARSSentence()
	for _, want := range []string{"the after-landing hooks", sentence} {
		if !strings.Contains(md, want) {
			t.Errorf("the render of 02's task 1 lacks %q", want)
		}
	}
}
