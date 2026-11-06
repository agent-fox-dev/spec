package afspec

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The checked-in fixtures satisfy schema validation and every rule C1 to C11.
func TestValidate_AcceptsEveryValidFixture(t *testing.T) {
	for _, fixture := range validFixtures {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			result := loadFixture(t, fixture).Validate()
			if !result.Valid {
				t.Fatalf("Validate reported %d errors on a valid fixture: %v", len(result.Errors), result.Errors)
			}
		})
	}
}

// One case per way of breaking each cross-file rule of §10.2. entity, when
// set, is the id the error must name; message, when set, a phrase it must
// contain.
func TestValidateCrossFile_ReportsEachBrokenRule(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*Spec)
		check   string
		entity  string
		message string
	}{
		{name: "requirements disagrees on spec_id", check: "C1",
			mutate: func(s *Spec) { s.Requirements.SpecId = "99" }},
		{name: "tasks disagrees on spec_name", check: "C1",
			mutate: func(s *Spec) { s.Tasks.SpecName = "other_name" }},
		{name: "folder name disagrees with spec_id", check: "C1",
			mutate: func(s *Spec) { s.Dir = "/tmp/07_test_feature" }},

		{name: "criterion ID in the wrong format", check: "C2",
			mutate: func(s *Spec) { s.Requirements.Requirements[0].Criteria[0].Id = "01-REQ-1.E1" }},
		{name: "test ID carries another spec's prefix", check: "C2",
			mutate: func(s *Spec) { s.TestSpec.Tests[0].Id = "TS-77-1" }},
		{name: "duplicate task ID", check: "C2",
			mutate: func(s *Spec) { s.Tasks.Tasks[1].Id = s.Tasks.Tasks[0].Id }},
		{name: "criterion listed under the wrong requirement", check: "C2",
			mutate: func(s *Spec) { s.Requirements.Requirements[1].Criteria[0].Id = "01-REQ-1.9" }},

		{name: "verifies names an unknown criterion", check: "C3", message: "01-REQ-9.9",
			mutate: func(s *Spec) { s.TestSpec.Tests[0].Verifies = []string{"01-REQ-9.9"} }},

		// TS-01-4 is the only test verifying 01-REQ-2.1.
		{name: "criterion verified by no test", check: "C4", entity: "01-REQ-2.1",
			mutate: func(s *Spec) { s.TestSpec.Tests[3].Verifies = []string{"01-REQ-1.1"} }},

		// Demote the smoke test; the path is then verified by nothing.
		{name: "path verified only by a unit test", check: "C5", entity: "01-PATH-1",
			mutate: func(s *Spec) {
				s.TestSpec.Tests[4].Kind = TestKindUnit
				s.TestSpec.Tests[4].RealComponents = nil
				s.Tasks.Tasks[2].Kind = TaskKindIntegration
			}},
		{name: "smoke test verifying no path", check: "C5",
			mutate: func(s *Spec) { s.TestSpec.Tests[4].Verifies = []string{"01-REQ-1.1"} }},

		{name: "task names an unknown criterion", check: "C6",
			mutate: func(s *Spec) { s.Tasks.Tasks[0].Criteria = []string{"01-REQ-8"} }},
		{name: "task names an unknown test", check: "C6",
			mutate: func(s *Spec) { s.Tasks.Tasks[0].Tests = []string{"TS-01-99"} }},
		{name: "depends_on names a missing task", check: "C6",
			mutate: func(s *Spec) { s.Tasks.Tasks[1].DependsOn = []int{42} }},
		{name: "depends_on names a later task", check: "C6", message: "lower task IDs",
			mutate: func(s *Spec) { s.Tasks.Tasks[0].DependsOn = []int{3} }},

		// The defect that motivated format v2: under v1 every edge-case and
		// property test in every archived spec was owned by no task, and
		// validation accepted it.
		{name: "test owned by no task", check: "C7", entity: "TS-01-3",
			mutate: func(s *Spec) { s.Tasks.Tasks[0].Tests = []string{"TS-01-1", "TS-01-2"} }},

		{name: "criterion claimed by no task", check: "C8", entity: "01-REQ-2.1",
			mutate: func(s *Spec) { s.Tasks.Tasks[1].Criteria = []string{"01-REQ-1.1"} }},
		{name: "criterion claimed only by the integration task", check: "C8",
			mutate: func(s *Spec) {
				s.Tasks.Tasks[1].Criteria = []string{"01-REQ-1.1"}
				s.Tasks.Tasks[2].Criteria = []string{"01-REQ-2.1"}
			}},

		{name: "no integration task", check: "C9",
			mutate: func(s *Spec) {
				s.Tasks.Tasks[2].Kind = TaskKindImplement
				s.Tasks.Tasks[2].Criteria = []string{"01-REQ-2.1"}
			}},
		{name: "integration task is not last", check: "C9",
			mutate: func(s *Spec) {
				s.Tasks.Tasks[0].Kind, s.Tasks.Tasks[2].Kind = TaskKindIntegration, TaskKindImplement
				s.Tasks.Tasks[2].Criteria = []string{"01-REQ-2.1"}
				s.Tasks.Tasks[0].Tests = append(s.Tasks.Tasks[0].Tests, "TS-01-5")
			}},
		{name: "integration task does not own every smoke test", check: "C9", entity: "TS-01-5",
			mutate: func(s *Spec) {
				s.Tasks.Tasks[2].Tests = []string{"TS-01-1"}
				s.Tasks.Tasks[0].Tests = append(s.Tasks.Tasks[0].Tests, "TS-01-5")
			}},
		{name: "two integration tasks", check: "C9",
			mutate: func(s *Spec) { s.Tasks.Tasks[1].Kind = TaskKindIntegration }},

		// 01-REQ-1.2 is the unwanted criterion.
		{name: "unwanted criterion without a contract", check: "C10", entity: "01-REQ-1.2",
			mutate: func(s *Spec) { s.Requirements.Requirements[0].Criteria[1].Contract = nil }},

		{name: "smoke test without real components", check: "C11",
			mutate: func(s *Spec) { s.TestSpec.Tests[4].RealComponents = nil }},
		{name: "unit test with real components", check: "C11",
			mutate: func(s *Spec) { s.TestSpec.Tests[0].RealComponents = []string{"a database"} }},
	} {
		t.Run(tc.check+" "+tc.name, func(t *testing.T) {
			result := mutateFixture(t, tc.mutate).ValidateCrossFile()
			if result.Valid {
				t.Fatalf("ValidateCrossFile reported the spec as valid; want a %s violation", tc.check)
			}
			if !hasCheck(result, tc.check) {
				t.Fatalf("no %s error; got checks %v (messages: %v)", tc.check, errorChecks(result), result.Errors)
			}
			if tc.entity != "" && !hasEntityID(result, tc.entity) {
				t.Errorf("%s did not name %s; got %v", tc.check, tc.entity, result.Errors)
			}
			if tc.message != "" && !strings.Contains(strings.Join(messages(result), " "), tc.message) {
				t.Errorf("no error says %q: %v", tc.message, messages(result))
			}
		})
	}
}

func TestValidateCrossFile_LetsARequirementIDClaimAllItsCriteria(t *testing.T) {
	spec := mutateFixture(t, func(s *Spec) {
		s.Tasks.Tasks[0].Criteria = []string{"01-REQ-1"}
		s.Tasks.Tasks[1].Criteria = []string{"01-REQ-2"}
	})
	if result := spec.ValidateCrossFile(); !result.Valid {
		t.Fatalf("claiming a requirement wholesale should cover its criteria; got %v", result.Errors)
	}
}

// The ownership errors say how to fix them, not just what is wrong.
func TestValidateCrossFile_OwnershipErrorsCarryARemedy(t *testing.T) {
	spec := mutateFixture(t, func(s *Spec) {
		for i := range s.Tasks.Tasks {
			s.Tasks.Tasks[i].Tests = nil
			s.Tasks.Tasks[i].Criteria = nil
		}
	})
	var c7, c8 string
	for _, e := range spec.ValidateCrossFile().Errors {
		switch e.Check {
		case "C7":
			c7 = e.Message
		case "C8":
			c8 = e.Message
		}
	}
	if !strings.Contains(c7, "add ") || !strings.Contains(c7, "tests of an existing task") {
		t.Errorf("C7 = %q", c7)
	}
	if !strings.Contains(c8, "implement task that builds it") || !strings.Contains(c8, "not the integration task") {
		t.Errorf("C8 = %q", c8)
	}
}

// Warnings never block, and each names the check that raised it, so a
// consumer printing "check: message" never renders a leading ": ".
func TestValidateCrossFile_WarnsWithoutFailing(t *testing.T) {
	for _, tc := range []struct {
		name, warning string
		mutate        func(*Spec)
	}{
		{"vague language", "vague term", func(s *Spec) {
			s.Requirements.Requirements[0].Criteria[0].Action =
				"handle the request appropriately and return a reasonable result"
		}},
		{"a task over the step limit", "steps (limit 12)", func(s *Spec) {
			s.Tasks.Tasks[0].Steps = make([]string, maxStepsPerTask+1)
			for i := range s.Tasks.Tasks[0].Steps {
				s.Tasks.Tasks[0].Steps[i] = "do a thing"
			}
		}},
		// §10.3: the v1 rule made every backticked identifier a validation
		// error and produced most of the repair churn. In v2 it is at most a
		// warning, and only from three uses.
		{"an undefined term used in three criteria", "WidgetRegistry", func(s *Spec) {
			for i := range s.Requirements.Requirements[0].Criteria {
				s.Requirements.Requirements[0].Criteria[i].Action += " using the `WidgetRegistry`"
			}
		}},
		{"an error outcome without a contract", "describes an error outcome but has no contract", func(s *Spec) {
			c := &s.Requirements.Requirements[0].Criteria[2] // the ubiquitous one
			c.Action = "for any malformed input, reject the request as invalid"
			c.Contract = nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := mutateFixture(t, tc.mutate).ValidateCrossFile()
			if !result.Valid {
				t.Fatalf("this must warn, not fail; got %v", result.Errors)
			}
			if !hasWarningContaining(result, tc.warning) {
				t.Errorf("no warning containing %q; got %v", tc.warning, result.Warnings)
			}
			for _, w := range result.Warnings {
				if w.Check == "" {
					t.Errorf("warning without a check: %+v", w)
				}
			}
		})
	}
}

func TestValidateCrossFile_GivesNoGlossaryHintBelowThreeUses(t *testing.T) {
	spec := mutateFixture(t, func(s *Spec) {
		s.Requirements.Requirements[0].Criteria[0].Action += " using the `WidgetRegistry`"
	})
	if hasWarningContaining(spec.ValidateCrossFile(), "WidgetRegistry") {
		t.Error("a term used in one criterion produced a glossary hint")
	}
}

func TestValidateCrossFile_ReportsAMissingArtifactAsIncomplete(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	spec.Tasks = nil
	result := spec.ValidateCrossFile()
	if result.Valid {
		t.Fatal("a spec with no tasks artifact validated")
	}
	if result.Errors[0].Check != "completeness" {
		t.Errorf("check = %q; want completeness", result.Errors[0].Check)
	}
}

func TestValidate_ReportsAScaffoldAsIncompleteNotSchemaInvalid(t *testing.T) {
	spec := CreateSpec("07", "new_feature")
	spec.Title = "New Feature"
	spec.PRDBody = "# New Feature\n\n## Intent\n\nSomething.\n"

	if !spec.IsScaffold() {
		t.Fatal("a freshly created spec is not reported as a scaffold")
	}
	result := spec.Validate()
	if result.Valid {
		t.Fatal("an empty scaffold must not validate")
	}
	if len(result.Errors) != 1 || result.Errors[0].Check != "completeness" {
		t.Fatalf("want a single completeness error, got %v", result.Errors)
	}
	if !strings.Contains(result.Errors[0].Message, "generate its requirements") {
		t.Errorf("the completeness message does not say what to do next: %q", result.Errors[0].Message)
	}
}

// A version 1 spec produces one sentence naming the version and where the
// conversion is described, not a schema error per artifact plus every shape
// difference underneath.
func TestValidate_ReportsALegacySpecAsOneVersionMismatch(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	spec.SchemaVersion = 1

	result := spec.Validate()
	if result.Valid {
		t.Fatal("a version 1 spec validated against the version 2 rules")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("want a single version error, got %d: %v", len(result.Errors), result.Errors)
	}
	if result.Errors[0].Check != "schema_version" {
		t.Errorf("check = %q; want schema_version", result.Errors[0].Check)
	}
	if !strings.Contains(result.Errors[0].Message, "§13") {
		t.Errorf("the message does not say where the conversion is described: %q", result.Errors[0].Message)
	}
}

// A package on disk with a key the schema does not allow still loads, and
// validation reports the key as a schema error.
func TestValidate_ReportsAnUnknownKeyOnDisk(t *testing.T) {
	dir := copyFixture(t, fixtureValidSpec)
	if clean := loadFixture(t, dir).Validate(); slices.Contains(errorChecks(clean), "unknown_field") {
		t.Fatalf("the clean fixture reports an unknown field: %+v", clean.Errors)
	}

	path := filepath.Join(dir, "tasks.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["traceability"] = []any{}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}

	res := loadFixture(t, dir).Validate()
	found := false
	for _, e := range res.Errors {
		if e.Check == "unknown_field" && e.Artifact == "tasks.json" && strings.Contains(e.Message, "/traceability") {
			found = true
		}
	}
	if !found || res.Valid {
		t.Errorf("valid=%v errors=%+v, want an unknown_field error for /traceability", res.Valid, res.Errors)
	}
}

func TestValidateStructured_ShapesEachKindOfEntry(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		out := loadFixture(t, withRender(t, copyFixture(t, fixtureValidSpec))).ValidateStructured()
		if valid, _ := out["valid"].(bool); !valid {
			t.Fatalf("valid = false on the canonical fixture: %v", out["errors"])
		}
		if entries, ok := out["errors"].([]map[string]any); !ok || len(entries) != 0 {
			t.Errorf("errors = %#v; want an empty []map[string]any", out["errors"])
		}
		if _, present := out["warnings"]; present {
			t.Errorf("the warnings key is present with no warnings: %v", out["warnings"])
		}
	})
	t.Run("integrity", func(t *testing.T) {
		out := mutateFixture(t, func(s *Spec) { s.Tasks.Tasks[0].Tests = []string{"TS-01-1"} }).ValidateStructured()
		if valid, _ := out["valid"].(bool); valid {
			t.Fatal("valid = true on a spec with orphaned tests")
		}
		entries := out["errors"].([]map[string]any)
		if len(entries) == 0 {
			t.Fatal("no error entries")
		}
		if entries[0]["category"] != "integrity" || entries[0]["check"] == "" || entries[0]["entity_id"] == nil {
			t.Errorf("integrity entry has the wrong shape: %v", entries[0])
		}
	})
	t.Run("schema", func(t *testing.T) {
		out := mutateFixture(t, func(s *Spec) { s.Requirements.Introduction = "" }).ValidateStructured()
		entries := out["errors"].([]map[string]any)
		if len(entries) == 0 {
			t.Fatal("no error entries")
		}
		e := entries[0]
		if e["category"] != "schema" || e["artifact"] != "requirements.json" || e["path"] == nil {
			t.Errorf("schema entry has the wrong shape: %v", e)
		}
	})
	t.Run("warning", func(t *testing.T) {
		out := mutateFixture(t, func(s *Spec) {
			s.Requirements.Requirements[0].Criteria[0].Action = "return a reasonable result"
		}).ValidateStructured()
		warnings, ok := out["warnings"].([]map[string]any)
		if !ok || len(warnings) == 0 || warnings[0]["category"] != "warning" {
			t.Errorf("warnings = %v; want warning entries", out["warnings"])
		}
	})
}

// ---------------------------------------------------------------------------
// ValidateCrossSpec (§10.4)
// ---------------------------------------------------------------------------

func TestValidateCrossSpec_AcceptsTwoConsistentSpecs(t *testing.T) {
	root := specRoot(t, map[string]string{
		"01_test_feature":  fixtureValidSpec,
		"02_draft_feature": fixtureDraft,
	})
	metas, err := DiscoverSpecs(root)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := BuildDependencyGraph(metas, root)
	if err != nil {
		t.Fatal(err)
	}
	specs := []*Spec{loadFixture(t, metas[0].Dir), loadFixture(t, metas[1].Dir)}
	if result := ValidateCrossSpec(specs, graph); !result.Valid {
		t.Fatalf("cross-spec validation failed on two consistent specs: %v", result.Errors)
	}
}

func TestValidateCrossSpec_ReportsEachBrokenRule(t *testing.T) {
	for _, tc := range []struct {
		name  string
		check string
		build func(t *testing.T) ([]*Spec, *DependencyGraph)
	}{
		{"a dependency outside the root", "cross_spec_unknown_dependency", func(t *testing.T) ([]*Spec, *DependencyGraph) {
			return []*Spec{loadFixture(t, fixtureDraft)}, &DependencyGraph{}
		}},
		{"a dependency with no shared actor", "cross_spec_actor", func(t *testing.T) ([]*Spec, *DependencyGraph) {
			downstream := loadFixture(t, fixtureDraft)
			for i := range downstream.Requirements.ExecutionPaths {
				for j := range downstream.Requirements.ExecutionPaths[i].Steps {
					downstream.Requirements.ExecutionPaths[i].Steps[j].Actor = "unrelated actor"
				}
			}
			graph := &DependencyGraph{Edges: []DependencyEdge{{FromSpec: "02", ToSpec: "01"}}}
			return []*Spec{loadFixture(t, fixtureValidSpec), downstream}, graph
		}},
		{"a cycle", "cross_spec_cycle", func(t *testing.T) ([]*Spec, *DependencyGraph) {
			a, b := loadFixture(t, fixtureValidSpec), loadFixture(t, fixtureValidSpec)
			b.SpecID = "02"
			a.Tasks.Dependencies = []Dependency{{Spec: "02", Reason: "x"}}
			b.Tasks.Dependencies = []Dependency{{Spec: "01", Reason: "y"}}
			graph := &DependencyGraph{Edges: []DependencyEdge{
				{FromSpec: "01", ToSpec: "02"},
				{FromSpec: "02", ToSpec: "01"},
			}}
			return []*Spec{a, b}, graph
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := ValidateCrossSpec(tc.build(t))
			if result.Valid || !hasCheck(result, tc.check) {
				t.Errorf("valid=%v checks=%v; want a %s error", result.Valid, errorChecks(result), tc.check)
			}
		})
	}
}

func TestValidateCrossSpec_WarnsOnAGlossaryConflict(t *testing.T) {
	a := loadFixture(t, fixtureValidSpec)
	b := loadFixture(t, fixtureValidSpec)
	b.SpecID = "02"
	b.Requirements.SpecId = "02"
	b.Requirements.Glossary["spec"] = "Something else entirely."
	b.Tasks.Dependencies = nil

	result := ValidateCrossSpec([]*Spec{a, b}, &DependencyGraph{})
	if !result.Valid {
		t.Fatalf("a glossary conflict must warn, not fail: %v", result.Errors)
	}
	if !hasWarningContaining(result, "defined differently") {
		t.Errorf("no glossary conflict warning: %v", result.Warnings)
	}
}

// renderedFindings returns the errors and the warnings the rendered rule
// raised.
func renderedFindings(result ValidationResult) (errs, warns []ValidationEntry) {
	for _, e := range result.Errors {
		if e.Check == "rendered" {
			errs = append(errs, e)
		}
	}
	for _, w := range result.Warnings {
		if w.Check == "rendered" {
			warns = append(warns, w)
		}
	}
	return errs, warns
}

var everyStatus = []string{"draft", "active", "sealed", "superseded", "archived"}

// A package read from disk with no spec.md is warned, in every status; one
// whose spec.md is its render gets nothing under the rule.
func TestValidate_WarnsAboutAMissingRenderAndPassesACurrentOne(t *testing.T) {
	for _, status := range everyStatus {
		t.Run(status+"/missing", func(t *testing.T) {
			dir := withRender(t, packageIn(t, t.TempDir(), "01_test_feature", status))
			if err := os.Remove(filepath.Join(dir, RenderedFileName)); err != nil {
				t.Fatal(err)
			}
			result := loadFixture(t, dir).Validate()
			errs, warns := renderedFindings(result)
			if len(errs) != 0 || len(warns) != 1 {
				t.Fatalf("rendered errors %v, warnings %v; want one warning", errs, warns)
			}
			w := warns[0]
			if w.Artifact != RenderedFileName || !strings.Contains(w.Message, "spec.md is missing") ||
				!strings.Contains(w.Message, "speca 01_test_feature --render") {
				t.Errorf("warning = %+v; want it on spec.md, saying it is missing and naming the call that renders it", w)
			}
			if !result.Valid {
				t.Errorf("a missing render made the package invalid: %v", result.Errors)
			}
		})
		t.Run(status+"/current", func(t *testing.T) {
			dir := withRender(t, packageIn(t, t.TempDir(), "01_test_feature", status))
			result := loadFixture(t, dir).Validate()
			if errs, warns := renderedFindings(result); len(errs)+len(warns) != 0 {
				t.Errorf("a current render was reported: errors %v, warnings %v", errs, warns)
			}
		})
	}
}

// A render that differs from the package is an error while the package can
// be saved, and a warning once it is history.
func TestValidate_ReportsAStaleRenderByStatus(t *testing.T) {
	for _, status := range everyStatus {
		t.Run(status, func(t *testing.T) {
			dir := withRender(t, packageIn(t, t.TempDir(), "01_test_feature", status))
			path := filepath.Join(dir, RenderedFileName)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(string(data), "\n")
			const changed = 7
			lines[changed-1] = "an edited line"
			if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
				t.Fatal(err)
			}

			result := loadFixture(t, dir).Validate()
			errs, warns := renderedFindings(result)
			editable := status == "draft" || status == "active"
			var found []ValidationEntry
			if editable {
				found = errs
				if len(warns) != 0 || result.Valid {
					t.Errorf("valid %v, rendered warnings %v; want an error and an invalid package", result.Valid, warns)
				}
			} else {
				found = warns
				if len(errs) != 0 || !result.Valid {
					t.Errorf("valid %v, rendered errors %v; want a warning and a valid package", result.Valid, errs)
				}
			}
			if len(found) != 1 {
				t.Fatalf("rendered findings = %v; want exactly one", found)
			}
			if f := found[0]; f.Artifact != RenderedFileName || !strings.Contains(f.Message, fmt.Sprintf("line %d", changed)) ||
				!strings.Contains(f.Message, "speca 01_test_feature --render") {
				t.Errorf("finding = %+v; want it to name line %d and the call that renders it", f, changed)
			}
		})
	}
}

// A package the library did not load is not held to a file on disk, wherever
// its Dir points: a dry run sets Dir on a package it never wrote.
func TestValidate_SaysNothingOfTheRenderOfAPackageNotReadFromDisk(t *testing.T) {
	other := packageIn(t, t.TempDir(), "01_test_feature", "draft")
	if err := os.WriteFile(filepath.Join(other, RenderedFileName), []byte("another package's render\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, dir string }{
		{"a directory that does not exist", filepath.Join(t.TempDir(), "missing")},
		{"an empty directory", t.TempDir()},
		{"a directory holding another package's spec.md", other},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := inMemory(t)
			want := spec.Validate()
			spec.Dir = tc.dir
			got := spec.Validate()
			if errs, warns := renderedFindings(got); len(errs)+len(warns) != 0 {
				t.Errorf("a package held in memory was reported: errors %v, warnings %v", errs, warns)
			}
			if len(got.Errors) != len(want.Errors) || len(got.Warnings) != len(want.Warnings) {
				t.Errorf("the other findings changed: %d errors, %d warnings; want %d and %d",
					len(got.Errors), len(got.Warnings), len(want.Errors), len(want.Warnings))
			}
		})
	}
}

// A spec.md that is there and cannot be read is a difference, with the
// reason, not a missing file.
func TestValidate_ReportsAnUnreadableRenderAsADifference(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a file without read permission is readable as root")
	}
	for _, status := range []string{"draft", "sealed"} {
		t.Run(status, func(t *testing.T) {
			dir := withRender(t, packageIn(t, t.TempDir(), "01_test_feature", status))
			path := filepath.Join(dir, RenderedFileName)
			if err := os.Chmod(path, 0o000); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

			errs, warns := renderedFindings(loadFixture(t, dir).Validate())
			found := warns
			if status == "draft" {
				found = errs
			}
			if len(errs)+len(warns) != 1 || len(found) != 1 {
				t.Fatalf("rendered errors %v, warnings %v; want one %s", errs, warns, map[bool]string{true: "error", false: "warning"}[status == "draft"])
			}
			if m := found[0].Message; !strings.Contains(m, "permission denied") || strings.Contains(m, "missing") {
				t.Errorf("message = %q; want the reason the read failed, and not that the file is missing", m)
			}
		})
	}
}

// inMemory is the valid fixture built in memory, as a generation run builds a
// package, rather than loaded by the library.
func inMemory(t *testing.T) *Spec {
	t.Helper()
	loaded := loadFixture(t, fixtureValidSpec)
	s := CreateSpec(loaded.SpecID, loaded.SpecName)
	s.Title, s.CreatedAt, s.UpdatedAt, s.PRDBody = loaded.Title, loaded.CreatedAt, loaded.UpdatedAt, loaded.PRDBody
	s.Requirements, s.TestSpec, s.Tasks = loaded.Requirements, loaded.TestSpec, loaded.Tasks
	return s
}
