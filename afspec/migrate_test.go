package afspec

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The migration of the format's own worked example gives the checked-in v3
// example, file for file: the conversion is deterministic.
func TestMigrateV2_ConvertsTheWorkedExampleToTheCheckedInOne(t *testing.T) {
	dir := copyFixture(t, fixtureV2Example)
	if err := MigrateV2(dir); err != nil {
		t.Fatalf("MigrateV2 = %v", err)
	}
	if exists(filepath.Join(dir, TestSpecFileName)) {
		t.Error("test_spec.json survived the migration")
	}
	for _, name := range append(append([]string{}, contractFiles...), ProgressFileName) {
		want, err := os.ReadFile(filepath.Join(fixtureV3Example, name))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("the migration wrote no %s: %v", name, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s differs from the checked-in migration:\n%s", name, firstDifference(string(want), string(got)))
		}
	}
	migrated := loadFixture(t, dir)
	if v := migrated.Validate(); !v.Valid {
		t.Errorf("the migrated package does not validate: %v", v.Errors)
	}
	if got, err := os.ReadFile(filepath.Join(dir, RenderedFileName)); err != nil || string(got) != string(migrated.RenderedBytes()) {
		t.Errorf("spec.md is not the render of the migrated package (%v)", err)
	}
	if again := copyFixture(t, dir); MigrateV2(again) == nil {
		t.Error("a migrated package was migrated again")
	}
}

// Each rule of format v3 §13, on a package built to exercise it.
func TestLoadV2_AppliesEachRuleOfTheConversion(t *testing.T) {
	dir := copyFixture(t, fixtureV2Example)
	editJSON(t, filepath.Join(dir, "requirements.json"), func(m map[string]any) {
		reqs := m["requirements"].([]any)
		first := reqs[0].(map[string]any)
		// REQ-1.2 of the example is complex_event; REQ-1.3 names no system.
		first["criteria"].([]any)[2].(map[string]any)["system"] = "another"
		first["x-jira"] = "ABC-1"
	})
	editJSON(t, filepath.Join(dir, "test_spec.json"), func(m map[string]any) {
		tests := m["tests"].([]any)
		// TS-09-1 verifies 09-REQ-1.1 alone; make it verify 09-REQ-2.1 too,
		// and drop TS-09-4 and TS-09-5, the tests of REQ-2, so that REQ-2.1
		// shares TS-09-1 and REQ-2.2 is verified by the smoke test alone.
		tests[0].(map[string]any)["verifies"] = []any{"09-REQ-1.1", "09-REQ-2.1"}
		smoke := tests[len(tests)-1].(map[string]any)
		smoke["verifies"] = []any{"09-PATH-1", "09-REQ-2.2"}
		m["tests"] = []any{tests[0], tests[1], tests[2], smoke}
	})
	editJSON(t, filepath.Join(dir, "tasks.json"), func(m map[string]any) {
		tasks := m["tasks"].([]any)
		tasks[0].(map[string]any)["state"] = "done"
		tasks[0].(map[string]any)["x-estimate"] = 2.0
		tasks[len(tasks)-1].(map[string]any)["state"] = "in_progress"
		for _, task := range tasks {
			task.(map[string]any)["optional"] = true
		}
	})
	prd := filepath.Join(dir, "prd.md")
	data, err := os.ReadFile(prd)
	if err != nil {
		t.Fatal(err)
	}
	withOwner := strings.Replace(string(data), "schema_version: 2\n", "schema_version: 2\nowner: \"someone\"\ntags: [\"a\", \"b\"]\n", 1)
	if err := os.WriteFile(prd, []byte(withOwner), 0o644); err != nil {
		t.Fatal(err)
	}

	spec, err := LoadV2(dir)
	if err != nil {
		t.Fatalf("LoadV2 = %v", err)
	}
	if spec.SchemaVersion != 3 || spec.Profile != "" || spec.Dir != dir {
		t.Errorf("version %d, profile %q, dir %q", spec.SchemaVersion, spec.Profile, spec.Dir)
	}
	req := spec.Requirements.Requirements[0]
	if req.Id != "REQ-1" || req.System != nil || req.Criteria[0].System == nil || *req.Criteria[0].System != "spec binary" {
		t.Errorf("a requirement whose criteria disagree on system had it hoisted: %+v", req)
	}
	if c := req.Criteria[1]; c.Pattern != CriterionPatternEventDriven || c.ConditionText() != "a command finishes and AF_AGENT=1 is set and --quiet was also passed" {
		t.Errorf("complex_event was not folded into the condition: %+v", c)
	}
	if second := spec.Requirements.Requirements[1]; second.System == nil || *second.System != "spec binary" || second.Criteria[0].System != nil {
		t.Errorf("a requirement whose criteria agree on system did not get it: %+v", second)
	}
	if c := req.Criteria[0].Check; c.KindText() != "unit" || len(c.Setup) != 2 || c.PseudocodeText() == "" {
		t.Errorf("the owner's check = %+v", c)
	}
	shared := spec.Requirements.Requirements[1].Criteria[0].Check
	if !shared.IsShared() || *shared.SameAs != "REQ-1.1" {
		t.Errorf("a criterion verified by another's test did not get same_as: %+v", shared)
	}
	bySmoke := spec.Requirements.Requirements[1].Criteria[1].Check
	if !bySmoke.IsShared() || *bySmoke.SameAs != "PATH-1" {
		t.Errorf("a criterion verified by a smoke test alone did not get same_as the path: %+v", bySmoke)
	}
	path := spec.Requirements.ExecutionPaths[0]
	if path.Id != "PATH-1" || len(path.Smoke.RealComponents) == 0 || len(path.Smoke.EndState) == 0 || len(path.Smoke.Setup) == 0 {
		t.Errorf("path = %+v", path)
	}
	if !strings.Contains(spec.PRDBody, "## Glossary\n\n- **agent mode**: The CLI behaviour") {
		t.Errorf("the glossary is not a PRD section:\n%s", spec.PRDBody)
	}
	if hash, err := ComputeIntentHash(spec.PRDBody); err != nil || hash != mustHash(t, string(data)) {
		t.Error("the intent hash changed")
	}
	for _, task := range spec.Tasks.Tasks {
		if task.Criteria[0] == "09-REQ-1" || strings.Contains(task.Criteria[0], "09-") {
			t.Errorf("task %d keeps a prefixed id: %v", task.Id, task.Criteria)
		}
	}
	plan := spec.Plan()
	if n := len(spec.Tasks.Tasks); n != 2 || len(plan) != 3 || plan[2].Kind != TaskKindIntegration {
		t.Errorf("tasks = %d, plan = %d; want the two implement tasks and the derived integration task", n, len(plan))
	}
	if spec.TaskState(1) != TaskStateDone || spec.TaskState(3) != TaskStateInProgress || spec.UpdatedAt == "" {
		t.Errorf("states: 1 %s, 3 %s, updated_at %q", spec.TaskState(1), spec.TaskState(3), spec.UpdatedAt)
	}
	if v := spec.Validate(); !v.Valid {
		t.Errorf("the converted package does not validate: %v", v.Errors)
	}

	out := t.TempDir()
	if err := spec.saveToDisk(out); err != nil {
		t.Fatal(err)
	}
	written := readJSON(t, filepath.Join(out, "requirements.json"))
	if written["requirements"].([]any)[0].(map[string]any)["x-jira"] != "ABC-1" {
		t.Error("the requirement's extension key did not follow it to its new id")
	}
	if readJSON(t, filepath.Join(out, "tasks.json"))["tasks"].([]any)[0].(map[string]any)["x-estimate"] != 2.0 {
		t.Error("the task's extension key was lost")
	}
	fm := readFrontmatter(t, out)
	if fm["x-owner"] != "someone" || len(fm["x-tags"].([]any)) != 2 || fm["owner"] != nil {
		t.Errorf("owner and tags were not moved to extension keys: %v", fm)
	}
	if raw, _ := os.ReadFile(filepath.Join(out, "tasks.json")); strings.Contains(string(raw), "optional") {
		t.Error("optional was written")
	}
}

func mustHash(t *testing.T, prd string) string {
	t.Helper()
	_, body, _ := strings.Cut(prd[4:], "\n---\n")
	hash, err := ComputeIntentHash(body)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// A criterion no test verifies, a path no smoke test verifies and a package
// of another version have no conversion.
func TestLoadV2_RefusesWhatItCannotConvert(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T, dir string)
		want    string
	}{
		{"a criterion no test verifies", func(t *testing.T, dir string) {
			editJSON(t, filepath.Join(dir, "test_spec.json"), func(m map[string]any) {
				tests := m["tests"].([]any)
				tests[0].(map[string]any)["verifies"] = []any{"09-REQ-1.2"}
				tests[len(tests)-1].(map[string]any)["verifies"] = []any{"09-PATH-1"}
			})
		}, "09-REQ-1.1 is verified by no test"},
		{"a path no smoke test verifies", func(t *testing.T, dir string) {
			editJSON(t, filepath.Join(dir, "test_spec.json"), func(m map[string]any) {
				tests := m["tests"].([]any)
				m["tests"] = tests[:len(tests)-1]
			})
		}, "09-PATH-1 is verified by no smoke test"},
		{"a version 3 package", func(t *testing.T, dir string) {
			copyDir(t, fixtureV3Example, dir)
		}, "schema_version 3"},
		{"a version 1 package", func(t *testing.T, dir string) {
			prd := filepath.Join(dir, "prd.md")
			data, _ := os.ReadFile(prd)
			if err := os.WriteFile(prd, []byte(strings.Replace(string(data), "schema_version: 2", "schema_version: 1", 1)), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "schema_version 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := copyFixture(t, fixtureV2Example)
			tc.prepare(t, dir)
			before := treeOf(t, dir)
			spec, err := LoadV2(dir)
			if spec != nil || err == nil || !errors.As(err, new(*LoadError)) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadV2 = %v, %v; want a *LoadError saying %q", spec, err, tc.want)
			}
			if err := MigrateV2(dir); err == nil {
				t.Error("MigrateV2 converted it")
			}
			if !sameTree(before, treeOf(t, dir)) {
				t.Error("a refused migration changed the package")
			}
		})
	}
}

// A sealed package is migrated as a draft is: the migration is a format
// move, not an edit, and the status and the intent hash stay.
func TestMigrateV2_WritesAPackageInAnyStatus(t *testing.T) {
	dir := copyFixture(t, fixtureV2Example)
	prd := filepath.Join(dir, "prd.md")
	data, _ := os.ReadFile(prd)
	sealed := strings.Replace(string(data), `status: "draft"`, `status: "sealed"`, 1)
	sealed = strings.Replace(sealed, "intent_hash: null", `intent_hash: "abc"`, 1)
	if err := os.WriteFile(prd, []byte(sealed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MigrateV2(dir); err != nil {
		t.Fatalf("MigrateV2 = %v", err)
	}
	spec := loadFixture(t, dir)
	if spec.Status != "sealed" || spec.IntentHash == nil || *spec.IntentHash != "abc" {
		t.Errorf("status %s, hash %v", spec.Status, spec.IntentHash)
	}
	var decoded map[string]any
	raw, _ := os.ReadFile(filepath.Join(dir, ProgressFileName))
	if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded["tasks"].([]any)) != len(spec.Plan()) {
		t.Errorf("progress = %v (%v); want one entry per task", decoded, err)
	}
	if !reflect.DeepEqual(spec.Requirements, mustLoadV2(t, fixtureV2Example).Requirements) {
		t.Error("the requirements written differ from the conversion in memory")
	}
}

func mustLoadV2(t *testing.T, dir string) *Spec {
	t.Helper()
	spec, err := LoadV2(dir)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}
