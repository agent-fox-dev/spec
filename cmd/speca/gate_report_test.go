//go:build unix

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
)

// twoUnits is a body for an all_tests script that prints two Go result lines.
const twoUnits = `printf 'ok  \tpkg/slow\t0.200s\nok  \tpkg/fast\t0.100s\n'`

// reportRepo is a repository with two packages, 05_gate and 06_two, whose
// all_tests fails while the repository holds a file called broken, and prints
// two Go result lines.
func reportRepo(t *testing.T) *gateRepo {
	t.Helper()
	g := newGateDir(t)
	addPackage(t, g.root, "06_two", "06", "two")
	linter := g.script(t, "linter", "")
	allTests := g.script(t, "all_tests", `if [ -f broken ]; then exit 1; fi; `+twoUnits)
	g.setCommands(t, linter, allTests)
	setCommandsOf(t, pkgDir(g.root, "06_two"), linter, allTests)
	gitInit(t, g.root)
	return g
}

// The report counts the runs of this package at the commit and at its
// descendants on this branch, and while a merge is uncommitted on the branch
// being merged; a run of another package, a gate that could not run and a run
// elsewhere are not counted.
func TestGateReport_CountsThePackagesRunsSinceTheCommit(t *testing.T) {
	g := reportRepo(t)
	base := git(t, g.root, "rev-parse", "HEAD")
	var full []float64

	full = append(full, g.gateOf(t, "05", "full").Result.Gate.Seconds) // B
	g.gateOf(t, "06", "full")                                          // B, another package
	git(t, g.root, "switch", "-q", "-c", "other")
	writeFile(t, filepath.Join(g.root, "x.txt"), "x")
	g.commit(t, "X")
	g.gateOf(t, "05", "full") // X, outside the range
	git(t, g.root, "switch", "-q", "-c", "feature", base)
	writeFile(t, filepath.Join(g.root, "f1.txt"), "f1")
	g.commit(t, "F1")
	g.gateOf(t, "05", "task")
	if g.gateOf(t, "05", "task").Result.Gate.Reused == nil { // F1, reused
		t.Fatal("the second task gate at F1 was not reused")
	}
	writeFile(t, filepath.Join(g.root, "broken"), "")
	g.commit(t, "F2")
	f2 := g.gateOf(t, "05", "full").Result.Gate
	if f2.Verdict != "fail" {
		t.Fatalf("the gate at F2: %q", f2.Verdict)
	}
	full = append(full, f2.Seconds)
	if err := os.Remove(filepath.Join(g.root, "broken")); err != nil {
		t.Fatal(err)
	}
	g.commit(t, "F3")
	// A gate that could not run, at F3, before the pass there that it would
	// otherwise reuse.
	linter := filepath.Join(g.bin, "linter")
	if err := os.Rename(linter, linter+".away"); err != nil {
		t.Fatal(err)
	}
	if v := g.gateOf(t, "05", "full").Result.Gate.Verdict; v != "not_run" {
		t.Fatalf("the gate with no linter: %q", v)
	}
	if err := os.Rename(linter+".away", linter); err != nil {
		t.Fatal(err)
	}
	f3 := g.gateOf(t, "05", "full").Result.Gate
	full = append(full, f3.Seconds)

	env, _ := g.report(t, "05", "--since", base)
	r := env.Result.GateReport
	if r.Since != base || r.Full.Runs != 3 || r.Full.Reused != 0 || r.Task.Runs != 2 || r.Task.Reused != 1 {
		t.Errorf("report %+v; want since %s, 3 full runs with none reused, 2 task runs with one reused", r, base)
	}
	if want := full[0] + full[1] + full[2]; r.Full.Seconds-want > 0.002 || want-r.Full.Seconds > 0.002 {
		t.Errorf("full.seconds %v; want %v", r.Full.Seconds, want)
	}
	if r.BaselineFullSeconds == nil || *r.BaselineFullSeconds != full[0] || r.LastFullSeconds == nil || *r.LastFullSeconds != f3.Seconds {
		t.Errorf("baseline %v, last %v; want %v and %v", r.BaselineFullSeconds, r.LastFullSeconds, full[0], f3.Seconds)
	}

	// The merged tree, before the merge is committed: its tree is F3's, so the
	// gate is reused, and the runs on the branch count through MERGE_HEAD.
	git(t, g.root, "switch", "-q", "main")
	git(t, g.root, "merge", "-q", "--no-ff", "--no-commit", "feature")
	if merged := g.gateOf(t, "05", "full").Result.Gate; merged.Reused == nil {
		t.Fatalf("the gate on the merged tree was not reused: %+v", merged)
	}
	env, _ = g.report(t, "05", "--since", base)
	r = env.Result.GateReport
	if r.Full.Runs != 4 || r.Full.Reused != 1 || r.Task.Runs != 2 {
		t.Errorf("after the merge: %+v; want 4 full runs with one reused, and 2 task runs", r)
	}
	if r.LastFullSeconds == nil || *r.LastFullSeconds != f3.Seconds {
		t.Errorf("last_full_seconds %v; want the recorded run's %v, not 0", r.LastFullSeconds, f3.Seconds)
	}
	want := []envelope.GateUnit{{Unit: "pkg/slow", Seconds: 0.2}, {Unit: "pkg/fast", Seconds: 0.1}}
	if !slices.Equal(r.Slowest, want) {
		t.Errorf("slowest %+v; want %+v", r.Slowest, want)
	}
}

// With no run to count, every count is zero, the durations are absent and
// the list of slowest units is empty; a line of the run log that cannot be
// parsed is skipped.
func TestGateReport_IsZeroWithNoRunToCount(t *testing.T) {
	for name, tc := range map[string]struct {
		set  func(t *testing.T, g *gateRepo)
		runs int
	}{
		"an empty state directory": {func(*testing.T, *gateRepo) {}, 0},
		"a run of another package only": {func(t *testing.T, g *gateRepo) {
			g.gateOf(t, "06", "full")
		}, 0},
		"a line that cannot be parsed, and a run of this package": {func(t *testing.T, g *gateRepo) {
			g.gateOf(t, "05", "full")
			logs, _ := filepath.Glob(filepath.Join(g.state, "agent-fox", "speca", "*", "runs.jsonl"))
			if len(logs) != 1 {
				t.Fatalf("run logs %v; want one", logs)
			}
			b, err := os.ReadFile(logs[0])
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, logs[0], "not json\n"+string(b))
		}, 1},
	} {
		t.Run(name, func(t *testing.T) {
			g := reportRepo(t)
			tc.set(t, g)
			env, members := g.report(t, "05", "--since", "HEAD")
			r := env.Result.GateReport
			if r.Full.Runs != tc.runs || r.Task != (envelope.GateRuns{}) || r.Full.Reused != 0 {
				t.Errorf("report %+v; want %d full runs and nothing else", r, tc.runs)
			}
			if tc.runs > 0 {
				return
			}
			if r.Full != (envelope.GateRuns{}) || string(members["slowest"]) != "[]" {
				t.Errorf("full %+v, slowest %s; want zeros and []", r.Full, members["slowest"])
			}
			for _, k := range []string{"baseline_full_seconds", "last_full_seconds"} {
				if _, ok := members[k]; ok {
					t.Errorf("%s is present: %s", k, members[k])
				}
			}
		})
	}
}

// Without --since the report counts from the merge base of HEAD and the base
// branch: the one origin/HEAD names, else main.
func TestGateReport_CountsFromTheMergeBaseWithTheBaseBranch(t *testing.T) {
	for name, set := range map[string]func(t *testing.T) (root, base string){
		"origin/HEAD names trunk": func(t *testing.T) (string, string) {
			origin := reportRepo(t)
			git(t, origin.root, "branch", "-m", "trunk")
			clone := filepath.Join(t.TempDir(), "clone")
			git(t, origin.root, "clone", "-q", origin.root, clone)
			return clone, "origin/trunk"
		},
		"no remote, a branch main": func(t *testing.T) (string, string) {
			return reportRepo(t).root, "main"
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, base := set(t)
			g := &gateRepo{root: root}
			git(t, root, "switch", "-q", "-c", "feature")
			for _, f := range []string{"a.txt", "b.txt"} {
				writeFile(t, filepath.Join(root, f), f)
				g.commit(t, f)
			}
			env, _ := g.report(t, "05")
			if got, want := env.Result.GateReport.Since, git(t, root, "merge-base", "HEAD", base); got != want {
				t.Errorf("since %s; want the merge base %s", got, want)
			}
		})
	}
}

// A --since that names no commit, no base branch to count from, or a
// directory that is not a git work tree is a usage error.
func TestGateReport_RefusesWhatItCannotCountFrom(t *testing.T) {
	for name, tc := range map[string]struct {
		set  func(t *testing.T) *gateRepo
		args []string
		says string
	}{
		"a --since that names no commit": {reportRepo, []string{"--since", "nosuchref"}, "nosuchref"},
		"no base branch": {func(t *testing.T) *gateRepo {
			g := reportRepo(t)
			git(t, g.root, "branch", "-m", "work")
			return g
		}, nil, "--since"},
		"no git work tree": {newGateDir, []string{"--since", "HEAD"}, "git"},
	} {
		t.Run(name, func(t *testing.T) {
			g := tc.set(t)
			e := refusedAsUsage(t, g.root, append([]string{"--dir", g.root, "05", "--gate-report"}, tc.args...), tc.says)
			if e != nil && !strings.Contains(e.Message, tc.says) {
				t.Errorf("message %q", e.Message)
			}
		})
	}
}
