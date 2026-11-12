package gate

import (
	"os/exec"
	"testing"
	"time"
)

// steppedClock is a clock that advances by the next step each time it is
// read after the first.
func steppedClock(steps ...time.Duration) func() time.Time {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	return func() time.Time {
		if len(steps) > 0 {
			defer func() { now, steps = now.Add(steps[0]), steps[1:] }()
		}
		return now
	}
}

// Each command's seconds are what the clock measured while it ran, and the
// gate's are their sum.
func TestRun_MeasuresEachCommandAndSumsThem(t *testing.T) {
	if _, err := exec.LookPath("true"); err != nil {
		t.Skip("no true on the path")
	}
	// Read before and after each command: 1.5 s pass during the first, nothing
	// between the two, and 2.25 s during the second.
	r := Runner{Dir: t.TempDir(), Timeout: time.Minute, Now: steppedClock(1500*time.Millisecond, 0, 2250*time.Millisecond)}
	g := r.Run([]Command{{Name: "linter", Line: "true"}, {Name: "all_tests", Line: "true"}})
	if g.Verdict != "pass" || len(g.Commands) != 2 {
		t.Fatalf("verdict %q, commands %+v", g.Verdict, g.Commands)
	}
	if g.Commands[0].Seconds != 1.5 || g.Commands[1].Seconds != 2.25 || g.Seconds != 3.75 {
		t.Errorf("seconds %v and %v, total %v; want 1.5, 2.25 and 3.75", g.Commands[0].Seconds, g.Commands[1].Seconds, g.Seconds)
	}
}

// A duration is kept to the millisecond.
func TestRun_KeepsSecondsToTheMillisecond(t *testing.T) {
	if _, err := exec.LookPath("true"); err != nil {
		t.Skip("no true on the path")
	}
	r := Runner{Dir: t.TempDir(), Timeout: time.Minute, Now: steppedClock(1234567 * time.Microsecond)}
	g := r.Run([]Command{{Name: "linter", Line: "true"}})
	if g.Commands[0].Seconds != 1.235 || g.Seconds != 1.235 {
		t.Errorf("seconds %v, total %v; want 1.235", g.Commands[0].Seconds, g.Seconds)
	}
}
