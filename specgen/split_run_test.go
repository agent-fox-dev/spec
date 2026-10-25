package specgen

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// The tests below drive the real pipeline over an input the PRD phase reports
// as three specs' worth of work. The scripted author answers the first PRD
// phase with the split and every follow-on phase with the fixture under the
// planned name, so what is exercised is the pipeline's own bookkeeping: the
// plan, the numbering, the landscape, the order and the resume.

const threePackages = "01_widget_core,02_widget_github,03_widget_adopt"

var planFile = filepath.Join(".specs", "widget_core"+SplitPlanSuffix)

// stopAtScope runs the split on ws with scope's PRD phase failing, leaving
// the plan behind.
func stopAtScope(t *testing.T, ws *workspace.Workspace, o Options, a *scriptedAuthor, scope string) {
	t.Helper()
	a.followOnErr[scope] = errors.New("the run stops here")
	if _, err := Run(context.Background(), o); err == nil {
		t.Fatalf("want the run to stop at %s", scope)
	}
	if !exists(filepath.Join(ws.Root(), planFile)) {
		t.Fatal("the plan was not left behind")
	}
}

func TestRun_WritesEveryScopeOfASplit(t *testing.T) {
	ws := newWorkspace(t)
	a := splitAuthor(t)
	// Without --total-budget, spend never stops a split.
	a.prdCost = 100

	got, err := Run(context.Background(), fileOptions(ws, a))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Three packages, numbered in the plan's order, each valid and active,
	// and the plan gone.
	if dirs := specDirs(t, ws); dirs != threePackages {
		t.Fatalf("the spec root holds %s, want %s", dirs, threePackages)
	}
	for _, dir := range strings.Split(threePackages, ",") {
		loaded, err := afspec.LoadSpec(filepath.Join(ws.Root(), ".specs", dir))
		if err != nil || !loaded.Validate().Valid || loaded.Status != "active" {
			t.Errorf("%s: err %v, status %s", dir, err, loaded.Status)
		}
	}
	if got.SpecDir != filepath.Join(".specs", "01_widget_core") || len(got.FollowOnSpecs) != 2 ||
		got.FollowOnSpecs[0].SpecID != "02" || got.FollowOnSpecs[1].SpecID != "03" {
		t.Errorf("the first package %q, then %+v", got.SpecDir, got.FollowOnSpecs)
	}
	for i, s := range got.Split {
		if s.Status != ScopeDone || s.SpecDir == "" || s.Name != threeScopes()[i].Name || s.Valid == nil || !*s.Valid {
			t.Errorf("Split[%d] = %+v", i, s)
		}
	}
	if len(got.Split) != 3 || got.SplitPlan != "" || got.Resumable() {
		t.Errorf("split %+v, plan %q", got.Split, got.SplitPlan)
	}

	// One PRD phase for the whole input, then one per remaining scope, each
	// told which scope it writes and shown the packages before it.
	if len(a.prdRequests) != 3 || a.prdRequests[0].Split != nil {
		t.Fatalf("%d PRD phases; the first must see the whole input", len(a.prdRequests))
	}
	third := a.prdRequests[2]
	if third.Split == nil || third.Split.Index != 2 || !third.Split.Plan.Scopes[1].Written() {
		t.Fatalf("third PRD request: %+v", third.Split)
	}
	var names []string
	for _, m := range third.Landscape {
		names = append(names, m.SpecID+"_"+m.SpecName)
	}
	if strings.Join(names, ",") != "01_widget_core,02_widget_github" {
		t.Errorf("the third scope's landscape lists %v", names)
	}
	block := splitBlock(third.Split)
	for _, want := range []string{"`01_widget_core`", "`02_widget_github`", "**this PRD**", "`spec_name` is `widget_adopt`"} {
		if !strings.Contains(block, want) {
			t.Errorf("the scope block lacks %q:\n%s", want, block)
		}
	}

	// The first package's fields are the top level of the JSON, as for an
	// undivided input.
	raw, _ := json.Marshal(got)
	var shape map[string]any
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatal(err)
	}
	if shape["spec_dir"] != got.SpecDir || shape["spec_id"] != "01" || shape["follow_on_specs"] == nil || shape["split"] == nil {
		t.Errorf("the result's shape: %s", raw)
	}
}

// Each scope's PRD names its own relevant files: its generation phases are
// given those, and its package reports them.
func TestRun_EachScopeKeepsItsOwnRelevantFiles(t *testing.T) {
	ws := newWorkspace(t)
	a := splitAuthor(t)
	files := map[string]string{"widget_core": "a.go", "widget_github": "b.go", "widget_adopt": "c.go"}
	a.prd.RelevantFiles = []RelevantFile{{Path: "a.go", Why: "scope 1"}}
	for _, name := range []string{"widget_github", "widget_adopt"} {
		prd := a.prd
		prd.SpecName, prd.Title, prd.RecommendedSplit = name, "Scope "+name, nil
		prd.RelevantFiles = []RelevantFile{{Path: files[name], Why: name}}
		a.followOn[name] = prd
	}
	got, err := Run(context.Background(), fileOptions(ws, a))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, req := range a.artifactReqs {
		if len(req.RelevantFiles) != 1 || req.RelevantFiles[0].Path != files[req.SpecName] {
			t.Errorf("%s %s was given %+v", req.SpecName, req.Step, req.RelevantFiles)
		}
	}
	for _, p := range packages(got) {
		if len(p.RelevantFiles) != 1 || p.RelevantFiles[0].Path != files[p.SpecName] {
			t.Errorf("%s reports %+v", p.SpecName, p.RelevantFiles)
		}
	}
}

// A scope whose generation fails is reported failed with no package, and its
// reservation is given back; the packages written before it are kept, and
// those in flight beside it finish.
func TestRun_AScopeWhoseGenerationFailsHasNoPackage(t *testing.T) {
	t.Run("the first scope", func(t *testing.T) {
		ws := newWorkspace(t)
		a := splitAuthor(t)
		a.artifactErr[afspec.StepRequirements] = errors.New("grammar too large")
		got, err := Run(context.Background(), fileOptions(ws, a))
		if err == nil || got == nil {
			t.Fatalf("want a result and an error, got %v, %v", got, err)
		}
		if got.SpecDir != "" || len(got.FollowOnSpecs) != 0 || got.Split[0].Status != ScopeFailed || got.Split[0].SpecDir != "" {
			t.Errorf("the result names packages that were not written: %q, %+v, %+v", got.SpecDir, got.FollowOnSpecs, got.Split[0])
		}
		if dirs := specDirs(t, ws); dirs != "widget_core"+SplitPlanSuffix {
			t.Errorf("the spec root holds %s, want only the plan", dirs)
		}
	})
	t.Run("a scope in flight beside others", func(t *testing.T) {
		ws := newWorkspace(t)
		a := &concurrentAuthor{inner: splitAuthor(t), failGenerate: map[string]error{"widget_github": errors.New("the provider went away")}}
		o := fileOptions(ws, a)
		o.MaxParallel = 3
		got, err := Run(context.Background(), o)
		if err == nil || !strings.Contains(err.Error(), "scope 2 of 3 (widget_github)") {
			t.Fatalf("err = %v", err)
		}
		if states := scopeStates(got); !strings.HasPrefix(states, "done,failed,") {
			t.Errorf("scope states = %s", states)
		}
		if exists(filepath.Join(ws.Root(), ".specs", "02_widget_github")) || got.SpecID != "01" || !got.Validation.Valid {
			t.Errorf("the failed reservation was kept or the first package lost: %+v", got.Package)
		}
	})
}

// A run that stops on a scope leaves the plan behind, and the next run on the
// same input continues from that scope without re-deciding the split.
func TestRun_AStoppedSplitResumesFromItsPlan(t *testing.T) {
	ws := newWorkspace(t)
	a := splitAuthor(t)
	cause := errors.New("budget exceeded")
	a.followOnErr["widget_github"] = cause
	got, err := Run(context.Background(), fileOptions(ws, a))
	if f := asFailure(t, err); f.Stage != "prd" || !strings.Contains(err.Error(), "scope 2 of 3 (widget_github)") || !errors.Is(err, cause) {
		t.Fatalf("err = %v; want the second scope's PRD phase named, its cause kept", err)
	}
	if got.SpecID != "01" || !got.Validation.Valid || scopeStates(got) != "done,failed,pending" {
		t.Fatalf("result %+v, states %s", got.Package, scopeStates(got))
	}
	if got.SplitPlan != planFile || !got.Resumable() || !exists(filepath.Join(ws.Root(), planFile)) {
		t.Errorf("SplitPlan = %q; the plan should be left to resume from", got.SplitPlan)
	}

	b := splitAuthor(t)
	o := fileOptions(ws, b)
	o.Name = "ignored_on_resume"
	got, err = Run(context.Background(), o)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(b.prdRequests) != 2 || b.prdRequests[0].Split == nil || b.prdRequests[0].Split.Index != 1 {
		t.Fatalf("the resumed run should start at scope 2 with the plan; %d PRD phases", len(b.prdRequests))
	}
	if dirs := specDirs(t, ws); dirs != threePackages {
		t.Fatalf("the spec root holds %s", dirs)
	}
	if got.SpecID != "02" || len(got.FollowOnSpecs) != 1 || got.FollowOnSpecs[0].SpecID != "03" ||
		got.Split[0].Status != ScopeDone || got.Split[0].SpecID != "01" || got.SplitPlan != "" {
		t.Errorf("the resumed run reports %s, %+v, %+v", got.SpecID, got.FollowOnSpecs, got.Split)
	}
	if _, ok := warning(o.Run, cli.WarnNameFlagIgnored); !ok {
		t.Error("--name should be reported as ignored on resume")
	}
	// The PRD phase of the input is not run again, so its relevant files are
	// not available to the scopes written now.
	if w, ok := warning(o.Run, cli.WarnRelevantFilesUnavailable); !ok || w.Severity != "low" {
		t.Errorf("warnings = %+v", o.Run.Warnings())
	}
}

// A resumed split that stops again leaves the plan on disk, and the result
// says so: SplitPlan names it, Resumable() is true and next[] suggests spec on
// the same input, whichever scope the second run stops on.
func TestRun_AResumedSplitThatStopsAgainIsStillResumable(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failScope string
		states    string
	}{
		{"at the first pending scope", "widget_github", "done,failed,pending"},
		{"after writing one more", "widget_adopt", "done,done,failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := newWorkspace(t)
			first := splitAuthor(t)
			stopAtScope(t, ws, fileOptions(ws, first), first, "widget_github")

			resumed := splitAuthor(t)
			resumed.followOnErr[tc.failScope] = errors.New("the resumed run stops here")
			o := fileOptions(ws, resumed)
			got, err := Run(context.Background(), o)
			if err == nil || resumed.prdRequests[0].Split == nil {
				t.Fatalf("err %v; the second run should resume the plan and stop", err)
			}
			if scopeStates(got) != tc.states || got.SplitPlan != planFile || !got.Resumable() ||
				!exists(filepath.Join(ws.Root(), planFile)) {
				t.Errorf("states %s, plan %q", scopeStates(got), got.SplitPlan)
			}
			var spec []cli.Next
			for _, n := range got.Next() {
				if n.Tool == "spec" {
					spec = append(spec, n)
				}
			}
			if len(spec) != 1 || spec[0].Input != cli.ResumePlaceholder(o.Input) {
				t.Errorf("next[] spec entries = %+v", spec)
			}
		})
	}
}

// An edited file resumes by origin, with a warning that the remaining scopes
// are written from the current text; the plan, not the model, names each
// package, and a scope the model reports as several is written as one.
func TestRun_AResumedSplitFollowsThePlan(t *testing.T) {
	ws := newWorkspace(t)
	first := splitAuthor(t)
	stopAtScope(t, ws, fileOptions(ws, first), first, "widget_github")

	b := splitAuthor(t)
	renamed := b.prd
	renamed.SpecName, renamed.Title = "github_store", "Renamed by the model"
	renamed.RecommendedSplit = []SplitScope{{Name: "github_store", Scope: "a"}, {Name: "github_auth", Scope: "b"}}
	b.followOn["widget_github"] = renamed
	o := fileOptions(ws, b)
	o.Input.Body += "\n\nedited after the first run"
	got, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got.SpecName != "widget_github" || got.SpecDir != filepath.Join(".specs", "02_widget_github") || len(got.Split) != 3 {
		t.Errorf("the plan's name must win: %q at %q, split %+v", got.SpecName, got.SpecDir, got.Split)
	}
	for _, code := range []cli.WarnCode{cli.WarnSplitPlanStale, cli.WarnScopeRenamed, cli.WarnScopeCountMismatch} {
		if _, ok := warning(o.Run, code); !ok {
			t.Errorf("no %s warning: %s", code, warningText(o.Run))
		}
	}
	if !strings.Contains(warningText(o.Run), `"github_store"`) {
		t.Errorf("the rename warning does not name the model's name: %s", warningText(o.Run))
	}
}

// A package that does not validate is on disk and stops nothing; the run
// still fails naming it, and the plan does not write it again.
func TestRun_AnInvalidScopeDoesNotStopTheSplit(t *testing.T) {
	ws := newWorkspace(t)
	a := splitAuthor(t)
	a.breakScope = "widget_github"
	got, err := Run(context.Background(), fileOptions(ws, a))
	if f := asFailure(t, err); f.Category != CategoryInvalid || !strings.Contains(err.Error(), "1 of 3 packages do not validate (.specs/02_widget_github)") {
		t.Fatalf("err = %v", err)
	}
	if dirs := specDirs(t, ws); dirs != threePackages {
		t.Fatalf("the spec root holds %s", dirs)
	}
	if scopeStates(got) != "done,invalid,done" || got.Split[1].Valid == nil || *got.Split[1].Valid || got.Split[1].ErrorCount == 0 {
		t.Errorf("split = %+v", got.Split)
	}
	if got.FollowOnSpecs[0].Validation.Valid || got.FollowOnSpecs[0].Status == "active" || got.SplitPlan != "" {
		t.Errorf("the invalid package must be reported as such, not activated, and the plan removed: %+v", got.FollowOnSpecs[0])
	}
}

// A dry run generates every scope and numbers them as a real run would, and
// leaves neither packages nor a plan behind; its packages are hypothetical,
// so no later scope is told they exist, no impl is suggested on them, and
// every spec_package artifact is marked dry_run.
func TestRun_ADryRunOfASplitWritesNothing(t *testing.T) {
	ws := newWorkspace(t)
	a := splitAuthor(t)
	o := fileOptions(ws, a)
	o.DryRun = true
	got, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exists(filepath.Join(ws.Root(), ".specs")) {
		t.Error("--dry-run created the spec root")
	}
	var ids []string
	for _, p := range packages(got) {
		ids = append(ids, p.SpecID)
	}
	if strings.Join(ids, ",") != "01,02,03" || got.SplitPlan != "" || scopeStates(got) != "done,done,done" {
		t.Errorf("ids %v, plan %q, states %s", ids, got.SplitPlan, scopeStates(got))
	}
	for _, req := range a.prdRequests[1:] {
		if len(req.Landscape) != 0 {
			t.Errorf("a later scope was shown dry-run packages as existing: %+v", req.Landscape)
		}
	}
	for _, n := range got.Next() {
		if n.Tool == "impl" {
			t.Errorf("next suggests impl on %s, which a dry run did not write", n.Input)
		}
	}
	arts := got.Artifacts()
	if len(arts) != 3 {
		t.Fatalf("artifacts = %+v", arts)
	}
	for _, art := range arts {
		if art.Kind != cli.ArtifactSpecPackage || !art.DryRun {
			t.Errorf("artifact %+v is not a dry-run spec_package", art)
		}
	}
}

// A split is pipelined: the second scope's PRD is written while the first
// scope's package is still being generated, and the third scope's package is
// generated before the first's last step is, yet the numbers, the result and
// the plan follow the plan's order.
func TestRun_ASplitIsPipelined(t *testing.T) {
	ws := newWorkspace(t)
	a := &concurrentAuthor{inner: splitAuthor(t)}
	secondPRD, thirdDone := make(chan struct{}), make(chan struct{})
	var once1, once3 sync.Once
	a.beforePRD = func(req prdRequest) {
		if req.Split != nil && req.Split.Scope().Name == "widget_github" {
			once1.Do(func() { close(secondPRD) })
		}
	}
	a.beforeGenerate = func(req artifactRequest) {
		switch {
		case req.SpecName == "widget_core" && req.Step == afspec.StepRequirements:
			// That could not happen if the scopes ran in sequence.
			wait(t, secondPRD, "the second scope's PRD")
		case req.SpecName == "widget_core" && req.Step == afspec.StepTasks:
			wait(t, thirdDone, "the third scope's package")
		}
	}
	a.afterGenerate = func(req artifactRequest) {
		if req.SpecName == "widget_adopt" && req.Step == afspec.StepTasks {
			once3.Do(func() { close(thirdDone) })
		}
	}
	o := fileOptions(ws, a)
	o.MaxParallel = 3
	got, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var dirs []string
	for _, p := range packages(got) {
		dirs = append(dirs, filepath.Base(p.SpecDir))
	}
	if strings.Join(dirs, ",") != threePackages || scopeStates(got) != "done,done,done" || got.SplitPlan != "" {
		t.Errorf("packages %v, states %s, plan %q", dirs, scopeStates(got), got.SplitPlan)
	}
	if exists(filepath.Join(ws.Root(), planFile)) {
		t.Error("the plan is still on disk")
	}
}

// A later scope's PRD is written against the PRD of an earlier scope whose
// package is still being generated: from memory, since it is not on disk.
func TestRun_ALaterPRDSeesAnEarlierScopeStillBeingGenerated(t *testing.T) {
	ws := newWorkspace(t)
	inner := splitAuthor(t)
	inner.prd.Body = "## Intent\n\nThe widget core holds the widget model.\n\n## Goals\n\n- One model.\n"
	a := &concurrentAuthor{inner: inner}
	release := make(chan struct{})
	var once sync.Once
	var seen *splitContext
	a.beforePRD = func(req prdRequest) {
		if req.Split != nil && req.Split.Scope().Name == "widget_github" {
			seen = req.Split
			once.Do(func() { close(release) })
		}
	}
	a.beforeGenerate = func(req artifactRequest) {
		if req.SpecName == "widget_core" {
			wait(t, release, "the second scope's PRD")
		}
	}
	o := fileOptions(ws, a)
	o.MaxParallel = 2
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if seen == nil {
		t.Fatal("the second scope's PRD phase never ran")
	}
	block := splitBlock(seen)
	for _, want := range []string{"The widget core holds the widget model.", "`widget_core`", "PRD written; its package is being generated"} {
		if !strings.Contains(block, want) {
			t.Errorf("the scope block lacks %q:\n%s", want, block)
		}
	}
}

// --max-parallel 1 keeps the scopes strictly in sequence: a scope's PRD waits
// for the package before it.
func TestRun_MaxParallelOneWritesTheScopesInSequence(t *testing.T) {
	a := &concurrentAuthor{inner: splitAuthor(t)}
	o := fileOptions(newWorkspace(t), a)
	o.MaxParallel = 1
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prd2, tasks1 := a.indexOf("prd widget_github"), a.indexOf("gen widget_core tasks"); prd2 < tasks1 {
		t.Errorf("the second PRD (event %d) ran before the first package's tasks (event %d): %v", prd2, tasks1, a.events)
	}
}

// A failure stops the run starting anything new, and the packages already
// being generated finish and are kept: here the second scope's PRD fails
// while the first scope's package is still being generated.
func TestRun_AFailedScopeLetsThePackagesInFlightFinish(t *testing.T) {
	inner := splitAuthor(t)
	inner.followOnErr["widget_github"] = errors.New("the model gave up")
	a := &concurrentAuthor{inner: inner}
	prdFailed := make(chan struct{})
	var once sync.Once
	a.afterPRD = func(req prdRequest) {
		if req.Split != nil && req.Split.Scope().Name == "widget_github" {
			once.Do(func() { close(prdFailed) })
		}
	}
	a.beforeGenerate = func(req artifactRequest) {
		if req.SpecName == "widget_core" && req.Step == afspec.StepRequirements {
			wait(t, prdFailed, "the second scope's PRD")
		}
	}
	o := fileOptions(newWorkspace(t), a)
	o.MaxParallel = 3
	got, err := Run(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "scope 2 of 3 (widget_github)") {
		t.Fatalf("err = %v, want the second scope's failure", err)
	}
	if scopeStates(got) != "done,failed,pending" || got.SpecID != "01" || !got.Validation.Valid || got.SplitPlan == "" {
		t.Errorf("states %s, first package %+v, plan %q", scopeStates(got), got.Package, got.SplitPlan)
	}
	if a.indexOf("prd widget_adopt") >= 0 {
		t.Error("the third scope's PRD ran after the second failed")
	}
}

// A package that fails while the next scope's PRD is being written stops the
// run there: the PRD just written is not given a number or a package.
func TestRun_APackageFailingDuringTheNextPRDStopsTheSplit(t *testing.T) {
	ws := newWorkspace(t)
	a := &concurrentAuthor{inner: splitAuthor(t), failGenerate: map[string]error{"widget_core": errors.New("the provider went away")}}
	a.beforePRD = func(req prdRequest) {
		if req.Split == nil || req.Split.Scope().Name != "widget_github" {
			return
		}
		// The first package's reservation is given back as its build fails;
		// the failure is recorded the moment after.
		deadline := time.Now().Add(10 * time.Second)
		for exists(filepath.Join(ws.Root(), ".specs", "01_widget_core")) || a.indexOf("gen widget_core requirements") < 0 {
			if time.Now().After(deadline) {
				t.Error("the first package never failed")
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(100 * time.Millisecond)
	}
	o := fileOptions(ws, a)
	o.MaxParallel = 3
	got, err := Run(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "scope 1 of 3 (widget_core)") {
		t.Fatalf("err = %v, want the first scope's failure", err)
	}
	if scopeStates(got) != "failed,pending,pending" || a.indexOf("gen widget_github requirements") >= 0 || a.indexOf("prd widget_adopt") >= 0 {
		t.Errorf("states %s, events %v; want nothing started after the failure", scopeStates(got), a.events)
	}
	if dirs := specDirs(t, ws); dirs != "widget_core"+SplitPlanSuffix {
		t.Errorf("the spec root holds %s, want only the plan", dirs)
	}
}

// --total-budget is checked before each scope's PRD phase after the first: a
// run already over it stops there with the plan in place, at stage budget,
// with or without the review, and the next run resumes.
func TestRun_ASplitStopsBetweenScopesAtTheTotalBudget(t *testing.T) {
	for _, adversarial := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "with --adversarial"}[adversarial], func(t *testing.T) {
			ws := newWorkspace(t)
			a := splitAuthor(t)
			a.prdCost = 0.30
			o := fileOptions(ws, a)
			o.TotalBudgetUSD = 0.25
			if adversarial {
				o.Adversarial, o.AdversarialRounds, o.reviewer = true, 1, &scriptedReviewer{t: t}
			}
			got, err := Run(context.Background(), o)
			f := asFailure(t, err)
			if f.Category != agentrun.CategoryBudget || f.Stage != "budget" || f.TotalBudgetUSD() != 0.25 ||
				!strings.Contains(err.Error(), "scope 2 of 3 (widget_github)") {
				t.Errorf("failure %s/%s %q", f.Stage, f.Category, err)
			}
			if info := cli.ErrorFrom("run", err); info.Category != "budget" || info.TotalBudgetUSD() != 0.25 {
				t.Errorf("ErrorFrom = %+v", info)
			}
			if len(a.prdRequests) != 1 || got.SpecID != "01" || got.SplitPlan == "" || scopeStates(got) != "done,failed,pending" {
				t.Errorf("%d PRD phases, package %q, plan %q, states %s", len(a.prdRequests), got.SpecID, got.SplitPlan, scopeStates(got))
			}
			if adversarial {
				return
			}
			got, err = Run(context.Background(), fileOptions(ws, splitAuthor(t)))
			if err != nil || scopeStates(got) != "done,done,done" {
				t.Errorf("resume: %v, states %s", err, scopeStates(got))
			}
		})
	}
}

// A scope is recorded in the plan the moment its package is on disk, before
// the read-back, the activation and the comment, so a run killed then does
// not write the package a second time.
func TestRun_RecordsAScopeOnceItsPackageIsWritten(t *testing.T) {
	ws := newWorkspace(t)
	specs := filepath.Join(ws.Root(), ".specs")
	o := fileOptions(ws, splitAuthor(t))
	calls, recorded := 0, 0
	forge := &mockForge{authenticated: true, commentURL: "u"}
	forge.hook = func() {
		calls++
		if p, err := loadSplitPlan(filepath.Join(specs, "widget_core"+SplitPlanSuffix)); err == nil && p.Scopes[calls-1].Written() {
			recorded++
		}
	}
	commentOn(&o, forge)
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 3 || recorded != 3 {
		t.Errorf("%d comments, %d scopes recorded before their comment; want every scope recorded once written", calls, recorded)
	}
}

// A scope's rounds run in the coordinator after the planned name replaces
// the model's, and before its package is reserved, while the earlier
// packages are still being generated.
func TestRun_AScopesReviewRunsBeforeItsPackageIsReserved(t *testing.T) {
	ws := newWorkspace(t)
	inner := splitAuthor(t)
	inner.followOn["widget_github"] = PRD{SpecName: "model_choice", Title: "Store", Body: inner.prd.Body}
	a := &concurrentAuthor{inner: inner}
	secondReviewed := make(chan struct{})
	var once sync.Once
	rv := &scriptedReviewer{t: t, reviews: cleanReviews(3)}
	rv.onReview = func(req reviewRequest) {
		a.log("review " + req.PRD.SpecName)
		if req.PRD.SpecName == "widget_github" {
			if exists(filepath.Join(ws.Root(), ".specs", "02_widget_github")) {
				t.Error("scope 2's package was reserved before its review")
			}
			once.Do(func() { close(secondReviewed) })
		}
	}
	a.beforeGenerate = func(req artifactRequest) {
		// The first package is still being generated while the coordinator
		// reviews the second scope's PRD.
		if req.SpecName == "widget_core" && req.Step == afspec.StepRequirements {
			wait(t, secondReviewed, "the second scope's review")
		}
	}
	o := fileOptions(ws, a)
	o.MaxParallel = 3
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 1, rv
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if names := rv.reviewedNames(); !slices.Equal(names, []string{"widget_core", "widget_github", "widget_adopt"}) {
		t.Errorf("reviewed %v", names)
	}
	if r, g := a.indexOf("review widget_github"), a.indexOf("gen widget_github requirements"); r < 0 || g < 0 || r > g {
		t.Errorf("scope 2: review at %d, generation at %d: %v", r, g, a.events)
	}
}

// The first scope is reviewed once the split plan is recorded, with the split
// context of scope 1.
func TestRun_TheFirstScopeIsReviewedWithItsSplitContext(t *testing.T) {
	ws := newWorkspace(t)
	rv := &scriptedReviewer{t: t, reviews: cleanReviews(3)}
	rv.onReview = func(req reviewRequest) {
		if req.PRD.SpecName != "widget_core" {
			return
		}
		if !exists(filepath.Join(ws.Root(), planFile)) {
			t.Error("the plan is not on disk when scope 1 is reviewed")
		}
		if req.Split == nil || req.Split.Index != 0 || len(req.Split.Plan.Scopes) != 3 {
			t.Errorf("scope 1's review has split %+v", req.Split)
		}
	}
	o := fileOptions(ws, splitAuthor(t))
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 1, rv
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if n, _ := rv.counts(); n != 3 {
		t.Errorf("%d reviews", n)
	}
}

// The next scope's PRD phase is shown the earlier scope's revised PRD and
// notes, not the originals: a dry run's from memory, a real run's from the
// prd.md it wrote.
func TestRun_TheNextScopeSeesTheRevisedPRD(t *testing.T) {
	scopeOneRevised := func() *scriptedReviewer {
		return &scriptedReviewer{t: t,
			reviews: []reviewOutcome{{severities: []string{"minor"}}, {}, {}},
			revisions: []revisionOutcome{{edit: func(p PRD) PRD {
				p.Body = "## Intent\n\nREVISED-1: the widget model.\n\n## Goals\n\n- One model.\n"
				p.CodebaseNotes = "NOTES-REV"
				return p
			}}},
		}
	}
	for _, dry := range []bool{true, false} {
		t.Run(map[bool]string{true: "a dry run", false: "a real run"}[dry], func(t *testing.T) {
			a := splitAuthor(t)
			a.prd.CodebaseNotes = "NOTES-ORIGINAL"
			o := fileOptions(newWorkspace(t), a)
			o.DryRun = dry
			o.Adversarial, o.AdversarialRounds, o.reviewer = true, 1, scopeOneRevised()
			if _, err := Run(context.Background(), o); err != nil {
				t.Fatal(err)
			}
			split := a.prdRequests[1].Split
			if block := splitBlock(split); !strings.Contains(block, "REVISED-1") || !strings.Contains(block, "NOTES-REV") ||
				strings.Contains(block, "NOTES-ORIGINAL") {
				t.Errorf("scope 2 was shown:\n%s", block)
			}
		})
	}
}

// A resumed split reviews only the scopes it writes, and leaves the packages
// of the earlier run as they were.
func TestRun_AResumedSplitReviewsOnlyWhatItWrites(t *testing.T) {
	ws := newWorkspace(t)
	a := splitAuthor(t)
	o := fileOptions(ws, a)
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 1, &scriptedReviewer{t: t, reviews: cleanReviews(1)}
	stopAtScope(t, ws, o, a, "widget_github")
	first := filepath.Join(ws.Root(), ".specs", "01_widget_core")
	before := dirHash(t, first)

	rv := &scriptedReviewer{t: t, reviews: cleanReviews(2)}
	o = fileOptions(ws, splitAuthor(t))
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 1, rv
	got, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if names := rv.reviewedNames(); !slices.Equal(names, []string{"widget_github", "widget_adopt"}) {
		t.Errorf("the resumed run reviewed %v", names)
	}
	if dirHash(t, first) != before {
		t.Error("the earlier run's package changed")
	}
	if got.SpecID != "02" || got.Review == nil || len(got.FollowOnSpecs) != 1 || got.FollowOnSpecs[0].Review == nil {
		t.Errorf("the resumed run reports %s %+v and %+v", got.SpecID, got.Review, got.FollowOnSpecs)
	}
}

// An interrupt while a scope's PRD is reviewed stops the split at that
// scope, before its number is reserved, with the plan kept to resume from.
func TestRun_AnInterruptedScopeReviewStopsTheSplit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ws := newWorkspace(t)
	a := splitAuthor(t)
	rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{err: phaseErr(agentrun.PhasePRDReview, agentrun.CategoryAborted)}},
		onReview: func(reviewRequest) { cancel() }}
	o := fileOptions(ws, a)
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 1, rv
	got, err := Run(ctx, o)
	if f := asFailure(t, err); f.Stage != agentrun.PhasePRDReview || f.Category != agentrun.CategoryAborted ||
		!strings.Contains(err.Error(), "scope 1 of 3 (widget_core)") {
		t.Fatalf("err = %v", err)
	}
	if scopeStates(got) != "failed,pending,pending" || got.SplitPlan != planFile || len(a.steps) != 0 {
		t.Errorf("states %s, plan %q, steps %v", scopeStates(got), got.SplitPlan, a.steps)
	}
	if dirs := specDirs(t, ws); dirs != "widget_core"+SplitPlanSuffix {
		t.Errorf("the spec root holds %s, want only the plan", dirs)
	}
}

// Every package a split reports carries its review; a scope that fails
// before its write reports no package, so it has none to carry one.
func TestRun_EveryPackageOfASplitCarriesItsReview(t *testing.T) {
	a := &concurrentAuthor{inner: splitAuthor(t), failGenerate: map[string]error{"widget_adopt": errors.New("no room")}}
	rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{severities: []string{"minor"}}, {}, {}},
		revisions: []revisionOutcome{{}}}
	o := fileOptions(newWorkspace(t), a)
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 2, rv
	got, err := Run(context.Background(), o)
	if err == nil {
		t.Fatal("the third scope should fail")
	}
	if r := got.Review; r == nil || r.RoundsAllowed != 2 || r.StopReason != envelope.ReviewStopNoSeriousFindings ||
		r.FindingsCount != 1 || r.Rounds[0].Round != 1 || len(r.Rounds[0].Findings) != 1 {
		t.Errorf("the first package's review = %+v", got.Review)
	}
	if len(got.FollowOnSpecs) != 1 || got.FollowOnSpecs[0].Review == nil ||
		got.FollowOnSpecs[0].Review.StopReason != envelope.ReviewStopNoFindings {
		t.Errorf("follow-on packages = %+v", got.FollowOnSpecs)
	}
}
