package specrun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	if got := outcomes(f.ledgerLines(t)); strings.Join(got, ",") != "landed,landed,reconciled" {
		t.Errorf("ledger outcomes %v", got)
	}
	entries, _ := os.ReadDir(f.logDir())
	var logs []string
	for _, e := range entries {
		if strings.Contains(e.Name(), "-implement-") && (strings.HasSuffix(e.Name(), ".jsonl") || strings.HasSuffix(e.Name(), ".log")) {
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

// A package lands with one labelled issue, one unlabelled, and a trailer:
// specrun has created the scheme, leaves the labelled issue alone, makes the
// unlabelled one a deviation with a comment, and files the trailer's counts
// in the ledger.
func TestMain_FilesTheIssuesAndTheTrailerOfALandedPackage(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	labelled := f.forge.add("01_a: a minor slip", "open", "af:defect", "af:severity-minor", "af:from-01")
	bare := f.forge.add("01_a: no caller", "open")
	f.plan(t, landWith(`AF-Run: {"gate":{"full":{"runs":3,"seconds":412.5,"reused":1},"task":{"runs":9,"seconds":95,"reused":0}},"open_items":{"opened":{"defect:minor":1,"deviation":1},"closed":{},"passed_on":{},"reclassified":{}}}`))
	if _, code, stderr := runMain(t, "--max", "1"); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	for _, l := range append(slices.Clone(fixedLabels), "af:from-01", "af:awaits-01") {
		if !f.forge.labels[l] {
			t.Errorf("the forge has no %s", l)
		}
	}
	for _, w := range f.forge.writes() {
		if strings.HasPrefix(w, fmt.Sprintf("AddLabels %d ", labelled)) || strings.HasPrefix(w, fmt.Sprintf("RemoveLabel %d ", labelled)) ||
			w == fmt.Sprintf("AddComment %d", labelled) {
			t.Errorf("the labelled issue was written: %s", w)
		}
	}
	base := git(t, f.root, "rev-parse", "main")
	if got := f.forge.labelsOf(bare); strings.Join(got, ",") != "af:deviation,af:from-01" {
		t.Errorf("the unlabelled issue carries %v", got)
	}
	if c := f.forge.comments[bare]; len(c) != 1 || !strings.Contains(c[0], "incomplete-labels") || !strings.Contains(c[0], base) {
		t.Errorf("comments %q", c)
	}
	l := f.ledgerLines(t)[0]
	gate, _ := l["gate"].(map[string]any)
	full, _ := gate["full"].(map[string]any)
	if l["outcome"] != "landed" || full["runs"] != 3.0 || l["open_items"] == nil {
		t.Errorf("ledger line %v", l)
	}
}

// A package lands with a data-loss defect and an item awaiting a package
// that landed long ago: specrun relabels the item, stops without starting the
// next package, and starts it once the person closed the defect.
func TestMain_StopsOnADataLossDefectUntilItIsClosed(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "archive/00_old", "00", "done")
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending")
	defect := f.forge.add("01_a: lost write", "open", "af:defect", "af:severity-data-loss", "af:from-01")
	awaiting := f.forge.add("01_a: wire it in 00", "open", "af:awaits", "af:awaits-00", "af:from-01")
	f.plan(t, land(0))

	env, code, _ := runMain(t)
	s := env.Result.Stopped
	if code != 1 || s == nil || s.Reason != "serious defect" || len(s.Issues) != 1 || s.Issues[0].Number != defect {
		t.Fatalf("the first call: code %d, stopped %+v; want a stop naming #%d", code, s, defect)
	}
	if got := strings.Join(f.forge.labelsOf(awaiting), ","); got != "af:defect,af:from-01,af:severity-correctness" || len(f.forge.comments[awaiting]) != 1 {
		t.Errorf("the awaiting issue: %s, comments %q", got, f.forge.comments[awaiting])
	}
	if got := specDirs(f.starts(t)); strings.Join(got, ",") != ".specs/01_a" {
		t.Errorf("sessions %v; want 01 alone", got)
	}

	f.forge.issue(defect).State = "closed"
	if _, code, stderr := runMain(t, "--max", "1"); code != 0 {
		t.Fatalf("the second call: code %d\n%s", code, stderr)
	}
	if got := specDirs(f.starts(t)); strings.Join(got, ",") != ".specs/01_a,.specs/02_b" {
		t.Errorf("sessions %v; want 02 next", got)
	}
}

// specrun reconciles after a package: the session adopts an old issue and
// closes a resolved one, and specrun closes the deviation a person
// accepted, counts it all and lists the deviation still open.
func TestMain_ReconcilesAfterAPackage(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "archive/00_zero", "00", "done")
	f.addPackage(t, "01_a", "01", "pending")
	old := f.forge.add("00_zero: no caller", "open")
	resolved := f.forge.add("00_zero: wire the cache", "open", "af:awaits", "af:awaits-01", "af:from-00")
	accepted := f.forge.add("00_zero: kept the old order", "open", "af:deviation", "af:accepted", "af:from-00")
	f.forge.issue(accepted).Body = "Recorded in docs/adr/03-x.md."
	f.forge.add("00_zero: renamed a flag", "open", "af:deviation", "af:from-00")
	b, _ := json.Marshal(map[string]any{"runs": []any{land(0)}, "reconcile": []any{map[string]any{"forge": []any{
		map[string]any{"op": "add_labels", "issue": old, "labels": []string{"af:untested", "af:from-00"}},
		map[string]any{"op": "close", "issue": resolved},
	}}}})
	writeFile(t, f.script, string(b))

	env, code, stderr := runMain(t, "--reconcile-every", "1")
	if code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	if got := prompts(f.starts(t)); strings.Join(got, ",") != ".specs/01_a,reconcile" {
		t.Errorf("sessions %v; want the package, then a reconcile", got)
	}
	adopt, _ := filepath.Glob(filepath.Join(f.logDir(), "*-adopt.json"))
	if len(adopt) != 1 {
		t.Fatalf("adopt files %v", adopt)
	}
	report := strings.TrimSuffix(adopt[0], "-adopt.json") + "-report.md"
	if want := "/af-impl --reconcile " + adopt[0] + " --report " + report; f.starts(t)[1].Argv[1] != want || strings.Contains(f.starts(t)[1].Argv[1], "--unattended") {
		t.Errorf("the reconcile's prompt %q; want %q", f.starts(t)[1].Argv[1], want)
	}
	if b, _ := os.ReadFile(adopt[0]); strings.Count(string(b), `"number"`) != 1 || !strings.Contains(string(b), fmt.Sprintf(`"number": %d`, old)) {
		t.Errorf("the adopt file holds %s; want the one old issue", b)
	}
	if got := strings.Join(f.forge.labelsOf(old), ","); got != "af:from-00,af:untested" {
		t.Errorf("the adopted issue carries %s", got)
	}
	for _, w := range f.forge.writes() {
		if strings.Contains(w, fmt.Sprintf(" %d ", old)) || strings.HasSuffix(w, fmt.Sprintf(" %d", old)) {
			t.Errorf("specrun wrote to the adopted issue: %s", w)
		}
	}
	if f.forge.issue(accepted).State != "closed" || !strings.Contains(strings.Join(f.forge.comments[accepted], ""), "docs/adr/03-x.md") {
		t.Errorf("the accepted deviation: %s, %q", f.forge.issue(accepted).State, f.forge.comments[accepted])
	}
	lines := f.ledgerLines(t)
	if l := lines[len(lines)-1]; l["kind"] != "reconcile" || l["adopted"] != 1.0 || l["closed"] != 2.0 || l["settled"] != 1.0 {
		t.Errorf("the reconcile's line %v", l)
	}
	if len(env.Result.OpenDeviations) != 1 {
		t.Errorf("open deviations %+v; want one", env.Result.OpenDeviations)
	}
}

// After a reconcile a defect and an untested item are repaired, each on a
// merge of its own, before the next package.
func TestMain_RepairsADefectAndAnUntestedItemAfterAReconcile(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "archive/00_zero", "00", "done")
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending")
	item := f.forge.add("00_zero: the order is not tested", "open", "af:untested", "af:from-00")
	defect := f.forge.add("00_zero: the order is wrong", "open", "af:defect", "af:severity-correctness", "af:from-00")
	repairPlan(t, f, map[string]any{"merge": true, "outcome": "fixed", "close": true},
		map[string]any{"merge": true, "outcome": "already_fixed", "close": true})

	_, code, stderr := runMain(t, "--reconcile-every", "1", "--max", "2", "--repair-max", "5")
	if code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	want := []string{".specs/01_a", "reconcile", fmt.Sprint("repair ", defect), fmt.Sprint("repair ", item), ".specs/02_b"}
	if got := prompts(f.starts(t)); len(got) < 5 || strings.Join(got[:5], ",") != strings.Join(want, ",") {
		t.Errorf("sessions %v; want %v first", got, want)
	}
	for _, n := range []int{defect, item} {
		if f.forge.issue(n).State != "closed" {
			t.Errorf("issue #%d is %s; want closed", n, f.forge.issue(n).State)
		}
	}
	var got []string
	for _, l := range repairLines(f, t) {
		got = append(got, fmt.Sprint(l["subject"], ":", l["outcome"]))
	}
	if w := fmt.Sprintf("%d:fixed,%d:already_fixed", defect, item); strings.Join(got, ",") != w {
		t.Errorf("repair lines %v; want %s", got, w)
	}
	merges := git(t, f.root, "log", "--merges", "--format=%(trailers:key=AF-Run,valueonly)", "main")
	for _, n := range []int{defect, item} {
		if !strings.Contains(merges, fmt.Sprintf(`"issue":%d`, n)) {
			t.Errorf("no merge commit on main names repair #%d:\n%s", n, merges)
		}
	}
	if s := git(t, f.root, "status", "--porcelain"); s != "" || git(t, f.root, "branch", "--show-current") != "main" {
		t.Errorf("main is not checked out clean: %q", s)
	}
}

// An operator runs the loop with --strict-handoffs: a package that leaves an
// inbound handoff untaken is refused before any session, and once its plan
// takes the handoff it is started.
func TestMain_RefusesAPackageThatLeavesAHandoffUntakenUntilItsPlanTakesIt(t *testing.T) {
	f := handoffFixture(t, false)
	env, code, _ := runMain(t, "--strict-handoffs")
	if code != 1 || env.Result.Stopped == nil || !strings.Contains(env.Result.Stopped.Reason, "01:H-1") || len(f.starts(t)) != 0 {
		t.Fatalf("code %d, stopped %+v, sessions %v; want a stop naming 01:H-1 and no session", code, env.Result.Stopped, f.starts(t))
	}
	editJSON(t, filepath.Join(f.root, ".specs", "02_b", "tasks.json"), func(m map[string]any) {
		m["tasks"].([]any)[0].(map[string]any)["takes_handoffs"] = []any{"01:H-1"}
	})
	f.commit(t, "02 takes the handoff")
	git(t, f.root, "push", "-q")
	if _, code, stderr := runMain(t, "--strict-handoffs", "--max", "1"); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	if got := specDirs(f.starts(t)); strings.Join(got, ",") != ".specs/02_b" {
		t.Errorf("sessions %v; want 02 started", got)
	}
}
