package specpkg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
)

// makePackage creates dir with the one file every package has, a prd.md. The
// resolver looks no further.
func makePackage(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prd.md"), []byte("---\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mkdirs creates each name as a directory under root.
func mkdirs(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// sameDir fails unless got and want are one directory, whatever symlinks the
// temporary directory goes through.
func sameDir(t *testing.T, got, want string) {
	t.Helper()
	g, gerr := filepath.EvalSymlinks(got)
	w, werr := filepath.EvalSymlinks(want)
	if gerr != nil || werr != nil || g != w {
		t.Errorf("resolved %q; want %q", got, want)
	}
}

// brokenSpec is the v2 example with its plan and tests damaged in every way
// the report names: a test no task owns, a criterion and a path no test
// verifies, and a term in several criteria with no glossary entry.
func brokenSpec(t *testing.T) *afspec.Spec {
	t.Helper()
	spec, err := afspec.LoadSpec(filepath.Join("..", "..", "afspec", "testdata", "v2_example"))
	if err != nil {
		t.Fatal(err)
	}
	spec.Tasks.Tasks[0].Tests = []string{"TS-09-1", "TS-09-2"}
	req := &spec.Requirements.Requirements[1]
	req.Criteria = append(req.Criteria, afspec.Criterion{Id: "09-REQ-2.3", Pattern: afspec.CriterionPatternUbiquitous, Action: "keep the `Frobnicate` stable"})
	spec.Requirements.ExecutionPaths = append(spec.Requirements.ExecutionPaths, afspec.ExecutionPath{
		Id: "09-PATH-2", Title: "An unverified path", Steps: spec.Requirements.ExecutionPaths[0].Steps})
	for i := range spec.Requirements.Requirements[0].Criteria {
		spec.Requirements.Requirements[0].Criteria[i].Action += " using `Frobnicate`"
	}
	return spec
}

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

// savedSpec writes s under root as its NN_name directory, and loads it again
// from there.
func savedSpec(t *testing.T, root string, s *afspec.Spec) *afspec.Spec {
	t.Helper()
	dir := filepath.Join(root, s.SpecID+"_"+s.SpecName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	back, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	return back
}

// saved is loadedSpec written under root and loaded again.
func saved(t *testing.T, root, id, name, actor string, deps ...string) *afspec.Spec {
	t.Helper()
	return savedSpec(t, root, loadedSpec(t, id, name, actor, deps...))
}

func rulesOf(r afspec.ValidationResult) []string { return Rules(r.Errors) }

// The PRD bodies a package's intent is hashed from.
const (
	bodyWithIntent   = "# A package\n\n## Intent\n\nDo the thing.\n\n## Goals\n\n- one\n"
	bodyEditedIntent = "# A package\n\n## Intent\n\nDo another thing.\n\n## Goals\n\n- one\n"
	bodyNoIntent     = "# A package\n\n## Goals\n\n- one\n"
)

func hashOf(t *testing.T, body string) *string {
	t.Helper()
	h, err := afspec.ComputeIntentHash(body)
	if err != nil {
		t.Fatalf("ComputeIntentHash: %v", err)
	}
	return &h
}

func specOf(status string, hash *string, body string) *afspec.Spec {
	return &afspec.Spec{Status: status, IntentHash: hash, PRDBody: body}
}

func ptr(s string) *string { return &s }
