package afspec

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadSpec_PopulatesEveryArtifact(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)

	if spec.SpecID != "01" || spec.SpecName != "test_feature" {
		t.Errorf("identity = %q/%q; want 01/test_feature", spec.SpecID, spec.SpecName)
	}
	if spec.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version = %d; want %d", spec.SchemaVersion, SchemaVersion)
	}
	if spec.Requirements == nil || spec.TestSpec == nil || spec.Tasks == nil {
		t.Fatal("one of the three JSON artifacts is nil")
	}
	if got := len(spec.Requirements.Requirements); got != 2 {
		t.Errorf("requirements = %d; want 2", got)
	}
	if got := len(spec.TestSpec.Tests); got != 5 {
		t.Errorf("tests = %d; want 5", got)
	}
	if got := len(spec.Tasks.Tasks); got != 3 {
		t.Errorf("tasks = %d; want 3", got)
	}
	if spec.Dir != fixtureValidSpec {
		t.Errorf("Dir = %q; want %q", spec.Dir, fixtureValidSpec)
	}
}

func TestLoadSpec_ReadsArchitectureOnlyWhenPresent(t *testing.T) {
	if got := loadFixture(t, fixtureValidSpec).Architecture; got != "" {
		t.Errorf("architecture = %q; want empty for a fixture with no architecture.md", got)
	}
	if loadFixture(t, fixtureWithArch).Architecture == "" {
		t.Error("architecture.md was present but Architecture is empty")
	}
}

func TestLoadSpec_RefusesABrokenPackageWithALoadError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, dir string }{
		{"missing artifact", "testdata/missing_req"},
		{"malformed JSON", "testdata/malformed_json"},
		{"malformed YAML frontmatter", "testdata/malformed_yaml"},
		{"directory does not exist", "testdata/does_not_exist"},
		{"a regular file", file},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := LoadSpec(tc.dir)
			if err == nil {
				t.Fatalf("LoadSpec(%q) = nil error; want a failure", tc.dir)
			}
			if spec != nil {
				t.Error("a failed load returned a non-nil Spec")
			}
			var loadErr *LoadError
			if !errors.As(err, &loadErr) {
				t.Errorf("error is %T; want it to unwrap to *LoadError", err)
			}
		})
	}
}

// The round-trip fidelity contract: a fixture loaded and saved without
// modification produces the same bytes. It covers the PRD renderer, the
// deterministic marshaller and the schema field ordering at once.
func TestSave_RoundTripIsByteIdentical(t *testing.T) {
	for _, fixture := range validFixtures {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			spec := loadFixture(t, fixture)
			dst := t.TempDir()
			if err := spec.Save(dst); err != nil {
				t.Fatalf("Save = %v", err)
			}

			names := []string{"prd.md", "requirements.json", "test_spec.json", "tasks.json"}
			if spec.Architecture != "" {
				names = append(names, "architecture.md")
			}
			for _, name := range names {
				want, err := os.ReadFile(filepath.Join(fixture, name))
				if err != nil {
					t.Fatalf("ReadFile(%s) = %v", name, err)
				}
				got, err := os.ReadFile(filepath.Join(dst, name))
				if err != nil {
					t.Fatalf("ReadFile(saved %s) = %v", name, err)
				}
				if string(got) != string(want) {
					t.Errorf("%s is not byte-identical after a save round trip:\n%s",
						name, firstDifference(string(want), string(got)))
				}
			}
		})
	}
}

func TestSave_LeavesNoTempFiles(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	dst := t.TempDir()
	if err := spec.Save(dst); err != nil {
		t.Fatalf("Save = %v", err)
	}
	for _, name := range entriesOf(t, dst) {
		if strings.Contains(name, ".tmp.") {
			t.Errorf("temp file %q survived a successful save", name)
		}
	}
}

// Format v2 §7.1 and §8.5: coverage and traceability are computed, so nothing
// resembling them may reach disk.
func TestSave_StoresNoDerivedData(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	dst := t.TempDir()
	if err := spec.Save(dst); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"test_spec.json", "tasks.json"} {
		data, err := os.ReadFile(filepath.Join(dst, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{`"coverage"`, `"traceability"`} {
			if strings.Contains(string(data), forbidden) {
				t.Errorf("%s contains %s; derived data must never be stored", name, forbidden)
			}
		}
	}
}

func TestSave_RefusesASpecInATerminalState(t *testing.T) {
	for _, status := range []string{"sealed", "superseded", "archived"} {
		t.Run(status, func(t *testing.T) {
			spec := loadFixture(t, fixtureValidSpec)
			spec.Status = status
			err := spec.Save(t.TempDir())
			var lifecycleErr *LifecycleError
			if !errors.As(err, &lifecycleErr) {
				t.Errorf("Save on a %s spec = %v; want a *LifecycleError", status, err)
			}
		})
	}
}

func TestSave_RefusesAChangedIdentityOnAnActiveSpec(t *testing.T) {
	for _, tc := range []struct {
		field  string
		mutate func(*Spec)
	}{
		{"spec_id", func(s *Spec) { s.SpecID = "02" }},
		{"spec_name", func(s *Spec) { s.SpecName = "renamed" }},
		{"created_at", func(s *Spec) { s.CreatedAt = "2030-01-01T00:00:00Z" }},
	} {
		t.Run(tc.field, func(t *testing.T) {
			dir := activeCopy(t)
			spec := loadFixture(t, dir)
			tc.mutate(spec)
			err := spec.Save(dir)
			var lifecycleErr *LifecycleError
			if !errors.As(err, &lifecycleErr) || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("Save with a changed %s = %v; want a *LifecycleError naming it", tc.field, err)
			}
		})
	}
}

func TestSave_RefusesIntentDriftOnAnActiveSpec(t *testing.T) {
	dir := activeCopy(t)
	drifted := loadFixture(t, dir)
	drifted.PRDBody = "# Test Feature\n\n## Intent\n\nSomething else entirely.\n"
	err := drifted.Save(dir)
	var intentErr *IntentError
	if !errors.As(err, &intentErr) {
		t.Errorf("Save with an edited Intent section = %v; want an *IntentError", err)
	}
}

// A scaffold must load so that a generation run can fill it in.
func TestSave_KeepsAScaffoldLoadable(t *testing.T) {
	dir := t.TempDir()
	spec := CreateSpec("07", "new_feature")
	spec.Title = "New Feature"
	spec.Status = "draft"
	spec.CreatedAt = "2026-01-01T00:00:00Z"
	spec.UpdatedAt = "2026-01-01T00:00:00Z"
	spec.PRDBody = "# New Feature\n\n## Intent\n\nSomething.\n"

	if err := spec.Save(dir); err != nil {
		t.Fatalf("Save of a scaffold = %v", err)
	}
	reloaded, err := LoadSpec(dir)
	if err != nil {
		t.Fatalf("LoadSpec of a scaffold = %v", err)
	}
	if !reloaded.IsScaffold() {
		t.Error("the reloaded scaffold is not reported as one")
	}
}

// Every writer of a package also writes its render, spec.md, through the same
// temporary files and renames as the artifacts.
func TestSave_WritesTheRenderWithEveryWrite(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(t *testing.T, dir string) *Spec
	}{
		{"Save on a draft", func(t *testing.T, dir string) *Spec {
			spec := loadFixture(t, dir)
			if err := spec.Save(dir); err != nil {
				t.Fatalf("Save = %v", err)
			}
			return spec
		}},
		{"Save on an active package", func(t *testing.T, dir string) *Spec {
			spec := loadFixture(t, activeInto(t, dir))
			spec.Title = "Retitled"
			if err := spec.Save(dir); err != nil {
				t.Fatalf("Save = %v", err)
			}
			return spec
		}},
		{"Transition from draft to active", func(t *testing.T, dir string) *Spec {
			next, err := loadFixture(t, dir).Transition("active", dir)
			if err != nil {
				t.Fatalf("Transition = %v", err)
			}
			return next
		}},
		{"Transition from active to sealed", func(t *testing.T, dir string) *Spec {
			next, err := loadFixture(t, activeInto(t, dir)).Transition("sealed", dir)
			if err != nil {
				t.Fatalf("Transition = %v", err)
			}
			return next
		}},
		{"Supersede on a sealed package", func(t *testing.T, dir string) *Spec {
			sealed, err := loadFixture(t, activeInto(t, dir)).Transition("sealed", dir)
			if err != nil {
				t.Fatal(err)
			}
			next, err := sealed.Supersede("02", dir)
			if err != nil {
				t.Fatalf("Supersede = %v", err)
			}
			return next
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := copyFixture(t, fixtureValidSpec)
			written := tc.write(t, dir)

			got, err := os.ReadFile(filepath.Join(dir, RenderedFileName))
			if err != nil {
				t.Fatalf("the write left no %s: %v", RenderedFileName, err)
			}
			if want := written.RenderedBytes(); string(got) != string(want) {
				t.Errorf("%s is not the render of the package written:\n%s", RenderedFileName, firstDifference(string(want), string(got)))
			}
			for _, name := range entriesOf(t, dir) {
				if strings.Contains(name, ".tmp.") {
					t.Errorf("temp file %q survived the write", name)
				}
			}
		})
	}
}

// A save that cannot create its temporary files changes no file of the
// package, the render included, whether or not one was there.
func TestSave_ThatCannotWriteLeavesEveryFileAsItWas(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rendered string // the spec.md in the directory before the save; empty for none
	}{
		{"with no render", ""},
		{"with a render", "a render written earlier\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := copyFixture(t, fixtureWithArch)
			if tc.rendered != "" {
				if err := os.WriteFile(filepath.Join(dir, RenderedFileName), []byte(tc.rendered), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			spec := loadFixture(t, dir)
			spec.Title = "Changed"
			before := treeOf(t, dir)
			readOnly(t, dir)

			err := spec.Save(dir)
			var saveErr *SaveError
			if !errors.As(err, &saveErr) {
				t.Fatalf("Save into a read-only directory = %v; want a *SaveError", err)
			}
			if changed := changedFiles(before, treeOf(t, dir)); len(changed) > 0 {
				t.Errorf("a failed save changed %v", changed)
			}
		})
	}
}

// LoadSpec reads the artifacts and nothing else: whatever spec.md holds, the
// package loaded is the same.
func TestLoadSpec_ReadsNothingOfTheRender(t *testing.T) {
	bare := loadFixture(t, copyFixture(t, fixtureWithArch))
	bare.Dir = ""
	const unrelated = "# Unrelated\n\nnothing the package says\n"
	for _, tc := range []struct{ name, content string }{
		{"its own render", string(bare.RenderedBytes())},
		{"unrelated text", unrelated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := copyFixture(t, fixtureWithArch)
			if err := os.WriteFile(filepath.Join(dir, RenderedFileName), []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			spec := loadFixture(t, dir)
			spec.Dir = ""
			if !reflect.DeepEqual(spec, bare) {
				t.Error("a package loaded beside a spec.md differs from the one loaded without it")
			}
			if strings.Contains(spec.PRDBody, "Unrelated") || strings.Contains(spec.Architecture, "Unrelated") {
				t.Error("the text of spec.md reached the loaded package")
			}
		})
	}
}

// steering_version is frontmatter, outside the hashed ## Intent, so an active
// package takes a change to it without drifting.
func TestSave_TakesAChangedSteeringVersionOnAnActivePackage(t *testing.T) {
	dir := copyFixture(t, fixtureValidSpec)
	withFrontmatterLine(t, dir, `steering_version: "1.0.0"`)
	activeInto(t, dir)
	spec := loadFixture(t, dir)
	if spec.SteeringVersion != "1.0.0" || spec.IntentHash == nil {
		t.Fatalf("steering_version %q, intent_hash %v; want 1.0.0 on an active package", spec.SteeringVersion, spec.IntentHash)
	}
	before := *spec.IntentHash

	spec.SteeringVersion = "2.0.0"
	if err := spec.Save(dir); err != nil {
		t.Fatalf("Save = %v", err)
	}
	again := loadFixture(t, dir)
	if again.SteeringVersion != "2.0.0" || again.IntentHash == nil || *again.IntentHash != before {
		t.Errorf("steering_version %q, intent_hash %v; want 2.0.0 and %s", again.SteeringVersion, again.IntentHash, before)
	}
	if hash, err := ComputeIntentHash(again.PRDBody); err != nil || hash != before {
		t.Errorf("the intent hashes to %s (%v); want %s, no drift", hash, err, before)
	}
	if v := again.Validate(); !v.Valid {
		t.Errorf("the package does not validate: %v", v.Errors)
	}
}
