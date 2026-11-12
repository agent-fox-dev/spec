package specrun

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Whatever a session prints, stdout holds one envelope, and stderr one line
// per step.
func TestMain_WritesOneEnvelopeAndOneLinePerStep(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.plan(t, map[string]any{"events": 200, "actions": []string{"land"}, "result": "done"})
	var stdout, stderr bytes.Buffer
	if code := Main(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr.String())
	}
	dec := json.NewDecoder(&stdout)
	var env map[string]any
	if err := dec.Decode(&env); err != nil || env["tool"] != "specrun" || dec.More() {
		t.Fatalf("stdout is not one specrun envelope: %v %v", err, env)
	}
	var steps []string
	for _, line := range strings.Split(strings.TrimSpace(stderr.String()), "\n") {
		if strings.Contains(line, "implement-01_a") || strings.Contains(line, "implement 01_a") {
			steps = append(steps, line)
		}
	}
	if len(steps) != 1 {
		t.Errorf("stderr names the step on %d lines; want one:\n%s", len(steps), stderr.String())
	}
}

// An operator lands every open package with one call: each package's session
// runs in dependency order, each landing is checked and recorded, and the
// call ends with every package done.
func TestMain_LandsEveryOpenPackageWithOneCall(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending", "01")
	f.plan(t, land(0))
	env, code, stderr := runMain(t)
	if code != 0 || env.Result == nil || !env.Result.AllDone || len(env.Result.Landed) != 2 {
		t.Fatalf("code %d, result %+v, error %+v\n%s", code, env.Result, env.Error, stderr)
	}
	for _, dir := range []string{"01_a", "02_b"} {
		b, err := os.ReadFile(filepath.Join(f.root, ".specs", dir, "progress.json"))
		if err != nil || strings.Contains(string(b), `"pending"`) || strings.Contains(string(b), `"in_progress"`) {
			t.Errorf("%s still has an open task: %s", dir, b)
		}
		if log := git(t, f.root, "log", "--merges", "--oneline", "main"); !strings.Contains(log, "feature/"+dir[:2]) {
			t.Errorf("no merge of %s on main:\n%s", dir, log)
		}
	}
	if got := outcomes(f.ledgerLines(t)); strings.Join(got, ",") != "landed,landed" {
		t.Errorf("ledger outcomes %v", got)
	}
	entries, _ := os.ReadDir(f.logDir())
	var logs []string
	for _, e := range entries {
		if e.Name() != "ledger.jsonl" && (strings.HasSuffix(e.Name(), ".jsonl") || strings.HasSuffix(e.Name(), ".log")) {
			logs = append(logs, e.Name())
		}
	}
	if len(logs) != 4 {
		t.Errorf("the log directory holds %v; want a .jsonl and a .log per step", logs)
	}
	b, _ := os.ReadFile(f.statePath())
	var s struct {
		Running any `json:"running"`
	}
	if err := json.Unmarshal(b, &s); err != nil || s.Running != nil {
		t.Errorf("the state %s still records a running step (%v)", b, err)
	}
}

// An operator restarts specrun after a session stalled: the stalled session is
// ended with what it started, and the next call takes up the same package,
// whose branch the stalled session left, and lands it.
func TestMain_TakesUpAStalledPackageWhenStartedAgain(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending")
	pids := filepath.Join(f.scratch, "pids")
	t.Setenv("SPECRUN_FAKE_PIDS", pids)
	f.plan(t, map[string]any{"events": 1, "actions": []string{"branch", "done_one"}, "child": true, "silence_s": 60},
		map[string]any{"events": 1, "actions": []string{"checkout", "done", "merge"}})

	if _, code, stderr := runMain(t, "--stall", "300ms"); code != 1 {
		t.Fatalf("the first call: code %d\n%s", code, stderr)
	}
	if got := outcomes(f.ledgerLines(t)); strings.Join(got, ",") != "stalled" {
		t.Fatalf("after the first call the ledger is %v", got)
	}
	if alive := pidsAlive(t, pids); len(alive) != 0 {
		t.Errorf("processes %v of the stalled session are alive", alive)
	}

	env, code, stderr := runMain(t, "--max", "1")
	if code != 0 || env.Result == nil || strings.Join(env.Result.Landed, ",") != "01_a" {
		t.Fatalf("the second call: code %d, result %+v, error %+v\n%s", code, env.Result, env.Error, stderr)
	}
	if got := specDirs(f.starts(t)); strings.Join(got, ",") != ".specs/01_a,.specs/01_a" {
		t.Errorf("sessions for %v; want the same package twice", got)
	}
	if got := outcomes(f.ledgerLines(t)); strings.Join(got, ",") != "stalled,landed" {
		t.Errorf("the ledger is %v", got)
	}
}
