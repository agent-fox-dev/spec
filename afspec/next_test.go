package afspec

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// short shows a package directory by its base name, as a caller would show it.
func short(path string) string { return filepath.Base(path) }

// The next package is the first open one in dependency order, ties broken by
// number, whose dependencies are all finished; the open packages that cannot
// start are listed with the dependencies they wait on.
func TestChooseNext_ChoosesTheFirstOpenPackageWhoseDependenciesAreFinished(t *testing.T) {
	for name, tc := range map[string]struct {
		setup   func(root string)
		next    string
		waiting []string
		done    bool
	}{
		"the lowest number": {func(root string) {
			pkgAt(t, root, "01_a", "01", TaskStatePending)
			pkgAt(t, root, "02_b", "02", TaskStatePending)
		}, "01_a", nil, false},
		"a dependency first": {func(root string) {
			pkgAt(t, root, "01_a", "01", TaskStatePending, "02")
			pkgAt(t, root, "02_b", "02", TaskStatePending)
		}, "02_b", []string{"01_a"}, false},
		"a finished dependency": {func(root string) {
			pkgAt(t, root, "01_a", "01", TaskStateDone)
			pkgAt(t, root, "02_b", "02", TaskStatePending, "01")
		}, "02_b", nil, false},
		"a dependency under archive/": {func(root string) {
			pkgAt(t, root, "archive/01_a", "01", TaskStatePending)
			pkgAt(t, root, "02_b", "02", TaskStatePending, "01")
		}, "02_b", nil, false},
		"nothing open": {func(root string) {
			pkgAt(t, root, "01_a", "01", TaskStateDone)
			pkgAt(t, root, "02_b", "02", TaskStateDropped)
		}, "", nil, true},
		"a package that does not load": {func(root string) {
			pkgAt(t, root, "01_a", "01", TaskStateDone)
			copyDir(t, "testdata/malformed_json", filepath.Join(root, "02_bad"))
		}, "", []string{"02_bad"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			tc.setup(root)
			c, err := ChooseNext(root, short)
			if err != nil {
				t.Fatalf("ChooseNext = %v", err)
			}
			got := ""
			if c.Next != nil {
				got = c.Next.Dir
				if c.Next.Spec == nil {
					t.Error("the next package has no spec")
				}
			}
			var waiting []string
			for _, w := range c.Waiting {
				waiting = append(waiting, w.Dir)
				if w.Reason == "" {
					t.Errorf("%s waits with no reason", w.Dir)
				}
			}
			if got != tc.next || !slices.Equal(waiting, tc.waiting) || c.AllDone != tc.done {
				t.Errorf("next %q, waiting %v, all done %v; want %q, %v, %v", got, waiting, c.AllDone, tc.next, tc.waiting, tc.done)
			}
		})
	}
}

// A waiting package names each dependency that is not finished, and says of
// each whether its package is open or missing.
func TestChooseNext_SaysWhatAPackageWaitsOn(t *testing.T) {
	root := t.TempDir()
	pkgAt(t, root, "01_a", "01", TaskStatePending, "42", "02", "03")
	pkgAt(t, root, "02_b", "02", TaskStatePending)
	pkgAt(t, root, "03_c", "03", TaskStateDone)
	c, err := ChooseNext(root, short)
	if err != nil || len(c.Waiting) != 1 {
		t.Fatalf("ChooseNext = %+v, %v", c, err)
	}
	w := c.Waiting[0]
	if w.SpecID != "01" || !slices.Equal(w.WaitsOn, []string{"42", "02"}) {
		t.Errorf("waiting %+v; want 01 waiting on 42 and 02", w)
	}
	for _, part := range []string{"42 is no package under the spec root or archive/", "02 is not finished"} {
		if !strings.Contains(w.Reason, part) {
			t.Errorf("reason %q does not say %q", w.Reason, part)
		}
	}
}

// Packages that depend on one another in a cycle are an error that names the
// cycle in order, each package depending on the next.
func TestChooseNext_RefusesADependencyCycle(t *testing.T) {
	root := t.TempDir()
	pkgAt(t, root, "01_a", "01", TaskStatePending, "02")
	pkgAt(t, root, "02_b", "02", TaskStatePending, "03")
	pkgAt(t, root, "03_c", "03", TaskStatePending, "02")
	_, err := ChooseNext(root, short)
	var cycle *CycleError
	if !errors.As(err, &cycle) || !slices.Equal(cycle.Dirs, []string{"02_b", "03_c", "02_b"}) {
		t.Fatalf("ChooseNext = %v; want a cycle 02_b → 03_c → 02_b", err)
	}
	if !strings.Contains(cycle.Error(), "02_b → 03_c → 02_b") {
		t.Errorf("message %q does not name the cycle", cycle.Error())
	}
}

// A spec root that cannot be read is an error of its own; an archive that
// cannot be read is reported beside the choice.
func TestChooseNext_TellsAnUnreadableRootFromAnUnreadableArchive(t *testing.T) {
	_, err := ChooseNext(filepath.Join(t.TempDir(), "missing"), short)
	var rootErr *SpecRootError
	if !errors.As(err, &rootErr) {
		t.Errorf("ChooseNext on a missing root = %v; want a SpecRootError", err)
	}

	root := t.TempDir()
	pkgAt(t, root, "01_a", "01", TaskStatePending)
	if err := os.WriteFile(filepath.Join(root, "archive"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := ChooseNext(root, short)
	if err != nil || c.ArchiveErr == nil || c.Next == nil || c.Next.Dir != "01_a" {
		t.Errorf("ChooseNext = %+v, %v; want 01_a with the archive's error beside it", c, err)
	}
}

// The package directories are listed in the order of their numbers, compared
// as numbers.
func TestListSpecDirs_OrdersByNumber(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"100_x", "99_y", "02_b", "notes", "01_a"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ListSpecDirs(root)
	if want := []string{"01_a", "02_b", "99_y", "100_x"}; err != nil || !slices.Equal(got, want) {
		t.Errorf("ListSpecDirs = %v, %v; want %v", got, err, want)
	}
}
