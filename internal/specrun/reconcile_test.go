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
	_ = filepath.Join
}
