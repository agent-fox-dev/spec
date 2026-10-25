package specpkg

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
)

// The findings returned are the ones that concern the spec: the actor rule is
// the downstream package's, and the upstream package it is about has none.
func TestCrossSpec_KeepsTheFindingsThatConcernTheSpec(t *testing.T) {
	root := t.TempDir()
	a := saved(t, root, "01", "a", "alpha")
	b := saved(t, root, "02", "b", "beta", "01")

	got, err := crossSpec(root, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"cross_spec_actor"}; !slices.Equal(rulesOf(got), want) || got.Errors[0].EntityID != "02" {
		t.Errorf("for the dependent: %+v; want %v about 02", got.Errors, want)
	}
	if got, err = crossSpec(root, a, nil); err != nil || len(got.Errors) != 0 {
		t.Errorf("for the package depended on: %+v, %v; want none", got.Errors, err)
	}
}

// A shared actor satisfies the rule, whatever its case.
func TestCrossSpec_IsSatisfiedByASharedActor(t *testing.T) {
	root := t.TempDir()
	saved(t, root, "01", "a", "Alpha")
	b := saved(t, root, "02", "b", "alpha", "01")

	if got, err := crossSpec(root, b, nil); err != nil || len(got.Errors) != 0 {
		t.Errorf("%+v, %v; want no finding", got.Errors, err)
	}
}

// A dependency the spec root does not hold is found, whatever it is called.
func TestCrossSpec_FindsADependencyOnNoSpec(t *testing.T) {
	root := t.TempDir()
	saved(t, root, "01", "a", "alpha")
	b := saved(t, root, "02", "b", "alpha", "01_a")

	got, err := crossSpec(root, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"cross_spec_unknown_dependency"}; !slices.Equal(rulesOf(got), want) {
		t.Errorf("%v; want %v", rulesOf(got), want)
	}
}

// The dependency cycle names no package: it is kept, with tasks.json as its
// artifact, for every package with an edge, and for no package without one.
func TestCrossSpec_KeepsACycleForThePackagesWithAnEdge(t *testing.T) {
	root := t.TempDir()
	a := saved(t, root, "01", "a", "alpha", "02")
	saved(t, root, "02", "b", "alpha", "01")
	c := saved(t, root, "03", "c", "alpha")

	got, err := crossSpec(root, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rulesOf(got), []string{"cross_spec_cycle"}) || got.Errors[0].Artifact != "tasks.json" {
		t.Errorf("for a package on the cycle: %+v; want cross_spec_cycle on tasks.json", got.Errors)
	}
	if got, err := crossSpec(root, c, nil); err != nil || len(got.Errors) != 0 {
		t.Errorf("for a package with no edge: %+v, %v; want none", got.Errors, err)
	}
}

// A glossary conflict is kept, under a rule of its own and on
// requirements.json, for a package that defines the term.
func TestCrossSpec_KeepsAGlossaryConflictForThePackagesThatDefineTheTerm(t *testing.T) {
	root := t.TempDir()
	other := loadedSpec(t, "01", "a", "alpha")
	other.Requirements.Glossary["spec"] = "something else entirely"
	savedSpec(t, root, other)
	b := saved(t, root, "02", "b", "alpha")
	undefining := loadedSpec(t, "03", "c", "alpha")
	delete(undefining.Requirements.Glossary, "spec")
	c := savedSpec(t, root, undefining)

	got, err := crossSpec(root, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Warnings) == 0 {
		t.Fatal("no glossary warning")
	}
	for _, w := range got.Warnings {
		if w.Check != "cross_spec_glossary" || w.Artifact != "requirements.json" || w.EntityID != "spec" {
			t.Errorf("warning = %+v; want cross_spec_glossary on requirements.json about spec", w)
		}
	}
	if got, err := crossSpec(root, c, nil); err != nil || len(got.Warnings) != 0 {
		t.Errorf("for a package that does not define the term: %+v, %v; want none", got.Warnings, err)
	}
}

// A spec that is not in the spec root, an archived one or one a dry run has not
// written, is checked with the packages in it. So is each package handed over
// beside it: the packages of a dry run are in memory only.
func TestCrossSpec_TakesPackagesThatAreNotOnDisk(t *testing.T) {
	root := t.TempDir()
	saved(t, root, "01", "a", "alpha")
	b := loadedSpec(t, "02", "b", "beta", "01")
	b.Dir = filepath.Join(root, "02_b") // where it would be written

	got, err := crossSpec(root, b, nil)
	if err != nil || !slices.Equal(rulesOf(got), []string{"cross_spec_actor"}) {
		t.Fatalf("on-disk upstream: %+v, %v; want cross_spec_actor", got.Errors, err)
	}

	empty := t.TempDir()
	a := loadedSpec(t, "01", "a", "alpha")
	a.Dir = filepath.Join(empty, "01_a")
	b.Dir = filepath.Join(empty, "02_b")
	got, err = crossSpec(empty, b, []*afspec.Spec{a})
	if err != nil || !slices.Equal(rulesOf(got), []string{"cross_spec_actor"}) {
		t.Fatalf("in-memory upstream: %+v, %v; want cross_spec_actor, not an unknown dependency", got.Errors, err)
	}
}

// The package handed over replaces the one on disk at its directory: what is
// validated is what the caller holds.
func TestCrossSpec_PrefersTheSpecHandedOverToTheOneOnDisk(t *testing.T) {
	root := t.TempDir()
	saved(t, root, "01", "a", "alpha")
	onDisk := saved(t, root, "02", "b", "beta") // no dependency on disk
	edited := loadedSpec(t, "02", "b", "beta", "01")
	edited.Dir = onDisk.Dir

	got, err := crossSpec(root, edited, nil)
	if err != nil || !slices.Equal(rulesOf(got), []string{"cross_spec_actor"}) {
		t.Errorf("%+v, %v; want the edited spec's dependency to be checked", got.Errors, err)
	}
}

// The verdict is the package's own findings and then the ones across specs, as
// one result; a package that passes its own rules does not validate when one
// across specs fails.
func TestVerdict_IsTheOwnFindingsAndThenTheCrossSpecOnes(t *testing.T) {
	root := t.TempDir()
	saved(t, root, "01", "a", "alpha")
	b := saved(t, root, "02", "b", "beta", "01")
	own := b.Validate()

	got, err := Verdict(root, b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Valid || len(got.Errors) != len(own.Errors)+1 {
		t.Fatalf("valid %v with %d errors; want the package's %d and one more", got.Valid, len(got.Errors), len(own.Errors))
	}
	if last := got.Errors[len(got.Errors)-1]; last.Check != "cross_spec_actor" {
		t.Errorf("the last finding is %+v; want cross_spec_actor after the package's own", last)
	}
}

// A spec root that cannot be read leaves the package's own verdict, and says
// why: the rules across specs were not run.
func TestVerdict_OfAnUnreadableRootIsThePackagesOwn(t *testing.T) {
	s := loadedSpec(t, "01", "a", "alpha")
	own := s.Validate()

	got, err := Verdict(filepath.Join(t.TempDir(), "missing"), s)
	if err == nil {
		t.Error("want the error of reading the spec root")
	}
	if got.Valid != own.Valid || len(got.Errors) != len(own.Errors) || len(got.Warnings) != len(own.Warnings) {
		t.Errorf("verdict %+v; want the package's own %+v", got, own)
	}
}
