package specrun

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/aflib/issuex"
)

// The fixed labels of the scheme.
var fixedLabels = []string{"af:awaits", "af:defect", "af:untested", "af:deviation", "af:severity-security",
	"af:severity-data-loss", "af:severity-correctness", "af:severity-minor", "af:accepted", "af:rejected", "af:repair-failed"}

// Before its first step specrun creates every label of the scheme the forge
// lacks; one that exists is no error.
func TestLabels_TheSchemeIsCreatedBeforeTheFirstStep(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "archive/00_old", "00", "done")
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending")
	f.forge.labels["af:defect"] = true
	f.plan(t, land(0))
	env, code, stderr := runMain(t, "--max", "1")
	if code != 0 {
		t.Fatalf("code %d, error %+v\n%s", code, env.Error, stderr)
	}
	want := append(slices.Clone(fixedLabels), "af:from-00", "af:awaits-00", "af:from-01", "af:awaits-01", "af:from-02", "af:awaits-02")
	for _, l := range want {
		if !f.forge.labels[l] {
			t.Errorf("the forge has no %s", l)
		}
	}
	for i, n := range f.forge.startsAtLabels {
		if n != 0 {
			t.Errorf("CreateLabel call %d came after %d session(s) started", i+1, n)
		}
	}
	if len(env.Warnings) != 0 {
		t.Errorf("warnings %+v", env.Warnings)
	}
}

// Without a forge client specrun refuses to start, naming the token variables
// or the remote.
func TestLabels_NoForgeRefusesTheStart(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(f *fixture)
		says  []string
	}{
		"no token": {func(f *fixture) {
			git(t, f.root, "remote", "set-url", "origin", "https://github.com/acme/widgets.git")
		}, []string{"GITHUB_TOKEN", "GH_TOKEN", "GITLAB_TOKEN"}},
		"no origin remote": {func(f *fixture) {
			git(t, f.root, "remote", "remove", "origin")
		}, []string{"origin"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			f.plan(t, land(0))
			tc.setup(f)
			realForge(t)
			env, code, _ := runMain(t)
			if code != 1 || env.Error == nil || env.Error.Stage != "preflight" {
				t.Fatalf("code %d, error %+v; want a refusal at preflight", code, env.Error)
			}
			for _, w := range tc.says {
				if !strings.Contains(env.Error.Message, w) {
					t.Errorf("message %q does not name %s", env.Error.Message, w)
				}
			}
			if len(f.starts(t)) != 0 {
				t.Error("a session started")
			}
		})
	}
}

// A dry run writes nothing to the forge, and without a forge it warns and
// still names the next step.
func TestLabels_ADryRunWritesNothingToTheForge(t *testing.T) {
	for _, withForge := range []bool{true, false} {
		f := newFixture(t)
		f.addPackage(t, "01_a", "01", "pending")
		if !withForge {
			git(t, f.root, "remote", "remove", "origin")
			realForge(t)
		}
		env, code, _ := runMain(t, "--dry-run")
		if code != 0 || env.Result == nil || env.Result.NextStep == nil {
			t.Fatalf("forge %v: code %d, result %+v, error %+v", withForge, code, env.Result, env.Error)
		}
		if w := f.forge.writes(); len(w) != 0 {
			t.Errorf("forge %v: writes %v", withForge, w)
		}
		var codes []string
		for _, w := range env.Warnings {
			codes = append(codes, string(w.Code))
		}
		if want := map[bool]string{true: "", false: "forge_unavailable"}[withForge]; strings.Join(codes, ",") != want {
			t.Errorf("forge %v: warnings %v; want %q", withForge, codes, want)
		}
	}
}

// After a package lands, its open issues that lack a label the scheme asks
// for become deviations, with a comment naming the rule and the base commit;
// complete ones, another package's and closed ones are left alone.
func TestLabels_AnIncompleteIssueOfALandedPackageBecomesADeviation(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	type row struct {
		n    int
		want []string // nil: no write
	}
	rows := map[string]row{
		"a complete defect":              {f.forge.add("01_a: a", "open", "af:defect", "af:severity-minor", "af:from-01"), nil},
		"a complete awaits":              {f.forge.add("01_a: b", "open", "af:awaits", "af:awaits-02", "af:from-01"), nil},
		"no label, titled for 01":        {f.forge.add("01_a: no caller", "open"), []string{"af:deviation", "af:from-01"}},
		"a defect with no severity":      {f.forge.add("x", "open", "af:defect", "af:from-01"), []string{"af:deviation", "af:from-01"}},
		"an awaits with no receiver":     {f.forge.add("y", "open", "af:awaits", "af:from-01"), []string{"af:deviation", "af:from-01"}},
		"two kinds":                      {f.forge.add("z", "open", "af:defect", "af:untested", "af:severity-minor", "af:from-01"), []string{"af:deviation", "af:from-01", "af:severity-minor"}},
		"another package's":              {f.forge.add("02_b: y", "open"), nil},
		"a closed issue of 01, no label": {f.forge.add("01_a: closed", "closed"), nil},
	}
	f.plan(t, land(0))
	if _, code, stderr := runMain(t); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	base := git(t, f.root, "rev-parse", "main")
	for name, r := range rows {
		comments := f.forge.comments[r.n]
		if r.want == nil {
			for _, w := range f.forge.writes() {
				if strings.HasPrefix(w, "AddLabels "+fmt.Sprint(r.n)+" ") || strings.HasPrefix(w, "RemoveLabel "+fmt.Sprint(r.n)+" ") || w == "AddComment "+fmt.Sprint(r.n) {
					t.Errorf("%s: written: %s", name, w)
				}
			}
			continue
		}
		if got := f.forge.labelsOf(r.n); strings.Join(got, ",") != strings.Join(r.want, ",") {
			t.Errorf("%s: labels %v; want %v", name, got, r.want)
		}
		if len(comments) != 1 || !strings.Contains(comments[0], "incomplete-labels") || !strings.Contains(comments[0], base) {
			t.Errorf("%s: comments %q; want one naming incomplete-labels and %s", name, comments, base)
		}
	}
}

// The rule changes nothing the second time it runs over the same issues.
func TestLabels_TheRuleChangesNothingTheSecondTime(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.forge.add("01_a: no caller", "open")
	f.forge.add("x", "open", "af:defect", "af:untested", "af:from-01")
	r := &run{ctx: context.Background(), root: f.root, stderr: io.Discard, forge: f.forge, repo: issuex.Repo{Owner: "acme", Name: "widgets"}}
	if err := r.checkLabels(".specs/01_a"); err != nil {
		t.Fatal(err)
	}
	before := len(f.forge.writes())
	if err := r.checkLabels(".specs/01_a"); err != nil {
		t.Fatal(err)
	}
	if after := f.forge.writes(); len(after) != before {
		t.Errorf("the second run wrote %v", after[before:])
	}
}

// A forge call of the rule that fails stops the loop once the package's
// landing is recorded; the next start applies the rule again first.
func TestLabels_AFailedForgeCallStopsTheLoopAndIsAppliedAgain(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending")
	n := f.forge.add("01_a: no caller", "open")
	f.forge.fail["AddLabels"] = 1
	f.plan(t, land(0))
	env, code, _ := runMain(t)
	if code != 1 || env.Result == nil || env.Result.Stopped == nil || !strings.Contains(env.Result.Stopped.Reason, "incomplete-labels") {
		t.Fatalf("code %d, result %+v, error %+v; want a stop at incomplete-labels", code, env.Result, env.Error)
	}
	if got := outcomes(f.ledgerLines(t)); strings.Join(got, ",") != "landed" {
		t.Errorf("ledger %v; want the landing recorded", got)
	}
	if got := specDirs(f.starts(t)); strings.Join(got, ",") != ".specs/01_a" {
		t.Errorf("sessions %v; want 01_a alone", got)
	}
	if _, code, stderr := runMain(t, "--max", "1"); code != 0 {
		t.Fatalf("the second call: code %d\n%s", code, stderr)
	}
	if got := f.forge.labelsOf(n); strings.Join(got, ",") != "af:deviation,af:from-01" {
		t.Errorf("labels %v after the second call", got)
	}
	if got := specDirs(f.starts(t)); strings.Join(got, ",") != ".specs/01_a,.specs/02_b" {
		t.Errorf("sessions %v; want 02_b next", got)
	}
}
