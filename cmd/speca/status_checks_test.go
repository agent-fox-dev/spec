package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each target has checks of its own. Activation needs an intent, which --force
// does not override, and a package that validates, keys of the frontmatter
// included, which it does. Sealing needs an intent that has not drifted, which
// --force does not override, and no task open, an optional one included, which
// it does. Sealing, archiving and superseding refuse a package that holds keys
// the schema does not allow, in prd.md or in any other artifact, naming each.
// Every refusal comes before anything is written.
func TestStatus_RefusesWhatTheChecksOfTheTargetRefuse(t *testing.T) {
	for name, tc := range map[string]struct {
		setup           func(t *testing.T, root string)
		argv            []string
		stage, category string
		want            []string
		forceHint       bool
	}{
		"activation without an intent, forced": {func(t *testing.T, root string) { removeIntent(t, pkgDir(root, "09_agent_mode")) },
			[]string{"09", "--status", "active", "--force"}, "status", "lifecycle",
			[]string{"`## Intent`", "spelled exactly so", "on a line of its own", "prd.md"}, false},
		"activation of a package that does not validate": {func(t *testing.T, root string) { breakValidity(t, pkgDir(root, "09_agent_mode")) },
			[]string{"09", "--status", "active"}, "validate", "invalid_spec",
			[]string{"09_agent_mode", "does not validate", "C6", "--validate"}, true},
		// The library reports no key of the frontmatter, and the write drops it.
		"activation of a package with a key in the frontmatter": {func(t *testing.T, root string) { withFrontmatterKey(t, pkgDir(root, "09_agent_mode")) },
			[]string{"09", "--status", "active"}, "validate", "invalid_spec",
			[]string{"unknown_field", "prd.md", "badkey"}, true},
		"sealing an edited intent, forced": {func(t *testing.T, root string) {
			driftIntent(t, activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil)))
		}, []string{"03_a", "--status", "sealed", "--force"}, "status", "lifecycle",
			[]string{"03_a", "restore the intent that was activated", "supersedes this one"}, false},
		"sealing a removed intent, forced": {func(t *testing.T, root string) {
			removeIntent(t, activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil)))
		}, []string{"03_a", "--status", "sealed", "--force"}, "status", "lifecycle",
			[]string{"03_a", "restore the intent that was activated", "supersedes this one"}, false},
		"sealing with open tasks": {func(t *testing.T, root string) { activePackage(t, root, "03_a", "03", "a", planWithOpenTasks()...) },
			[]string{"03_a", "--status", "sealed"}, "status", "lifecycle",
			[]string{"task 1 (pending)", "task 2 (in_progress)", "finish or drop"}, true},
		"sealing with a key in the frontmatter": {func(t *testing.T, root string) {
			withFrontmatterKey(t, activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil)))
		}, []string{"03_a", "--status", "sealed"}, "status", "lifecycle", []string{"prd.md", "badkey"}, true},
		"archiving with a key in tasks.json": {func(t *testing.T, root string) { addUnknownKey(t, packageWith(t, root, "04_s", "04", "s", "sealed")) },
			[]string{"04_s", "--status", "archived"}, "status", "lifecycle", []string{"tasks.json", "traceability"}, true},
		"archiving with a key in requirements.json": {func(t *testing.T, root string) {
			editJSON(t, filepath.Join(packageWith(t, root, "05_r", "05", "r", "sealed"), "requirements.json"), func(m map[string]any) { m["stray"] = 1 })
		}, []string{"05_r", "--status", "archived"}, "status", "lifecycle", []string{"requirements.json", "stray"}, true},
		"archiving with a key in test_spec.json": {func(t *testing.T, root string) {
			editJSON(t, filepath.Join(packageWith(t, root, "06_t", "06", "t", "sealed"), "test_spec.json"), func(m map[string]any) { m["stray"] = 1 })
		}, []string{"06_t", "--status", "archived"}, "status", "lifecycle", []string{"test_spec.json", "stray"}, true},
		"superseding with a key in tasks.json": {func(t *testing.T, root string) {
			addUnknownKey(t, packageWith(t, root, "07_old", "07", "old", "sealed"))
			packageWith(t, root, "12_new", "12", "new", "draft")
		}, []string{"07_old", "--status", "superseded", "--by", "12_new"}, "status", "lifecycle", []string{"tasks.json", "traceability"}, true},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			tc.setup(t, root)
			e := refusedAt(t, root, tc.stage, tc.category, tc.argv...)
			if e == nil {
				return
			}
			saysAll(t, tc.argv, e.Message, tc.want...)
			switch h := e.FixHint; {
			case tc.forceHint && (h == nil || h.Flag != "--force"):
				t.Errorf("fix_hint %+v; want --force, which overrides the check", h)
			case !tc.forceHint && h != nil:
				t.Errorf("fix_hint %+v; want none, since --force does not override the check", h)
			}
		})
	}
}

// --force goes past the checks it overrides, with a high warning that says
// what it skipped, and the package it writes holds none of the keys it dropped.
func TestStatus_ForceGoesPastTheChecksItOverrides(t *testing.T) {
	for name, tc := range map[string]struct {
		setup  func(t *testing.T, root string) string
		ref    string
		target string
		code   string
		stage  string
		want   []string
		gone   string
	}{
		"activation of a package that does not validate": {func(t *testing.T, root string) string {
			dir := pkgDir(root, "09_agent_mode")
			breakValidity(t, dir)
			return dir
		}, "09", "active", "spec_invalid", "validate", []string{"C6"}, ""},
		"activation of a package with a key in the frontmatter": {func(t *testing.T, root string) string {
			dir := pkgDir(root, "09_agent_mode")
			withFrontmatterKey(t, dir)
			return dir
		}, "09", "active", "spec_invalid", "validate", []string{"badkey"}, "badkey"},
		"sealing with open tasks": {func(t *testing.T, root string) string {
			return activePackage(t, root, "03_a", "03", "a", planWithOpenTasks()...)
		}, "03_a", "sealed", "tasks_open", "status", []string{"task 1", "pending", "task 2", "in_progress"}, ""},
		"sealing with a key in the frontmatter": {func(t *testing.T, root string) string {
			dir := activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil))
			withFrontmatterKey(t, dir)
			return dir
		}, "03_a", "sealed", "spec_invalid", "validate", []string{"badkey"}, "badkey"},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			dir := tc.setup(t, root)
			env, r := succeeds(t, root, tc.ref, "--status", tc.target, "--force")
			if r.Status != tc.target || statusOnDisk(t, dir) != tc.target {
				t.Errorf("status %q, on disk %q; want %s", r.Status, statusOnDisk(t, dir), tc.target)
			}
			w := oneWarning(t, env, tc.code, "high")
			saysAll(t, nil, w.Message, tc.want...)
			if w.Stage != tc.stage {
				t.Errorf("warning stage %q; want %s", w.Stage, tc.stage)
			}
			if tc.gone != "" {
				if b, _ := os.ReadFile(filepath.Join(dir, "prd.md")); strings.Contains(string(b), tc.gone) {
					t.Errorf("%s is still in prd.md", tc.gone)
				}
			}
		})
	}
}
