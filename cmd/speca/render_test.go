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
