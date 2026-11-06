package main

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// Migrating one version 2 package rewrites it as version 3 in place, whatever
// its status: requirements.json takes the checks of test_spec.json, which is
// removed, progress.json takes the task states, and the result is the worked
// example of format v3, which validates.
func TestMigrate_ConvertsAVersion2Package(t *testing.T) {
	for _, status := range []string{"draft", "sealed"} {
		t.Run(status, func(t *testing.T) {
			root := t.TempDir()
			dir := addV2Package(t, root, "09_agent_mode", "09", "agent_mode")
			if status != "draft" {
				setStatus(t, dir, status)
			}
			if versionOnDisk(t, dir) != 2 {
				t.Fatal("the copy is not version 2")
			}

			env, r := succeeds(t, root, "09", "--migrate")
			want := []envelope.MigratedInfo{{SpecDir: ".specs/09_agent_mode", SpecID: "09", SpecName: "agent_mode", Status: status, FromVersion: 2, Migrated: "written"}}
			if !slices.Equal(r.Migrated, want) || r.DryRun {
				t.Errorf("migrated %+v, dry_run %v; want %+v", r.Migrated, r.DryRun, want)
			}
			if env.Summary != "speca: 1 written, 0 unchanged, 0 failed" || len(env.Next) != 0 || len(env.Warnings) != 0 {
				t.Errorf("summary %q, next %+v, warnings %v", env.Summary, env.Next, warningCodes(env))
			}
			if exists(filepath.Join(dir, afspec.TestSpecFileName)) || !exists(filepath.Join(dir, afspec.ProgressFileName)) || !exists(filepath.Join(dir, afspec.RenderedFileName)) {
				t.Error("test_spec.json is still there, or progress.json or spec.md is not")
			}
			spec := loadSpec(t, dir)
			if spec.SchemaVersion != afspec.SchemaVersion || spec.Status != status || spec.UpdatedAt != "2026-09-08T09:00:00Z" {
				t.Errorf("on disk: version %d, status %q, updated_at %q; want version 3, %s and the frontmatter's updated_at moved to progress.json", spec.SchemaVersion, spec.Status, spec.UpdatedAt, status)
			}
			if errs, _ := ownChecks(t, dir); len(errs) != 0 {
				t.Errorf("the converted package breaks %v", errs)
			}
			for _, f := range []string{"prd.md", afspec.RequirementsFileName} {
				got, _ := os.ReadFile(filepath.Join(dir, f))
				example, _ := os.ReadFile(filepath.Join(fixture("v3_example"), f))
				if status == "draft" && string(got) != string(example) {
					t.Errorf("%s is not the v3 example's:\n%s", f, got)
				}
			}
			if got := onDisk(t, dir); !maps.Equal(got, map[int]string{1: "pending", 2: "pending", 3: "pending"}) {
				t.Errorf("the states on disk are %v; want the two tasks and their integration task pending", got)
			}
		})
	}
}

// A version 3 package is reported unchanged and nothing is written.
func TestMigrate_LeavesAVersion3PackageAlone(t *testing.T) {
	root := newRepo(t)
	before := snapshot(t, root)
	env, r := succeeds(t, root, "09", "--migrate")
	want := []envelope.MigratedInfo{{SpecDir: ".specs/09_agent_mode", SpecID: "09", SpecName: "agent_mode", Status: "draft", FromVersion: 3, Migrated: "unchanged"}}
	if !slices.Equal(r.Migrated, want) || env.Summary != "speca: 0 written, 1 unchanged, 0 failed" {
		t.Errorf("migrated %+v, summary %q; want %+v", r.Migrated, env.Summary, want)
	}
	if !maps.Equal(before, snapshot(t, root)) {
		t.Error("the repository changed")
	}
}

// A version 2 package with a criterion no test verifies has no deterministic
// conversion: it fails the call at stage migrate, with the library's reason,
// and is left as it is, under --dry-run too.
func TestMigrate_RefusesAVersion2PackageWithAGap(t *testing.T) {
	root := t.TempDir()
	dir := addV2Package(t, root, "09_agent_mode", "09", "agent_mode")
	breakV2Package(t, dir, "09")
	for _, argv := range [][]string{{"09", "--migrate"}, {"09", "--migrate", "--dry-run"}} {
		e := refusedAt(t, root, "migrate", envelope.CategoryInput, argv...)
		if e == nil {
			continue
		}
		saysAll(t, argv, e.Message, "09-REQ-2.2", "verified by no test")
		if e.Retryable || e.Resumable {
			t.Errorf("%v: retryable %v, resumable %v", argv, e.Retryable, e.Resumable)
		}
	}
	if versionOnDisk(t, dir) != 2 || !exists(filepath.Join(dir, afspec.TestSpecFileName)) {
		t.Error("the package was changed")
	}
}

// The one-package form fails on a package that cannot be read.
func TestMigrate_FailsOnAPackageItCannotRead(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	writeFile(t, filepath.Join(dir, "requirements.json"), "{ not json")
	if e := refusedAt(t, root, "migrate", envelope.CategoryInput, "09", "--migrate"); e != nil {
		saysAll(t, []string{"09", "--migrate"}, e.Message, "requirements.json")
	}
}

// A dry run reports what the call would do and writes nothing; the real call
// then does what the dry run said.
func TestMigrate_ADryRunWritesNothing(t *testing.T) {
	root := t.TempDir()
	addV2Package(t, root, "09_agent_mode", "09", "agent_mode")
	addPackage(t, root, "01_a", "01", "a")
	before := snapshot(t, root)

	env, r := succeeds(t, root, "09", "--migrate", "--dry-run")
	if got := migrateOutcomes(r.Migrated); !r.DryRun || !slices.Equal(got, []string{".specs/09_agent_mode written"}) {
		t.Errorf("dry_run %v, migrated %v; want true and written", r.DryRun, got)
	}
	if env.Summary != "speca: 1 written, 0 unchanged, 0 failed (dry run, nothing written)" {
		t.Errorf("summary %q", env.Summary)
	}
	env, r = succeeds(t, root, "--migrate", "--dry-run")
	if got := migrateOutcomes(r.Migrated); !r.DryRun || !slices.Equal(got, []string{".specs/01_a unchanged", ".specs/09_agent_mode written"}) {
		t.Errorf("the sweep: dry_run %v, migrated %v", r.DryRun, got)
	}
	if !strings.HasSuffix(env.Summary, "(dry run, nothing written)") {
		t.Errorf("the sweep: summary %q", env.Summary)
	}
	if !maps.Equal(before, snapshot(t, root)) {
		t.Fatal("a dry run changed the spec root")
	}

	_, r = succeeds(t, root, "09", "--migrate")
	if got := migrateOutcomes(r.Migrated); r.DryRun || !slices.Equal(got, []string{".specs/09_agent_mode written"}) {
		t.Errorf("the real call: dry_run %v, migrated %v", r.DryRun, got)
	}
	if versionOnDisk(t, pkgDir(root, "09_agent_mode")) != afspec.SchemaVersion {
		t.Error("the real call did not convert the package")
	}
}

// With no package the form converts every package of the spec root in number
// order, and with --all those under archive/ after them. A package that cannot
// be converted is an entry with its reason, not a failure of the call, and
// every other package is converted all the same.
func TestMigrate_SweepsTheSpecRootAndWithAllTheArchive(t *testing.T) {
	root := t.TempDir()
	addPackage(t, root, "01_a", "01", "a")
	setStatus(t, addV2Package(t, root, "02_b", "02", "b"), "sealed")
	breakV2Package(t, addV2Package(t, root, "03_c", "03", "c"), "03")
	writeFile(t, filepath.Join(addPackage(t, root, "04_d", "04", "d"), "prd.md"), "---\nspec_id: [unclosed\n---\n")
	addV2Package(t, root, "10_e", "10", "e")
	archived := addV2Package(t, root, "archive/05_f", "05", "f")
	addPackage(t, root, "archive/06_g", "06", "g")

	env, r := succeeds(t, root, "--migrate")
	want := []string{".specs/01_a unchanged", ".specs/02_b written", ".specs/03_c failed", ".specs/04_d failed", ".specs/10_e written"}
	if got := migrateOutcomes(r.Migrated); !slices.Equal(got, want) {
		t.Fatalf("migrated %v; want %v", got, want)
	}
	if env.Summary != "speca: 2 written, 1 unchanged, 2 failed" || len(env.Next) != 0 {
		t.Errorf("summary %q, next %+v", env.Summary, env.Next)
	}
	for _, e := range r.Migrated {
		if (e.Migrated == "failed") != (e.Reason != "") || e.Archived {
			t.Errorf("%s: migrated %s, reason %q, archived %v", e.SpecDir, e.Migrated, e.Reason, e.Archived)
		}
	}
	if gap := r.Migrated[2]; !strings.Contains(gap.Reason, "03-REQ-2.2") || gap.FromVersion != 2 || gap.SpecID != "03" {
		t.Errorf("the package with a gap: %+v; want its reason and its identity", gap)
	}
	if broken := r.Migrated[3]; broken.SpecID != "" || broken.FromVersion != 0 || !strings.Contains(broken.Reason, "prd.md") {
		t.Errorf("the package that does not load: %+v; want its directory and the library's reason alone", broken)
	}
	for _, name := range []string{"02_b", "10_e"} {
		if dir := pkgDir(root, name); versionOnDisk(t, dir) != afspec.SchemaVersion || exists(filepath.Join(dir, afspec.TestSpecFileName)) {
			t.Errorf("%s was not converted", name)
		}
	}
	if versionOnDisk(t, pkgDir(root, "02_b")) != 3 || statusOnDisk(t, pkgDir(root, "02_b")) != "sealed" {
		t.Error("the sealed package was not converted, or lost its status")
	}
	if versionOnDisk(t, pkgDir(root, "03_c")) != 2 || versionOnDisk(t, archived) != 2 {
		t.Error("the package with a gap, or the archived package, was converted")
	}

	_, r = succeeds(t, root, "--migrate", "--all")
	want = []string{".specs/01_a unchanged", ".specs/02_b unchanged", ".specs/03_c failed", ".specs/04_d failed", ".specs/10_e unchanged",
		".specs/archive/05_f written", ".specs/archive/06_g unchanged"}
	if got := migrateOutcomes(r.Migrated); !slices.Equal(got, want) {
		t.Errorf("migrated with --all %v; want %v", got, want)
	}
	for i, e := range r.Migrated {
		if e.Archived != (i >= 5) {
			t.Errorf("%s: archived %v", e.SpecDir, e.Archived)
		}
	}
	if versionOnDisk(t, archived) != afspec.SchemaVersion {
		t.Error("the archived package was not converted under --all")
	}
}

// The sweep fails on a spec root it cannot read, and reports an empty list for
// one that holds no package.
func TestMigrate_SweepOfAnUnreadableOrEmptySpecRoot(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	writeFile(t, file, "")
	for name, specsDir := range map[string]string{"missing": filepath.Join(t.TempDir(), "missing"), "a file": file} {
		env, code, _ := run(t, "--specs-dir", specsDir, "--migrate")
		if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "load" || env.Result != nil {
			t.Errorf("%s: code %d: %+v", name, code, env.Error)
			continue
		}
		if !strings.Contains(env.Error.Message, specsDir) {
			t.Errorf("%s: message %q does not name the root", name, env.Error.Message)
		}
		if h := env.Error.FixHint; h == nil || h.Flag != "--specs-dir" || !slices.Equal(h.Env, []string{afspec.SpecDirEnv}) {
			t.Errorf("%s: fix_hint = %+v; want --specs-dir and %s", name, h, afspec.SpecDirEnv)
		}
	}

	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".specs"))
	env, raw := rawRun(t, "--dir", root, "--migrate")
	if env.ExitCode != cli.ExitOK || string(raw["migrated"]) != "[]" || env.Summary != "speca: 0 written, 0 unchanged, 0 failed" {
		t.Errorf("exit %d, migrated = %s, summary %q; want 0, [] and no counts", env.ExitCode, raw["migrated"], env.Summary)
	}
}

// Once converted, a package takes every other form: the report reads its
// tasks and their states, and the task form moves them.
func TestMigrate_AConvertedPackageTakesTheOtherForms(t *testing.T) {
	root := t.TempDir()
	dir := addV2Package(t, root, "09_agent_mode", "09", "agent_mode")
	editJSON(t, filepath.Join(dir, afspec.TasksFileName), func(m map[string]any) {
		m["tasks"].([]any)[0].(map[string]any)["state"] = "done"
	})
	if env, _, _ := run(t, "--dir", root, "09", "--task", "2", "--state", "in_progress"); env.Error == nil || env.Error.Stage != "usage" {
		t.Fatalf("the task form on a version 2 package: %+v; want a usage refusal", env.Error)
	}

	succeeds(t, root, "09", "--migrate")
	env, r := succeeds(t, root, "09")
	if len(env.Warnings) != 0 || r.Valid == nil || !*r.Valid || !maps.Equal(onDisk(t, dir), map[int]string{1: "done", 2: "pending", 3: "pending"}) {
		t.Errorf("the report: warnings %v, valid %v, states %v; want a valid package with task 1 done", warningCodes(env), r.Valid, onDisk(t, dir))
	}
	if r.NextTask == nil || r.NextTask.ID != 2 || !r.NextTask.Ready {
		t.Errorf("next_task %+v; want task 2, ready", r.NextTask)
	}
	_, r = succeeds(t, root, "09", "--task", "2", "--state", "in_progress")
	if !slices.Equal(changed(r), []int{2}) || onDisk(t, dir)[2] != "in_progress" {
		t.Errorf("the task form: changes %+v, on disk %v", r.Changes, onDisk(t, dir))
	}
}
