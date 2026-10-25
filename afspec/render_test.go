package afspec

import (
	"fmt"
	"strings"
	"testing"
)

func TestRequirementsRender_ShowsEveryCriterionAndContract(t *testing.T) {
	out := loadFixture(t, fixtureValidSpec).Requirements.Render()

	for _, want := range []string{
		"## Introduction",
		"## Glossary",
		"### 01-REQ-1: Data model",
		"**Rationale:**",
		"#### Criteria",
		"[01-REQ-1.1] WHEN a spec directory is loaded from disk, THE afspec library SHALL",
		"→ a non-nil *Spec and a nil error",
		"[01-REQ-1.2] IF a required artifact file is missing",
		"## Execution Paths",
		"### 01-PATH-1:",
		"1. **consumer** calls LoadSpec(dir)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the requirements render is missing %q", want)
		}
	}
}

func TestRequirementsRender_FlagsAnUnverifiedPackage(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	spec.Requirements.ExternalApis = []ExternalApi{{
		Package: "example.com/guess", Version: "v0.1.0", Verified: false,
		Symbols: []ExternalApiSymbol{{Name: "Do", ImportPath: "example.com/guess", Signature: "func Do()"}},
	}}
	if out := spec.Requirements.Render(); !strings.Contains(out, "UNVERIFIED") {
		t.Errorf("an unverified package is not flagged in the render:\n%s", out)
	}
}

func TestTasksRender_ShowsThePlan(t *testing.T) {
	out := loadFixture(t, fixtureValidSpec).Tasks.Render()

	for _, want := range []string{
		"### [ ] 1. Load and save spec artifacts (implement, pending)",
		"**Criteria:** 01-REQ-1",
		"**Tests:** TS-01-1, TS-01-2, TS-01-3",
		"**Steps:**",
		"**Touches:**",
		"`afspec/afspec.go`",
		"**Depends on:** 1, 2",
		"**Done when:**",
		"- the tests listed above exist, are executable and pass",
		"- `go test ./... -count=1` passes",
		"- `go vet ./...` passes",
		"`go test ./afspec/... -count=1` passes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the tasks render is missing %q", want)
		}
	}
}

// Each step is numbered task.step, so a step can be named across tasks.
func TestTasksRender_NumbersEachStepByItsTask(t *testing.T) {
	out := loadFixture(t, fixtureValidSpec).Tasks.Render()

	for _, want := range []string{
		"- 1.1 Write TS-01-1, TS-01-2 and TS-01-3 and confirm they fail\n",
		"- 1.4 Write the artifacts back through the deterministic marshaller\n",
		"- 2.1 Write TS-01-4 and confirm it fails\n",
		"- 3.3 Confirm Validate is called from production code, not only from tests\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the tasks render is missing %q", want)
		}
	}
	if strings.Contains(out, "\n1. ") {
		t.Error("a step is still rendered as an unnumbered ordered-list item")
	}
}

// Nothing in the package is dropped by an unbudgeted render: every value of
// every artifact, and every frontmatter field, appears in it. This is what
// guarantees every test renders all its fields whatever its kind, and every
// contract is shown (§11.2).
func TestRender_DropsNothingFromThePackage(t *testing.T) {
	s := fullySetSpec(t)
	// $schema and the artifacts' copies of the identity are format
	// constants and duplicates of the metadata; the pattern is the EARS
	// sentence's keyword.
	skip := map[string]bool{"$schema": true, "schema_version": true, "spec_id": true, "spec_name": true, "pattern": true}

	renders := map[string]string{"combined": s.RenderCombined()}
	ind := s.RenderIndividual()
	renders["individual"] = ind["prd"] + ind["requirements"] + ind["test_spec"] + ind["tasks"]
	for name, render := range renders {
		render = unescape(render)
		for _, artifact := range []any{s.Requirements, s.TestSpec, s.Tasks} {
			for path, value := range leaves(t, artifact, skip) {
				if !strings.Contains(render, value) {
					t.Errorf("%s: %s = %q is not in the render", name, path, value)
				}
			}
		}
		for _, want := range []string{"01_test_feature", s.Title, s.Status, s.CreatedAt, s.UpdatedAt, s.Owner,
			s.Source, "00_old_loader", "core", "io", "sha256:abc123", "Schema version:** 2",
			"verified", "_(optional)_"} {
			if !strings.Contains(render, want) {
				t.Errorf("%s: the metadata lacks %q", name, want)
			}
		}
	}
}

// A table cell keeps its row: a pipe or a newline in a value is escaped.
func TestRender_EscapesTableCells(t *testing.T) {
	out := fullySetSpec(t).Requirements.Render()
	for _, want := range []string{`| pipe \| term | a definition<br>over two lines |`, "returns a \\| b |"} {
		if !strings.Contains(out, want) {
			t.Errorf("the render lacks %q:\n%s", want, out)
		}
	}
	if got := code("a`b"); got != "``a`b``" {
		t.Errorf("code(a`b) = %q", got)
	}
}

func TestRender_IsDeterministic(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	first := spec.RenderCombined()
	for range 5 {
		if again := spec.RenderCombined(); again != first {
			t.Fatalf("two renders of the same spec differ:\n%s", firstDifference(first, again))
		}
	}
}

func TestRenderCombined_OrdersTheSections(t *testing.T) {
	out := loadFixture(t, fixtureWithArch).RenderCombined()

	last := -1
	for _, section := range []string{"# PRD", "# Architecture", "# Requirements", "# Test Specification", "# Tasks"} {
		i := strings.Index(out, section)
		if i < 0 {
			t.Fatalf("section %q is missing", section)
		}
		if i < last {
			t.Errorf("section %q appears out of order", section)
		}
		last = i
	}
}

func TestRenderIndividual_HasAnArchitectureKeyOnlyWhenPresent(t *testing.T) {
	out := loadFixture(t, fixtureValidSpec).RenderIndividual()
	for _, key := range []string{"prd", "requirements", "test_spec", "tasks"} {
		if out[key] == "" {
			t.Errorf("key %q is empty", key)
		}
	}
	if _, present := out["architecture"]; present {
		t.Error("the architecture key is present for a spec with no architecture.md")
	}
	if loadFixture(t, fixtureWithArch).RenderIndividual()["architecture"] == "" {
		t.Error("the architecture key is missing for a spec that has one")
	}
}

// §11.1: scoping reads task.criteria and task.tests directly.
func TestRenderIndividualScoped_RendersTheTaskInFullAndTheRestByID(t *testing.T) {
	out := loadFixture(t, fixtureValidSpec).RenderIndividualScoped(2)

	reqs := out["requirements"]
	if !strings.Contains(reqs, "## Spec Overview") {
		t.Error("the scoped requirements render has no overview")
	}
	if !strings.Contains(reqs, "**01-REQ-1:** Data model (other task)") {
		t.Error("the out-of-scope requirement is not summarised as one line")
	}
	if !strings.Contains(reqs, "**01-REQ-2:** Cross-file validation (included below)") {
		t.Error("the in-scope requirement is not marked as included")
	}
	if !strings.Contains(reqs, "### 01-REQ-2: Cross-file validation") {
		t.Error("the in-scope requirement is not rendered in full")
	}
	if strings.Contains(reqs, "### 01-REQ-1: Data model") {
		t.Error("an out-of-scope requirement was rendered in full")
	}

	tests := out["test_spec"]
	if !strings.Contains(tests, "TS-01-4") {
		t.Error("the task's own test is missing")
	}
	if strings.Contains(tests, "### TS-01-1 ") {
		t.Error("a test belonging to another task was rendered in full")
	}
	if !strings.Contains(tests, "## Other tests") || !strings.Contains(tests, "- **TS-01-1** (") {
		t.Error("a test belonging to another task is not listed by id")
	}

	tasks := out["tasks"]
	if !strings.Contains(tasks, "### [ ] 2. Cross-file validation") {
		t.Error("the target task is not rendered in full")
	}
	if !strings.Contains(tasks, "- [ ] 1. Load and save spec artifacts") {
		t.Error("another task is not summarised as one line")
	}
	if strings.Contains(tasks, "### [ ] 1. Load and save spec artifacts") {
		t.Error("another task was rendered in full")
	}
}

func TestRenderIndividualScoped_IncludesThePathsTheTasksTestsVerify(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)

	// Task 3 owns the smoke test, which verifies 01-PATH-1.
	if !strings.Contains(spec.RenderIndividualScoped(3)["requirements"], "### 01-PATH-1:") {
		t.Error("the path verified by the task's smoke test is missing from the scoped render")
	}
	// Task 2 owns no smoke test, so no path is in scope.
	if strings.Contains(spec.RenderIndividualScoped(2)["requirements"], "## Execution Paths") {
		t.Error("a path was rendered for a task whose tests verify none")
	}
}

// A scoped render shows everything that belongs to the whole spec, and names
// by id what belongs to other tasks: nothing in the package is missing from
// it altogether.
func TestRenderIndividualScoped_MissesNothingFromThePackage(t *testing.T) {
	s := fullySetSpec(t)
	for _, task := range s.Tasks.Tasks {
		out := s.RenderIndividualScoped(task.Id)
		render := unescape(out["prd"] + out["requirements"] + out["test_spec"] + out["tasks"])
		specWide := []any{s.Requirements.ExternalApis, s.Requirements.Glossary, s.Tasks.Dependencies, s.Tasks.TestCommands}
		for _, part := range specWide {
			for path, value := range leaves(t, part, nil) {
				if !strings.Contains(render, value) {
					t.Errorf("task %d: %s = %q is not in the scoped render", task.Id, path, value)
				}
			}
		}
		var ids []string
		for _, r := range s.Requirements.Requirements {
			ids = append(ids, r.Id)
		}
		for _, p := range s.Requirements.ExecutionPaths {
			ids = append(ids, p.Id)
		}
		for _, tt := range s.TestSpec.Tests {
			ids = append(ids, tt.Id)
		}
		for _, other := range s.Tasks.Tasks {
			ids = append(ids, fmt.Sprintf("%d. %s", other.Id, other.Title))
		}
		for _, id := range append(ids, "01_test_feature", s.Requirements.Introduction) {
			if !strings.Contains(render, id) {
				t.Errorf("task %d: the scoped render does not name %q", task.Id, id)
			}
		}
	}
}

func TestRenderIndividualScoped_FallsBackForAnUnknownTask(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	if spec.RenderIndividualScoped(99)["requirements"] != spec.RenderIndividual()["requirements"] {
		t.Error("an unknown task ID should fall back to the unscoped render")
	}
}
