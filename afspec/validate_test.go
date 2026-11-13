package afspec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The checked-in fixtures satisfy schema validation and every rule C1 to C8.
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

// The proofs of format v2 that moved into the schema: a criterion without a
// check, a path without a smoke check, and a check that mixes its two forms
// cannot be written at all.
func TestValidateSchema_RequiresACheckAndASmokeCheck(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Spec)
		path   string
	}{
		{"a criterion with no check", func(s *Spec) { s.Requirements.Requirements[0].Criteria[0].Check = Check{} }, "/requirements/0/criteria/0/check"},
		{"a check with both forms", func(s *Spec) {
			c := &s.Requirements.Requirements[0].Criteria[0]
			c.Check.SameAs = strPtr("REQ-1.2")
		}, "/requirements/0/criteria/0/check"},
		{"a path with no real components", func(s *Spec) { s.Requirements.ExecutionPaths[0].Smoke.RealComponents = nil }, "/execution_paths/0/smoke"},
		{"a path with no end state", func(s *Spec) { s.Requirements.ExecutionPaths[0].Smoke.EndState = []string{} }, "/execution_paths/0/smoke/end_state"},
		{"a complex_event criterion", func(s *Spec) { s.Requirements.Requirements[0].Criteria[0].Pattern = "complex_event" }, "/requirements/0/criteria/0/pattern"},
		{"a task with tests", func(s *Spec) { s.Tasks.Tasks[0].Criteria = nil }, "/tasks/0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := mutateFixture(t, tc.mutate).ValidateSchema()
			if result.Valid {
				t.Fatal("the schema accepted the package")
			}
			found := false
			for _, e := range result.Errors {
				if e.Check == "json_schema" && strings.HasPrefix(e.Path, tc.path) {
					found = true
				}
			}
			if !found {
				t.Errorf("no json_schema error under %s; got %+v", tc.path, result.Errors)
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
		{name: "progress disagrees on spec_id", check: "C1",
			mutate: func(s *Spec) { s.Progress.SpecId = "99" }},
		{name: "folder name disagrees with spec_id", check: "C1",
			mutate: func(s *Spec) { s.Dir = "/tmp/07_test_feature" }},

		{name: "criterion ID in the wrong format", check: "C2",
			mutate: func(s *Spec) { s.Requirements.Requirements[0].Criteria[0].Id = "REQ-1.E1" }},
		{name: "requirement ID carries a spec prefix", check: "C2",
			mutate: func(s *Spec) { s.Requirements.Requirements[0].Id = "01-REQ-1" }},
		{name: "duplicate task ID", check: "C2",
			mutate: func(s *Spec) { s.Tasks.Tasks[1].Id = s.Tasks.Tasks[0].Id }},
		{name: "criterion listed under the wrong requirement", check: "C2",
			mutate: func(s *Spec) { s.Requirements.Requirements[1].Criteria[0].Id = "REQ-1.9" }},
		{name: "duplicate path ID", check: "C2",
			mutate: func(s *Spec) {
				s.Requirements.ExecutionPaths = append(s.Requirements.ExecutionPaths, s.Requirements.ExecutionPaths[0])
			}},

		{name: "same_as names an unknown criterion", check: "C3", entity: "REQ-2.1", message: "REQ-9.9",
			mutate: func(s *Spec) { s.Requirements.Requirements[1].Criteria[0].Check = SameAs("REQ-9.9") }},
		{name: "same_as names the criterion itself", check: "C3", entity: "REQ-2.1",
			mutate: func(s *Spec) { s.Requirements.Requirements[1].Criteria[0].Check = SameAs("REQ-2.1") }},
		{name: "same_as chains through a shared check", check: "C3", entity: "REQ-1.3", message: "itself shared",
			mutate: func(s *Spec) {
				s.Requirements.Requirements[0].Criteria[1].Check = SameAs("REQ-1.1")
				s.Requirements.Requirements[0].Criteria[2].Check = SameAs("REQ-1.2")
			}},

		{name: "task names an unknown criterion", check: "C4",
			mutate: func(s *Spec) { s.Tasks.Tasks[0].Criteria = []string{"REQ-8"} }},
		{name: "depends_on names a missing task", check: "C4",
			mutate: func(s *Spec) { s.Tasks.Tasks[1].DependsOn = []int{42} }},
		{name: "depends_on names a later task", check: "C4", message: "lower task IDs",
			mutate: func(s *Spec) { s.Tasks.Tasks[0].DependsOn = []int{2} }},

		{name: "criterion claimed by no task", check: "C5", entity: "REQ-2.1",
			mutate: func(s *Spec) { s.Tasks.Tasks[1].Criteria = []string{"REQ-1.1"} }},

		// REQ-1.2 is the unwanted criterion.
		{name: "unwanted criterion without a contract", check: "C6", entity: "REQ-1.2",
			mutate: func(s *Spec) { s.Requirements.Requirements[0].Criteria[1].Contract = nil }},

		{name: "a full package without a path", check: "C7", message: "profile: lite",
			mutate: func(s *Spec) { s.Requirements.ExecutionPaths = nil }},
		{name: "a lite package with a path", check: "C7", message: "lite",
			mutate: func(s *Spec) { s.Profile = ProfileLite }},
		{name: "a lite package with four requirements", check: "C7", message: "at most 3",
			mutate: func(s *Spec) {
				s.Profile = ProfileLite
				s.Requirements.ExecutionPaths = nil
				for i := 3; i <= 5; i++ {
					s.Requirements.Requirements = append(s.Requirements.Requirements, Requirement{
						Id: fmt.Sprintf("REQ-%d", i), Title: "more",
						Criteria: []Criterion{UbiquitousCriterion(fmt.Sprintf("REQ-%d.1", i), "", "do").WithCheck(CheckKindUnit, nil, "")},
					})
					s.Tasks.Tasks[0].Criteria = append(s.Tasks.Tasks[0].Criteria, fmt.Sprintf("REQ-%d", i))
				}
			}},

		{name: "a progress entry for a task that is not in the plan", check: "C8", entity: "9",
			mutate: func(s *Spec) { s.Progress.Tasks = append(s.Progress.Tasks, TaskProgress{Id: 9, State: TaskStateDone}) }},
		{name: "two progress entries for one task", check: "C8", entity: "1", message: "twice",
			mutate: func(s *Spec) { s.Progress.Tasks = append(s.Progress.Tasks, TaskProgress{Id: 1, State: TaskStateDone}) }},
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
		s.Tasks.Tasks[0].Criteria = []string{"REQ-1"}
		s.Tasks.Tasks[1].Criteria = []string{"REQ-2"}
	})
	if result := spec.ValidateCrossFile(); !result.Valid {
		t.Fatalf("claiming a requirement wholesale should cover its criteria; got %v", result.Errors)
	}
}

// A shared check may name a criterion with its own check, or a path; the
// integration task's progress entry is a task of the package.
func TestValidateCrossFile_AcceptsSharedChecksAndTheIntegrationEntry(t *testing.T) {
	spec := mutateFixture(t, func(s *Spec) {
		s.Requirements.Requirements[0].Criteria[1].Check = SameAs("REQ-1.1")
		s.Requirements.Requirements[1].Criteria[0].Check = SameAs("PATH-1")
		s.Progress.Tasks = []TaskProgress{{Id: 3, State: TaskStateDone}}
	})
	if result := spec.ValidateCrossFile(); !result.Valid {
		t.Fatalf("got %v", result.Errors)
	}
}

// A lite package of three requirements and no path validates, and has no
// integration task; a package with no progress file validates too.
func TestValidateCrossFile_AcceptsALitePackageAndAPackageWithoutProgress(t *testing.T) {
	lite := mutateFixture(t, func(s *Spec) {
		s.Profile = ProfileLite
		s.Requirements.ExecutionPaths = nil
		s.Progress = nil
	})
	if result := lite.Validate(); !result.Valid {
		t.Fatalf("a lite package does not validate: %v", result.Errors)
	}
	if _, ok := lite.IntegrationTaskID(); ok {
		t.Error("a lite package has an integration task")
	}
	if len(lite.Plan()) != 2 {
		t.Errorf("plan = %d tasks; want the two of tasks.json", len(lite.Plan()))
	}
}

// The ownership error says how to fix it, not just what is wrong.
func TestValidateCrossFile_OwnershipErrorCarriesARemedy(t *testing.T) {
	spec := mutateFixture(t, func(s *Spec) {
		for i := range s.Tasks.Tasks {
			s.Tasks.Tasks[i].Criteria = []string{"REQ-1.1"}
		}
	})
	var c5 string
	for _, e := range spec.ValidateCrossFile().Errors {
		if e.Check == "C5" {
			c5 = e.Message
		}
	}
	if !strings.Contains(c5, "add ") || !strings.Contains(c5, "task that builds it") {
		t.Errorf("C5 = %q", c5)
	}
}

// The validator matches no words (§0, rule 3): a criterion worded vaguely is
// for the generator's hint and the review rubrics, not a warning of the
// package's verdict.
func TestValidateCrossFile_DoesNotMatchVagueWords(t *testing.T) {
	result := mutateFixture(t, func(s *Spec) {
		s.Requirements.Requirements[0].Criteria[0].Action =
			"handle the request appropriately and return a reasonable result, etc"
	}).ValidateCrossFile()
	for _, w := range result.Warnings {
		if w.Check == "vague_language" || strings.Contains(w.Message, "vague") {
			t.Errorf("the validator warned on wording: %+v", w)
		}
	}
}

// Warnings never block, and each names the check that raised it, so a
// consumer printing "check: message" never renders a leading ": ".
func TestValidateCrossFile_WarnsWithoutFailing(t *testing.T) {
	for _, tc := range []struct {
		name, warning string
		mutate        func(*Spec)
	}{
		{"a task over the step limit", "steps (limit 12)", func(s *Spec) {
			s.Tasks.Tasks[0].Steps = make([]string, maxStepsPerTask+1)
			for i := range s.Tasks.Tasks[0].Steps {
				s.Tasks.Tasks[0].Steps[i] = "do a thing"
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

// The glossary is a PRD section now: a backticked term in the criteria is
// nobody's hint.
func TestValidateCrossFile_GivesNoGlossaryHint(t *testing.T) {
	spec := mutateFixture(t, func(s *Spec) {
		for i := range s.Requirements.Requirements[0].Criteria {
			s.Requirements.Requirements[0].Criteria[i].Action += " using the `WidgetRegistry`"
		}
	})
	if hasWarningContaining(spec.ValidateCrossFile(), "WidgetRegistry") {
		t.Error("a backticked term produced a glossary hint")
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

// A spec of another version produces one sentence naming the version and the
// way to convert it, not a schema error per artifact plus every shape
// difference underneath.
func TestValidate_ReportsALegacySpecAsOneVersionMismatch(t *testing.T) {
	for _, tc := range []struct {
		version int
		want    string
	}{
		{2, "speca <package> --migrate"},
		{1, "no conversion"},
	} {
		t.Run(fmt.Sprint(tc.version), func(t *testing.T) {
			spec := loadFixture(t, fixtureValidSpec)
			spec.SchemaVersion = tc.version

			result := spec.Validate()
			if result.Valid {
				t.Fatalf("a version %d spec validated against the version 3 rules", tc.version)
			}
			if len(result.Errors) != 1 {
				t.Fatalf("want a single version error, got %d: %v", len(result.Errors), result.Errors)
			}
			if result.Errors[0].Check != "schema_version" {
				t.Errorf("check = %q; want schema_version", result.Errors[0].Check)
			}
			if m := result.Errors[0].Message; !strings.Contains(m, "§13") || !strings.Contains(m, tc.want) {
				t.Errorf("the message does not say where the conversion is described and how it is made: %q", m)
			}
		})
	}
	// A version 2 package on disk loads, and is reported the same way.
	result := loadFixture(t, fixtureV2Example).Validate()
	if result.Valid || len(result.Errors) != 1 || result.Errors[0].Check != "schema_version" {
		t.Errorf("a v2 package on disk: valid %v, errors %v", result.Valid, result.Errors)
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
		out := mutateFixture(t, func(s *Spec) { s.Tasks.Tasks[1].Criteria = []string{"REQ-1.1"} }).ValidateStructured()
		if valid, _ := out["valid"].(bool); valid {
			t.Fatal("valid = true on a spec with an unowned criterion")
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
		out := mutateFixture(t, func(s *Spec) { s.Requirements.Requirements[0].Title = "" }).ValidateStructured()
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
			c := &s.Requirements.Requirements[0].Criteria[2]
			c.Action = "for any malformed input, reject the request as invalid"
			c.Contract = nil
		}).ValidateStructured()
		warnings, ok := out["warnings"].([]map[string]any)
		if !ok || len(warnings) == 0 || warnings[0]["category"] != "warning" {
			t.Errorf("warnings = %v; want warning entries", out["warnings"])
		}
	})
}

// ---------------------------------------------------------------------------
// ValidateCrossSpec (§10.3)
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

// The actor rule holds only between two packages that have paths: a lite
// package, on either side of the edge, is held to nothing.
func TestValidateCrossSpec_SkipsTheActorRuleForALitePackage(t *testing.T) {
	graph := &DependencyGraph{Edges: []DependencyEdge{{FromSpec: "02", ToSpec: "01"}}}
	t.Run("lite downstream", func(t *testing.T) {
		downstream := loadFixture(t, fixtureDraft)
		downstream.Profile, downstream.Requirements.ExecutionPaths = ProfileLite, nil
		if result := ValidateCrossSpec([]*Spec{loadFixture(t, fixtureValidSpec), downstream}, graph); !result.Valid {
			t.Errorf("got %v", result.Errors)
		}
	})
	t.Run("lite upstream", func(t *testing.T) {
		upstream := loadFixture(t, fixtureValidSpec)
		upstream.Profile, upstream.Requirements.ExecutionPaths = ProfileLite, nil
		downstream := loadFixture(t, fixtureDraft)
		for i := range downstream.Requirements.ExecutionPaths {
			for j := range downstream.Requirements.ExecutionPaths[i].Steps {
				downstream.Requirements.ExecutionPaths[i].Steps[j].Actor = "unrelated actor"
			}
		}
		if result := ValidateCrossSpec([]*Spec{upstream, downstream}, graph); !result.Valid {
			t.Errorf("got %v", result.Errors)
		}
	})
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
	s.Requirements, s.Tasks, s.Progress = loaded.Requirements, loaded.Tasks, loaded.Progress
	return s
}

// A steering_version outside MAJOR.MINOR.PATCH is a schema error on prd.md; an
// empty one is the field being absent.
func TestValidate_RefusesASteeringVersionOutsideTheVersionForm(t *testing.T) {
	for _, value := range []string{"2.1", "2.1.0.0", "02.1.0", "v2.1.0", "2.1.x", "2.1.0-rc1"} {
		t.Run(value, func(t *testing.T) {
			dir := copyFixture(t, fixtureValidSpec)
			withFrontmatterLine(t, dir, `steering_version: "`+value+`"`)
			v := loadFixture(t, dir).Validate()
			var found []ValidationEntry
			for _, e := range v.Errors {
				if e.Check == "json_schema" && e.Artifact == "prd.md" && strings.Contains(e.Path, "steering_version") {
					found = append(found, e)
				}
			}
			if len(found) != 1 || v.Valid {
				t.Errorf("valid %v, errors %+v; want one json_schema error on prd.md naming steering_version", v.Valid, v.Errors)
			}
		})
	}
	t.Run("empty", func(t *testing.T) {
		dir := copyFixture(t, fixtureValidSpec)
		withFrontmatterLine(t, dir, `steering_version: ""`)
		spec := loadFixture(t, dir)
		if v := spec.Validate(); !v.Valid {
			t.Errorf("an empty steering_version made the package invalid: %v", v.Errors)
		}
		if out := string(renderPRD(spec)); strings.Contains(out, "steering_version") {
			t.Errorf("an empty steering_version was written back:\n%s", out)
		}
	})
}

// A profile outside full and lite is a schema error on prd.md.
func TestValidate_RefusesAnUnknownProfile(t *testing.T) {
	dir := copyFixture(t, fixtureValidSpec)
	withFrontmatterLine(t, dir, `profile: "medium"`)
	v := loadFixture(t, dir).Validate()
	found := false
	for _, e := range v.Errors {
		if e.Check == "json_schema" && e.Artifact == "prd.md" && strings.Contains(e.Path, "profile") {
			found = true
		}
	}
	if !found || v.Valid {
		t.Errorf("valid %v, errors %+v; want a json_schema error on prd.md naming profile", v.Valid, v.Errors)
	}
}

// An evidence command holds the placeholder {tests} exactly once, as an
// argument of its own once the command is split as the gate splits it; an
// empty one is a schema error.
func TestValidate_ChecksThePlaceholderOfTheEvidenceCommand(t *testing.T) {
	for name, tc := range map[string]struct {
		command string
		check   string
	}{
		"an argument of its own": {"run-tests {tests}", ""},
		"quoted, still its own":  {`run-tests "{tests}"`, ""},
		"none":                   {"run-tests", "evidence_tests_placeholder"},
		"twice":                  {"run-tests {tests} {tests}", "evidence_tests_placeholder"},
		"inside an argument":     {"run-tests --only={tests}", "evidence_tests_placeholder"},
		"an open quote":          {`run-tests "{tests}`, "evidence_tests_placeholder"},
		"empty":                  {"", "json_schema"},
	} {
		t.Run(name, func(t *testing.T) {
			s := loadFixture(t, fixtureValidSpec)
			s.Tasks.TestCommands.EvidenceTests = &tc.command
			v := s.Validate()
			if tc.check == "" {
				if !v.Valid {
					t.Fatalf("errors %v; want valid", v.Errors)
				}
				return
			}
			if v.Valid || len(v.Errors) != 1 || v.Errors[0].Check != tc.check {
				t.Fatalf("errors %v; want exactly one %s", v.Errors, tc.check)
			}
			e := v.Errors[0]
			if tc.check == "evidence_tests_placeholder" &&
				(e.Artifact != TasksFileName || e.EntityID != "test_commands.evidence_tests" ||
					!strings.Contains(e.Message, strconv.Quote(tc.command)) || !strings.Contains(e.Message, "{tests}")) {
				t.Errorf("error %+v; want it on tasks.json, test_commands.evidence_tests, naming the command and {tests}", e)
			}
		})
	}
}

// An affected-tests command holds the placeholder {base} exactly once; an
// empty one is a schema error.
func TestValidate_ChecksTheBaseOfTheAffectedTestsCommand(t *testing.T) {
	for name, tc := range map[string]struct {
		command string
		check   string
		found   string
	}{
		"none":        {"make test-affected", "affected_tests_base", "0"},
		"twice":       {"x {base} {base}", "affected_tests_base", "2"},
		"upper case":  {"x {BASE}", "affected_tests_base", "0"},
		"exactly one": {"{base}", "", ""},
		"in a word":   {"make test-affected BASE={base}", "", ""},
		"empty":       {"", "json_schema", ""},
	} {
		t.Run(name, func(t *testing.T) {
			s := loadFixture(t, fixtureValidSpec)
			s.Tasks.TestCommands.AffectedTests = &tc.command
			v := s.Validate()
			if tc.check == "" {
				if !v.Valid {
					t.Fatalf("errors %v; want valid", v.Errors)
				}
				return
			}
			if v.Valid {
				t.Fatal("valid; want an error")
			}
			var e *ValidationEntry
			for i := range v.Errors {
				if v.Errors[i].Check == tc.check {
					e = &v.Errors[i]
				}
			}
			if e == nil || e.Artifact != TasksFileName {
				t.Fatalf("errors %v; want one %s on %s", v.Errors, tc.check, TasksFileName)
			}
			if tc.found != "" && (!strings.Contains(e.Message, "found "+tc.found) || !strings.Contains(e.Message, "exactly one")) {
				t.Errorf("message %q; want it to say it found %s and that exactly one is needed", e.Message, tc.found)
			}
		})
	}
}

// A save keeps the affected-tests command, after the other test commands.
func TestSave_KeepsTheAffectedTestsCommand(t *testing.T) {
	dir := copyFixture(t, fixtureValidSpec)
	s := loadFixture(t, dir)
	spec, affected := "go test ./afspec", "make test-affected BASE={base}"
	s.Tasks.TestCommands.SpecTests, s.Tasks.TestCommands.AffectedTests = &spec, &affected
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	again := loadFixture(t, dir)
	if got := again.Tasks.TestCommands.AffectedTests; got == nil || *got != affected {
		t.Errorf("affected_tests after a round trip = %v; want %q", got, affected)
	}
	b, _ := os.ReadFile(filepath.Join(dir, TasksFileName))
	at := func(k string) int { return strings.Index(string(b), `"`+k+`"`) }
	if !(at("all_tests") < at("linter") && at("linter") < at("spec_tests") && at("spec_tests") < at("affected_tests")) {
		t.Errorf("the test commands are out of order:\n%s", b)
	}
}

// handoffFixture is the v3 example with one handoff and one task taking one.
func handoffFixture(t *testing.T, dir string) *Spec {
	t.Helper()
	s := loadFixture(t, dir)
	s.Requirements.Handoffs = []Handoff{{Id: "H-1", What: "after-landing hooks of internal/land", To: "02", Criteria: []string{"REQ-1.1"}}}
	s.Tasks.Tasks[0].TakesHandoffs = []string{"01:H-1"}
	return s
}

// A package may declare handoffs and list the handoffs a task takes; a save
// keeps both, and a package with neither saves as it did.
func TestSave_KeepsTheHandoffs(t *testing.T) {
	plain := copyFixture(t, "testdata/v3_example")
	before := map[string][]byte{}
	for _, f := range []string{RequirementsFileName, TasksFileName} {
		before[f], _ = os.ReadFile(filepath.Join(plain, f))
	}
	if err := loadFixture(t, plain).Save(plain); err != nil {
		t.Fatal(err)
	}
	for f, b := range before {
		if after, _ := os.ReadFile(filepath.Join(plain, f)); !bytes.Equal(after, b) {
			t.Errorf("%s changed on a save of a package without handoffs", f)
		}
	}

	dir := copyFixture(t, "testdata/v3_example")
	s := handoffFixture(t, dir)
	if v := s.ValidateSchema(); !v.Valid {
		t.Fatalf("schema errors %v", v.Errors)
	}
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	again := loadFixture(t, dir)
	if !reflect.DeepEqual(again.Requirements.Handoffs, s.Requirements.Handoffs) || !slices.Equal(again.Tasks.Tasks[0].TakesHandoffs, []string{"01:H-1"}) {
		t.Errorf("after a round trip: handoffs %+v, takes %v", again.Requirements.Handoffs, again.Tasks.Tasks[0].TakesHandoffs)
	}
}

// A malformed handoff or takes_handoffs entry makes the package invalid: a
// value the schema refuses, a duplicate id (C2), a criterion it does not have
// (C4).
func TestValidate_RefusesAMalformedHandoff(t *testing.T) {
	for name, tc := range map[string]struct {
		edit  func(s *Spec)
		check string
		names []string
	}{
		"an id without its hyphen": {func(s *Spec) { s.Requirements.Handoffs[0].Id = "H1" }, "json_schema", nil},
		"an id twice": {func(s *Spec) {
			s.Requirements.Handoffs = append(s.Requirements.Handoffs, s.Requirements.Handoffs[0])
		}, "C2", []string{"H-1"}},
		"an empty what":          {func(s *Spec) { s.Requirements.Handoffs[0].What = "" }, "json_schema", nil},
		"no criteria":            {func(s *Spec) { s.Requirements.Handoffs[0].Criteria = []string{} }, "json_schema", nil},
		"an unknown criterion":   {func(s *Spec) { s.Requirements.Handoffs[0].Criteria = []string{"REQ-9.9"} }, "C4", []string{"H-1", "REQ-9.9"}},
		"a requirement id":       {func(s *Spec) { s.Requirements.Handoffs[0].Criteria = []string{"REQ-1"} }, "json_schema", nil},
		"a take with no package": {func(s *Spec) { s.Tasks.Tasks[0].TakesHandoffs = []string{"H-1"} }, "json_schema", nil},
		"a take twice":           {func(s *Spec) { s.Tasks.Tasks[0].TakesHandoffs = []string{"01:H-1", "01:H-1"} }, "json_schema", nil},
	} {
		t.Run(name, func(t *testing.T) {
			s := handoffFixture(t, "testdata/v3_example")
			if v := s.Validate(); !v.Valid {
				t.Fatalf("the fixture is invalid before the edit: %v", v.Errors)
			}
			tc.edit(s)
			v := s.Validate()
			if v.Valid {
				t.Fatal("valid; want an error")
			}
			var found bool
			for _, e := range v.Errors {
				if e.Check != tc.check {
					continue
				}
				found = true
				for _, n := range tc.names {
					if !strings.Contains(e.Message, n) {
						t.Errorf("%s message %q does not name %s", tc.check, e.Message, n)
					}
				}
			}
			if !found {
				t.Errorf("errors %v; want one of %s", v.Errors, tc.check)
			}
		})
	}
}
