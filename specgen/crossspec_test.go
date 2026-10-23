package specgen

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/afspec"
)

// The tests below are about the rules that span specs (format v2 §10.4) in a
// run. A package is written beside the others of the spec root, so it is
// validated with them: a package that passes its own rules and breaks one of
// these is on disk and reported, and is not activated. They drive the real
// pipeline, as the tests of pipeline_test.go do, with the model scripted.

// packageShape is how a test bends the fixture for one package.
type packageShape struct {
	// dependsOn is the spec id its tasks declare a dependency on, or "".
	dependsOn string
	// actor, when set, is the actor of every step of its execution paths.
	actor string
}

func (s packageShape) apply(step afspec.GenerationStep, content map[string]any) {
	switch step {
	case afspec.StepRequirements:
		if s.actor == "" {
			return
		}
		for _, p := range content["execution_paths"].([]any) {
			for _, st := range p.(map[string]any)["steps"].([]any) {
				st.(map[string]any)["actor"] = s.actor
			}
		}
	case afspec.StepTasks:
		if s.dependsOn != "" {
			content["dependencies"] = []any{map[string]any{"spec": s.dependsOn, "reason": "builds on it"}}
		}
	}
}

// writeUpstream writes 01_widget_core through the pipeline: a valid, active
// package with the fixture's actors, "consumer" and "afspec library".
func writeUpstream(t *testing.T, ws *workspace.Workspace) {
	t.Helper()
	got, err := Run(context.Background(), newOptions(ws, newAuthor(t, "01", "widget_core")))
	if err != nil || got.Status != "active" {
		t.Fatalf("the upstream package: status %q, err %v", got.Status, err)
	}
}

// runDownstream writes 02_widget_github, shaped by s, beside the upstream.
func runDownstream(t *testing.T, ws *workspace.Workspace, s packageShape) (*Result, error) {
	t.Helper()
	a := newAuthor(t, "02", "widget_github")
	a.shapes = map[string]packageShape{"widget_github": s}
	return Run(context.Background(), newOptions(ws, a))
}

func checksOf(items []ValidationItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Check)
	}
	return out
}

// statusOnDisk is the status the package in dir carries in its prd.md.
func statusOnDisk(t *testing.T, dir string) string {
	t.Helper()
	loaded, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatalf("LoadSpec(%s): %v", dir, err)
	}
	return loaded.Status
}

// A package whose paths share no actor with the spec it depends on breaks the
// rule speca reports as cross_spec_actor. It is written and reported with the
// rule named, and left in draft.
func TestAPackageSharingNoActorWithItsDependencyDoesNotValidate(t *testing.T) {
	ws := newWorkspace(t)
	writeUpstream(t, ws)

	got, err := runDownstream(t, ws, packageShape{dependsOn: "01", actor: "somebody else"})
	if !isInvalid(err) {
		t.Fatalf("err = %v; want the failure of a package that does not validate", err)
	}
	if got.Validation.Valid || !slices.Equal(checksOf(got.Validation.Errors), []string{"cross_spec_actor"}) {
		t.Fatalf("validation = %+v; want the one error cross_spec_actor", got.Validation)
	}
	if e := got.Validation.Errors[0]; e.Entity != "02" || !strings.Contains(e.Message, "spec 02 depends on spec 01") {
		t.Errorf("the finding = %+v; want it about spec 02 and its dependency on 01", e)
	}
	if got.Status != "draft" {
		t.Errorf("Status = %q; a package that breaks a cross-spec rule is not activated", got.Status)
	}
	dir := filepath.Join(ws.Root(), got.SpecDir)
	if status := statusOnDisk(t, dir); status != "draft" {
		t.Errorf("the package on disk is %q, want draft", status)
	}
}

// The same package with an actor of the upstream's paths validates and is
// activated: the rule asks for one shared name, not for sameness.
func TestAPackageSharingAnActorWithItsDependencyIsActivated(t *testing.T) {
	ws := newWorkspace(t)
	writeUpstream(t, ws)

	got, err := runDownstream(t, ws, packageShape{dependsOn: "01"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !got.Validation.Valid || got.Status != "active" {
		t.Errorf("valid %v, status %q; want a valid, active package: %+v", got.Validation.Valid, got.Status, got.Validation.Errors)
	}
}

// A dependency is named by the number of the spec. The directory name, which
// the model is as likely to write, names no spec: the package is reported with
// the rule, not activated.
func TestADependencyNamedByDirectoryDoesNotValidate(t *testing.T) {
	ws := newWorkspace(t)
	writeUpstream(t, ws)

	got, err := runDownstream(t, ws, packageShape{dependsOn: "01_widget_core"})
	if !isInvalid(err) {
		t.Fatalf("err = %v; want the failure of a package that does not validate", err)
	}
	if !slices.Equal(checksOf(got.Validation.Errors), []string{"cross_spec_unknown_dependency"}) {
		t.Errorf("errors = %+v; want cross_spec_unknown_dependency", got.Validation.Errors)
	}
	if got.Status != "draft" {
		t.Errorf("Status = %q, want draft", got.Status)
	}
}

// The findings of the package's own rules and those of the rules across specs
// are one report, the package's own first, as speca --validate gives them.
func TestTheCrossSpecFindingsFollowThePackagesOwnInOneReport(t *testing.T) {
	ws := newWorkspace(t)
	writeUpstream(t, ws)
	a := newAuthor(t, "02", "widget_github")
	a.shapes = map[string]packageShape{"widget_github": {dependsOn: "01", actor: "somebody else"}}
	a.skipStepValidation = true
	dropOneOwnedTest(a.artifacts[afspec.StepTasks])

	got, err := Run(context.Background(), newOptions(ws, a))
	if !isInvalid(err) {
		t.Fatalf("err = %v", err)
	}
	if want := []string{"C7", "cross_spec_actor"}; !slices.Equal(checksOf(got.Validation.Errors), want) {
		t.Errorf("errors = %v; want %v", checksOf(got.Validation.Errors), want)
	}
	if got.Validation.ErrorCount != len(got.Validation.Errors) {
		t.Errorf("error_count %d for %d findings", got.Validation.ErrorCount, len(got.Validation.Errors))
	}
}

// A package depends on one written earlier in the same run, and the one it
// depends on is not on disk yet when the dependent has been generated: its
// package is checked once the other has settled, not before. The dependency is
// found, and the rule applies to it.
func TestADependencyOnASiblingStillBeingGeneratedIsCheckedAfterItIsWritten(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape packageShape
		want  []string
	}{
		{"an actor in common", packageShape{dependsOn: "01"}, nil},
		{"no actor in common", packageShape{dependsOn: "01", actor: "somebody else"}, []string{"cross_spec_actor"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := newWorkspace(t)
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
			o := fileOptions(ws, a)
			o.MaxParallel = 3

			got, err := Run(context.Background(), o)

			if tc.want == nil && err != nil {
				t.Fatalf("Run: %v", err)
			}
			if tc.want != nil && !isInvalid(err) {
				t.Fatalf("err = %v; want the failure of a package that does not validate", err)
			}
			for _, p := range append([]Package{got.Package}, got.FollowOnSpecs...) {
				var want []string
				if p.SpecName == "widget_github" {
					want = tc.want
				}
				if got := checksOf(p.Validation.Errors); !slices.Equal(got, want) {
					t.Errorf("%s: errors %v; want %v", p.SpecName, got, want)
				}
				if wantStatus := map[bool]string{true: "active", false: "draft"}[want == nil]; p.Status != wantStatus {
					t.Errorf("%s: status %q, want %q", p.SpecName, p.Status, wantStatus)
				}
			}
		})
	}
}

// A dry run writes no package, and the packages of its split are checked
// against each other all the same.
func TestADryRunChecksASiblingDependency(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape packageShape
		want  []string
	}{
		{"an actor in common", packageShape{dependsOn: "01"}, nil},
		{"no actor in common", packageShape{dependsOn: "01", actor: "somebody else"}, []string{"cross_spec_actor"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := newWorkspace(t)
			inner := splitAuthor(t)
			inner.shapes = map[string]packageShape{"widget_github": tc.shape}
			o := fileOptions(ws, &concurrentAuthor{inner: inner})
			o.DryRun = true
			o.MaxParallel = 3

			got, err := Run(context.Background(), o)

			if (err != nil) != (tc.want != nil) {
				t.Fatalf("err = %v; want a failure: %v", err, tc.want != nil)
			}
			for _, p := range append([]Package{got.Package}, got.FollowOnSpecs...) {
				var want []string
				if p.SpecName == "widget_github" {
					want = tc.want
				}
				if got := checksOf(p.Validation.Errors); !slices.Equal(got, want) {
					t.Errorf("%s: errors %v; want %v", p.SpecName, got, want)
				}
			}
		})
	}
}

// The requirements step is shown the actors of the specs that exist, because
// the rule is decided by names the model has not seen unless it is shown them.
// The steps after it, which write no execution path, are not.
func TestTheRequirementsStepIsShownTheActorsOfTheExistingSpecs(t *testing.T) {
	ws := newWorkspace(t)
	writeUpstream(t, ws)
	a := newAuthor(t, "02", "widget_github")
	seen := map[afspec.GenerationStep][]specActors{}
	a.onGenerate = func(req artifactRequest) { seen[req.Step] = req.Actors }

	if _, err := Run(context.Background(), newOptions(ws, a)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []specActors{{Spec: "01_widget_core", Actors: []string{"afspec library", "consumer"}}}
	if got := seen[afspec.StepRequirements]; !slices.EqualFunc(got, want, equalActors) {
		t.Errorf("the requirements step was shown %+v; want %+v", got, want)
	}
	for _, step := range []afspec.GenerationStep{afspec.StepTestSpec, afspec.StepTasks} {
		if len(seen[step]) != 0 {
			t.Errorf("the %s step was shown actors: %+v", step, seen[step])
		}
	}
}

func equalActors(a, b specActors) bool { return a.Spec == b.Spec && slices.Equal(a.Actors, b.Actors) }

// A package of a split is shown the actors of the earlier packages of the run
// whose requirements are written, though their packages are not on disk yet: it
// is the only way it can name a component the earlier spec owns as that spec
// does.
func TestTheRequirementsStepIsShownTheActorsOfAnEarlierScopeStillBeingGenerated(t *testing.T) {
	ws := newWorkspace(t)
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
	o := fileOptions(ws, a)
	o.MaxParallel = 3

	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []specActors{{Spec: "01_widget_core", Actors: []string{"afspec library", "consumer"}}}
	if !slices.EqualFunc(seen, want, equalActors) {
		t.Errorf("the second package's requirements step was shown %+v; want %+v", seen, want)
	}
}

// The block names each spec and its actors, and says what the actors are for.
func TestTheActorsBlockNamesEachSpecAndItsActors(t *testing.T) {
	if got := actorsBlock(nil); got != "" {
		t.Errorf("no spec, no block: %q", got)
	}
	got := actorsBlock([]specActors{
		{Spec: "11_agent_fox_service_core", Actors: []string{"agent-fox", "hub"}},
		{Spec: "12_agent_fox_jobs", Actors: []string{"client"}},
	})
	for _, want := range []string{"`11_agent_fox_service_core`", "`agent-fox`, `hub`", "`12_agent_fox_jobs`", "`client`",
		"`dependencies`", "exactly"} {
		if !strings.Contains(got, want) {
			t.Errorf("the block lacks %q:\n%s", want, got)
		}
	}
}

// What a dependency is checked on, and how it is named, is in the prompts the
// model is given: the requirements step, for the actor, and the landscape, which
// every step that lists dependencies sees, for the number.
func TestThePromptsTeachTheCrossSpecRules(t *testing.T) {
	requirements := stepInstructions(afspec.StepRequirements)
	for _, want := range []string{"every spec", "dependencies", "actor"} {
		if !strings.Contains(requirements, want) {
			t.Errorf("the requirements instructions lack %q", want)
		}
	}
	if !strings.Contains(requirements, "share an actor") && !strings.Contains(requirements, "same actor") {
		t.Errorf("the requirements instructions do not state the shared-actor rule:\n%s", requirements)
	}
	landscape := landscapeBlock([]afspec.SpecMeta{{SpecID: "01", SpecName: "core", Status: "active"}}, ".specs")
	if !strings.Contains(landscape, "number") {
		t.Errorf("the landscape does not say a dependency is named by its number:\n%s", landscape)
	}
	if tasks := stepInstructions(afspec.StepTasks); !strings.Contains(tasks, "number") {
		t.Errorf("the tasks instructions do not say a dependency is named by its number")
	}
}
