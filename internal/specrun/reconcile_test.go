package specrun

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fivePackages adds five open packages, 01 to 05.
func fivePackages(t *testing.T, f *fixture) {
	t.Helper()
	for i := 1; i <= 5; i++ {
		f.addPackage(t, fmt.Sprintf("%02d_p%d", i, i), fmt.Sprintf("%02d", i), "pending")
	}
}

// A reconcile runs after every --reconcile-every landed packages, and once at
// the end unless one was the last step.
func TestReconcile_RunsEveryFewPackagesAndAtTheEnd(t *testing.T) {
	r := "reconcile"
	for every, want := range map[string][]string{
		"1": {"01", r, "02", r, "03", r, "04", r, "05", r},
		"2": {"01", "02", r, "03", "04", r, "05", r},
		"5": {"01", "02", "03", "04", "05", r},
		"0": {"01", "02", "03", "04", "05", r},
	} {
		t.Run("every "+every, func(t *testing.T) {
			f := newFixture(t)
			fivePackages(t, f)
			f.plan(t, land(0))
			if _, code, stderr := runMain(t, "--reconcile-every", every); code != 0 {
				t.Fatalf("code %d\n%s", code, stderr)
			}
			var got []string
			for _, p := range prompts(f.starts(t)) {
				if p != r {
					p = strings.TrimPrefix(p, ".specs/")[:2]
				}
				got = append(got, p)
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("sessions %v; want %v", got, want)
			}
		})
	}
}

// The count since the last reconcile survives a restart, and a reconcile
// recorded as running is the first session after it.
func TestReconcile_ResumesItsCountAndARunningReconcile(t *testing.T) {
	f := newFixture(t)
	fivePackages(t, f)
	f.plan(t, land(0))
	runMain(t, "--max", "1", "--reconcile-every", "2")
	before := len(f.starts(t))
	if _, code, stderr := runMain(t, "--max", "1", "--reconcile-every", "2"); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	if got := prompts(f.starts(t)[before:]); strings.Join(got, ",") != ".specs/02_p2,reconcile" {
		t.Errorf("the second run's sessions %v; want 02, then a reconcile", got)
	}

	g := newFixture(t)
	g.addPackage(t, "01_a", "01", "pending")
	writeFile(t, g.statePath(), `{"done":[],"running":{"kind":"reconcile","subject":"reconcile","started_at":"2026-11-12T08:00:00Z","pid":0}}`)
	g.plan(t, land(0))
	if _, code, stderr := runMain(t, "--max", "1"); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	if got := prompts(g.starts(t)); len(got) == 0 || got[0] != "reconcile" {
		t.Errorf("sessions %v; want the reconcile first", got)
	}
}

// A reconcile hands the session the open issues with no af: label titled
// after a package, and nothing else, in a file the prompt names.
func TestReconcile_HandsTheSessionTheIssuesItMayAdopt(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "archive/00_zero", "00", "done")
	f.addPackage(t, "01_alpha", "01", "pending")
	f.addPackage(t, "02_beta", "02", "pending")
	// 01 lands before the reconcile, and its titled issues are labelled then.
	selected := []int{f.forge.add("02_beta: no caller", "open"), f.forge.add("00_zero: gap", "open", "bug")}
	for _, title := range []string{"alpha: y", "01_alphabet: z", "09_none: q", "Crash on start"} {
		f.forge.add(title, "open")
	}
	f.forge.add("01_alpha: x", "open", "af:defect", "af:severity-minor", "af:from-01")
	f.forge.add("02_beta: w", "closed")
	f.plan(t, land(0))
	if _, code, stderr := runMain(t, "--max", "1", "--reconcile-every", "1"); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	var prompt string
	for _, s := range f.starts(t) {
		if strings.HasPrefix(s.Argv[1], "/af-impl --reconcile ") {
			prompt = s.Argv[1]
		}
	}
	fields := strings.Fields(prompt)
	if len(fields) != 5 || fields[3] != "--report" || !strings.HasPrefix(fields[2], f.logDir()) || !strings.HasPrefix(fields[4], f.logDir()) ||
		!strings.HasSuffix(fields[2], "-adopt.json") || !strings.HasSuffix(fields[4], "-report.md") {
		t.Fatalf("prompt %q", prompt)
	}
	b, err := os.ReadFile(fields[2])
	if err != nil {
		t.Fatal(err)
	}
	var adopt []struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		URL     string `json:"url"`
		Package string `json:"package"`
	}
	if err := json.Unmarshal(b, &adopt); err != nil || len(adopt) != 2 || adopt[0].Number != selected[0] || adopt[0].Package != "02_beta" ||
		adopt[1].Number != selected[1] || adopt[1].Package != "00_zero" || adopt[0].URL == "" {
		t.Errorf("the adopt file %s (%v)", b, err)
	}
	if !exists(strings.TrimSuffix(fields[2], "-adopt.json") + ".jsonl") {
		t.Error("no stream file for the reconcile step")
	}
}

// A reconcile session that moved or dirtied the repository, or stalled,
// stops the loop, and no rule is applied.
func TestReconcile_StopsWhenTheSessionChangedTheRepository(t *testing.T) {
	for name, tc := range map[string]struct {
		plan    map[string]any
		reason  string
		outcome string
	}{
		"commits on main":             {map[string]any{"actions": []string{"commit"}}, "HEAD moved", "stopped"},
		"leaves an untracked file":    {map[string]any{"actions": []string{"dirty"}}, "working tree not clean", "stopped"},
		"ends on another branch":      {map[string]any{"actions": []string{"branch"}}, "not on the base branch", "stopped"},
		"writes nothing for too long": {map[string]any{"silence_s": 30}, "stalled", "stalled"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			f.addPackage(t, "02_b", "02", "pending")
			n := f.forge.add("02_b: settled by a person", "open", "af:deviation", "af:accepted", "af:from-02")
			b, _ := json.Marshal(map[string]any{"runs": []any{land(0)}, "reconcile": []any{tc.plan}})
			writeFile(t, f.script, string(b))
			env, code, _ := runMain(t, "--reconcile-every", "1", "--stall", "2s")
			if code != 1 || env.Result.Stopped == nil || env.Result.Stopped.Reason != tc.reason {
				t.Fatalf("code %d, stopped %+v; want %q", code, env.Result.Stopped, tc.reason)
			}
			lines := f.ledgerLines(t)
			if last := lines[len(lines)-1]; last["kind"] != "reconcile" || last["outcome"] != tc.outcome {
				t.Errorf("last ledger line %v", last)
			}
			for _, w := range f.forge.writes() {
				if strings.HasSuffix(w, fmt.Sprintf(" %d", n)) || strings.Contains(w, fmt.Sprintf(" %d ", n)) {
					t.Errorf("a rule ran after the failed session: %s", w)
				}
			}
			if got := specDirs(f.starts(t)); strings.Join(got, ",") != ".specs/01_a" {
				t.Errorf("package sessions %v", got)
			}
		})
	}
}

// A reconcile session that wrote no report is a warning, and the reconcile
// goes on.
func TestReconcile_WarnsWhenTheReportIsMissing(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	b, _ := json.Marshal(map[string]any{"runs": []any{land(0)}, "reconcile": []any{map[string]any{"no_report": true}}})
	writeFile(t, f.script, string(b))
	env, code, stderr := runMain(t)
	if code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	var codes []string
	for _, w := range env.Warnings {
		codes = append(codes, string(w.Code))
	}
	if strings.Join(codes, ",") != "reconcile_report_missing" || !strings.Contains(env.Warnings[0].Message, f.logDir()) {
		t.Errorf("warnings %+v", env.Warnings)
	}
	lines := f.ledgerLines(t)
	if last := lines[len(lines)-1]; last["outcome"] != "reconciled" {
		t.Errorf("last ledger line %v", last)
	}
}

// reconcilePlan writes a script whose package sessions land and whose
// reconcile session does the forge actions.
func reconcilePlan(t *testing.T, f *fixture, actions ...map[string]any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"runs": []any{land(0)}, "reconcile": []any{map[string]any{"forge": actions}}})
	writeFile(t, f.script, string(b))
}

// An issue of the adopt file the session left with no kind becomes a
// deviation of its package; one the session labelled is left to it.
func TestReconcile_LabelsAnAdoptionTheSessionLeftUnsettled(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "archive/00_zero", "00", "done")
	f.addPackage(t, "01_a", "01", "pending")
	labelled := f.forge.add("00_zero: a", "open")
	left := f.forge.add("00_zero: b", "open")
	reconcilePlan(t, f, map[string]any{"op": "add_labels", "issue": labelled, "labels": []string{"af:defect", "af:severity-minor", "af:from-00"}})
	if _, code, stderr := runMain(t); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	if got := strings.Join(f.forge.labelsOf(left), ","); got != "af:deviation,af:from-00" {
		t.Errorf("the issue left: %s", got)
	}
	if c := f.forge.comments[left]; len(c) != 1 || !strings.Contains(c[0], "adoption-unsettled") || !strings.Contains(c[0], git(t, f.root, "rev-parse", "main")) {
		t.Errorf("comments %q", c)
	}
	for _, w := range f.forge.writes() {
		if strings.Contains(w, fmt.Sprintf(" %d ", labelled)) || strings.HasSuffix(w, fmt.Sprintf(" %d", labelled)) {
			t.Errorf("specrun wrote to the issue the session adopted: %s", w)
		}
	}
}

// A person settles a deviation with af:accepted or af:rejected: accepted is
// closed naming the ADR it cites, rejected becomes a correctness defect, both
// is left alone with a warning.
func TestReconcile_SettlesTheDeviationsAPersonDecided(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending") // open, so 02's items are not reconsidered
	cites := f.forge.add("02_b: a", "open", "af:deviation", "af:accepted", "af:from-02")
	f.forge.issue(cites).Body = "Recorded in docs/adr/07-x.md, and docs/adr/09-y.md."
	plain := f.forge.add("02_b: b", "open", "af:deviation", "af:accepted", "af:from-02")
	rejected := f.forge.add("02_b: c", "open", "af:deviation", "af:rejected", "af:from-02")
	open := f.forge.add("02_b: d", "open", "af:deviation", "af:from-02")
	both := f.forge.add("02_b: e", "open", "af:deviation", "af:accepted", "af:rejected", "af:from-02")
	defect := f.forge.add("02_b: f", "open", "af:defect", "af:severity-minor", "af:accepted", "af:from-02")
	reconcilePlan(t, f)
	env, code, stderr := runMain(t, "--max", "1", "--reconcile-every", "1")
	if code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	base := git(t, f.root, "rev-parse", "main")
	if is := f.forge.issue(cites); is.State != "closed" || !strings.Contains(strings.Join(f.forge.comments[cites], ""), "docs/adr/07-x.md") ||
		!strings.Contains(strings.Join(f.forge.comments[cites], ""), "deviation-accepted") || !strings.Contains(strings.Join(f.forge.comments[cites], ""), base) {
		t.Errorf("the accepted deviation citing an ADR: %s, %q", is.State, f.forge.comments[cites])
	}
	if is := f.forge.issue(plain); is.State != "closed" || !strings.Contains(strings.Join(f.forge.comments[plain], ""), "cites no ADR") {
		t.Errorf("the accepted deviation citing none: %s, %q", is.State, f.forge.comments[plain])
	}
	if got := strings.Join(f.forge.labelsOf(rejected), ","); f.forge.issue(rejected).State != "open" || got != "af:defect,af:from-02,af:severity-correctness" ||
		!strings.Contains(strings.Join(f.forge.comments[rejected], ""), "deviation-rejected") || !strings.Contains(strings.Join(f.forge.comments[rejected], ""), base) {
		t.Errorf("the rejected deviation: %s, %q", got, f.forge.comments[rejected])
	}
	for _, n := range []int{open, both, defect} {
		for _, w := range f.forge.writes() {
			if strings.Contains(w, fmt.Sprintf(" %d ", n)) || strings.HasSuffix(w, fmt.Sprintf(" %d", n)) {
				t.Errorf("issue #%d was written: %s", n, w)
			}
		}
	}
	var conflict []string
	for _, w := range env.Warnings {
		if w.Code == "deviation_conflict" {
			conflict = append(conflict, w.Message)
		}
	}
	if len(conflict) != 1 || !strings.Contains(conflict[0], fmt.Sprintf("#%d", both)) {
		t.Errorf("deviation_conflict warnings %q", conflict)
	}
}

// A reconcile's ledger line counts what was adopted, closed and settled, and
// the open deviations are listed oldest first; the label rules run over every
// open af: issue.
func TestReconcile_ReportsWhatItDidAndWhatIsLeft(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "archive/00_zero", "00", "done")
	f.addPackage(t, "01_a", "01", "pending")
	a1, a2 := f.forge.add("00_zero: a", "open"), f.forge.add("00_zero: b", "open")
	resolved := f.forge.add("00_zero: wired", "open", "af:awaits", "af:awaits-01", "af:from-00")
	f.forge.add("00_zero: accepted", "open", "af:deviation", "af:accepted", "af:from-00")
	var plain []int
	for _, title := range []string{"00_zero: d3", "00_zero: d1", "00_zero: d2"} {
		plain = append(plain, f.forge.add(title, "open", "af:deviation", "af:from-00"))
	}
	f.forge.issue(plain[0]).CreatedAt, f.forge.issue(plain[1]).CreatedAt = f.forge.issue(plain[1]).CreatedAt, f.forge.issue(plain[0]).CreatedAt
	stale := f.forge.add("00_zero: wire it in 00", "open", "af:awaits", "af:awaits-00", "af:from-00")
	reconcilePlan(t, f,
		map[string]any{"op": "add_labels", "issue": a1, "labels": []string{"af:untested", "af:from-00"}},
		map[string]any{"op": "add_labels", "issue": a2, "labels": []string{"af:untested", "af:from-00"}},
		map[string]any{"op": "close", "issue": resolved})
	env, code, stderr := runMain(t)
	if code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	lines := f.ledgerLines(t)
	l := lines[len(lines)-1]
	if l["kind"] != "reconcile" || l["outcome"] != "reconciled" || l["adopted"] != 2.0 || l["closed"] != 2.0 || l["settled"] != 1.0 {
		t.Errorf("the reconcile's line %v; want adopted 2, closed 2, settled 1", l)
	}
	var got []int
	for _, d := range env.Result.OpenDeviations {
		got = append(got, d.Number)
	}
	if want := []int{plain[1], plain[0], plain[2]}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("open deviations %v; want %v, oldest first", got, want)
	}
	if !strings.Contains(stderr, fmt.Sprintf("#%d", plain[1])) {
		t.Errorf("stderr does not list the open deviations:\n%s", stderr)
	}
	if got := strings.Join(f.forge.labelsOf(stale), ","); got != "af:defect,af:from-00,af:severity-correctness" {
		t.Errorf("the item awaiting the archived 00: %s", got)
	}
}

// A dry run says a reconcile is next, what it would hand over, and the
// deviation changes, and writes nothing.
func TestReconcile_ADryRunShowsTheReconcile(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	writeFile(t, f.statePath(), `{"done":[],"since_reconcile":5}`)
	f.forge.add("01_a: old", "open")
	f.forge.add("01_a: yes", "open", "af:deviation", "af:accepted", "af:from-01")
	f.forge.add("01_a: no", "open", "af:deviation", "af:rejected", "af:from-01")
	env, code, _ := runMain(t, "--dry-run")
	r := env.Result
	if code != 0 || r.NextStep == nil || r.NextStep.Kind != "reconcile" || len(r.WouldAdopt) != 1 {
		t.Fatalf("code %d, next %+v, would_adopt %+v", code, r.NextStep, r.WouldAdopt)
	}
	rules := map[string]bool{}
	for _, c := range r.WouldChange {
		rules[c.Rule] = true
	}
	if !rules["deviation-accepted"] || !rules["deviation-rejected"] {
		t.Errorf("would_change %+v", r.WouldChange)
	}
	entries, _ := os.ReadDir(f.logDir())
	if w := f.forge.writes(); len(w) != 0 || len(f.starts(t)) != 0 || len(entries) != 0 {
		t.Errorf("writes %v, %d sessions, log files %v", w, len(f.starts(t)), entries)
	}
}

// With nothing to adopt the adopt file is an empty array.
func TestReconcile_WritesAnEmptyAdoptFileWhenThereIsNothingToAdopt(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.plan(t, land(0))
	if _, code, stderr := runMain(t); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	files, _ := filepath.Glob(filepath.Join(f.logDir(), "*-adopt.json"))
	if len(files) != 1 {
		t.Fatalf("adopt files %v", files)
	}
	if b, _ := os.ReadFile(files[0]); strings.TrimSpace(string(b)) != "[]" {
		t.Errorf("the adopt file holds %q; want []", b)
	}
}

// A reconcile already due when a run starts, as after a restart, runs before
// any package.
func TestReconcile_ADueReconcileRunsBeforeTheNextPackage(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	writeFile(t, f.statePath(), `{"done":[],"since_reconcile":5}`)
	f.plan(t, land(0))
	if _, code, stderr := runMain(t, "--max", "1"); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	if got := prompts(f.starts(t)); len(got) == 0 || got[0] != "reconcile" {
		t.Errorf("sessions %v; want the due reconcile first", got)
	}
}

// A package that landed and then stopped the loop is counted, so the
// reconcile it makes due still runs.
func TestReconcile_ALandedPackageThatStoppedTheLoopIsCounted(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	n := f.forge.add("01_a: leak", "open", "af:defect", "af:severity-security", "af:from-01")
	// A reconcile ran before, so only the count can make the next one due.
	writeFile(t, filepath.Join(f.logDir(), "ledger.jsonl"),
		`{"kind":"reconcile","subject":"reconcile","started_at":"2026-11-12T08:00:00Z","ended_at":"2026-11-12T08:01:00Z","wall_minutes":1,"outcome":"reconciled"}`+"\n")
	f.plan(t, land(0))
	if _, code, _ := runMain(t, "--reconcile-every", "1"); code != 1 {
		t.Fatalf("code %d; want the stop on the security defect", code)
	}
	f.forge.issue(n).State = "closed"
	if _, code, stderr := runMain(t, "--reconcile-every", "1"); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	if got := prompts(f.starts(t)); strings.Join(got, ",") != ".specs/01_a,reconcile" {
		t.Errorf("sessions %v; want the reconcile the landing made due", got)
	}
}

// Each reconcile lists the open deviations as they stand, not added to the
// ones before.
func TestReconcile_ListsEachOpenDeviationOnce(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending")
	f.addPackage(t, "archive/00_zero", "00", "done")
	f.forge.add("00_zero: kept", "open", "af:deviation", "af:from-00")
	f.plan(t, land(0))
	env, code, stderr := runMain(t, "--reconcile-every", "1")
	if code != 0 || len(env.Result.OpenDeviations) != 1 {
		t.Errorf("code %d, open deviations %+v; want one\n%s", code, env.Result.OpenDeviations, stderr)
	}
}
