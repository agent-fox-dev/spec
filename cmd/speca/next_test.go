package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// finished gives the package a plan with nothing left to implement.
func finished(t *testing.T, dir string) string {
	t.Helper()
	setTasks(t, dir, planTask(1, "done", nil), planTask(2, "dropped", nil), integrationTask("done"))
	return dir
}

// open gives the package a plan with a task left to implement.
func open(t *testing.T, dir string) string {
	t.Helper()
	setTasks(t, dir, planTask(1, "done", nil), planTask(2, "pending", nil))
	return dir
}

// The next package is the first open one in dependency order, ties broken by
// number; a dependency is finished when its package is sealed, under archive/
// or has no task open. all_done is true only when nothing is open and every
// package can be read, and waiting says what cannot start and why.
func TestNext_ChoosesTheFirstOpenPackageWhoseDependenciesAreFinished(t *testing.T) {
	for name, tc := range map[string]struct {
		setup   func(t *testing.T, root string)
		next    string
		allDone bool
		waiting []string
		summary string
	}{
		"the first open package by number": {
			setup: func(t *testing.T, root string) {
				finished(t, addPackage(t, root, "01_a", "01", "a"))
				open(t, addPackage(t, root, "02_b", "02", "b"))
				open(t, addPackage(t, root, "03_c", "03", "c"))
			},
			next: ".specs/02_b", summary: "speca: next is .specs/02_b",
		},
		"a dependency before its dependent, whatever their numbers": {
			setup: func(t *testing.T, root string) {
				a := open(t, addPackage(t, root, "01_a", "01", "a"))
				setDependencies(t, a, "03")
				finished(t, addPackage(t, root, "02_b", "02", "b"))
				open(t, addPackage(t, root, "03_c", "03", "c"))
			},
			next: ".specs/03_c", waiting: []string{".specs/01_a"}, summary: "speca: next is .specs/03_c; 1 more waiting",
		},
		"dependencies sealed, under archive/ or with no task open": {
			setup: func(t *testing.T, root string) {
				a := open(t, addPackage(t, root, "01_a", "01", "a"))
				setDependencies(t, a, "02", "03", "05")
				open(t, packageWith(t, root, "02_b", "02", "b", "sealed"))
				finished(t, packageWith(t, root, "03_c", "03", "c", "active"))
				open(t, addPackage(t, root, "archive/05_e", "05", "e"))
			},
			next: ".specs/01_a", summary: "speca: next is .specs/01_a",
		},
		"a task in progress keeps a package open": {
			setup: func(t *testing.T, root string) {
				setTasks(t, addPackage(t, root, "01_a", "01", "a"), planTask(1, "done", nil), planTask(2, "in_progress", nil))
			},
			next: ".specs/01_a", summary: "speca: next is .specs/01_a",
		},
		"everything finished": {
			setup: func(t *testing.T, root string) {
				finished(t, addPackage(t, root, "01_a", "01", "a"))
				open(t, packageWith(t, root, "02_b", "02", "b", "sealed"))
			},
			allDone: true, summary: "speca: every package is done; nothing is left to implement",
		},
		"an empty spec root": {
			setup:   func(t *testing.T, root string) { mustMkdir(t, filepath.Join(root, ".specs")) },
			allDone: true, summary: "speca: every package is done; nothing is left to implement",
		},
		"each open package waits on one that is not finished": {
			setup: func(t *testing.T, root string) {
				setDependencies(t, open(t, addPackage(t, root, "01_a", "01", "a")), "02")
				setDependencies(t, open(t, addPackage(t, root, "02_b", "02", "b")), "42")
			},
			waiting: []string{".specs/01_a", ".specs/02_b"}, summary: "speca: no package can start; 2 waiting",
		},
		"a package that does not load is not done": {
			setup: func(t *testing.T, root string) {
				finished(t, addPackage(t, root, "01_a", "01", "a"))
				writeFile(t, filepath.Join(addPackage(t, root, "02_bad", "02", "bad"), "prd.md"), "---\nspec_id: [unclosed\n---\n")
			},
			waiting: []string{".specs/02_bad"}, summary: "speca: no package can start; 1 waiting",
		},
		"a version 2 package is not done, and blocks nothing else": {
			setup: func(t *testing.T, root string) {
				open(t, addPackage(t, root, "01_a", "01", "a"))
				addLegacyPackage(t, root)
			},
			next: ".specs/01_a", waiting: []string{".specs/07_legacy"}, summary: "speca: next is .specs/01_a; 1 more waiting",
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			tc.setup(t, root)
			env, raw, code, _ := runRaw(t, "--dir", root, "--next")
			if code != cli.ExitOK || env.Result == nil {
				t.Fatalf("exit %d: %+v", code, env.Error)
			}
			r := env.Result
			got := ""
			if r.NextSpec != nil {
				got = filepath.ToSlash(r.NextSpec.SpecDir)
			}
			if got != tc.next {
				t.Errorf("next_spec %q; want %q", got, tc.next)
			}
			if r.AllDone == nil || *r.AllDone != tc.allDone {
				t.Errorf("all_done %v; want %v", r.AllDone, tc.allDone)
			}
			if _, ok := raw["waiting"]; !ok {
				t.Error("waiting is not written")
			}
			var waiting []string
			for _, w := range r.Waiting {
				waiting = append(waiting, filepath.ToSlash(w.SpecDir))
			}
			if !slices.Equal(waiting, tc.waiting) {
				t.Errorf("waiting %v; want %v", waiting, tc.waiting)
			}
			if env.Summary != tc.summary {
				t.Errorf("summary %q; want %q", env.Summary, tc.summary)
			}
			want := []envelope.Next(nil)
			if tc.next != "" {
				want = []envelope.Next{{Tool: "speca", Input: tc.next, Flags: []string{}}}
			}
			if len(env.Next) != len(want) || (len(want) > 0 && !sameCall(env.Next[0], want[0])) {
				t.Errorf("next %+v; want %+v", env.Next, want)
			}
		})
	}
}

// A package that waits names the dependencies that are not finished, and says
// of each whether its package is open or missing altogether.
func TestNext_SaysWhatAPackageWaitsOn(t *testing.T) {
	root := t.TempDir()
	setDependencies(t, open(t, addPackage(t, root, "01_a", "01", "a")), "42", "02", "03")
	open(t, addPackage(t, root, "02_b", "02", "b"))
	finished(t, addPackage(t, root, "03_c", "03", "c"))

	env, _, _ := run(t, "--dir", root, "--next")
	if env.Result == nil || len(env.Result.Waiting) != 1 {
		t.Fatalf("result %+v: %+v", env.Result, env.Error)
	}
	w := env.Result.Waiting[0]
	if w.SpecID != "01" || !slices.Equal(w.WaitsOn, []string{"42", "02"}) {
		t.Errorf("waiting entry %+v; want spec 01 waiting on 42 and 02", w)
	}
	for _, part := range []string{"42 is no package under the spec root or archive/", "02 is not finished"} {
		if !strings.Contains(w.Reason, part) {
			t.Errorf("reason %q does not say %q", w.Reason, part)
		}
	}
}

// Packages that depend on one another in a cycle fail the call, whatever their
// state, and the message names the cycle in order.
func TestNext_RefusesADependencyCycle(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(t *testing.T, root string)
		cycle string
	}{
		"two packages": {
			setup: func(t *testing.T, root string) {
				setDependencies(t, open(t, addPackage(t, root, "01_a", "01", "a")), "02")
				setDependencies(t, open(t, addPackage(t, root, "02_b", "02", "b")), "01")
			},
			cycle: ".specs/01_a → .specs/02_b → .specs/01_a",
		},
		"a package on itself": {
			setup: func(t *testing.T, root string) {
				setDependencies(t, open(t, addPackage(t, root, "01_a", "01", "a")), "01")
			},
			cycle: ".specs/01_a → .specs/01_a",
		},
		"a cycle behind a package that is not in it": {
			setup: func(t *testing.T, root string) {
				setDependencies(t, open(t, addPackage(t, root, "01_a", "01", "a")), "02")
				setDependencies(t, open(t, addPackage(t, root, "02_b", "02", "b")), "03")
				setDependencies(t, open(t, addPackage(t, root, "03_c", "03", "c")), "02")
			},
			cycle: ".specs/02_b → .specs/03_c → .specs/02_b",
		},
		"finished packages": {
			setup: func(t *testing.T, root string) {
				setDependencies(t, packageWith(t, root, "01_a", "01", "a", "sealed"), "02")
				setDependencies(t, packageWith(t, root, "02_b", "02", "b", "sealed"), "01")
			},
			cycle: ".specs/01_a → .specs/02_b → .specs/01_a",
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			tc.setup(t, root)
			env, code, _ := run(t, "--dir", root, "--next")
			if code != cli.ExitFailed || env.OK || env.Result != nil || env.Error == nil {
				t.Fatalf("exit %d, result %+v, error %+v; want a failure with no result", code, env.Result, env.Error)
			}
			if env.Error.Stage != "graph" || !strings.Contains(filepath.ToSlash(env.Error.Message), tc.cycle) {
				t.Errorf("error %+v; want stage graph naming %s", env.Error, tc.cycle)
			}
		})
	}
}
