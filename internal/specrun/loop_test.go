package specrun

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// With nothing open, specrun starts no package: every package is done. The
// one session is the reconcile due when no reconcile was ever recorded
// (docs/errata/20_specrun_driver.md).
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
	if got := prompts(f.starts(t)); strings.Join(got, ",") != "reconcile" {
		t.Errorf("sessions %v; want the reconcile alone", got)
	}
	if _, code, _ := runMain(t); code != 0 || len(f.starts(t)) != 1 {
		t.Errorf("a second call: code %d, %d sessions; want none, a reconcile being the last step", code, len(f.starts(t)))
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
		// The reconcile due when none was ever recorded
		// (docs/errata/20_specrun_driver.md).
		"every package done": {func(f *fixture) {
			f.addPackage(t, "01_a", "01", "done")
		}, "reconcile", 0, true},
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
				if r.NextStep.Kind == "reconcile" {
					next = "reconcile"
				} else if r.NextStep.Kind != "implement" {
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
// The merge commit carries the AF-Run trailer af-impl writes.
func land(code int) map[string]any {
	return map[string]any{"events": 2, "actions": []string{"land"}, "result": "landed", "exit": code, "trailer": minimalTrailer}
}

// minimalTrailer is an AF-Run trailer with nothing counted.
const minimalTrailer = `AF-Run: {"gate":{"full":{"runs":0,"seconds":0,"reused":0},"task":{"runs":0,"seconds":0,"reused":0}},"open_items":{"opened":{},"closed":{},"passed_on":{},"reclassified":{}}}`

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
	want := []string{"-p", "/af-impl .specs/01_a --unattended", "--output-format", "stream-json", "--verbose",
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
			if got := len(specDirs(f.starts(t))); got != want {
				t.Errorf("%d package sessions; want %d", got, want)
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
		"a diverged main, started on another branch": {func(t *testing.T, f *fixture) {
			other := filepath.Join(f.scratch, "other")
			git(t, f.scratch, "clone", "-q", f.origin, other)
			writeFile(t, filepath.Join(other, "README.md"), "theirs\n")
			git(t, other, "commit", "-q", "-am", "theirs")
			git(t, other, "push", "-q")
			writeFile(t, filepath.Join(f.root, "README.md"), "ours\n")
			f.commit(t, "ours")
			git(t, f.root, "switch", "-q", "-c", "elsewhere")
		}, "git pull"},
		"a dirty tree on another branch": {func(t *testing.T, f *fixture) {
			git(t, f.root, "switch", "-q", "-c", "other")
			writeFile(t, filepath.Join(f.root, "README.md"), "other\n")
			f.commit(t, "other")
			writeFile(t, filepath.Join(f.root, "README.md"), "uncommitted\n")
		}, "git switch"},
		"no claude on PATH": {func(t *testing.T, f *fixture) {
			f.setenv("PATH", strings.Join(slices.DeleteFunc(filepath.SplitList(os.Getenv("PATH")), func(d string) bool {
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
			branch := git(t, f.root, "branch", "--show-current")
			env, code, _ := runMain(t)
			if code != 1 || env.Error == nil || env.Error.Stage != "preflight" || !strings.Contains(env.Error.Message, tc.says) {
				t.Fatalf("code %d, error %+v; want stage preflight saying %q", code, env.Error, tc.says)
			}
			if len(f.starts(t)) != 0 {
				t.Error("a session was started")
			}
			if git(t, f.root, "rev-parse", "HEAD") != head || git(t, f.root, "status", "--porcelain") != status ||
				git(t, f.root, "branch", "--show-current") != branch {
				t.Error("the repository changed")
			}
			if exists(filepath.Join(f.state, "agent-fox", "specrun")) {
				t.Error("a state file was written")
			}
		})
	}
}

// Without git on PATH specrun stops before anything, saying so.
func TestLoop_StopsWithoutGit(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.plan(t, land(0))
	f.setenv("PATH", fakeBin(t))
	env, code, _ := runMain(t)
	if code != 1 || env.Error == nil || env.Error.Stage != "preflight" || !strings.Contains(env.Error.Message, "git") {
		t.Errorf("code %d, error %+v; want stage preflight naming git", code, env.Error)
	}
	if len(f.starts(t)) != 0 {
		t.Error("a session was started")
	}
}

// handoffFixture is a repository with 01 finished, handing H-1 to 02, and 02
// open; with taken, 02's first task takes it.
func handoffFixture(t *testing.T, taken bool) *fixture {
	t.Helper()
	f := newFixture(t)
	one := f.addPackage(t, "01_a", "01", "done")
	two := f.addPackage(t, "02_b", "02", "pending", "01")
	editJSON(t, filepath.Join(one, "requirements.json"), func(m map[string]any) {
		m["handoffs"] = []any{map[string]any{"id": "H-1", "what": "after-landing hooks", "to": "02", "criteria": []any{"REQ-1.1"}}}
	})
	if taken {
		editJSON(t, filepath.Join(two, "tasks.json"), func(m map[string]any) {
			m["tasks"].([]any)[0].(map[string]any)["takes_handoffs"] = []any{"01:H-1"}
		})
	}
	f.commit(t, "handoffs")
	git(t, f.root, "push", "-q")
	f.plan(t, land(0))
	return f
}

// With --strict-handoffs a package that leaves an inbound handoff untaken is
// not started: the loop stops, naming it; a dry run says it would.
func TestLoop_StrictHandoffsStopBeforeAPackageThatDoesNotTakeOne(t *testing.T) {
	for name, tc := range map[string]struct {
		taken bool
		argv  []string
		env   map[string]string
		stop  bool
	}{
		"no flag":                      {false, nil, nil, false},
		"--strict-handoffs, untaken":   {false, []string{"--strict-handoffs"}, nil, true},
		"--strict-handoffs, taken":     {true, []string{"--strict-handoffs"}, nil, false},
		"SPECRUN_STRICT_HANDOFFS=true": {false, nil, map[string]string{"SPECRUN_STRICT_HANDOFFS": "true"}, true},
	} {
		t.Run(name, func(t *testing.T) {
			f := handoffFixture(t, tc.taken)
			for k, v := range tc.env {
				f.setenv(k, v)
			}
			env, code, _ := runMain(t, append(tc.argv, "--max", "1")...)
			started := specDirs(f.starts(t))
			if !tc.stop {
				if code != 0 || strings.Join(started, ",") != ".specs/02_b" {
					t.Errorf("code %d, sessions %v; want 02 started", code, started)
				}
				return
			}
			s := env.Result.Stopped
			if code != 1 || len(started) != 0 || s == nil || !strings.HasPrefix(s.Reason, "package does not validate") ||
				!strings.Contains(s.Reason, "handoff_not_taken") || !strings.Contains(s.Reason, "01:H-1") {
				t.Errorf("code %d, sessions %v, stopped %+v; want a stop naming the untaken handoff and no session", code, started, s)
			}
		})
	}

	f := handoffFixture(t, false)
	env, code, _ := runMain(t, "--dry-run", "--strict-handoffs")
	if code != 0 || len(f.starts(t)) != 0 || !slices.ContainsFunc(env.Result.WouldStop, func(s StopIssue) bool {
		return s.SpecDir == ".specs/02_b" && strings.Contains(s.Reason, "01:H-1")
	}) {
		t.Errorf("code %d, would_stop %+v; want 02 named, nothing started", code, env.Result.WouldStop)
	}
}

// A session that ended in an error and left its package unfinished stops the
// loop with the error it ended with, not with the landing check the error
// happened to leave failing; an API error is reported as retryable. A session
// that ended in an error after its package landed has landed.
func TestLoop_StopsWithTheErrorASessionEndedWith(t *testing.T) {
	const cloudflare = "Failed to authenticate. API Error: 403 Attention Required! | Cloudflare"
	for name, tc := range map[string]struct {
		session   map[string]any
		reason    string
		category  string
		retryable bool
	}{
		"an API error on the feature branch": {
			map[string]any{"events": 1, "actions": []string{"branch", "done"}, "result": cloudflare, "is_error": true, "api_status": 403},
			"the session ended with an API error (403): " + cloudflare, "api", true},
		"an API error that changed nothing": {
			map[string]any{"events": 1, "result": "", "is_error": true, "api_status": 529},
			"the session ended with an API error (529)", "api", true},
		"another error, its first line": {
			map[string]any{"events": 1, "actions": []string{"branch", "done"}, "result": "Reached max turns\nmore", "is_error": true},
			"the session ended with an error: Reached max turns", "input", false},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			f.plan(t, tc.session)
			env, code, stderr := runMain(t)
			r := env.Result
			if code != 1 || r == nil || r.Stopped == nil || r.Stopped.Reason != tc.reason {
				t.Fatalf("code %d, result %+v, error %+v; want a stop: %s\n%s", code, r, env.Error, tc.reason, stderr)
			}
			if e := env.Error; e.Stage != "session" || e.Category != tc.category || e.Retryable != tc.retryable {
				t.Errorf("error %+v; want stage session, category %s, retryable %t", e, tc.category, tc.retryable)
			}
		})
	}
	t.Run("an API error after the package landed", func(t *testing.T) {
		f := newFixture(t)
		f.addPackage(t, "01_a", "01", "pending")
		session := land(1)
		session["result"], session["is_error"], session["api_status"] = cloudflare, true, 403
		f.plan(t, session)
		env, code, stderr := runMain(t)
		if code != 0 || env.Result == nil || strings.Join(env.Result.Landed, ",") != "01_a" {
			t.Fatalf("code %d, result %+v, error %+v; want the package landed\n%s", code, env.Result, env.Error, stderr)
		}
	})
}
