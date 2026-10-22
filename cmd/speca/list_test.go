package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// rawPackages runs speca and returns the members of each entry of packages as
// they were written.
func rawPackages(t *testing.T, argv ...string) (envelope.AdminEnvelope, []map[string]json.RawMessage) {
	t.Helper()
	env, raw := rawRun(t, argv...)
	var packages []map[string]json.RawMessage
	if err := json.Unmarshal(raw["packages"], &packages); err != nil {
		t.Fatalf("packages = %s (%v); error %+v", raw["packages"], err, env.Error)
	}
	return env, packages
}

func dirs(packages []envelope.PackageInfo) []string {
	var out []string
	for _, p := range packages {
		out = append(out, filepath.ToSlash(p.SpecDir))
	}
	return out
}

// TS-10-40: the packages come in the order of their numbers, a number
// compared as a number, and a directory that is not a package is not listed.
func TestListIsInNumericOrder_TS10_40(t *testing.T) {
	root := t.TempDir()
	for _, p := range []struct{ id, name string }{{"100", "x"}, {"99", "y"}, {"02", "b"}, {"02", "a"}} {
		addPackage(t, root, p.id+"_"+p.name, p.id, p.name)
	}
	if err := os.MkdirAll(pkgDir(root, "misc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pkgDir(root, "03_noprd"), 0o755); err != nil { // a directory with no prd.md at all
		t.Fatal(err)
	}

	env, code, _ := run(t, "--dir", root, "--list")
	if code != cli.ExitOK || env.Result == nil {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	want := []string{".specs/02_a", ".specs/02_b", ".specs/03_noprd", ".specs/99_y", ".specs/100_x"}
	if got := dirs(env.Result.Packages); !slices.Equal(got, want) {
		t.Errorf("packages %v; want %v", got, want)
	}
	if p := env.Result.Packages[2]; p.LoadError == "" || p.SpecID != "" {
		t.Errorf("a package with no prd.md: %+v; want it listed with its load_error", p)
	}
}

// TS-10-41: a package that loads has a full entry; one that does not has its
// directory and the library's reason, and the list goes on.
func TestListEntries_TS10_41(t *testing.T) {
	root := t.TempDir()
	good := addPackage(t, root, "01_good", "01", "good")
	setTasks(t, good, planTask(1, "done", nil), planTask(2, "pending", []int{1}), planTask(3, "pending", []int{2}))
	bad := addPackage(t, root, "02_invalid", "02", "invalid")
	editFile(t, filepath.Join(bad, "tasks.json"), `"TS-09-6"`, `"TS-09-99"`)
	malformed := addPackage(t, root, "03_malformed", "03", "malformed")
	if err := os.WriteFile(filepath.Join(malformed, "prd.md"), []byte("---\nspec_id: [unclosed\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pkgDir(root, "04_noprd"), 0o755); err != nil {
		t.Fatal(err)
	}
	after := addPackage(t, root, "05_after", "05", "after")
	setTasks(t, after, planTask(1, "done", nil))

	env, packages := rawPackages(t, "--dir", root, "--list")
	if env.ExitCode != cli.ExitOK {
		t.Fatalf("exit %d: %+v", env.ExitCode, env.Error)
	}
	if len(packages) != 5 {
		t.Fatalf("%d entries; want 5", len(packages))
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
		keys := make([]string, 0, len(packages[i]))
		for k := range packages[i] {
			keys = append(keys, k)
		}
		slices.Sort(keys)
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

// TS-10-42: --all adds what is under archive/, marked archived whatever its
// status; a root with no archive/ adds nothing.
func TestListAll_TS10_42(t *testing.T) {
	root := t.TempDir()
	addPackage(t, root, "01_a", "01", "a")
	old := addPackage(t, root, "archive/05_old", "05", "old")
	setStatus(t, old, "superseded")
	addPackage(t, root, "archive/03_older", "03", "older")

	env, code, _ := run(t, "--dir", root, "--list", "--all")
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	got := result(t, env).Packages
	if want := []string{".specs/01_a", ".specs/archive/03_older", ".specs/archive/05_old"}; !slices.Equal(dirs(got), want) {
		t.Fatalf("packages %v; want %v", dirs(got), want)
	}
	if got[0].Archived || !got[1].Archived || !got[2].Archived || got[2].Status != "superseded" {
		t.Errorf("archived: %v %v %v; the last has status %q", got[0].Archived, got[1].Archived, got[2].Archived, got[2].Status)
	}

	env, _, _ = run(t, "--dir", root, "--list")
	if len(result(t, env).Packages) != 1 {
		t.Errorf("without --all: %v", dirs(env.Result.Packages))
	}

	bare := t.TempDir()
	addPackage(t, bare, "01_a", "01", "a")
	env, code, _ = run(t, "--dir", bare, "--list", "--all")
	if code != cli.ExitOK || len(result(t, env).Packages) != 1 || len(env.Warnings) != 0 {
		t.Errorf("no archive/: code %d, %d packages, warnings %v", code, len(result(t, env).Packages), warningCodes(env))
	}

	// An archive/ that exists and cannot be read is not a missing one.
	blocked := t.TempDir()
	addPackage(t, blocked, "01_a", "01", "a")
	if err := os.WriteFile(filepath.Join(blocked, ".specs", "archive"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	env, code, _ = run(t, "--dir", blocked, "--list", "--all")
	if code != cli.ExitOK || len(result(t, env).Packages) != 1 || len(env.Warnings) != 1 ||
		env.Warnings[0].Code != cli.WarnSpecsDirUnreadable || env.Warnings[0].Severity != "low" || !strings.Contains(env.Warnings[0].Message, "archive") {
		t.Errorf("an archive that is a file: code %d, warnings %+v", code, env.Warnings)
	}
}

// A package under archive/ that does not load is listed with its directory and
// its reason alone: the entry that failed holds two fields, and its path says
// where it was.
func TestListAnArchivedPackageThatDoesNotLoad(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".specs", "archive", "04_gone"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, packages := rawPackages(t, "--dir", root, "--list", "--all")
	if len(packages) != 1 || len(packages[0]) != 2 || packages[0]["load_error"] == nil {
		t.Errorf("entries %v; want one holding spec_dir and load_error", packages)
	}
}

// TS-10-43: a spec root that cannot be read fails the call, saying which and
// what to change.
func TestListOfAnUnreadableSpecRoot_TS10_43(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
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

// TS-10-44: a spec root with nothing in it is an empty list, not an error.
func TestListOfAnEmptySpecRoot_TS10_44(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	env, raw := rawRun(t, "--dir", root, "--list")
	if env.ExitCode != cli.ExitOK || string(raw["packages"]) != "[]" {
		t.Errorf("exit %d, packages = %s; want 0 and []", env.ExitCode, raw["packages"])
	}
	if env.Summary != "speca: 0 packages" || len(env.Next) != 0 {
		t.Errorf("summary %q next %v", env.Summary, env.Next)
	}
}
