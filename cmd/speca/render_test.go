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

// renderOf is what the package in dir must hold in its spec.md.
func renderOf(t *testing.T, dir string) string {
	t.Helper()
	return string(loadSpec(t, dir).RenderedBytes())
}

// renderedFile is what the package in dir holds in its spec.md, or "" when it
// has none.
func renderedFile(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, afspec.RenderedFileName))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

// withoutRender is the files of a tree, spec.md left out.
func withoutRender(tree map[string]string) map[string]string {
	out := maps.Clone(tree)
	delete(out, afspec.RenderedFileName)
	return out
}

// outcomes is each entry of a render result as "spec_dir rendered".
func outcomes(entries []envelope.RenderedInfo) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.SpecDir+" "+e.Rendered)
	}
	return out
}

// Rendering one package writes its spec.md, whatever its status and whatever
// was there, and changes no other file, so updated_at stays as it was.
func TestRender_WritesThePackagesRenderAndNothingElse(t *testing.T) {
	for _, status := range []string{"draft", "active", "sealed", "superseded", "archived"} {
		for _, start := range []string{"absent", "stale"} {
			t.Run(status+"/"+start, func(t *testing.T) {
				root := t.TempDir()
				dir := packageWith(t, root, "03_p", "03", "p", status)
				if start == "stale" {
					writeFile(t, filepath.Join(dir, afspec.RenderedFileName), "an older render\n")
				}
				before := withoutRender(treeBytes(t, dir))

				_, r := succeeds(t, root, "03_p", "--render")
				if got, want := outcomes(r.Rendered), []string{".specs/03_p written"}; !slices.Equal(got, want) {
					t.Errorf("rendered %v; want %v", got, want)
				}
				if renderedFile(t, dir) != renderOf(t, dir) {
					t.Error("spec.md is not the package's render")
				}
				if !maps.Equal(before, withoutRender(treeBytes(t, dir))) {
					t.Error("a file other than spec.md changed")
				}
			})
		}
	}
}

// A render that is already current is reported unchanged, and the file is not
// written again.
func TestRender_LeavesACurrentRenderAlone(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	succeeds(t, root, "09", "--render")
	before := treeBytes(t, dir)

	for range 2 {
		_, r := succeeds(t, root, "09", "--render")
		if got, want := outcomes(r.Rendered), []string{".specs/09_agent_mode unchanged"}; !slices.Equal(got, want) {
			t.Errorf("rendered %v; want %v", got, want)
		}
		if !maps.Equal(before, treeBytes(t, dir)) {
			t.Error("the package changed, spec.md's bytes or time included")
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "*.tmp.*")); len(matches) > 0 {
		t.Errorf("temp files were left behind: %v", matches)
	}
}

// A dry run reports what the call would do, and writes nothing.
func TestRender_ADryRunWritesNothing(t *testing.T) {
	root := t.TempDir()
	absent := addPackage(t, root, "01_absent", "01", "absent")
	current := addPackage(t, root, "02_current", "02", "current")
	writeFile(t, filepath.Join(current, afspec.RenderedFileName), renderOf(t, current))
	stale := addPackage(t, root, "03_stale", "03", "stale")
	writeFile(t, filepath.Join(stale, afspec.RenderedFileName), "an older render\n")
	before := snapshot(t, root)

	for dir, want := range map[string]string{absent: "written", current: "unchanged", stale: "written"} {
		ref := filepath.Base(dir)
		env, r := succeeds(t, root, ref, "--render", "--dry-run")
		if !r.DryRun {
			t.Errorf("%s: dry_run is not true", ref)
		}
		if got := outcomes(r.Rendered); !slices.Equal(got, []string{".specs/" + ref + " " + want}) {
			t.Errorf("%s: rendered %v; want %s", ref, got, want)
		}
		if !strings.Contains(env.Summary, "dry run") {
			t.Errorf("%s: summary %q does not say it was a dry run", ref, env.Summary)
		}
	}
	env, r := succeeds(t, root, "--render", "--dry-run")
	want := []string{".specs/01_absent written", ".specs/02_current unchanged", ".specs/03_stale written"}
	if got := outcomes(r.Rendered); !r.DryRun || !slices.Equal(got, want) {
		t.Errorf("the sweep: dry_run %v, rendered %v; want true and %v", r.DryRun, got, want)
	}
	if !strings.Contains(env.Summary, "dry run") {
		t.Errorf("the sweep: summary %q does not say it was a dry run", env.Summary)
	}
	if !maps.Equal(before, snapshot(t, root)) {
		t.Error("a dry run changed the spec root")
	}
}

// The one-package form fails on a package it cannot load, and on a spec.md it
// cannot write, and leaves every file of the package as it was.
func TestRender_FailsOnAPackageItCannotLoadOrWrite(t *testing.T) {
	t.Run("load", func(t *testing.T) {
		root := newRepo(t)
		dir := pkgDir(root, "09_agent_mode")
		writeFile(t, filepath.Join(dir, "requirements.json"), "{ not json")
		e := refusedAt(t, root, "load", "input", "09", "--render")
		if e != nil {
			saysAll(t, []string{"09", "--render"}, e.Message, dir, "requirements.json")
		}
	})
	t.Run("write", func(t *testing.T) {
		root := newRepo(t)
		dir := pkgDir(root, "09_agent_mode")
		readOnly(t, dir)
		e := refusedAt(t, root, "write", envelope.CategoryDisk, "09", "--render")
		if e != nil {
			saysAll(t, []string{"09", "--render"}, e.Message, filepath.Join(dir, afspec.RenderedFileName), "permission denied")
		}
	})
}

// With no package the form renders every package of the spec root in number
// order, and with --all those under archive/ after them.
func TestRender_SweepsTheSpecRootAndWithAllTheArchive(t *testing.T) {
	root := t.TempDir()
	for _, p := range []struct{ id, name string }{{"10", "c"}, {"02", "a"}, {"03", "b"}} {
		addPackage(t, root, p.id+"_"+p.name, p.id, p.name)
	}
	archived := []string{addPackage(t, root, "archive/05_e", "05", "e"), addPackage(t, root, "archive/04_d", "04", "d")}

	_, r := succeeds(t, root, "--render")
	want := []string{".specs/02_a written", ".specs/03_b written", ".specs/10_c written"}
	if got := outcomes(r.Rendered); !slices.Equal(got, want) {
		t.Errorf("rendered %v; want %v", got, want)
	}
	for _, name := range []string{"02_a", "03_b", "10_c"} {
		if dir := pkgDir(root, name); renderedFile(t, dir) != renderOf(t, dir) {
			t.Errorf("%s holds no current spec.md", name)
		}
	}
	for _, dir := range archived {
		if renderedFile(t, dir) != "" {
			t.Errorf("%s was rendered without --all", dir)
		}
	}

	_, r = succeeds(t, root, "--render", "--all")
	want = []string{".specs/02_a unchanged", ".specs/03_b unchanged", ".specs/10_c unchanged",
		".specs/archive/04_d written", ".specs/archive/05_e written"}
	if got := outcomes(r.Rendered); !slices.Equal(got, want) {
		t.Errorf("rendered with --all %v; want %v", got, want)
	}
	for i, e := range r.Rendered {
		if e.Archived != (i >= 3) {
			t.Errorf("%s: archived %v", e.SpecDir, e.Archived)
		}
	}
}

// A package the sweep cannot load or write is an entry with its reason, and
// every other package is rendered all the same.
func TestRender_SweepGoesOnPastAFailedPackage(t *testing.T) {
	root := t.TempDir()
	good := []string{addPackage(t, root, "01_a", "01", "a"), addPackage(t, root, "04_d", "04", "d")}
	writeFile(t, filepath.Join(addPackage(t, root, "02_b", "02", "b"), "prd.md"), "---\nspec_id: [unclosed\n---\n")
	readOnly(t, addPackage(t, root, "03_c", "03", "c"))

	_, r := succeeds(t, root, "--render")
	want := []string{".specs/01_a written", ".specs/02_b failed", ".specs/03_c failed", ".specs/04_d written"}
	if got := outcomes(r.Rendered); !slices.Equal(got, want) {
		t.Fatalf("rendered %v; want %v", got, want)
	}
	for _, e := range r.Rendered {
		if (e.Rendered == "failed") != (e.Reason != "") {
			t.Errorf("%s: rendered %s, reason %q", e.SpecDir, e.Rendered, e.Reason)
		}
	}
	if !strings.Contains(r.Rendered[2].Reason, "permission denied") {
		t.Errorf("reason %q does not say why the write failed", r.Rendered[2].Reason)
	}
	for _, dir := range good {
		if renderedFile(t, dir) != renderOf(t, dir) {
			t.Errorf("%s holds no current spec.md", dir)
		}
	}
}

// The sweep fails on a spec root it cannot read, and reports an empty list for
// one that holds no package.
func TestRender_SweepOfAnUnreadableOrEmptySpecRoot(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	writeFile(t, file, "")
	for name, specsDir := range map[string]string{"missing": filepath.Join(t.TempDir(), "missing"), "a file": file} {
		env, code, _ := run(t, "--specs-dir", specsDir, "--render")
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
	env, raw := rawRun(t, "--dir", root, "--render")
	if env.ExitCode != cli.ExitOK || string(raw["rendered"]) != "[]" {
		t.Errorf("exit %d, rendered = %s; want 0 and []", env.ExitCode, raw["rendered"])
	}
}

// The summary counts what was written, what was unchanged and what failed.
func TestRender_SummaryCountsTheOutcomes(t *testing.T) {
	root := t.TempDir()
	addPackage(t, root, "01_absent", "01", "absent")
	current := addPackage(t, root, "02_current", "02", "current")
	writeFile(t, filepath.Join(current, afspec.RenderedFileName), renderOf(t, current))
	stale := addPackage(t, root, "03_stale", "03", "stale")
	writeFile(t, filepath.Join(stale, afspec.RenderedFileName), "an older render\n")
	writeFile(t, filepath.Join(addPackage(t, root, "04_broken", "04", "broken"), "prd.md"), "---\nspec_id: [unclosed\n---\n")

	env, r := succeeds(t, root, "--render")
	if want := "speca: 2 written, 1 unchanged, 1 failed"; env.Summary != want {
		t.Errorf("summary %q; want %q", env.Summary, want)
	}
	if len(r.Rendered) != 2+1+1 {
		t.Errorf("%d entries; want 4", len(r.Rendered))
	}
	env, _ = succeeds(t, root, "01", "--render")
	if want := "speca: 0 written, 1 unchanged, 0 failed"; env.Summary != want {
		t.Errorf("one package: summary %q; want %q", env.Summary, want)
	}
}
