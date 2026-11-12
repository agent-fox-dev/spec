package specrun

import (
	"slices"
	"strings"
	"testing"
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
