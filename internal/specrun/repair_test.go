package specrun

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agent-fox-dev/aflib/issuex"
)

// day is an issue created on day n of the year.
func day(n int, title, state string, labels ...string) issuex.Issue {
	return issuex.Issue{Number: 100 + n, Title: title, State: state, Labels: labels,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)}
}

// The queue is the open defects of a severity in --repair-on and the untested
// items, without failed repairs and, unless allowed, security defects; most
// severe first, then oldest first, cut to --repair-max.
func TestRepair_TheQueueFollowsTheLabels(t *testing.T) {
	defects := []issuex.Issue{
		day(1, "minor", "open", "af:defect", "af:severity-minor"),
		day(3, "data-loss", "open", "af:defect", "af:severity-data-loss"),
		day(2, "correctness", "open", "af:defect", "af:severity-correctness"),
		day(0, "untested", "open", "af:untested"),
		day(4, "security", "open", "af:defect", "af:severity-security"),
	}
	var seven []issuex.Issue
	for i := 7; i >= 1; i-- {
		seven = append(seven, day(10+i, fmt.Sprint("minor ", i), "open", "af:defect", "af:severity-minor"))
	}
	for name, tc := range map[string]struct {
		issues []issuex.Issue
		argv   []string
		want   string
	}{
		"the defaults":             {defects, nil, "data-loss,correctness,minor,untested"},
		"security allowed":         {defects, []string{"--repair-security"}, "security,data-loss,correctness,minor,untested"},
		"two of one severity":      {[]issuex.Issue{day(5, "late", "open", "af:defect", "af:severity-correctness"), day(2, "early", "open", "af:defect", "af:severity-correctness")}, nil, "early,late"},
		"repair-on two severities": {defects, []string{"--repair-on", "correctness,minor"}, "correctness,minor"},
		"a failed repair":          {[]issuex.Issue{day(1, "failed", "open", "af:defect", "af:severity-minor", "af:repair-failed")}, nil, ""},
		"seven, the default cap":   {seven, nil, "minor 1,minor 2,minor 3,minor 4,minor 5"},
		"repair-max 2":             {defects, []string{"--repair-max", "2"}, "data-loss,correctness"},
		"repair-max 0":             {defects, []string{"--repair-max", "0"}, ""},
		"closed":                   {[]issuex.Issue{day(1, "closed", "closed", "af:defect", "af:severity-minor")}, nil, ""},
		"a deviation":              {[]issuex.Issue{day(1, "deviation", "open", "af:deviation")}, nil, ""},
		"no severity":              {[]issuex.Issue{day(1, "bare", "open", "af:defect")}, nil, ""},
	} {
		t.Run(name, func(t *testing.T) {
			o, err := parseOptions(tc.argv, envOf(nil))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, q := range repairQueue(tc.issues, o) {
				got = append(got, q.Title)
			}
			if strings.Join(got, ",") != tc.want {
				t.Errorf("queue %v; want %s", got, tc.want)
			}
		})
	}
}

// repairPlan writes a script whose packages land, whose reconciles do
// nothing, and whose repairs follow plans.
func repairPlan(t *testing.T, f *fixture, plans ...map[string]any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"runs": []any{land(0)}, "repair": plans})
	writeFile(t, f.script, string(b))
}

// After a reconcile the queue is repaired, one session each in order, before
// the next package.
func TestRepair_RunsTheQueueAfterAReconcile(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "archive/00_zero", "00", "done")
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending")
	minor := f.forge.add("00_zero: minor", "open", "af:defect", "af:severity-minor", "af:from-00")
	correctness := f.forge.add("00_zero: correctness", "open", "af:defect", "af:severity-correctness", "af:from-00")
	repairPlan(t, f, map[string]any{"close": true})
	if _, code, stderr := runMain(t, "--reconcile-every", "1", "--repair-max", "5"); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	want := []string{".specs/01_a", "reconcile", fmt.Sprint("repair ", correctness), fmt.Sprint("repair ", minor), ".specs/02_b", "reconcile"}
	if got := prompts(f.starts(t)); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("sessions %v; want %v", got, want)
	}
	logs, _ := os.ReadDir(f.logDir())
	found := false
	for _, e := range logs {
		found = found || e.Name() == fmt.Sprintf("0003-repair-%d.jsonl", correctness)
	}
	if !found {
		t.Errorf("no stream file 0003-repair-%d.jsonl among %v", correctness, logs)
	}
}

// The rest of a queue is in the state file, and a restart goes on with it,
// the interrupted repair first.
func TestRepair_ARestartGoesOnWithTheRestOfTheQueue(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "archive/00_zero", "00", "done")
	f.addPackage(t, "01_a", "01", "pending")
	minor := f.forge.add("00_zero: minor", "open", "af:defect", "af:severity-minor", "af:from-00")
	correctness := f.forge.add("00_zero: correctness", "open", "af:defect", "af:severity-correctness", "af:from-00")
	repairPlan(t, f, map[string]any{"silence_s": 30}, map[string]any{"close": true})
	if _, code, _ := runMain(t, "--reconcile-every", "1", "--repair-max", "5", "--stall", "1s"); code != 1 {
		t.Fatalf("code %d; want the first repair to stall", code)
	}
	b, _ := os.ReadFile(f.statePath())
	if !strings.Contains(string(b), fmt.Sprintf(`"number": %d`, minor)) {
		t.Errorf("the state %s does not hold the rest of the queue", b)
	}
	before := len(f.starts(t))
	if _, code, stderr := runMain(t, "--reconcile-every", "1", "--repair-max", "5"); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	want := []string{fmt.Sprint("repair ", correctness), fmt.Sprint("repair ", minor)}
	if got := prompts(f.starts(t)[before:]); len(got) < 2 || strings.Join(got[:2], ",") != strings.Join(want, ",") {
		t.Errorf("the restart's sessions %v; want %v first", got, want)
	}
}

// repairFixture is a repository with an archived 00, a package 01 to land
// and a package 02 after it, and on the forge the issue to repair.
func repairFixture(t *testing.T, labels ...string) (*fixture, int) {
	t.Helper()
	f := newFixture(t)
	f.addPackage(t, "archive/00_zero", "00", "done")
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending")
	return f, f.forge.add("00_zero: the issue", "open", append(labels, "af:from-00")...)
}

// repairLines are the ledger lines of the repairs.
func repairLines(f *fixture, t *testing.T) []map[string]any {
	var out []map[string]any
	for _, l := range f.ledgerLines(t) {
		if l["kind"] == "repair" {
			out = append(out, l)
		}
	}
	return out
}

// A repair that closed its issue on a clean base branch is done: already
// fixed when the merge commit's trailer says so for this issue, else fixed.
func TestRepair_RecordsTheOutcomeOfAClosedIssue(t *testing.T) {
	for name, tc := range map[string]struct {
		plan map[string]any
		want string
	}{
		"fixed":                    {map[string]any{"merge": true, "outcome": "fixed", "close": true}, "fixed"},
		"already fixed":            {map[string]any{"merge": true, "outcome": "already_fixed", "close": true}, "already_fixed"},
		"no trailer":               {map[string]any{"merge": true, "close": true}, "fixed"},
		"a trailer of another one": {map[string]any{"merge": true, "outcome": "already_fixed", "for": 999, "close": true}, "fixed"},
	} {
		t.Run(name, func(t *testing.T) {
			f, n := repairFixture(t, "af:defect", "af:severity-minor")
			repairPlan(t, f, tc.plan)
			if _, code, stderr := runMain(t, "--reconcile-every", "1", "--repair-max", "5"); code != 0 {
				t.Fatalf("code %d\n%s", code, stderr)
			}
			lines := repairLines(f, t)
			if len(lines) != 1 || lines[0]["outcome"] != tc.want || lines[0]["subject"] != fmt.Sprint(n) {
				t.Errorf("repair lines %v; want one %s for #%d", lines, tc.want, n)
			}
			if got := specDirs(f.starts(t)); strings.Join(got, ",") != ".specs/01_a,.specs/02_b" {
				t.Errorf("package sessions %v; want 02 after the repair", got)
			}
		})
	}
}

// A repair that marked its issue failed is recorded failed; it stops the loop
// only when the issue's severity is in --stop-on, and no later queue holds it.
func TestRepair_RecordsAFailedRepair(t *testing.T) {
	for name, tc := range map[string]struct {
		severity string
		flags    []string
		stop     bool
	}{
		"a minor defect":                   {"minor", nil, false},
		"a data-loss defect":               {"data-loss", nil, true},
		"a data-loss defect, stop on none": {"data-loss", []string{"--stop-on", ""}, false},
	} {
		t.Run(name, func(t *testing.T) {
			f, n := repairFixture(t, "af:defect", "af:severity-"+tc.severity)
			repairPlan(t, f, map[string]any{"fail": true})
			env, code, _ := runMain(t, append([]string{"--reconcile-every", "1", "--repair-max", "5"}, tc.flags...)...)
			lines := repairLines(f, t)
			if len(lines) == 0 || lines[0]["outcome"] != "failed" {
				t.Fatalf("repair lines %v; want failed", lines)
			}
			if tc.stop {
				s := env.Result.Stopped
				if code != 1 || s == nil || s.Reason != "repair failed" || len(s.Issues) != 1 || s.Issues[0].Number != n {
					t.Errorf("code %d, stopped %+v; want a stop naming #%d", code, s, n)
				}
				return
			}
			if code != 0 || len(lines) != 1 {
				t.Errorf("code %d, repair lines %v; want the loop to go on and the failed issue never queued again", code, lines)
			}
		})
	}
}

// A repair session that left the repository off the base branch or dirty, or
// its issue open and unmarked, or that stalled, stops the loop.
func TestRepair_StopsWhenTheSessionLeftSomethingElse(t *testing.T) {
	for name, tc := range map[string]struct {
		plan   map[string]any
		reason string
	}{
		"ends on the fix branch":      {map[string]any{"stay_on_branch": true}, "not on the base branch"},
		"leaves an untracked file":    {map[string]any{"dirty": true}, "working tree not clean"},
		"leaves the issue untouched":  {map[string]any{}, "issue neither closed nor marked failed"},
		"writes nothing for too long": {map[string]any{"silence_s": 30}, "stalled"},
	} {
		t.Run(name, func(t *testing.T) {
			f, _ := repairFixture(t, "af:defect", "af:severity-minor")
			repairPlan(t, f, tc.plan)
			env, code, _ := runMain(t, "--reconcile-every", "1", "--repair-max", "5", "--stall", "2s")
			if code != 1 || env.Result.Stopped == nil || env.Result.Stopped.Reason != tc.reason {
				t.Fatalf("code %d, stopped %+v; want %q", code, env.Result.Stopped, tc.reason)
			}
			want := "stopped"
			if tc.reason == "stalled" {
				want = "stalled"
			}
			if l := repairLines(f, t); len(l) != 1 || l[0]["outcome"] != want {
				t.Errorf("repair lines %v; want one with outcome %s", l, want)
			}
			if got := specDirs(f.starts(t)); strings.Join(got, ",") != ".specs/01_a" {
				t.Errorf("package sessions %v; want none after the repair", got)
			}
		})
	}
}

// A repair recorded as running is done at the next start when its issue is
// closed, and is run again when it is open.
func TestRepair_ARunningRepairIsCheckedAtTheNextStart(t *testing.T) {
	for _, closed := range []bool{true, false} {
		t.Run(fmt.Sprint("closed ", closed), func(t *testing.T) {
			f, n := repairFixture(t, "af:defect", "af:severity-minor")
			if closed {
				f.forge.issue(n).State = "closed"
			}
			writeFile(t, f.statePath(), fmt.Sprintf(`{"done":[],"since_reconcile":0,"running":{"kind":"repair","subject":"%d","started_at":"2026-11-12T08:00:00Z","pid":0}}`, n))
			repairPlan(t, f, map[string]any{"close": true})
			if _, code, stderr := runMain(t, "--max", "1", "--repair-max", "0"); code != 0 {
				t.Fatalf("code %d\n%s", code, stderr)
			}
			got := prompts(f.starts(t))
			if closed {
				if len(got) == 0 || strings.HasPrefix(got[0], "repair") || len(repairLines(f, t)) != 1 || repairLines(f, t)[0]["outcome"] != "fixed" {
					t.Errorf("sessions %v, repair lines %v; want no repair session and one fixed line", got, repairLines(f, t))
				}
				return
			}
			if len(got) == 0 || got[0] != fmt.Sprint("repair ", n) {
				t.Errorf("sessions %v; want the repair run again first", got)
			}
		})
	}
}

// With --repair a landed package's serious defects are repaired, most severe
// first then oldest first, with no cap, before the next package; one still
// open after its repair stops the loop.
func TestRepair_RepairsASeriousDefectInsteadOfStopping(t *testing.T) {
	for name, tc := range map[string]struct {
		severities []string
		plan       map[string]any
		code       int
		want       []string // the sessions, with the defects as their severities
	}{
		"data-loss and security, both fixed": {[]string{"data-loss", "security"}, map[string]any{"close": true}, 0,
			[]string{".specs/01_a", "repair security", "repair data-loss", ".specs/02_b"}},
		"data-loss, the repair failed": {[]string{"data-loss"}, map[string]any{"fail": true}, 1,
			[]string{".specs/01_a", "repair data-loss"}},
		"data-loss, the repair left it open and clean": {[]string{"data-loss"}, map[string]any{"merge": true}, 1,
			[]string{".specs/01_a", "repair data-loss"}},
		"a correctness defect alone": {[]string{"correctness"}, map[string]any{"close": true}, 0,
			[]string{".specs/01_a", ".specs/02_b"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			f.addPackage(t, "02_b", "02", "pending")
			names := map[string]string{}
			for i, sev := range tc.severities {
				n := f.forge.add(fmt.Sprintf("01_a: %s %d", sev, i), "open", "af:defect", "af:severity-"+sev, "af:from-01")
				names[fmt.Sprint("repair ", n)] = "repair " + sev
			}
			repairPlan(t, f, tc.plan)
			_, code, stderr := runMain(t, "--repair", "--repair-security")
			var got []string
			for _, p := range prompts(f.starts(t)) {
				if p == "reconcile" {
					continue
				}
				if n, ok := names[p]; ok {
					p = n
				}
				got = append(got, p)
			}
			if code != tc.code || strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("code %d, sessions %v; want %d, %v\n%s", code, got, tc.code, tc.want, stderr)
			}
		})
	}
}

// Without --repair-security a security defect is not repaired: the loop stops
// as the stop rule does, before any repair.
func TestRepair_LeavesASecurityDefectToAPerson(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending")
	security := f.forge.add("01_a: security", "open", "af:defect", "af:severity-security", "af:from-01")
	f.forge.add("01_a: data-loss", "open", "af:defect", "af:severity-data-loss", "af:from-01")
	repairPlan(t, f, map[string]any{"close": true})
	env, code, _ := runMain(t, "--repair")
	s := env.Result.Stopped
	if code != 1 || s == nil || s.Reason != "serious defect" || !slices.ContainsFunc(s.Issues, func(i StopIssue) bool { return i.Number == security }) {
		t.Fatalf("code %d, stopped %+v; want the stop rule's stop naming #%d", code, s, security)
	}
	if got := prompts(f.starts(t)); strings.Join(got, ",") != ".specs/01_a" {
		t.Errorf("sessions %v; want none after 01", got)
	}
}

// A dry run reports the queue the labels give, capped, and starts and writes
// nothing.
func TestRepair_TheDryRunShowsTheQueue(t *testing.T) {
	for name, tc := range map[string]struct {
		issues [][]string
		want   string
	}{
		"three, capped at two": {[][]string{{"minor", "af:defect", "af:severity-minor"}, {"correctness", "af:defect", "af:severity-correctness"},
			{"untested", "af:untested"}}, "correctness:correctness,minor:minor"},
		"none": {nil, ""},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			for _, is := range tc.issues {
				f.forge.add(is[0], "open", is[1:]...)
			}
			env, code, stderr := runMain(t, "--dry-run", "--repair-max", "2")
			if code != 0 || env.Result.RepairQueue == nil {
				t.Fatalf("code %d, repair_queue %v; want 0 and a list\n%s", code, env.Result.RepairQueue, stderr)
			}
			var got []string
			for _, q := range env.Result.RepairQueue {
				if q.URL == "" || q.Number == 0 {
					t.Errorf("queued %+v has no number or URL", q)
				}
				got = append(got, q.Title+":"+q.Severity)
			}
			if strings.Join(got, ",") != tc.want {
				t.Errorf("repair_queue %v; want %s", got, tc.want)
			}
			if len(f.starts(t)) != 0 || len(f.forge.writes()) != 0 {
				t.Errorf("sessions %v, forge writes %v; want none", f.starts(t), f.forge.writes())
			}
		})
	}
}

// A repair step is one session with the prompt the af-impl repair mode takes
// and the flags of every session, its stream under the step's name.
func TestRepair_StartsOneSessionForTheIssue(t *testing.T) {
	f, issue := repairFixture(t, "af:defect", "af:severity-minor")
	repairPlan(t, f, map[string]any{"merge": true, "outcome": "fixed", "close": true})
	if _, code, stderr := runMain(t, "--reconcile-every", "1", "--repair-max", "1", "--max", "1"); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	var pkg, rep []string
	for _, s := range f.starts(t) {
		for i, a := range s.Argv {
			switch {
			case strings.Contains(a, "--unattended") && strings.Contains(a, "--repair"):
				t.Errorf("the repair's prompt %q asks for the unattended mode", a)
			case a == fmt.Sprintf("/af-impl --repair %d", issue):
				rep = slices.Delete(slices.Clone(s.Argv), i, i+1)
			case strings.HasPrefix(a, "/af-impl .specs/"):
				pkg = slices.Delete(slices.Clone(s.Argv), i, i+1)
			}
		}
	}
	if rep == nil || pkg == nil || strings.Join(rep, " ") != strings.Join(pkg, " ") {
		t.Errorf("the repair's flags %v; want a session with the prompt and the package session's flags %v", rep, pkg)
	}
	if !exists(fmt.Sprintf("%s/0003-repair-%d.jsonl", f.logDir(), issue)) {
		t.Errorf("no stream file 0003-repair-%d.jsonl", issue)
	}
}

// With --repair, a repair that closes its defect but leaves another serious
// one of the package open stops the loop on the check made after them.
func TestRepair_ChecksThePackageAgainAfterItsRepairs(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending")
	f.forge.add("01_a: data-loss", "open", "af:defect", "af:severity-data-loss", "af:from-01")
	repairPlan(t, f, map[string]any{"merge": true, "close": true,
		"opens": [][]string{{"01_a: another data-loss", "af:defect", "af:severity-data-loss", "af:from-01"}}})
	env, code, _ := runMain(t, "--repair")
	s := env.Result.Stopped
	if code != 1 || s == nil || s.Reason != "serious defect" || len(s.Issues) != 1 || s.Issues[0].Title != "01_a: another data-loss" {
		t.Fatalf("code %d, stopped %+v; want the stop rule's stop naming the new defect", code, s)
	}
	if got := specDirs(f.starts(t)); strings.Join(got, ",") != ".specs/01_a" {
		t.Errorf("package sessions %v; want none after 01", got)
	}
}

// The queue a run left in the state is checked against the forge and the
// flags of the next one: an issue closed since, or a security defect this run
// may not repair, gets no session.
func TestRepair_ARestartRepairsOnlyWhatItMayStill(t *testing.T) {
	for name, tc := range map[string]struct {
		first, second []string
		labels        []string
		closeSecond   bool
	}{
		"the second issue closed since": {nil, nil, []string{"af:defect", "af:severity-minor"}, true},
		"a security defect, no longer allowed": {[]string{"--repair-security"}, nil,
			[]string{"af:defect", "af:severity-security"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "archive/00_zero", "00", "done")
			f.addPackage(t, "01_a", "01", "pending")
			first := f.forge.add("00_zero: first", "open", append(slices.Clone(tc.labels), "af:from-00")...)
			second := f.forge.add("00_zero: second", "open", "af:defect", "af:severity-minor", "af:from-00")
			repairPlan(t, f, map[string]any{"silence_s": 30}, map[string]any{"close": true})
			argv := append([]string{"--reconcile-every", "1", "--repair-max", "5", "--stop-on", ""}, tc.first...)
			if _, code, _ := runMain(t, append(argv, "--stall", "1s")...); code != 1 {
				t.Fatalf("code %d; want the first repair to stall", code)
			}
			want := []string{fmt.Sprint("repair ", first)}
			if tc.closeSecond {
				f.forge.issue(second).State = "closed"
			} else {
				want = []string{fmt.Sprint("repair ", second)}
			}
			before := len(f.starts(t))
			if _, code, stderr := runMain(t, append([]string{"--reconcile-every", "1", "--repair-max", "5", "--stop-on", ""}, tc.second...)...); code != 0 {
				t.Fatalf("code %d\n%s", code, stderr)
			}
			var got []string
			for _, p := range prompts(f.starts(t)[before:]) {
				if strings.HasPrefix(p, "repair ") {
					got = append(got, p)
				}
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("the restart's repairs %v; want %v", got, want)
			}
		})
	}
}
