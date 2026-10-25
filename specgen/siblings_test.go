package specgen

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
)

// The packages of a split are built concurrently and judged with the specs
// written before them; these tests are about the waits that makes necessary.
// The fixture's paths use the actors "consumer" and "afspec library".

var fixtureActors = []specActors{{Spec: "01_widget_core", Actors: []string{"afspec library", "consumer"}}}

func equalActors(a, b specActors) bool { return a.Spec == b.Spec && slices.Equal(a.Actors, b.Actors) }

// checkDependentScope asserts that only widget_github has the findings want,
// and that a valid package of a real run is activated.
func checkDependentScope(t *testing.T, got *Result, err error, want []string, activated bool) {
	t.Helper()
	if (err != nil) != (want != nil) || (want != nil && !isInvalid(err)) {
		t.Fatalf("err = %v; want findings %v", err, want)
	}
	for _, p := range packages(got) {
		var w []string
		if p.SpecName == "widget_github" {
			w = want
		}
		if checks := checksOf(p.Validation.Errors); !slices.Equal(checks, w) {
			t.Errorf("%s: errors %v; want %v", p.SpecName, checks, w)
		}
		if wantStatus := map[bool]string{true: "active", false: "draft"}[activated && w == nil]; p.Status != wantStatus {
			t.Errorf("%s: status %q, want %q", p.SpecName, p.Status, wantStatus)
		}
	}
}

// A package that depends on one written earlier in the same run is checked
// once the other has settled, though it was generated first: the dependency
// is found, and the rule applies to it.
func TestRun_ChecksADependencyOnASiblingOnceItIsWritten(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape packageShape
		want  []string
	}{
		{"an actor in common", packageShape{dependsOn: "01"}, nil},
		{"no actor in common", packageShape{dependsOn: "01", actor: "somebody else"}, []string{"cross_spec_actor"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inner := splitAuthor(t)
			inner.shapes = map[string]packageShape{"widget_github": tc.shape}
			a := &concurrentAuthor{inner: inner}
			dependentGenerated := make(chan struct{})
			var once sync.Once
			a.beforeGenerate = func(req artifactRequest) {
				// The first package is written after the second has been generated.
				if req.SpecName == "widget_core" && req.Step == afspec.StepTasks {
					wait(t, dependentGenerated, "the dependent package's generation")
				}
			}
			a.afterGenerate = func(req artifactRequest) {
				switch {
				case req.SpecName == "widget_github" && req.Step == afspec.StepTasks:
					once.Do(func() { close(dependentGenerated) })
				case req.SpecName == "widget_core" && req.Step == afspec.StepTasks:
					// Time for the dependent package to be written and
					// checked, were it not made to wait for this one.
					time.Sleep(300 * time.Millisecond)
				}
			}
			o := fileOptions(newWorkspace(t), a)
			o.MaxParallel = 3
			got, err := Run(context.Background(), o)
			checkDependentScope(t, got, err, tc.want, true)
		})
	}
}

// A dry run writes no package, and the packages of its split are checked
// against each other all the same, from memory.
func TestRun_ADryRunChecksASiblingDependency(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape packageShape
		want  []string
	}{
		{"an actor in common", packageShape{dependsOn: "01"}, nil},
		{"no actor in common", packageShape{dependsOn: "01", actor: "somebody else"}, []string{"cross_spec_actor"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inner := splitAuthor(t)
			inner.shapes = map[string]packageShape{"widget_github": tc.shape}
			o := fileOptions(newWorkspace(t), &concurrentAuthor{inner: inner})
			o.DryRun, o.MaxParallel = true, 3
			got, err := Run(context.Background(), o)
			checkDependentScope(t, got, err, tc.want, false)
		})
	}
}

// The requirements step is shown the actors of the specs on disk, since the
// rule is decided by names the model has not seen unless it is shown them;
// the steps after it, which write no execution path, are not.
func TestRun_ShowsTheRequirementsStepTheActorsOfTheSpecsOnDisk(t *testing.T) {
	ws := newWorkspace(t)
	if _, err := Run(context.Background(), newOptions(ws, newAuthor(t, "01", "widget_core"))); err != nil {
		t.Fatal(err)
	}
	a := newAuthor(t, "02", "widget_github")
	if _, err := Run(context.Background(), newOptions(ws, a)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, req := range a.artifactReqs {
		want := fixtureActors
		if req.Step != afspec.StepRequirements {
			want = nil
		}
		if !slices.EqualFunc(req.Actors, want, equalActors) {
			t.Errorf("the %s step was shown %+v; want %+v", req.Step, req.Actors, want)
		}
	}
}

// A package of a split is shown the actors of an earlier package of the run
// whose requirements are written, though that package is not on disk yet: it
// is the only way it can name a component the earlier spec owns as that spec
// does. Its requirements step waits for theirs, and no further.
func TestRun_ShowsTheRequirementsStepTheActorsOfAnEarlierScope(t *testing.T) {
	a := &concurrentAuthor{inner: splitAuthor(t)}
	laterRequirements := make(chan struct{})
	var once sync.Once
	var seen []specActors
	a.beforeGenerate = func(req artifactRequest) {
		switch {
		case req.SpecName == "widget_github" && req.Step == afspec.StepRequirements:
			seen = req.Actors
		case req.SpecName == "widget_core" && req.Step == afspec.StepTestSpec:
			// The first package is not written until the second has its requirements.
			wait(t, laterRequirements, "the second package's requirements")
		}
	}
	a.afterGenerate = func(req artifactRequest) {
		switch {
		case req.SpecName == "widget_core" && req.Step == afspec.StepRequirements:
			// Time for the second package to start its own requirements step,
			// were it not made to wait for this one.
			time.Sleep(200 * time.Millisecond)
		case req.SpecName == "widget_github" && req.Step == afspec.StepRequirements:
			once.Do(func() { close(laterRequirements) })
		}
	}
	o := fileOptions(newWorkspace(t), a)
	o.MaxParallel = 3
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !slices.EqualFunc(seen, fixtureActors, equalActors) {
		t.Errorf("the second package's requirements step was shown %+v; want %+v", seen, fixtureActors)
	}
}

// The actors of a spec's paths are each named once, whatever their case,
// since the rule ignores it, in alphabetical order.
func TestActorsOf_NamesEachActorOnce(t *testing.T) {
	r := &afspec.RequirementsV2Json{ExecutionPaths: []afspec.ExecutionPath{
		{Steps: []afspec.PathStep{{Actor: "Hub"}, {Actor: " client "}, {Actor: ""}}},
		{Steps: []afspec.PathStep{{Actor: "hub"}, {Actor: "agent-fox"}}},
	}}
	if got, want := actorsOf(r), []string{"agent-fox", "client", "Hub"}; !slices.Equal(got, want) {
		t.Errorf("actorsOf = %v, want %v", got, want)
	}
	if actorsOf(nil) != nil {
		t.Error("no requirements, no actors")
	}
}
