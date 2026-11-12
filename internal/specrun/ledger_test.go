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
	stubClock(t, time.Minute)
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
		if got, want := l["wall_minutes"].(float64), end.Sub(start).Minutes(); got != want || got <= 0 {
			t.Errorf("line %d wall_minutes %v; want %v", i+1, got, want)
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
	stubClock(t, 2*time.Minute) // a step of two minutes, four times the median
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
	if code := Main([]string{"--ledger"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "no step is recorded") {
		t.Errorf("with no ledger: code %d, stdout %q", code, stdout.String())
	}
	line := func(subject, outcome string) string {
		return `{"kind":"implement","subject":"` + subject + `","started_at":"2026-11-12T08:00:00Z","ended_at":"2026-11-12T08:12:00Z","wall_minutes":12,"outcome":"` + outcome + `"}` + "\n"
	}
	writeFile(t, filepath.Join(f.logDir(), "ledger.jsonl"), line("01_a", "landed")+"not json\n"+line("02_b", "stopped")+line("02_b", "stalled"))
	stdout.Reset()
	if code := Main([]string{"--ledger"}, &stdout, &stderr); code != 0 {
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
