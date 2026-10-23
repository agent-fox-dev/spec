package specpkg

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
)

// loadedSpec is the repository's valid fixture relabelled as spec id, depending
// on the specs in deps, with every step of its paths played by actor.
func loadedSpec(t *testing.T, id, name, actor string, deps ...string) *afspec.Spec {
	t.Helper()
	s, err := afspec.LoadSpec(filepath.Join("..", "..", "afspec", "testdata", "valid_spec"))
	if err != nil {
		t.Fatal(err)
	}
	s.SpecID, s.SpecName = id, name
	for i := range s.Requirements.ExecutionPaths {
		for j := range s.Requirements.ExecutionPaths[i].Steps {
			s.Requirements.ExecutionPaths[i].Steps[j].Actor = actor
		}
	}
	s.Tasks.Dependencies = nil
	for _, d := range deps {
		s.Tasks.Dependencies = append(s.Tasks.Dependencies, afspec.Dependency{Spec: d, Reason: "needs " + d})
	}
	return s
}

// savedSpec is loadedSpec written under root as its NN_name directory, and
// loaded again from there.
func savedSpec(t *testing.T, root, id, name, actor string, deps ...string) *afspec.Spec {
	t.Helper()
	dir := filepath.Join(root, id+"_"+name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := loadedSpec(t, id, name, actor, deps...).Save(dir); err != nil {
		t.Fatal(err)
	}
	s, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func rulesOf(r afspec.ValidationResult) []string { return Rules(r.Errors) }

// The findings returned are the ones that concern the spec: the actor rule is
// the downstream package's, and the upstream package it is about has none.
func TestCrossSpecKeepsTheFindingsThatConcernTheSpec(t *testing.T) {
	root := t.TempDir()
	a := savedSpec(t, root, "01", "a", "alpha")
	b := savedSpec(t, root, "02", "b", "beta", "01")

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
func TestCrossSpecIsSatisfiedByASharedActor(t *testing.T) {
	root := t.TempDir()
	savedSpec(t, root, "01", "a", "Alpha")
	b := savedSpec(t, root, "02", "b", "alpha", "01")

	if got, err := crossSpec(root, b, nil); err != nil || len(got.Errors) != 0 {
		t.Errorf("%+v, %v; want no finding", got.Errors, err)
	}
}

// A dependency the spec root does not hold is found, whatever it is called.
func TestCrossSpecFindsADependencyOnNoSpec(t *testing.T) {
	root := t.TempDir()
	savedSpec(t, root, "01", "a", "alpha")
	b := savedSpec(t, root, "02", "b", "alpha", "01_a")

	got, err := crossSpec(root, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"cross_spec_unknown_dependency"}; !slices.Equal(rulesOf(got), want) {
		t.Errorf("%v; want %v", rulesOf(got), want)
	}
}

// A spec that is not in the spec root, an archived one or one a dry run has not
// written, is checked with the packages in it. So is each package handed over
// beside it: the packages of a dry run are in memory only.
func TestCrossSpecTakesPackagesThatAreNotOnDisk(t *testing.T) {
	root := t.TempDir()
	savedSpec(t, root, "01", "a", "alpha")
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
func TestCrossSpecPrefersTheSpecHandedOverToTheOneOnDisk(t *testing.T) {
	root := t.TempDir()
	savedSpec(t, root, "01", "a", "alpha")
	onDisk := savedSpec(t, root, "02", "b", "beta") // no dependency on disk
	edited := loadedSpec(t, "02", "b", "beta", "01")
	edited.Dir = onDisk.Dir

	got, err := crossSpec(root, edited, nil)
	if err != nil || !slices.Equal(rulesOf(got), []string{"cross_spec_actor"}) {
		t.Errorf("%+v, %v; want the edited spec's dependency to be checked", got.Errors, err)
	}
}

// A spec root that cannot be read is an error, not a verdict: the rules were
// not run.
func TestCrossSpecReportsAnUnreadableRoot(t *testing.T) {
	s := loadedSpec(t, "01", "a", "alpha")
	if _, err := crossSpec(filepath.Join(t.TempDir(), "missing"), s, nil); err == nil {
		t.Error("want the error of reading the spec root")
	}
}

// The verdict is the package's own findings and then the ones across specs, as
// one result; a package that passes its own rules does not validate when one
// across specs fails.
func TestVerdictIsTheOwnFindingsAndThenTheCrossSpecOnes(t *testing.T) {
	root := t.TempDir()
	savedSpec(t, root, "01", "a", "alpha")
	b := savedSpec(t, root, "02", "b", "beta", "01")
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

// A spec root that cannot be read leaves the package's own verdict, and says why.
func TestVerdictOfAnUnreadableRootIsThePackagesOwn(t *testing.T) {
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
