package main

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
)

// A package that is superseded or archived cannot replace another, and --force
// does not change that; the hint names the flag to change.
func TestStatus_RefusesAReplacingPackageThatIsFinished(t *testing.T) {
	root := newRepo(t)
	packageWith(t, root, "03_old", "03", "old", "sealed")
	packageWith(t, root, "11_a", "11", "a", "superseded")
	packageWith(t, root, "archive/12_b", "12", "b", "archived")

	for _, by := range []string{"11_a", ".specs/archive/12_b"} {
		argv := []string{"03_old", "--status", "superseded", "--by", by, "--force"}
		e := refusedAt(t, root, "status", envelope.CategoryLifecycle, argv...)
		if e == nil {
			continue
		}
		saysAll(t, argv, e.Message, "--by", "cannot replace")
		if e.FixHint == nil || e.FixHint.Flag != "--by" {
			t.Errorf("%v: fix_hint %+v; want --by", argv, e.FixHint)
		}
	}
}

// Superseding writes the banner and the status, records the id in the replacing
// package, a draft or an active one, and moves the package under archive/.
func TestStatus_Supersedes(t *testing.T) {
	for name, replacingStatus := range map[string]string{"by a draft": "draft", "by an active package": "active"} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			packageWith(t, root, "03_old", "03", "old", "sealed")
			replacing := addPackage(t, root, "12_new", "12", "new")
			if replacingStatus == "active" {
				activate(t, replacing)
			}

			env, r := succeeds(t, root, "03_old", "--status", "superseded", "--by", "12_new")
			want := []envelope.Change{
				{Kind: "status", From: "sealed", To: "superseded"},
				{Kind: "supersedes", Spec: "12", Added: "03"},
				{Kind: "moved", From: ".specs/03_old", To: ".specs/archive/03_old"},
			}
			if !slices.Equal(r.Changes, want) || len(env.Warnings) != 0 {
				t.Errorf("changes %+v, warnings %v; want %+v and no warning", r.Changes, warningCodes(env), want)
			}
			if r.SpecDir != ".specs/archive/03_old" || r.Status != "superseded" || exists(pkgDir(root, "03_old")) {
				t.Errorf("spec_dir %q, status %q; want it superseded under archive/", r.SpecDir, r.Status)
			}
			old := pkgDir(root, "archive/03_old")
			if b, _ := os.ReadFile(filepath.Join(old, "prd.md")); !strings.Contains(string(b), "DEPRECATED") || !strings.Contains(string(b), "superseded by spec 12") {
				t.Errorf("the banner is missing from the prd.md of the superseded package:\n%s", b)
			}
			if statusOnDisk(t, old) != "superseded" {
				t.Errorf("the status on disk is %q", statusOnDisk(t, old))
			}
			if got := supersedesOf(t, replacing); !slices.Equal(got, []string{"03"}) || statusOnDisk(t, replacing) != replacingStatus {
				t.Errorf("the replacing package: supersedes %v, status %q; want [03], still %s", got, statusOnDisk(t, replacing), replacingStatus)
			}
		})
	}
}

// A replacing package that cannot be saved is not a failure: the call goes
// through, with a low warning that names it, the id and why, and the replacing
// package is left as it was. --force does not reach it.
func TestStatus_AReplacingPackageThatCannotBeSavedIsAWarning(t *testing.T) {
	for name, tc := range map[string]struct {
		prepare func(t *testing.T, root string) string
		reason  string
	}{
		"sealed": {func(t *testing.T, root string) string {
			return packageWith(t, root, "12_new", "12", "new", "sealed")
		}, "sealed"},
		"active with drift": {func(t *testing.T, root string) string {
			dir := activePackage(t, root, "12_new", "12", "new")
			driftIntent(t, dir)
			return dir
		}, "Intent"},
		"unknown keys": {func(t *testing.T, root string) string {
			dir := addPackage(t, root, "12_new", "12", "new")
			addUnknownKey(t, dir)
			return dir
		}, "traceability"},
		"a key in the frontmatter": {func(t *testing.T, root string) string {
			dir := addPackage(t, root, "12_new", "12", "new")
			withFrontmatterKey(t, dir)
			return dir
		}, "badkey"},
		"a save that fails": {func(t *testing.T, root string) string {
			dir := addPackage(t, root, "12_new", "12", "new")
			readOnly(t, dir)
			return dir
		}, "save"},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			packageWith(t, root, "03_old", "03", "old", "sealed")
			replacing := tc.prepare(t, root)
			before := treeBytes(t, replacing)

			env, r := succeeds(t, root, "03_old", "--status", "superseded", "--by", "12_new", "--force")
			w := oneWarning(t, env, "supersedes_not_recorded", "low")
			saysAll(t, nil, w.Message, ".specs/12_new", "03", tc.reason)
			if w.Stage != "status" {
				t.Errorf("warning stage %q; want status", w.Stage)
			}
			if !slices.Equal(kinds(r.Changes), []string{"status", "moved"}) {
				t.Errorf("changes %+v; want status and moved, no supersedes", r.Changes)
			}
			if r.Status != "superseded" || r.SpecDir != ".specs/archive/03_old" {
				t.Errorf("status %q, spec_dir %q; want superseded under archive/", r.Status, r.SpecDir)
			}
			if !maps.Equal(before, treeBytes(t, replacing)) {
				t.Error("a file of the replacing package changed")
			}
		})
	}
}

// A move-only supersede call skips the banner and the status, adds the id the
// replacing package lacks, and moves the package: nothing of the old one is
// rewritten. When the replacing package has the id already, it only moves, and
// says nothing of a record that was not needed.
func TestStatus_AMoveOnlySupersedeCallRecordsOnlyAMissingID(t *testing.T) {
	for name, tc := range map[string]struct {
		recorded bool
		changes  []string
	}{
		"the id missing":  {false, []string{"supersedes", "moved"}},
		"the id recorded": {true, []string{"moved"}},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			old := packageWith(t, root, "03_old", "03", "old", "superseded")
			replacing := addPackage(t, root, "12_new", "12", "new")
			if tc.recorded {
				editFile(t, filepath.Join(replacing, "prd.md"), "schema_version: 2", "schema_version: 2\nsupersedes: [\"03\"]")
			}
			oldBefore, replacingBefore := treeBytes(t, old), treeBytes(t, replacing)

			env, r := succeeds(t, root, "03_old", "--status", "superseded", "--by", "12_new")
			if !slices.Equal(kinds(r.Changes), tc.changes) || len(env.Warnings) != 0 {
				t.Errorf("changes %+v, warnings %v; want %v and no warning", r.Changes, warningCodes(env), tc.changes)
			}
			if after := treeBytes(t, pkgDir(root, "archive/03_old")); !maps.Equal(oldBefore, after) {
				t.Error("a file of the old package differs after the move")
			}
			if got := supersedesOf(t, replacing); !slices.Equal(got, []string{"03"}) {
				t.Errorf("supersedes of the replacing package = %v; want [03]", got)
			}
			if tc.recorded && !maps.Equal(replacingBefore, treeBytes(t, replacing)) {
				t.Error("the replacing package was rewritten")
			}
		})
	}
}

// However many times the call is made, and wherever an interrupted move left
// it, the old id is in the replacing package's supersedes once, after the ids
// that were there, which keep their order.
func TestStatus_TheOldIDIsInTheReplacingPackageExactlyOnce(t *testing.T) {
	for _, tc := range []struct {
		initial string
		want    []string
	}{
		{"", []string{"03"}},
		{`["01"]`, []string{"01", "03"}},
		{`["01", "02"]`, []string{"01", "02", "03"}},
		{`["05", "01"]`, []string{"05", "01", "03"}},
		{`["02", "03", "05"]`, []string{"02", "03", "05"}},
	} {
		for _, interrupted := range []bool{false, true} {
			root := newRepo(t)
			packageWith(t, root, "03_old", "03", "old", "sealed")
			replacing := addPackage(t, root, "12_new", "12", "new")
			if tc.initial != "" {
				editFile(t, filepath.Join(replacing, "prd.md"), "schema_version: 2", "schema_version: 2\nsupersedes: "+tc.initial)
			}
			if interrupted {
				// The status and the record are written; the move is not.
				readOnly(t, filepath.Join(root, ".specs"))
				run(t, "--dir", root, ".specs/03_old", "--status", "superseded", "--by", "12_new")
				writable(t, filepath.Join(root, ".specs"))
			}
			// Each call finds the package where the one before left it, so the
			// state after the k-th call is that of k calls on a fresh repository.
			for calls := 1; calls <= 5; calls++ {
				path := ".specs/03_old"
				if exists(pkgDir(root, "archive/03_old")) {
					path = ".specs/archive/03_old"
				}
				run(t, "--dir", root, path, "--status", "superseded", "--by", "12_new")
				if got := supersedesOf(t, replacing); !slices.Equal(got, tc.want) {
					t.Errorf("starting from %q, interrupted %v, %d calls: supersedes = %v; want %v", tc.initial, interrupted, calls, got, tc.want)
				}
				if !exists(pkgDir(root, "archive/03_old")) || statusOnDisk(t, pkgDir(root, "archive/03_old")) != "superseded" {
					t.Errorf("starting from %q, interrupted %v, %d calls: the old package is not superseded under archive/", tc.initial, interrupted, calls)
				}
			}
		}
	}
}
