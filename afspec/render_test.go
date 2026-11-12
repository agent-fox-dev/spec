package afspec

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRequirementsRender_ShowsEveryCriterionContractAndCheck(t *testing.T) {
	out := loadFixture(t, fixtureValidSpec).Requirements.Render()

	for _, want := range []string{
		"### REQ-1: Data model",
		"**Rationale:**",
		"#### Criteria",
		"[REQ-1.1] WHEN a spec directory is loaded from disk, THE afspec library SHALL",
		"→ a non-nil *Spec and a nil error",
		"   Check: unit\n   - setup: a directory holding prd.md and the three JSON artifacts\n   ```\n   s, err = LoadSpec(dir); assert err == nil; assert s.Requirements != nil\n   ```\n",
		"[REQ-1.2] IF a required artifact file is missing",
		"   Check: property\n",
		"## Execution Paths",
		"### PATH-1:",
		"1. **consumer** calls LoadSpec(dir)",
		"**Smoke check — real components (must not be mocked):** afspec library, spec directory on disk",
		"**Setup:**\n\n- a spec directory on disk",
		"**End state:**\n\n- LoadSpec returns no error\n- Validate reports the spec as valid",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the requirements render is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "## Introduction") || strings.Contains(out, "## Glossary") {
		t.Error("the render has a section version 3 has no data for")
	}
}

// The requirement's system is the criterion's when the criterion names none;
// a shared check names the id it is the same as.
func TestRequirementsRender_ResolvesTheSystemAndNamesASharedCheck(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	spec.Requirements.Requirements[0].Criteria[1].System = strPtr("the loader")
	spec.Requirements.Requirements[0].Criteria[2].Check = SameAs("REQ-1.1")
	out := spec.Requirements.Render()
	for _, want := range []string{
		"THE afspec library SHALL return a Spec",
		"THEN THE the loader SHALL return a LoadError",
		"[REQ-1.3] THE afspec library SHALL for any spec",
		"   Check: same as REQ-1.1\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the render lacks %q:\n%s", want, out)
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

// The plan renders every task of tasks.json and then the derived integration
// task, with no state.
func TestRenderTasks_ShowsThePlanAndTheIntegrationTask(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	spec.Progress = &ProgressV3Json{Tasks: []TaskProgress{{Id: 1, State: TaskStateDone}}}
	out := spec.RenderTasks()

	for _, want := range []string{
		"### 1. Load and save spec artifacts (implement)",
		"**Criteria:** REQ-1",
		"**Steps:**",
		"**Touches:**",
		"`afspec/afspec.go`",
		"**Depends on:** 1\n",
		"**Done when:**",
		"- the checks of the criteria listed above exist as tests, are executable and pass",
		"- `go test ./... -count=1` passes",
		"- `go vet ./...` passes",
		"### 3. " + IntegrationTaskTitle + " (integration)",
		"**Depends on:** 1, 2\n",
		"- 3.1 Run the smoke check of PATH-1 (A consumer loads and validates a spec) with afspec library, spec directory on disk unmocked and confirm its end state: LoadSpec returns no error; Validate reports the spec as valid",
		"- 3.2 Trace PATH-1 through the real code",
		"For every criterion with a contract (REQ-1.1, REQ-1.2, REQ-1.3, REQ-2.1), confirm",
		"Search every path in touches for stub markers",
		"- `afspec/validate.go`",
		"- every smoke check above runs with its real components and reaches its end state",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the tasks render is missing %q:\n%s", want, out)
		}
	}
	for _, gone := range []string{"[x]", "[ ]", "pending", "done)", "**Tests:**"} {
		if strings.Contains(out, gone) {
			t.Errorf("the tasks render holds %q, which is state or version 2:\n%s", gone, out)
		}
	}
	if strings.Contains(spec.Tasks.Render(), IntegrationTaskTitle) {
		t.Error("the tasks artifact's own render carries the derived integration task")
	}
}

// Each step is numbered task.step, so a step can be named across tasks.
func TestRenderTasks_NumbersEachStepByItsTask(t *testing.T) {
	out := loadFixture(t, fixtureValidSpec).RenderTasks()

	for _, want := range []string{
		"- 1.1 Write TS-01-1, TS-01-2 and TS-01-3 and confirm they fail\n",
		"- 1.4 Write the artifacts back through the deterministic marshaller\n",
		"- 2.1 Write TS-01-4 and confirm it fails\n",
		"- 3.1 Run the smoke check of PATH-1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the tasks render is missing %q", want)
		}
	}
	if strings.Contains(out, "\n1. ") {
		t.Error("a step is still rendered as an unnumbered ordered-list item")
	}
}

// Nothing in the contract is dropped by an unbudgeted render: every value of
// every artifact, and every frontmatter field, appears in it (§11.2).
func TestRender_DropsNothingFromTheContract(t *testing.T) {
	s := fullySetSpec(t)
	// $schema and the artifacts' copies of the identity are format
	// constants and duplicates of the metadata; the pattern is the EARS
	// sentence's keyword.
	skip := map[string]bool{"$schema": true, "schema_version": true, "spec_id": true, "spec_name": true, "pattern": true}

	renders := map[string]string{"combined": s.RenderCombined()}
	ind := s.RenderIndividual()
	renders["individual"] = ind["prd"] + ind["requirements"] + ind["tasks"]
	for name, render := range renders {
		render = unescape(render)
		for _, artifact := range []any{s.Requirements, s.Tasks} {
			for path, value := range leaves(t, artifact, skip) {
				if !strings.Contains(render, value) {
					t.Errorf("%s: %s = %q is not in the render", name, path, value)
				}
			}
		}
		for _, want := range []string{"01_test_feature", s.Title, s.Status, s.CreatedAt,
			s.Source, "00_old_loader", "sha256:abc123", "Schema version:** 3", "Steering version:** 3.1.4",
			"verified", "same as REQ-1.1"} {
			if !strings.Contains(render, want) {
				t.Errorf("%s: the metadata lacks %q", name, want)
			}
		}
	}
}

// The progress is not the contract: a task move changes no byte of the
// render, and the render holds no state and no updated_at.
func TestRender_IgnoresTheProgress(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	spec.UpdatedAt = "2026-02-02T02:02:02Z"
	before := spec.RenderedBytes()
	spec.Progress = spec.CompleteTaskStates([]int{1, 2, 3})
	spec.UpdatedAt = "2027-03-03T03:03:03Z"
	if after := spec.RenderedBytes(); string(after) != string(before) {
		t.Errorf("the render changed with the progress:\n%s", firstDifference(string(before), string(after)))
	}
	if r := string(before); strings.Contains(r, "2026-02-02") || strings.Contains(r, "Updated") {
		t.Error("the render holds updated_at")
	}
}

// A table cell keeps its row: a pipe or a newline in a value is escaped.
func TestRender_EscapesTableCells(t *testing.T) {
	s := fullySetSpec(t)
	s.Requirements.ExternalApis[0].Symbols[0].Notes = strPtr("returns a | b\nover two lines")
	out := s.Requirements.Render()
	if want := `| returns a \| b<br>over two lines |`; !strings.Contains(out, want) {
		t.Errorf("the render lacks %q:\n%s", want, out)
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
	for _, section := range []string{"# PRD", "# Architecture", "# Requirements", "# Tasks"} {
		i := strings.Index(out, section)
		if i < 0 {
			t.Fatalf("section %q is missing", section)
		}
		if i < last {
			t.Errorf("section %q appears out of order", section)
		}
		last = i
	}
	if strings.Contains(out, "# Test Specification") {
		t.Error("the render has a test specification section")
	}
}

func TestRenderIndividual_HasAnArchitectureKeyOnlyWhenPresent(t *testing.T) {
	out := loadFixture(t, fixtureValidSpec).RenderIndividual()
	for _, key := range []string{"prd", "requirements", "tasks"} {
		if out[key] == "" {
			t.Errorf("key %q is empty", key)
		}
	}
	for _, key := range []string{"architecture", "test_spec"} {
		if _, present := out[key]; present {
			t.Errorf("the %s key is present", key)
		}
	}
	if loadFixture(t, fixtureWithArch).RenderIndividual()["architecture"] == "" {
		t.Error("the architecture key is missing for a spec that has one")
	}
}

// §11.1: scoping reads task.criteria directly.
func TestRenderIndividualScoped_RendersTheTaskInFullAndTheRestByID(t *testing.T) {
	out := loadFixture(t, fixtureValidSpec).RenderIndividualScoped(2)

	reqs := out["requirements"]
	if !strings.Contains(reqs, "## Spec Overview") {
		t.Error("the scoped requirements render has no overview")
	}
	if !strings.Contains(reqs, "**REQ-1:** Data model (other task)") {
		t.Error("the out-of-scope requirement is not summarised as one line")
	}
	if !strings.Contains(reqs, "**REQ-2:** Cross-file validation (included below)") {
		t.Error("the in-scope requirement is not marked as included")
	}
	if !strings.Contains(reqs, "### REQ-2: Cross-file validation") || !strings.Contains(reqs, "Check: integration") {
		t.Error("the in-scope requirement is not rendered in full with its check")
	}
	if strings.Contains(reqs, "### REQ-1: Data model") {
		t.Error("an out-of-scope requirement was rendered in full")
	}
	if !strings.Contains(reqs, "**PATH-1:** A consumer loads and validates a spec (verified by the integration task)") ||
		strings.Contains(reqs, "## Execution Paths") {
		t.Error("the path is not listed by id alone")
	}

	tasks := out["tasks"]
	if !strings.Contains(tasks, "### 2. Cross-file validation (implement)") {
		t.Error("the target task is not rendered in full")
	}
	if !strings.Contains(tasks, "- 1. Load and save spec artifacts (implement)") || !strings.Contains(tasks, "- 3. "+IntegrationTaskTitle+" (integration)") {
		t.Error("another task is not summarised as one line")
	}
	if strings.Contains(tasks, "### 1. Load and save spec artifacts") {
		t.Error("another task was rendered in full")
	}
}

// A path is in a task's scope when a check of the task's criteria is the
// same as the path's smoke check.
func TestRenderIndividualScoped_IncludesThePathsASharedCheckNames(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	spec.Requirements.Requirements[1].Criteria[0].Check = SameAs("PATH-1")

	if out := spec.RenderIndividualScoped(2)["requirements"]; !strings.Contains(out, "### PATH-1:") || !strings.Contains(out, "**Smoke check") {
		t.Error("the path a shared check names is missing from the scoped render")
	}
	if strings.Contains(spec.RenderIndividualScoped(1)["requirements"], "## Execution Paths") {
		t.Error("a path was rendered for a task whose checks name none")
	}
}

// The integration task's render holds every path with its smoke check, every
// requirement one line each, the contracts, and the task in full.
func TestRenderIndividualScoped_RendersTheIntegrationTask(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	out := spec.RenderIndividualScoped(3)
	reqs := out["requirements"]
	for _, want := range []string{
		"**REQ-1:** Data model (other task)", "**REQ-2:** Cross-file validation (other task)",
		"**PATH-1:** A consumer loads and validates a spec (included below)",
		"## Contracts", "- **REQ-1.2:** a nil *Spec and a LoadError whose File field is the missing path",
		"### PATH-1:", "**Smoke check", "**End state:**",
	} {
		if !strings.Contains(reqs, want) {
			t.Errorf("the integration render lacks %q:\n%s", want, reqs)
		}
	}
	if strings.Contains(reqs, "### REQ-1: Data model") {
		t.Error("a requirement was rendered in full for the integration task")
	}
	tasks := out["tasks"]
	if !strings.Contains(tasks, "### 3. "+IntegrationTaskTitle+" (integration)") || !strings.Contains(tasks, "- 1. Load and save spec artifacts (implement)") {
		t.Errorf("the integration task is not the one in full:\n%s", tasks)
	}
}

// A scoped render shows everything that belongs to the whole spec, and names
// by id what belongs to other tasks: nothing in the package is missing from
// it altogether.
func TestRenderIndividualScoped_MissesNothingFromThePackage(t *testing.T) {
	s := fullySetSpec(t)
	for _, task := range s.Plan() {
		render := unescape(scopedRender(s.RenderIndividualScoped(task.Id)))
		specWide := []any{s.Requirements.ExternalApis, s.Tasks.Dependencies, s.Tasks.TestCommands}
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
		for _, other := range s.Plan() {
			ids = append(ids, fmt.Sprintf("%d. %s", other.Id, other.Title))
		}
		for _, id := range append(ids, "01_test_feature") {
			if !strings.Contains(render, id) {
				t.Errorf("task %d: the scoped render does not name %q", task.Id, id)
			}
		}
	}
}

// A task's render leaves out the PRD's Background and Requirements, which its
// criteria restate, names what it left out, and keeps every other section;
// the unscoped render keeps them all.
func TestRenderIndividualScoped_LeavesOutThePRDsProseRequirements(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	spec.PRDBody = "# Feature\n\n## Intent\n\nWhy it exists.\n\n## Background\n\nThe code before.\n\n" +
		"## Requirements\n\n### 1. Area\n\nThe prose requirement.\n\n```md\n## Not a heading\n```\n\n" +
		"## Design Decisions\n\n1. A decision.\n"

	prd := spec.RenderIndividualScoped(2)["prd"]
	for _, gone := range []string{"## Background", "The code before.", "## Requirements", "The prose requirement.", "## Not a heading"} {
		if strings.Contains(prd, gone) {
			t.Errorf("the task's render still holds %q:\n%s", gone, prd)
		}
	}
	for _, kept := range []string{"## Intent", "Why it exists.", "## Design Decisions", "1. A decision.", "**Status:**",
		"_The PRD's Background and Requirements are not in a task's render: the criteria this task owns are below. " +
			"Read them in prd.md or spec.md._"} {
		if !strings.Contains(prd, kept) {
			t.Errorf("the task's render lost %q:\n%s", kept, prd)
		}
	}
	if strings.Index(prd, "_The PRD's") > strings.Index(prd, "## Design Decisions") {
		t.Error("the note is not where the first section was")
	}

	full := spec.RenderCombined()
	for _, kept := range []string{"## Background", "The prose requirement.", "## Not a heading"} {
		if !strings.Contains(full, kept) {
			t.Errorf("the unscoped render lost %q", kept)
		}
	}
	if strings.Contains(full, "_The PRD's") {
		t.Error("the unscoped render carries the scoped note")
	}
}

// A body with one of the two sections is named alone; a body with neither is
// rendered as it was, with no note; a heading inside a fence starts nothing.
func TestRenderIndividualScoped_NamesOnlyTheSectionsItLeftOut(t *testing.T) {
	cases := []struct {
		name, body, want, wantGone string
	}{
		{"background only", "## Intent\n\nx\n\n## Background\n\nold\n\n## Goals\n\ng\n",
			"_The PRD's Background is not in a task's render: the criteria this task owns are below. Read it in prd.md or spec.md._",
			"old"},
		{"requirements last", "## Intent\n\nx\n\n## Requirements\n\nprose\n",
			"_The PRD's Requirements is not in a task's render", "prose"},
		{"neither", "## Intent\n\nx\n\n## Goals\n\ng\n", "## Goals\n\ng\n", "_The PRD's"},
		{"fenced heading", "## Intent\n\n```\n## Requirements\n```\n\nafter\n", "## Requirements\n```\n\nafter", "_The PRD's"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := loadFixture(t, fixtureValidSpec)
			spec.PRDBody = tc.body
			prd := spec.RenderIndividualScoped(1)["prd"]
			if !strings.Contains(prd, tc.want) {
				t.Errorf("the render lacks %q:\n%s", tc.want, prd)
			}
			if strings.Contains(prd, tc.wantGone) {
				t.Errorf("the render holds %q:\n%s", tc.wantGone, prd)
			}
		})
	}
	spec := loadFixture(t, fixtureValidSpec)
	spec.PRDBody = "## Intent\n\nx\n\n## Goals\n\ng\n"
	if got, want := spec.RenderIndividualScoped(1)["prd"], spec.RenderIndividual()["prd"]; got != want {
		t.Errorf("a PRD with neither section renders differently in a task's render:\n%s\nwant\n%s", got, want)
	}
}

func TestRenderIndividualScoped_FallsBackForAnUnknownTask(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	if spec.RenderIndividualScoped(99)["requirements"] != spec.RenderIndividual()["requirements"] {
		t.Error("an unknown task ID should fall back to the unscoped render")
	}
}

// The rendered file is the header line, a blank line and the whole unscoped,
// unbudgeted render, and a file written from the exported bytes is the one a
// save writes.
func TestRenderedBytes_AreTheHeaderAndTheWholeRender(t *testing.T) {
	const header = "<!-- Generated by afspec from prd.md, requirements.json, tasks.json and architecture.md. Do not edit: change those files, or run speca <package> --render. -->"
	if RenderedFileName != "spec.md" {
		t.Errorf("RenderedFileName = %q; want spec.md", RenderedFileName)
	}
	for _, fixture := range []string{fixtureValidSpec, fixtureWithArch} {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			spec := loadFixture(t, fixture)
			b := string(spec.RenderedBytes())
			lines := strings.SplitN(b, "\n", 3)
			if len(lines) < 3 {
				t.Fatalf("the rendered bytes have %d lines", len(lines))
			}
			if lines[0] != header {
				t.Errorf("first line = %q; want the header", lines[0])
			}
			if lines[1] != "" {
				t.Errorf("second line = %q; want it empty", lines[1])
			}
			if lines[2] != spec.RenderCombined() {
				t.Errorf("the bytes after the header are not the combined render:\n%s", firstDifference(spec.RenderCombined(), lines[2]))
			}

			dir := t.TempDir()
			if err := spec.Save(dir); err != nil {
				t.Fatal(err)
			}
			saved, err := os.ReadFile(filepath.Join(dir, RenderedFileName))
			if err != nil {
				t.Fatal(err)
			}
			if string(saved) != b {
				t.Errorf("the saved %s differs from RenderedBytes:\n%s", RenderedFileName, firstDifference(b, string(saved)))
			}
		})
	}
}

// The same package always renders to the same bytes, and they hold no time
// but the one the frontmatter carries.
func TestRenderedBytes_AreTheSameEveryTime(t *testing.T) {
	scaffold := CreateSpec("07", "new_feature")
	scaffold.Title = "New Feature"
	scaffold.Status = "draft"
	scaffold.CreatedAt = "2026-01-01T00:00:00Z"
	scaffold.UpdatedAt = "2026-01-02T00:00:00Z"
	scaffold.PRDBody = "# New Feature\n\n## Intent\n\nSomething.\n"

	instant := regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`)
	for _, tc := range []struct {
		name string
		spec *Spec
	}{
		{"an empty scaffold", scaffold},
		{"a full package", loadFixture(t, fixtureValidSpec)},
		{"a package with an architecture", loadFixture(t, fixtureWithArch)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := tc.spec.RenderedBytes()
			if again := tc.spec.RenderedBytes(); string(again) != string(first) {
				t.Fatalf("two compositions differ:\n%s", firstDifference(string(first), string(again)))
			}
			dir := t.TempDir()
			if err := tc.spec.Save(dir); err != nil {
				t.Fatal(err)
			}
			if reloaded := loadFixture(t, dir).RenderedBytes(); string(reloaded) != string(first) {
				t.Errorf("the reloaded package composes differently:\n%s", firstDifference(string(first), string(reloaded)))
			}
			for _, at := range instant.FindAllString(string(first), -1) {
				if at != tc.spec.CreatedAt {
					t.Errorf("the render holds the instant %s, which is not created_at", at)
				}
			}
		})
	}
}

// The render lists the affected-tests command with the other test commands,
// and a package without one renders as it did.
func TestRenderTasks_ListsTheAffectedTestsCommand(t *testing.T) {
	s := loadFixture(t, fixtureValidSpec)
	before := s.RenderTasks()
	affected := "make test-affected BASE={base}"
	s.Tasks.TestCommands.AffectedTests = &affected
	if out := s.RenderTasks(); !strings.Contains(out, "- **affected_tests:** `make test-affected BASE={base}`\n") {
		t.Errorf("the render has no line for affected_tests:\n%s", out)
	}
	s.Tasks.TestCommands.AffectedTests = nil
	if s.RenderTasks() != before {
		t.Error("the render of a package without affected_tests changed")
	}
}
