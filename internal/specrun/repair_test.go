package specrun

import (
	"encoding/json"
	"fmt"
	"os"
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
