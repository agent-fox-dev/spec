package specrun

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// land is a plan for one session that implements its package and merges it
// into main, exiting with code.
func land(code int) map[string]any {
	return map[string]any{"events": 2, "actions": []string{"land"}, "result": "landed", "exit": code}
}

// The loop brings the base branch up to date and starts one session for the
// package afspec names next, with af-impl's command line, in the repository.
func TestLoop_StartsTheSessionForTheNextPackageOnAnUpToDateBase(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending", "01")
	// origin moves on after the clone: the session must start on its commit.
	other := filepath.Join(f.scratch, "other")
	git(t, f.scratch, "clone", "-q", f.origin, other)
	writeFile(t, filepath.Join(other, "later.txt"), "later")
	git(t, other, "add", "-A")
	git(t, other, "commit", "-q", "-m", "later")
	git(t, other, "push", "-q")
	upstream := git(t, other, "rev-parse", "HEAD")
	f.plan(t, land(0))

	env, code, stderr := runMain(t, "--max", "1")
	if code != 0 || !env.OK {
		t.Fatalf("code %d, error %+v\n%s", code, env.Error, stderr)
	}
	starts := f.starts(t)
	if len(starts) != 1 {
		t.Fatalf("%d sessions; want one", len(starts))
	}
	want := []string{"-p", "/af-impl .specs/01_a", "--output-format", "stream-json", "--verbose",
		"--permission-mode", "bypassPermissions", "--disallowedTools", "ScheduleWakeup,CronCreate"}
	if s := starts[0]; strings.Join(s.Argv, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv %q; want %q", s.Argv, want)
	}
	if cwd, _ := filepath.EvalSymlinks(starts[0].Cwd); cwd != realPath(t, f.root) {
		t.Errorf("the session ran in %s; want %s", starts[0].Cwd, f.root)
	}
	if starts[0].WaitMS != "1800000" || starts[0].Head != upstream {
		t.Errorf("wait %q, head %s; want 1800000 and origin's %s", starts[0].WaitMS, starts[0].Head, upstream)
	}
}

// A step lands when the session leaves the base branch checked out and clean,
// HEAD moved and no task open, whatever the session's exit code; the loop then
// goes on to the next package.
func TestLoop_GoesOnWhenAPackageLandedWhateverTheExitCode(t *testing.T) {
	for _, exit := range []int{0, 1} {
		t.Run(fmt.Sprint("exit ", exit), func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			f.addPackage(t, "02_b", "02", "pending", "01")
			f.plan(t, land(exit))
			env, code, stderr := runMain(t)
			if code != 0 || env.Result == nil || strings.Join(env.Result.Landed, ",") != "01_a,02_b" || !env.Result.AllDone {
				t.Fatalf("code %d, result %+v, error %+v\n%s", code, env.Result, env.Error, stderr)
			}
			if got := specDirs(f.starts(t)); strings.Join(got, ",") != ".specs/01_a,.specs/02_b" {
				t.Errorf("sessions for %v", got)
			}
			if env.Summary != "specrun: every package is done, landed 2" {
				t.Errorf("summary %q", env.Summary)
			}
		})
	}
}

// A step that fails a landing check stops the loop, naming the check, and no
// further session starts.
func TestLoop_StopsAtTheFirstLandingCheckThatFails(t *testing.T) {
	for name, tc := range map[string]struct {
		actions []string
		reason  string
	}{
		"stays on the feature branch":         {[]string{"branch", "done"}, "not on the base branch"},
		"merges and leaves an untracked file": {[]string{"land", "dirty"}, "working tree not clean"},
		"changes nothing":                     {nil, "HEAD did not move"},
		"merges with a task left pending":     {[]string{"branch", "leave_pending", "merge"}, "tasks still open"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			f.addPackage(t, "02_b", "02", "pending")
			f.plan(t, map[string]any{"events": 1, "actions": tc.actions, "exit": 0})
			env, code, _ := runMain(t)
			r := env.Result
			if code != 1 || env.OK || r == nil || r.Stopped == nil || r.Stopped.Reason != tc.reason {
				t.Fatalf("code %d, result %+v, error %+v; want a stop: %s", code, r, env.Error, tc.reason)
			}
			if r.Stopped.Kind != "implement" || r.Stopped.Subject != "01_a" || !strings.Contains(r.Stopped.Step, "implement-01_a") {
				t.Errorf("stopped %+v", r.Stopped)
			}
			if n := len(f.starts(t)); n != 1 {
				t.Errorf("%d sessions; want one", n)
			}
		})
	}
}

// --max N ends the loop once N packages have landed in this run.
func TestLoop_EndsAfterMaxPackagesLanded(t *testing.T) {
	for _, n := range []int{1, 2, 5} {
		t.Run(fmt.Sprint("max ", n), func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			f.addPackage(t, "02_b", "02", "pending")
			f.addPackage(t, "03_c", "03", "pending")
			f.plan(t, land(0))
			env, code, _ := runMain(t, "--max", fmt.Sprint(n))
			want := min(n, 3)
			if code != 0 || env.Result == nil || len(env.Result.Landed) != want || env.Result.AllDone != (want == 3) {
				t.Fatalf("code %d, result %+v, error %+v; want %d landed", code, env.Result, env.Error, want)
			}
			if got := len(f.starts(t)); got != want {
				t.Errorf("%d sessions; want %d", got, want)
			}
		})
	}
}

// A base branch that cannot be switched to or pulled, or a missing claude,
// stops specrun before any session and changes nothing.
func TestLoop_StopsBeforeASessionWhenThePreflightFails(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(t *testing.T, f *fixture)
		says  string
	}{
		"a local main that diverged from origin": {func(t *testing.T, f *fixture) {
			other := filepath.Join(f.scratch, "other")
			git(t, f.scratch, "clone", "-q", f.origin, other)
			writeFile(t, filepath.Join(other, "README.md"), "theirs\n")
			git(t, other, "commit", "-q", "-am", "theirs")
			git(t, other, "push", "-q")
			writeFile(t, filepath.Join(f.root, "README.md"), "ours\n")
			f.commit(t, "ours")
		}, "git pull"},
		"a dirty tree on another branch": {func(t *testing.T, f *fixture) {
			git(t, f.root, "switch", "-q", "-c", "other")
			writeFile(t, filepath.Join(f.root, "README.md"), "other\n")
			f.commit(t, "other")
			writeFile(t, filepath.Join(f.root, "README.md"), "uncommitted\n")
		}, "git switch"},
		"no claude on PATH": {func(t *testing.T, f *fixture) {
			t.Setenv("PATH", strings.Join(slices.DeleteFunc(filepath.SplitList(os.Getenv("PATH")), func(d string) bool {
				return exists(filepath.Join(d, "claude"))
			}), string(os.PathListSeparator)))
		}, "claude"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			f.plan(t, land(0))
			tc.setup(t, f)
			head, status := git(t, f.root, "rev-parse", "HEAD"), git(t, f.root, "status", "--porcelain")
			env, code, _ := runMain(t)
			if code != 1 || env.Error == nil || env.Error.Stage != "preflight" || !strings.Contains(env.Error.Message, tc.says) {
				t.Fatalf("code %d, error %+v; want stage preflight saying %q", code, env.Error, tc.says)
			}
			if len(f.starts(t)) != 0 {
				t.Error("a session was started")
			}
			if git(t, f.root, "rev-parse", "HEAD") != head || git(t, f.root, "status", "--porcelain") != status {
				t.Error("the repository changed")
			}
			if exists(filepath.Join(f.state, "agent-fox", "specrun")) {
				t.Error("a state file was written")
			}
		})
	}
}
