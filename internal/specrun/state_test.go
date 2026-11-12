package specrun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The state is written while a step runs, naming it, its start and the
// process; after the step it is among the steps done. Nothing is written in
// the repository.
func TestState_RecordsTheRunningStepAndThenTheDoneOne(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	copied := filepath.Join(f.scratch, "state-copy.json")
	t.Setenv("SPECRUN_FAKE_STATE_COPY", copied)
	f.plan(t, map[string]any{"events": 1, "actions": []string{"copy_state", "land"}})
	if _, code, stderr := runMain(t); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	var during struct {
		Running *struct {
			Subject   string `json:"subject"`
			StartedAt string `json:"started_at"`
			PID       int    `json:"pid"`
		} `json:"running"`
	}
	b, err := os.ReadFile(copied)
	if err != nil {
		t.Fatalf("the fake claude found no state file: %v", err)
	}
	if err := json.Unmarshal(b, &during); err != nil || during.Running == nil || during.Running.Subject != "01_a" || during.Running.PID != os.Getpid() {
		t.Fatalf("the state during the session: %s (%v)", b, err)
	}
	if _, err := time.Parse(time.RFC3339, during.Running.StartedAt); err != nil {
		t.Errorf("started_at %q: %v", during.Running.StartedAt, err)
	}
	var after struct {
		Done    []map[string]any `json:"done"`
		Running any              `json:"running"`
	}
	b, _ = os.ReadFile(f.statePath())
	if err := json.Unmarshal(b, &after); err != nil || len(after.Done) != 1 || after.Done[0]["subject"] != "01_a" || after.Running != nil {
		t.Errorf("the state after the run: %s (%v)", b, err)
	}
	if status := git(t, f.root, "status", "--porcelain"); status != "" {
		t.Errorf("the repository holds %q", status)
	}
}

// A step recorded as running is checked against the repository before
// anything else: one that landed is counted, one with its branch or nothing
// to tell is run again, and one whose package is gone is dropped.
func TestState_ResumesTheRunningStepFromTheRepository(t *testing.T) {
	for name, tc := range map[string]struct {
		setup  func(t *testing.T, f *fixture, head string)
		first  string
		landed int // ledger lines the resume adds
	}{
		"it landed on main": {func(t *testing.T, f *fixture, head string) {
			git(t, f.root, "switch", "-q", "-c", "feature/01-a")
			done(t, f, "01_a")
			git(t, f.root, "switch", "-q", "main")
			git(t, f.root, "merge", "-q", "--no-ff", "-m", "merge 01", "feature/01-a")
			git(t, f.root, "push", "-q")
		}, ".specs/02_b", 1},
		"its branch exists with a task done": {func(t *testing.T, f *fixture, head string) {
			git(t, f.root, "switch", "-q", "-c", "feature/01-a")
			f.commit(t, "task 1")
			git(t, f.root, "switch", "-q", "main")
		}, ".specs/01_a", 0},
		"no branch and nothing done": {func(*testing.T, *fixture, string) {}, ".specs/01_a", 0},
		"its package is gone": {func(t *testing.T, f *fixture, head string) {
			if err := os.RemoveAll(filepath.Join(f.root, ".specs", "01_a")); err != nil {
				t.Fatal(err)
			}
			f.commit(t, "remove 01")
			git(t, f.root, "push", "-q")
		}, ".specs/02_b", 0},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			f.addPackage(t, "02_b", "02", "pending")
			head := git(t, f.root, "rev-parse", "HEAD")
			f.writeState(t, ".specs/01_a", head, 0)
			tc.setup(t, f, head)
			f.plan(t, land(0))
			env, code, stderr := runMain(t, "--max", "1")
			if code != 0 {
				t.Fatalf("code %d, error %+v\n%s", code, env.Error, stderr)
			}
			if got := specDirs(f.starts(t)); len(got) == 0 || got[0] != tc.first {
				t.Errorf("the first session was for %v; want %s", got, tc.first)
			}
			if got := len(f.ledgerLines(t)); got != tc.landed+1 {
				t.Errorf("%d ledger lines; want %d", got, tc.landed+1)
			}
		})
	}
}

// done marks every task of the package done and commits it.
func done(t *testing.T, f *fixture, dirName string) {
	t.Helper()
	editJSON(t, filepath.Join(f.root, ".specs", dirName, "progress.json"), func(m map[string]any) {
		for _, task := range m["tasks"].([]any) {
			task.(map[string]any)["state"] = "done"
		}
	})
	f.commit(t, "done "+dirName)
}

// --reset-state removes the state file and does nothing else; with none, it
// does nothing at all.
func TestState_ResetRemovesTheFileAndStartsNothing(t *testing.T) {
	for _, withFile := range []bool{true, false} {
		f := newFixture(t)
		f.addPackage(t, "01_a", "01", "pending")
		if withFile {
			f.writeState(t, ".specs/01_a", "abc", 0)
		}
		env, code, _ := runMain(t, "--reset-state")
		if code != 0 || !env.OK || exists(f.statePath()) || len(f.starts(t)) != 0 {
			t.Errorf("with a file %v: code %d, error %+v, file left %v, sessions %d", withFile, code, env.Error, exists(f.statePath()), len(f.starts(t)))
		}
	}
}

// A state file that cannot be parsed, or one whose running step belongs to a
// live process, refuses the start and is left as it is.
func TestState_RefusesToStartOnAStateItCannotTrust(t *testing.T) {
	for name, write := range map[string]func(f *fixture){
		"not json":              func(f *fixture) { writeFile(t, f.statePath(), "not json") },
		"a live process's step": func(f *fixture) { f.writeState(t, ".specs/01_a", "abc", os.Getpid()) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			f.plan(t, land(0))
			write(f)
			before, _ := os.ReadFile(f.statePath())
			env, code, _ := runMain(t)
			if code != 1 || env.Error == nil || env.Error.Stage != "state" || !strings.Contains(env.Error.Message, f.statePath()) {
				t.Fatalf("code %d, error %+v; want a refusal at stage state naming the file", code, env.Error)
			}
			if name == "not json" && !strings.Contains(env.Error.Message, "--reset-state") {
				t.Errorf("message %q does not name --reset-state", env.Error.Message)
			}
			after, _ := os.ReadFile(f.statePath())
			if string(after) != string(before) || len(f.starts(t)) != 0 {
				t.Error("the file changed, or a session started")
			}
		})
	}
}
