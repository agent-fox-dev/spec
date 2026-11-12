package specrun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// With nothing open, specrun ends at once: every package is done.
func TestLoop_EndsWhenEveryPackageIsDone(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "done")
	f.addPackage(t, "02_b", "02", "dropped")
	env, code, _ := runMain(t)
	if code != 0 || !env.OK || env.Result == nil || !env.Result.AllDone || len(env.Result.Landed) != 0 {
		t.Fatalf("code %d, result %+v, error %+v; want every package done", code, env.Result, env.Error)
	}
	if env.Summary != "specrun: every package is done, landed 0" {
		t.Errorf("summary %q", env.Summary)
	}
	if len(f.starts(t)) != 0 {
		t.Error("a session was started")
	}
}

// When packages are open and none can start, specrun stops before any
// session and says why, as speca --next words it.
func TestLoop_StopsWhenNoPackageCanStart(t *testing.T) {
	for name, tc := range map[string]struct {
		setup   func(f *fixture)
		waiting []string
		stage   string
	}{
		"a missing dependency": {func(f *fixture) {
			f.addPackage(t, "02_b", "02", "pending", "07")
		}, []string{".specs/02_b"}, "waiting"},
		"a package that does not load": {func(f *fixture) {
			f.addPackage(t, "01_a", "01", "done")
			writeFile(t, filepath.Join(f.root, ".specs", "03_bad", "prd.md"), "---\nspec_id: [\n---\n")
			f.commit(t, "a broken package")
		}, []string{".specs/03_bad"}, "waiting"},
		"a cycle": {func(f *fixture) {
			f.addPackage(t, "01_a", "01", "pending", "02")
			f.addPackage(t, "02_b", "02", "pending", "01")
		}, nil, "graph"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			tc.setup(f)
			env, code, _ := runMain(t)
			if code != 1 || env.OK || env.Error == nil || env.Error.Stage != tc.stage {
				t.Fatalf("code %d, error %+v; want exit 1 at stage %s", code, env.Error, tc.stage)
			}
			var waiting []string
			if env.Result != nil {
				for _, w := range env.Result.Waiting {
					waiting = append(waiting, w.SpecDir)
					if w.Reason == "" {
						t.Errorf("%s waits with no reason", w.SpecDir)
					}
				}
			}
			if strings.Join(waiting, ",") != strings.Join(tc.waiting, ",") {
				t.Errorf("waiting %v; want %v", waiting, tc.waiting)
			}
			if tc.stage == "graph" && !strings.Contains(env.Error.Message, ".specs/01_a → .specs/02_b → .specs/01_a") {
				t.Errorf("message %q does not name the cycle", env.Error.Message)
			}
			if len(f.starts(t)) != 0 {
				t.Error("a session was started")
			}
		})
	}
}

// A dry run says what the next step would be and changes nothing: no branch
// is switched, nothing is fetched, no session starts, no state or log exists.
func TestLoop_DryRunChangesNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		setup   func(f *fixture)
		next    string
		waiting int
		done    bool
	}{
		"an open package": {func(f *fixture) {
			f.addPackage(t, "01_a", "01", "done")
			f.addPackage(t, "02_b", "02", "pending", "01")
		}, ".specs/02_b", 0, false},
		"nothing can start": {func(f *fixture) {
			f.addPackage(t, "02_b", "02", "pending", "07")
		}, "", 1, false},
		"every package done": {func(f *fixture) {
			f.addPackage(t, "01_a", "01", "done")
		}, "", 0, true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			tc.setup(f)
			// origin moves on, and the dry run starts on another branch.
			other := filepath.Join(f.scratch, "other")
			git(t, f.scratch, "clone", "-q", f.origin, other)
			writeFile(t, filepath.Join(other, "later.txt"), "later")
			git(t, other, "add", "-A")
			git(t, other, "commit", "-q", "-m", "later")
			git(t, other, "push", "-q")
			git(t, f.root, "switch", "-q", "-c", "feature/elsewhere")
			originBefore := git(t, f.root, "rev-parse", "refs/remotes/origin/main")

			env, code, _ := runMain(t, "--dry-run")
			r := env.Result
			if code != 0 || !env.OK || r == nil || !r.DryRun {
				t.Fatalf("code %d, result %+v, error %+v; want a dry run", code, r, env.Error)
			}
			next := ""
			if r.NextStep != nil {
				next = r.NextStep.SpecDir
				if r.NextStep.Kind != "implement" {
					t.Errorf("next step kind %q", r.NextStep.Kind)
				}
			}
			if next != tc.next || len(r.Waiting) != tc.waiting || r.AllDone != tc.done {
				t.Errorf("next %q, waiting %d, all done %v; want %q, %d, %v", next, len(r.Waiting), r.AllDone, tc.next, tc.waiting, tc.done)
			}
			if b := git(t, f.root, "branch", "--show-current"); b != "feature/elsewhere" {
				t.Errorf("the branch is %s", b)
			}
			if after := git(t, f.root, "rev-parse", "refs/remotes/origin/main"); after != originBefore {
				t.Error("origin was fetched")
			}
			if len(f.starts(t)) != 0 {
				t.Error("a session was started")
			}
			for _, d := range []string{filepath.Join(f.state, "agent-fox", "specrun"), filepath.Join(f.cache, "agent-fox", "specrun")} {
				if exists(d) {
					entries, _ := os.ReadDir(d)
					t.Errorf("%s exists, holding %v", d, entries)
				}
			}
		})
	}
}
