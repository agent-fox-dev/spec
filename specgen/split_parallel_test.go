package specgen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/agentrun"
)

// concurrentAuthor makes a scriptedAuthor safe to call from the goroutines a
// pipelined split runs: calls to it are serialised, and the hooks run
// outside the lock, so a hook may block to force one phase to overlap
// another.
type concurrentAuthor struct {
	mu    sync.Mutex
	inner *scriptedAuthor

	beforePRD      func(req prdRequest)
	afterPRD       func(req prdRequest)
	beforeGenerate func(req artifactRequest)
	afterGenerate  func(req artifactRequest)
	// failGenerate fails every generation phase of the named spec.
	failGenerate map[string]error

	events []string
}

func (a *concurrentAuthor) log(e string) {
	a.mu.Lock()
	a.events = append(a.events, e)
	a.mu.Unlock()
}

func (a *concurrentAuthor) WritePRD(ctx context.Context, req prdRequest) (PRD, agentrun.Result, error) {
	name := "first"
	if req.Split != nil {
		name = req.Split.Scope().Name
	}
	a.log("prd " + name)
	if a.beforePRD != nil {
		a.beforePRD(req)
	}
	a.mu.Lock()
	prd, res, err := a.inner.WritePRD(ctx, req)
	a.mu.Unlock()
	if a.afterPRD != nil {
		a.afterPRD(req)
	}
	return prd, res, err
}

func (a *concurrentAuthor) GenerateArtifact(ctx context.Context, req artifactRequest) (map[string]any, agentrun.Result, error) {
	a.log("gen " + req.SpecName + " " + string(req.Step))
	if a.beforeGenerate != nil {
		a.beforeGenerate(req)
	}
	if err := a.failGenerate[req.SpecName]; err != nil {
		return nil, agentrun.Result{Name: "generate:" + string(req.Step)}, err
	}
	a.mu.Lock()
	content, res, err := a.inner.GenerateArtifact(ctx, req)
	a.mu.Unlock()
	if a.afterGenerate != nil {
		a.afterGenerate(req)
	}
	return content, res, err
}

func (a *concurrentAuthor) WriteArchitecture(ctx context.Context, req architectureRequest) (string, agentrun.Result, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.inner.WriteArchitecture(ctx, req)
}

func (a *concurrentAuthor) indexOf(event string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, e := range a.events {
		if e == event {
			return i
		}
	}
	return -1
}

// wait blocks until ch is closed. The deadline is a deadlock guard, not a
// performance bound: an implementation that never overlaps the phases would
// otherwise hang the test instead of failing it.
func wait(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(20 * time.Second):
		t.Errorf("gave up waiting for %s: the phases did not overlap", what)
	}
}

// A split is pipelined: the second scope's PRD is written while the first
// scope's package is still being generated, the third scope's package
// finishes before the first's, and the numbers, the result and the plan still
// follow the plan's order.
func TestASplitIsPipelined(t *testing.T) {
	ws := newWorkspace(t)
	a := &concurrentAuthor{inner: splitAuthor(t)}
	secondPRD := make(chan struct{})
	thirdDone := make(chan struct{})
	var once1, once3 sync.Once
	a.beforePRD = func(req prdRequest) {
		if req.Split != nil && req.Split.Scope().Name == "widget_github" {
			once1.Do(func() { close(secondPRD) })
		}
	}
	a.beforeGenerate = func(req artifactRequest) {
		if req.SpecName == "widget_core" && req.Step == afspec.StepRequirements {
			// The first package waits for the second PRD and for the third
			// package: neither could happen if the scopes ran in sequence.
			wait(t, secondPRD, "the second scope's PRD")
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
	for _, p := range append([]Package{got.Package}, got.FollowOnSpecs...) {
		dirs = append(dirs, p.SpecDir)
	}
	want := []string{
		filepath.Join(".specs", "01_widget_core"),
		filepath.Join(".specs", "02_widget_github"),
		filepath.Join(".specs", "03_widget_adopt"),
	}
	if strings.Join(dirs, ",") != strings.Join(want, ",") {
		t.Errorf("packages = %v, want %v in plan order", dirs, want)
	}
	for _, s := range got.Split {
		if s.Status != ScopeDone {
			t.Errorf("scope %s = %s", s.Name, s.Status)
		}
	}
	if got.SplitPlan != "" {
		t.Errorf("the plan should be removed: %s", got.SplitPlan)
	}
	if _, err := os.Stat(filepath.Join(ws.Root, ".specs", "widget_core"+SplitPlanSuffix)); !os.IsNotExist(err) {
		t.Errorf("the plan is still on disk: %v", err)
	}
}

// A later scope's PRD is written against the PRD of an earlier scope whose
// package is still being generated: from memory, since it is not on disk.
func TestALaterPRDSeesAnEarlierScopeStillBeingGenerated(t *testing.T) {
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
	for _, want := range []string{"The widget core holds the widget model.", "`widget_core`",
		"PRD written; its package is being generated"} {
		if !strings.Contains(block, want) {
			t.Errorf("the scope block lacks %q:\n%s", want, block)
		}
	}
}

// --max-parallel 1 keeps the scopes strictly in sequence: a scope's PRD
// waits for the package before it.
func TestMaxParallelOneWritesTheScopesInSequence(t *testing.T) {
	ws := newWorkspace(t)
	a := &concurrentAuthor{inner: splitAuthor(t)}
	o := fileOptions(ws, a)
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
func TestAFailedScopeLetsThePackagesInFlightFinish(t *testing.T) {
	ws := newWorkspace(t)
	inner := splitAuthor(t)
	inner.followOnErr = map[string]error{"widget_github": errors.New("the model gave up")}
	a := &concurrentAuthor{inner: inner}
	prdFailed := make(chan struct{})
	var once sync.Once
	a.afterPRD = func(req prdRequest) {
		if req.Split != nil && req.Split.Scope().Name == "widget_github" {
			// The first package continues once the failing PRD has returned.
			once.Do(func() { close(prdFailed) })
		}
	}
	a.beforeGenerate = func(req artifactRequest) {
		if req.SpecName == "widget_core" && req.Step == afspec.StepRequirements {
			wait(t, prdFailed, "the second scope's PRD")
		}
	}
	o := fileOptions(ws, a)
	o.MaxParallel = 3

	got, err := Run(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "scope 2 of 3 (widget_github)") {
		t.Fatalf("err = %v, want the second scope's failure", err)
	}
	var states []string
	for _, s := range got.Split {
		states = append(states, s.Status)
	}
	if strings.Join(states, ",") != "done,failed,pending" {
		t.Errorf("scope states = %v", states)
	}
	if got.SpecID != "01" || !got.Validation.Valid {
		t.Errorf("the first package should be written: %+v", got.Package)
	}
	if a.indexOf("prd widget_adopt") >= 0 {
		t.Error("the third scope's PRD ran after the second failed")
	}
	if got.SplitPlan == "" {
		t.Error("the plan should be kept to resume from")
	}

	// The next run resumes from the second scope.
	b := &concurrentAuthor{inner: splitAuthor(t)}
	o = fileOptions(ws, b)
	o.MaxParallel = 3
	got, err = Run(context.Background(), o)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(got.Split) != 3 || got.Split[1].Status != ScopeDone || got.Split[2].Status != ScopeDone {
		t.Errorf("Split after resume = %+v", got.Split)
	}
	if b.indexOf("prd first") >= 0 {
		t.Error("the resumed run re-ran the PRD for the whole input")
	}
}

// A package that fails while others are in flight is reported as failed, its
// reservation is given back, and the other packages are kept.
func TestAFailedPackageDoesNotTakeTheOthersWithIt(t *testing.T) {
	ws := newWorkspace(t)
	a := &concurrentAuthor{inner: splitAuthor(t),
		failGenerate: map[string]error{"widget_github": errors.New("the provider went away")}}
	o := fileOptions(ws, a)
	o.MaxParallel = 3

	got, err := Run(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "widget_github") {
		t.Fatalf("err = %v", err)
	}
	statuses := map[string]string{}
	for _, s := range got.Split {
		statuses[s.Name] = s.Status
	}
	if statuses["widget_core"] != ScopeDone || statuses["widget_github"] != ScopeFailed {
		t.Errorf("statuses = %v", statuses)
	}
	if _, err := os.Stat(filepath.Join(ws.Root, ".specs", "02_widget_github")); !os.IsNotExist(err) {
		t.Errorf("the failed package's reservation was not given back: %v", err)
	}
}
