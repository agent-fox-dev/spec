package specrun

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

// rewrittenBase is the plan of a session that finishes the package on its
// branch and pushes it while someone replaces main with an unrelated commit,
// so that the session cannot merge; it says so, and stops.
func rewrittenBase() map[string]any {
	return map[string]any{"events": 1, "actions": []string{"branch", "done", "push", "rewrite_base"},
		"result": "01_a is finished and pushed on feature/01-a, but it is not merged.\nmain no longer shares history with origin/main.\n\nDetails follow."}
}

// A step that stops on a landing check is explained from the repository:
// the cause in one sentence, the base branch and the package's branch as git
// shows them, the start of the session's last report with the file that
// holds it, and the steps that continue the work, specrun last.
func TestDiagnose_ExplainsAStoppedStepAndHowToContinue(t *testing.T) {
	for name, tc := range map[string]struct {
		plan     map[string]any
		check    func(t *testing.T, d *Diagnosis)
		cause    []string
		recovery []string // the commands of the steps, in order
	}{
		"main rewritten under a package finished on its branch": {rewrittenBase(), func(t *testing.T, d *Diagnosis) {
			w := d.WorkBranch
			if d.BaseSinceStart != "rewritten" || d.BaseVsOrigin != "unrelated" || !strings.Contains(d.BaseLastMove, "a direct write of the ref") ||
				w == nil || !w.Exists || !w.Pushed || w.Merged || w.TasksDone != w.TasksTotal || w.TasksTotal == 0 {
				t.Errorf("diagnosis %+v, work branch %+v", d, w)
			}
		}, []string{"the base branch main was rewritten during the step", "no history shared with origin/main",
			"01_a is finished on feature/01-a", "pushed) but not merged into main"},
			[]string{"git branch -f main origin/main", "specrun"}},
		"a package left with one task done": {map[string]any{"events": 1, "actions": []string{"branch", "done_one"}}, func(t *testing.T, d *Diagnosis) {
			if w := d.WorkBranch; d.BaseSinceStart != "unchanged" || d.BaseVsOrigin != "same" || w == nil || w.TasksDone != 1 || w.TasksTotal < 2 || w.Pushed {
				t.Errorf("diagnosis %+v, work branch %+v", d, d.WorkBranch)
			}
		}, []string{"01_a has 1 of", "tasks done on feature/01-a"}, []string{"specrun"}},
		"a merge that leaves an untracked file": {map[string]any{"events": 1, "actions": []string{"land", "dirty"}}, func(t *testing.T, d *Diagnosis) {
			if d.Clean || !slices.Equal(d.Uncommitted, []string{"stray.txt"}) || d.BaseSinceStart != "advanced" || !d.WorkBranch.Merged {
				t.Errorf("diagnosis %+v", d)
			}
		}, []string{"the working tree on main has uncommitted changes (stray.txt)"}, []string{"git status", "specrun"}},
		"a session that does nothing": {map[string]any{"events": 1}, func(t *testing.T, d *Diagnosis) {
			if d.WorkBranch == nil || d.WorkBranch.Exists {
				t.Errorf("work branch %+v; want one that does not exist", d.WorkBranch)
			}
		}, []string{"there is no branch feature/01-a"}, []string{"specrun"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			f.plan(t, tc.plan)
			env, code, stderr := runMain(t)
			r := env.Result
			if code != 1 || r.Stopped == nil || r.Diagnosis == nil || env.Error == nil || !env.Error.Resumable {
				t.Fatalf("code %d, result %+v, error %+v; want a resumable stop with a diagnosis", code, r, env.Error)
			}
			tc.check(t, r.Diagnosis)
			for _, want := range tc.cause {
				if !strings.Contains(r.Diagnosis.Cause, want) || !strings.Contains(env.Error.Message, want) || !strings.Contains(stderr, want) {
					t.Errorf("cause %q, message %q; want both, and stderr, to say %q", r.Diagnosis.Cause, env.Error.Message, want)
				}
			}
			var runs []string
			for _, s := range r.Recovery {
				runs = append(runs, s.Run)
				if !strings.Contains(stderr, "$ "+s.Run) {
					t.Errorf("stderr does not show %q:\n%s", s.Run, stderr)
				}
			}
			if !slices.Equal(runs, tc.recovery) || r.Recovery[len(r.Recovery)-1].Decide {
				t.Errorf("recovery %+v; want the commands %v, specrun last and nobody's call", r.Recovery, tc.recovery)
			}
			for _, s := range r.Recovery[:len(r.Recovery)-1] {
				if !s.Decide {
					t.Errorf("recovery step %+v discards something; want it marked as the person's call", s)
				}
			}
			if said, _ := tc.plan["result"].(string); said != "" {
				if want := strings.ReplaceAll(strings.SplitN(said, "\n\n", 2)[0], "\n", " "); r.Stopped.SessionReport != want {
					t.Errorf("session report %q; want %q", r.Stopped.SessionReport, want)
				}
				if b, err := os.ReadFile(r.Stopped.Log); err != nil || string(b) != said {
					t.Errorf("log %s: %q, %v; want the whole report", r.Stopped.Log, b, err)
				}
			}
		})
	}
}

// Doing what the recovery says continues the stopped step: a dry run first
// names the step it takes up and why it stopped, and once main is back on
// origin's, the next start lands the package from its branch.
func TestDiagnose_FollowingTheRecoveryContinuesTheStep(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.plan(t, rewrittenBase(), map[string]any{"events": 1, "actions": []string{"checkout", "merge"}, "trailer": minimalTrailer})
	env, code, _ := runMain(t)
	if code != 1 || env.Result.Recovery == nil {
		t.Fatalf("code %d, result %+v; want a stop with a recovery", code, env.Result)
	}
	recovery := env.Result.Recovery

	dry, code, _ := runMain(t, "--dry-run")
	if n := dry.Result.NextStep; code != 0 || n == nil || !n.Resumes || n.SpecDir != ".specs/01_a" ||
		dry.Result.Diagnosis == nil || !strings.Contains(dry.Summary, "was rewritten") {
		t.Fatalf("dry run: code %d, summary %q, next step %+v; want it to take up 01_a again, saying why", code, dry.Summary, n)
	}
	if len(f.starts(t)) != 1 {
		t.Fatal("the dry run started a session")
	}

	for _, s := range recovery {
		if s.Run == "specrun" {
			continue
		}
		cmd := exec.Command("sh", "-c", s.Run)
		cmd.Dir = f.root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", s.Run, err, out)
		}
	}
	env, code, stderr := runMain(t)
	if code != 0 || !slices.Equal(env.Result.Landed, []string{"01_a"}) || !env.Result.AllDone {
		t.Fatalf("after the recovery: code %d, result %+v, error %+v\n%s", code, env.Result, env.Error, stderr)
	}
	if got := specDirs(f.starts(t)); !slices.Equal(got, []string{".specs/01_a", ".specs/01_a"}) {
		t.Errorf("sessions %v; want 01_a taken up again", got)
	}
}

// While a session runs, a base branch that stops holding the commit the
// session started from is warned about at once, not hours later at the
// merge; a base branch the session only moves forward is not.
func TestDiagnose_WarnsWhileASessionRunsWhenTheBaseIsRewritten(t *testing.T) {
	for name, tc := range map[string]struct {
		plan map[string]any
		warn bool
	}{
		"main rewritten":   {rewrittenBase(), true},
		"main merged into": {map[string]any{"events": 1, "actions": []string{"land"}, "trailer": minimalTrailer}, false},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.baseWatch = 20 * time.Millisecond
			f.addPackage(t, "01_a", "01", "pending")
			tc.plan["silence_s"] = 1 // time for the watch to look
			f.plan(t, tc.plan)
			env, _, stderr := runMain(t)
			var warned []string
			for _, w := range env.Warnings {
				if w.Code == warnBaseRewritten {
					warned = append(warned, w.Message)
				}
			}
			if !tc.warn {
				if len(warned) != 0 || strings.Contains(stderr, "was rewritten") {
					t.Errorf("warnings %v; want none", warned)
				}
				return
			}
			if len(warned) != 1 || !strings.Contains(warned[0], "the base branch main was rewritten") ||
				!strings.Contains(warned[0], "implement-01_a runs") || !strings.Contains(stderr, "warning: "+warned[0]) {
				t.Errorf("warnings %v; want one, on stderr too, naming main and the step\n%s", warned, stderr)
			}
		})
	}
}
