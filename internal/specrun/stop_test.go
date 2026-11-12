package specrun

import (
	"fmt"
	"strings"
	"testing"
)

// After a package lands and the label rules ran, an open defect of it whose
// severity is in --stop-on stops the loop; anything else does not.
func TestStop_ASeriousDefectOfALandedPackageStopsTheLoop(t *testing.T) {
	for name, tc := range map[string]struct {
		title  string
		state  string
		labels []string
		flags  []string
		stop   bool
	}{
		"a data-loss defect":                    {"01_a: x", "open", []string{"af:defect", "af:severity-data-loss", "af:from-01"}, nil, true},
		"a security defect":                     {"01_a: x", "open", []string{"af:defect", "af:severity-security", "af:from-01"}, nil, true},
		"a correctness defect":                  {"01_a: x", "open", []string{"af:defect", "af:severity-correctness", "af:from-01"}, nil, false},
		"a minor defect":                        {"01_a: x", "open", []string{"af:defect", "af:severity-minor", "af:from-01"}, nil, false},
		"a correctness defect, stop on it":      {"01_a: x", "open", []string{"af:defect", "af:severity-correctness", "af:from-01"}, []string{"--stop-on", "correctness"}, true},
		"an untested item":                      {"01_a: x", "open", []string{"af:untested", "af:from-01"}, nil, false},
		"a deviation":                           {"01_a: x", "open", []string{"af:deviation", "af:from-01"}, nil, false},
		"another package's data-loss defect":    {"00_x: x", "open", []string{"af:defect", "af:severity-data-loss", "af:from-00"}, nil, false},
		"a closed data-loss defect":             {"01_a: x", "closed", []string{"af:defect", "af:severity-data-loss", "af:from-01"}, nil, false},
		"an item relabelled by awaits-finished": {"01_a: x", "open", []string{"af:awaits", "af:awaits-09", "af:from-01"}, nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			f.addPackage(t, "02_b", "02", "pending")
			n := f.forge.add(tc.title, tc.state, tc.labels...)
			f.plan(t, land(0))
			env, code, _ := runMain(t, tc.flags...)
			started02 := strings.Contains(strings.Join(specDirs(f.starts(t)), ","), "02_b")
			if !tc.stop {
				if code != 0 || !started02 {
					t.Errorf("code %d, 02 started %v, result %+v; want the loop to go on", code, started02, env.Result)
				}
				return
			}
			s := env.Result.Stopped
			if code != 1 || started02 || s == nil || s.Reason != "serious defect" || len(s.Issues) != 1 || s.Issues[0].Number != n ||
				s.Issues[0].Title != tc.title || s.Issues[0].Severity == "" {
				t.Fatalf("code %d, 02 started %v, stopped %+v; want a stop naming issue %d", code, started02, s, n)
			}
			l := f.ledgerLines(t)[0]
			if l["outcome"] != "landed" || !strings.Contains(fmt.Sprint(l["issues"]), tc.title) {
				t.Errorf("ledger line %v; want landed with the issue", l)
			}
		})
	}
}

// The next start makes the same check for the last package that landed, and
// goes on once the person closed the issue, changed its severity, or changed
// --stop-on.
func TestStop_TheNextStartChecksTheLastLandedPackageAgain(t *testing.T) {
	for name, tc := range map[string]struct {
		act   func(f *fixture, n int)
		flags []string
	}{
		"the issue closed": {func(f *fixture, n int) { f.forge.issue(n).State = "closed" }, nil},
		"the severity made minor": {func(f *fixture, n int) {
			f.forge.issue(n).Labels = []string{"af:defect", "af:severity-minor", "af:from-01"}
		}, nil},
		"--stop-on no longer holding it": {func(*fixture, int) {}, []string{"--stop-on", "security"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			f.addPackage(t, "02_b", "02", "pending")
			n := f.forge.add("01_a: lost write", "open", "af:defect", "af:severity-data-loss", "af:from-01")
			f.plan(t, land(0))
			env1, code, _ := runMain(t)
			if code != 1 {
				t.Fatalf("the first call: code %d; want a stop", code)
			}
			lastStop := env1.Result.Stopped
			first := lastStop
			env, code, _ := runMain(t)
			s := env.Result.Stopped
			if code != 1 || s == nil || len(f.starts(t)) != 1 || first == nil || s.Reason != first.Reason || s.Step != first.Step ||
				s.Kind != first.Kind || s.Subject != first.Subject || len(s.Issues) != 1 || s.Issues[0] != first.Issues[0] {
				t.Fatalf("the second call: code %d, stopped %+v, %d sessions; want the first stop %+v again and no session", code, s, len(f.starts(t)), first)
			}
			tc.act(f, n)
			if _, code, stderr := runMain(t, append([]string{"--max", "1"}, tc.flags...)...); code != 0 {
				t.Fatalf("after the person acted: code %d\n%s", code, stderr)
			}
			if got := specDirs(f.starts(t)); strings.Join(got, ",") != ".specs/01_a,.specs/02_b" {
				t.Errorf("sessions %v; want 02 started", got)
			}
		})
	}
}
