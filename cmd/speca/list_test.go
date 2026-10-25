package main

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// The packages come in the order of their numbers, a number compared as a
// number, and a directory that is not a package is not listed.
func TestList_IsInNumberOrder(t *testing.T) {
	root := t.TempDir()
	for _, p := range []struct{ id, name string }{{"100", "x"}, {"99", "y"}, {"02", "b"}, {"02", "a"}} {
		addPackage(t, root, p.id+"_"+p.name, p.id, p.name)
	}
	mustMkdir(t, pkgDir(root, "misc"))
	mustMkdir(t, pkgDir(root, "03_noprd")) // a directory with no prd.md at all

	env, code, _ := run(t, "--dir", root, "--list")
	if code != cli.ExitOK || env.Result == nil {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	want := []string{".specs/02_a", ".specs/02_b", ".specs/03_noprd", ".specs/99_y", ".specs/100_x"}
	if got := dirs(env.Result.Packages); !slices.Equal(got, want) {
		t.Errorf("packages %v; want %v", got, want)
	}
}

// A package that loads has a full entry; one that does not has its directory
// and the library's reason alone, and the list goes on.
func TestList_Entries(t *testing.T) {
	root := t.TempDir()
	setTasks(t, addPackage(t, root, "01_good", "01", "good"), planTask(1, "done", nil), planTask(2, "pending", []int{1}), planTask(3, "pending", []int{2}))
	breakValidity(t, addPackage(t, root, "02_invalid", "02", "invalid"))
	writeFile(t, filepath.Join(addPackage(t, root, "03_malformed", "03", "malformed"), "prd.md"), "---\nspec_id: [unclosed\n---\n")
	mustMkdir(t, pkgDir(root, "04_noprd"))
	setTasks(t, addPackage(t, root, "05_after", "05", "after"), planTask(1, "done", nil))

	env, packages := rawPackages(t, "--dir", root, "--list")
	if env.ExitCode != cli.ExitOK || len(packages) != 5 {
		t.Fatalf("exit %d, %d entries: %+v", env.ExitCode, len(packages), env.Error)
	}

	r := env.Result.Packages
	if g := r[0]; g.SpecID != "01" || g.SpecName != "good" || g.Title == "" || g.Status != "draft" || g.Valid == nil || g.Archived ||
		g.TaskCounts == nil || *g.TaskCounts != (envelope.TaskCounts{Pending: 2, Done: 1, Total: 3}) ||
		g.NextTask == nil || g.NextTask.ID != 2 || !g.NextTask.Ready {
		t.Errorf("a package that loads: %+v", g)
	}
	if r[1].Valid == nil || *r[1].Valid || r[1].LoadError != "" {
		t.Errorf("an invalid package: %+v; want it listed in full, with valid false", r[1])
	}
	for _, i := range []int{2, 3} {
		keys := slices.Sorted(maps.Keys(packages[i]))
		if !slices.Equal(keys, []string{"load_error", "spec_dir"}) || r[i].LoadError == "" {
			t.Errorf("entry %d holds %v; want only spec_dir and load_error", i, keys)
		}
	}
	if !strings.Contains(r[3].LoadError, "prd.md") {
		t.Errorf("load_error = %q; want the library's reason", r[3].LoadError)
	}
	if r[4].SpecID != "05" || r[4].NextTask != nil {
		t.Errorf("the package after the failures: %+v", r[4])
	}
	if _, ok := packages[4]["next_task"]; ok {
		t.Error("a package with every task done writes next_task")
	}
}

// --all adds what is under archive/, marked archived whatever its status; the
// summary counts every status and the packages that did not load, and next
// names the first active package that is waiting for someone. Nothing is
// written.
func TestList_AllAddsTheArchive(t *testing.T) {
	root := t.TempDir()
	setTasks(t, addPackage(t, root, "01_a", "01", "a"), planTask(1, "pending", nil))
	two := packageWith(t, root, "02_b", "02", "b", "active")
	setTasks(t, two, planTask(1, "in_progress", nil))
	three := packageWith(t, root, "03_c", "03", "c", "active")
	setTasks(t, three, planTask(1, "done", nil), planTask(2, "pending", []int{1}))
	setTasks(t, packageWith(t, root, "04_d", "04", "d", "sealed"), planTask(1, "done", nil))
	writeFile(t, filepath.Join(addPackage(t, root, "05_bad", "05", "bad"), "prd.md"), "---\nspec_id: [unclosed\n---\n")
	packageWith(t, root, "archive/06_old", "06", "old", "superseded")
	addPackage(t, root, "archive/07_older", "07", "older")
	mustMkdir(t, pkgDir(root, "archive/08_gone")) // under archive/, and does not load
	before := snapshot(t, root)

	env, packages := rawPackages(t, "--dir", root, "--list", "--all")
	if env.ExitCode != cli.ExitOK || !env.OK {
		t.Fatalf("exit %d: %+v", env.ExitCode, env.Error)
	}
	got := env.Result.Packages
	if want := []string{".specs/01_a", ".specs/02_b", ".specs/03_c", ".specs/04_d", ".specs/05_bad",
		".specs/archive/06_old", ".specs/archive/07_older", ".specs/archive/08_gone"}; !slices.Equal(dirs(got), want) {
		t.Fatalf("packages %v; want %v", dirs(got), want)
	}
	for i, p := range got {
		if wantArchived := i == 5 || i == 6; p.Archived != wantArchived {
			t.Errorf("%s: archived %v", p.SpecDir, p.Archived)
		}
	}
	if got[5].Status != "superseded" {
		t.Errorf("the superseded package under archive/: %+v", got[5])
	}
	// A package that does not load is described by its directory and its reason
	// alone, under archive/ too: where it was is in its path.
	if len(packages[7]) != 2 || got[7].LoadError == "" {
		t.Errorf("the package under archive/ that does not load holds %v", packages[7])
	}
	if want := "speca: 8 packages (2 draft, 2 active, 1 sealed, 1 superseded, 2 unreadable)"; env.Summary != want {
		t.Errorf("summary = %q; want %q", env.Summary, want)
	}
	want := []envelope.Next{claim(".specs/03_c", 2), {Tool: "specr", Input: ".specs/03_c", Flags: []string{"--task", "2"}}}
	if len(env.Next) != 2 || !sameCall(env.Next[0], want[0]) || !sameCall(env.Next[1], want[1]) {
		t.Errorf("next = %+v; want the claim of .specs/03_c's task 2, then specr .specs/03_c --task 2", env.Next)
	}
	if !maps.Equal(before, snapshot(t, root)) {
		t.Error("the repository changed")
	}

	env, _, _ = run(t, "--dir", root, "--list")
	if len(result(t, env).Packages) != 5 {
		t.Errorf("without --all: %v", dirs(env.Result.Packages))
	}
}

// A root with no archive/ adds nothing under --all, and says nothing of it;
// an archive/ that exists and cannot be read is not a missing one.
func TestList_AllWithoutAReadableArchive(t *testing.T) {
	bare := t.TempDir()
	addPackage(t, bare, "01_a", "01", "a")
	env, code, _ := run(t, "--dir", bare, "--list", "--all")
	if code != cli.ExitOK || len(result(t, env).Packages) != 1 || len(env.Warnings) != 0 {
		t.Errorf("no archive/: code %d, %d packages, warnings %v", code, len(result(t, env).Packages), warningCodes(env))
	}

	blocked := t.TempDir()
	addPackage(t, blocked, "01_a", "01", "a")
	writeFile(t, filepath.Join(blocked, ".specs", "archive"), "")
	env, code, _ = run(t, "--dir", blocked, "--list", "--all")
	if code != cli.ExitOK || len(result(t, env).Packages) != 1 || len(env.Warnings) != 1 ||
		env.Warnings[0].Code != cli.WarnSpecsDirUnreadable || env.Warnings[0].Severity != "low" ||
		!strings.HasPrefix(env.Warnings[0].Message, "the archive ") || strings.Contains(env.Warnings[0].Message, "spec root") {
		t.Errorf("an archive that is a file: code %d, warnings %+v", code, env.Warnings)
	}
}

// The list suggests the claim for the first active package that can take one:
// nothing in progress, a ready task, and no drift in its intent.
func TestList_SuggestsTheClaimOfTheFirstPackageThatCanTakeIt(t *testing.T) {
	root := t.TempDir()
	activePackage(t, root, "05_busy", "05", "busy", planTask(1, "in_progress", nil))
	driftIntent(t, activePackage(t, root, "07_drifted", "07", "drifted", planTask(1, "pending", nil)))
	activePackage(t, root, "08_first", "08", "first", planTask(1, "pending", nil))
	activePackage(t, root, "09_second", "09", "second", planTask(1, "pending", nil))

	env, code, _ := run(t, "--dir", root, "--list")
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	want := []envelope.Next{claim(".specs/08_first", 1), {Tool: "specr", Input: ".specs/08_first", Flags: []string{"--task", "1"}}}
	if len(env.Next) != 2 || !sameCall(env.Next[0], want[0]) || !sameCall(env.Next[1], want[1]) {
		t.Errorf("next = %+v; want the claim of 08's task 1, then its render, and nothing for 07 or 09", env.Next)
	}
}

// A spec root that cannot be read fails the call, saying which and what to
// change: an empty list would hide a mistyped --specs-dir.
func TestList_FailsOnAnUnreadableSpecRoot(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	writeFile(t, file, "")
	missing := filepath.Join(t.TempDir(), "missing")
	for name, specsDir := range map[string]string{"missing": missing, "a file": file} {
		env, code, stderr := run(t, "--specs-dir", specsDir, "--list")
		if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "load" || env.Error.Category != "input" || env.Result != nil || env.OK {
			t.Errorf("%s: code %d: %+v", name, code, env.Error)
			continue
		}
		if !strings.Contains(env.Error.Message, specsDir) {
			t.Errorf("%s: message %q does not name the root", name, env.Error.Message)
		}
		if env.Error.FixHint == nil || env.Error.FixHint.Flag != "--specs-dir" {
			t.Errorf("%s: fix_hint = %+v; want the flag --specs-dir", name, env.Error.FixHint)
		}
		if stderr != "" {
			t.Errorf("%s: a failure that is not a usage error writes %q to stderr", name, stderr)
		}
	}
}

// A spec root with nothing in it is an empty list, not an error.
func TestList_OfAnEmptySpecRoot(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".specs"))
	env, raw := rawRun(t, "--dir", root, "--list")
	if env.ExitCode != cli.ExitOK || string(raw["packages"]) != "[]" {
		t.Errorf("exit %d, packages = %s; want 0 and []", env.ExitCode, raw["packages"])
	}
	if env.Summary != "speca: 0 packages" || len(env.Next) != 0 {
		t.Errorf("summary %q next %v", env.Summary, env.Next)
	}
}
