package specrun

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every step adds one ledger line with its kind, subject, times, wall minutes
// and outcome.
func TestLedger_HasOneLinePerStep(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending")
	f.addPackage(t, "03_c", "03", "pending")
	f.stubClock(time.Minute)
	f.plan(t, land(0), map[string]any{"events": 1}, map[string]any{"events": 1, "silence_s": 30})
	runMain(t)                     // 01 lands, 02 changes nothing and stops
	runMain(t, "--stall", "300ms") // 02 again, and it stalls
	lines := f.ledgerLines(t)
	if got := outcomes(lines); strings.Join(got, ",") != "landed,stopped,stalled" {
		t.Fatalf("outcomes %v", got)
	}
	for i, l := range lines {
		for _, k := range []string{"kind", "subject", "started_at", "ended_at", "wall_minutes", "outcome"} {
			if _, ok := l[k]; !ok {
				t.Errorf("line %d has no %s: %v", i+1, k, l)
			}
		}
		start, err1 := time.Parse(time.RFC3339, fmt.Sprint(l["started_at"]))
		end, err2 := time.Parse(time.RFC3339, fmt.Sprint(l["ended_at"]))
		if err1 != nil || err2 != nil {
			t.Fatalf("line %d times: %v %v", i+1, err1, err2)
		}
		// The clock moves a minute each time it is read, and a step reads it
		// at its start and at its end.
		if got := l["wall_minutes"].(float64); got != end.Sub(start).Minutes() || got != 1 {
			t.Errorf("line %d wall_minutes %v; want the clock's 1 minute", i+1, got)
		}
	}
	if lines[0]["kind"] != "implement" || lines[0]["subject"] != "01_a" || lines[2]["subject"] != "02_b" {
		t.Errorf("kinds and subjects %v", lines)
	}
}

// A landed package that took more than twice the median of the packages that
// landed before it is called out; stopped and stalled steps are no part of the
// median.
func TestLedger_CallsOutAPackageThatTookTwiceTheMedian(t *testing.T) {
	landed := func(minutes ...float64) []ledgerLine {
		var out []ledgerLine
		for _, m := range minutes {
			out = append(out, ledgerLine{Kind: kindImplement, Outcome: outcomeLanded, WallMinutes: m})
		}
		return out
	}
	for name, tc := range map[string]struct {
		earlier []ledgerLine
		minutes float64
		warn    bool
		median  float64
	}{
		"none before":     {nil, 50, false, 0},
		"more than twice": {landed(20), 41, true, 20},
		"exactly twice":   {landed(20), 40, false, 20},
		"an odd count":    {landed(20, 30, 100), 61, true, 30},
		"an even count":   {landed(20, 30), 51, true, 25},
		"stopped and stalled out": {append(landed(20), ledgerLine{Kind: kindImplement, Outcome: outcomeStopped, WallMinutes: 1},
			ledgerLine{Kind: kindImplement, Outcome: outcomeStalled, WallMinutes: 1}), 41, true, 20},
	} {
		t.Run(name, func(t *testing.T) {
			median, warn := slowerThanUsual(tc.earlier, tc.minutes)
			if warn != tc.warn || (warn && median != tc.median) {
				t.Errorf("slowerThanUsual = %v, %v; want %v, %v", median, warn, tc.median, tc.warn)
			}
		})
	}
}

// After a landed step, stderr has the ledger line in short form, and the
// warning when the package took more than twice the median.
func TestLedger_IsPrintedOnStderrWithItsWarning(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	writeFile(t, filepath.Join(f.logDir(), "ledger.jsonl"),
		`{"kind":"implement","subject":"00_x","started_at":"2026-11-12T08:00:00Z","ended_at":"2026-11-12T08:00:30Z","wall_minutes":0.5,"outcome":"landed"}`+"\n")
	f.stubClock(2 * time.Minute) // a step of two minutes, four times the median
	f.plan(t, land(0))
	_, code, stderr := runMain(t)
	if code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "0002-implement-01_a landed in") {
		t.Errorf("stderr has no short ledger line:\n%s", stderr)
	}
	if !strings.Contains(stderr, "warning: 01_a took") || !strings.Contains(stderr, "median of 0.5") {
		t.Errorf("stderr has no warning:\n%s", stderr)
	}
}

// --ledger prints the ledger as a table, skipping a line that does not parse,
// and starts nothing.
func TestLedger_PrintsTheLedgerAsATable(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	var stdout, stderr bytes.Buffer
	if code := runOn(f.host(), []string{"--ledger"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "no step is recorded") {
		t.Errorf("with no ledger: code %d, stdout %q", code, stdout.String())
	}
	line := func(subject, outcome string) string {
		return `{"kind":"implement","subject":"` + subject + `","started_at":"2026-11-12T08:00:00Z","ended_at":"2026-11-12T08:12:00Z","wall_minutes":12,"outcome":"` + outcome + `"}` + "\n"
	}
	writeFile(t, filepath.Join(f.logDir(), "ledger.jsonl"), line("01_a", "landed")+"not json\n"+line("02_b", "stopped")+line("02_b", "stalled"))
	stdout.Reset()
	if code := runOn(f.host(), []string{"--ledger"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code %d", code)
	}
	rows := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(rows) != 4 || !strings.Contains(rows[0], "OUTCOME") || !strings.Contains(rows[3], "stalled") {
		t.Errorf("table:\n%s", stdout.String())
	}
	if len(f.starts(t)) != 0 || exists(filepath.Join(f.state, "agent-fox", "specrun")) {
		t.Error("--ledger started a session or wrote state")
	}
	if _, err := os.Stat(filepath.Join(f.logDir(), "0001-implement-01_a.jsonl")); err == nil {
		t.Error("--ledger ran a step")
	}
}

// landWith is a plan for a session that lands its package with a merge
// commit ending in the paragraph trailer.
func landWith(trailer string) map[string]any {
	return map[string]any{"events": 1, "actions": []string{"land"}, "trailer": trailer}
}

// The AF-Run trailer of a landed package's merge commit goes into its ledger
// line, its gate and open-item counts into the short form.
func TestLedger_ReadsTheTrailerOfTheMergeCommit(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.plan(t, landWith(`AF-Run: {"gate":{"full":{"runs":3,"seconds":412.5,"reused":1},"task":{"runs":9,"seconds":95,"reused":0},"baseline_full_seconds":130.2,"last_full_seconds":141},"open_items":{"opened":{"awaits":1,"defect:minor":2},"closed":{},"passed_on":{},"reclassified":{}},"extra":1}`))
	env, code, stderr := runMain(t)
	if code != 0 || len(env.Warnings) != 0 {
		t.Fatalf("code %d, warnings %+v\n%s", code, env.Warnings, stderr)
	}
	l := f.ledgerLines(t)[0]
	gate, _ := l["gate"].(map[string]any)
	full, _ := gate["full"].(map[string]any)
	items, _ := l["open_items"].(map[string]any)
	opened, _ := items["opened"].(map[string]any)
	if full["runs"] != 3.0 || opened["defect:minor"] != 2.0 {
		t.Errorf("ledger line %v", l)
	}
	if _, ok := l["extra"]; ok {
		t.Error("the ledger line holds the trailer's other key")
	}
	for _, want := range []string{"3 full", "412.5", "1 reused", "3 opened"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not say %q:\n%s", want, stderr)
		}
	}
}

// A landed package whose merge commit has no trailer, or one that is not a
// JSON object, still counts as landed, with a warning.
func TestLedger_WarnsWhenTheTrailerIsMissing(t *testing.T) {
	for name, trailer := range map[string]string{"no trailer": "", "not json": "AF-Run: not json", "an array": "AF-Run: [1]"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			f.addPackage(t, "02_b", "02", "pending")
			f.plan(t, landWith(trailer))
			env, code, stderr := runMain(t)
			if code != 0 || len(env.Result.Landed) != 2 {
				t.Fatalf("code %d, result %+v\n%s", code, env.Result, stderr)
			}
			l := f.ledgerLines(t)[0]
			_, hasGate := l["gate"]
			_, hasItems := l["open_items"]
			if l["outcome"] != "landed" || hasGate || hasItems {
				t.Errorf("ledger line %v", l)
			}
			var codes []string
			for _, w := range env.Warnings {
				codes = append(codes, string(w.Code))
			}
			if strings.Join(codes, ",") != "trailer_missing,trailer_missing" {
				t.Errorf("warnings %v; want trailer_missing for each package", codes)
			} else if head := git(t, f.root, "rev-parse", "main"); !strings.Contains(env.Warnings[1].Message, head) {
				t.Errorf("warning %q does not name the merge commit %s", env.Warnings[1].Message, head)
			}
		})
	}
}
