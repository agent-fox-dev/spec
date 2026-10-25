package agentrun

import (
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/core"
)

func TestResolveModel(t *testing.T) {
	for _, tc := range []struct {
		name     string
		spec     string
		effort   core.Effort
		explicit bool
		id       string
		want     core.Effort
		clamped  core.Effort
	}{
		{name: "a tier carries its base model and effort", spec: "standard", id: "claude-sonnet-5-5", want: core.EffortHigh},
		{name: "a model id with the provider prefix", spec: "anthropic/claude-opus-5-5", id: "claude-opus-5-5"},
		{name: "an explicit empty effort sends none", spec: "ADVANCED", explicit: true, id: "claude-opus-5-5"},
		{name: "an effort the model does not take is clamped", spec: "claude-haiku-4-5", effort: core.EffortXHigh, explicit: true,
			id: "claude-haiku-4-5", want: core.EffortHigh, clamped: core.EffortXHigh},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := ResolveModel(tc.spec, tc.effort, tc.explicit)
			if err != nil {
				t.Fatal(err)
			}
			if m.ID() != tc.id || m.Effort != tc.want || m.Clamped != tc.clamped || m.Spec != tc.spec {
				t.Errorf("model = %+v", m)
			}
		})
	}
	for name, spec := range map[string]string{"unknown model": "gpt-9", "empty": "  "} {
		t.Run(name, func(t *testing.T) {
			if _, err := ResolveModel(spec, "", false); CategoryOf(err) != CategoryModel {
				t.Errorf("err = %v, want a model error", err)
			}
		})
	}
}

func TestParseEffort(t *testing.T) {
	for raw, want := range map[string]core.Effort{"off": "", "LOW": core.EffortLow, " max ": core.EffortMax} {
		t.Run(raw, func(t *testing.T) {
			if got, err := ParseEffort(raw); err != nil || got != want {
				t.Errorf("%q = %q, %v", raw, got, err)
			}
		})
	}
	t.Run("minimal", func(t *testing.T) {
		_, err := ParseEffort("minimal")
		if err == nil || !strings.Contains(err.Error(), strings.Join(EffortValues(), ", ")) {
			t.Errorf("err = %v; want a refusal listing the accepted values", err)
		}
	})
}

// A tier chooses a model and effort for every phase; the two review phases,
// which no tier lists, run on its base entry, the PRD phase's.
func TestResolvePlan_ATierChoosesAModelPerPhase(t *testing.T) {
	for tier, want := range map[string]map[string]string{
		"SIMPLE": {
			PhasePRD: "claude-sonnet-5-5/medium", PhasePRDReview: "claude-sonnet-5-5/medium", PhasePRDRevision: "claude-sonnet-5-5/medium",
			PhaseRequirements: "claude-sonnet-5-5/medium", PhaseTestSpec: "claude-sonnet-5-5/low",
			PhaseTasks: "claude-sonnet-5-5/low", PhaseArchitecture: "claude-sonnet-5-5/low",
		},
		"STANDARD": {
			PhasePRD: "claude-sonnet-5-5/high", PhasePRDReview: "claude-sonnet-5-5/high", PhasePRDRevision: "claude-sonnet-5-5/high",
			PhaseRequirements: "claude-sonnet-5-5/high", PhaseTestSpec: "claude-sonnet-5-5/medium",
			PhaseTasks: "claude-sonnet-5-5/medium", PhaseArchitecture: "claude-sonnet-5-5/medium",
		},
		"advanced": {
			PhasePRD: "claude-opus-5-5/xhigh", PhasePRDReview: "claude-opus-5-5/xhigh", PhasePRDRevision: "claude-opus-5-5/xhigh",
			PhaseRequirements: "claude-opus-5-5/high", PhaseTestSpec: "claude-sonnet-5-5/high",
			PhaseTasks: "claude-sonnet-5-5/high", PhaseArchitecture: "claude-sonnet-5-5/high",
		},
	} {
		t.Run(tier, func(t *testing.T) {
			plan, err := ResolvePlan(tier, "", false, nil)
			if err != nil {
				t.Fatal(err)
			}
			for phase, w := range want {
				if got := plan.For(phase).String(); got != w {
					t.Errorf("%s = %s, want %s", phase, got, w)
				}
			}
			if plan.Default.String() != want[PhasePRD] {
				t.Errorf("default = %s, want the base entry %s", plan.Default, want[PhasePRD])
			}
			if got := plan.For("no_such_phase"); got.String() != plan.Default.String() {
				t.Errorf("a phase the plan does not hold runs on %s, not the default", got)
			}
		})
	}
}

// --model and --effort reach every phase, the review phases included.
func TestResolvePlan_AModelIdAndAnExplicitEffortApplyToEveryPhase(t *testing.T) {
	for _, tc := range []struct {
		name     string
		spec     string
		effort   core.Effort
		explicit bool
		want     string
	}{
		{"an explicit effort over a tier", "ADVANCED", core.EffortLow, true, "/low"},
		{"a model id with no effort", "claude-opus-5-5", "", false, "claude-opus-5-5"},
		{"a model id with an effort", "claude-haiku-4-5", core.EffortHigh, true, "claude-haiku-4-5/high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := ResolvePlan(tc.spec, tc.effort, tc.explicit, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, phase := range Phases {
				if got := plan.For(phase).String(); !strings.HasSuffix(got, tc.want) {
					t.Errorf("%s = %s, want %s", phase, got, tc.want)
				}
			}
		})
	}
}

// A --phase-model choice decides its phase alone: a choice for prd does not
// move the review phases, and one for prd_review moves only it.
func TestResolvePlan_APhaseChoiceDecidesItsPhaseAlone(t *testing.T) {
	choices := []PhaseChoice{
		mustChoice(t, "tasks=claude-haiku-4-5:medium"),
		mustChoice(t, "test_spec=ADVANCED"),
		mustChoice(t, "architecture=claude-opus-5-5"),
		mustChoice(t, "prd=claude-opus-5-5"),
		mustChoice(t, "prd_review=claude-haiku-4-5:low"),
	}
	plan, err := ResolvePlan("STANDARD", "", false, choices)
	if err != nil {
		t.Fatal(err)
	}
	for phase, w := range map[string]string{
		PhasePRD:          "claude-opus-5-5",
		PhasePRDReview:    "claude-haiku-4-5/low",
		PhasePRDRevision:  "claude-sonnet-5-5/high", // STANDARD's base entry, not the prd choice
		PhaseRequirements: "claude-sonnet-5-5/high",
		PhaseTasks:        "claude-haiku-4-5/medium",
		PhaseTestSpec:     "claude-sonnet-5-5/high", // ADVANCED's test_spec entry
		PhaseArchitecture: "claude-opus-5-5",        // an id with no effort sends none
	} {
		if got := plan.For(phase).String(); got != w {
			t.Errorf("%s = %s, want %s", phase, got, w)
		}
	}

	t.Run("an explicit effort reaches only a choice that names none", func(t *testing.T) {
		plan, _ := ResolvePlan("STANDARD", core.EffortLow, true, choices)
		if plan.For(PhaseArchitecture).Effort != core.EffortLow || plan.For(PhaseTasks).Effort != core.EffortMedium {
			t.Errorf("efforts = %v", plan.Phases)
		}
	})
	t.Run("an effort the model does not take is clamped", func(t *testing.T) {
		plan, _ := ResolvePlan("STANDARD", "", false, []PhaseChoice{mustChoice(t, "tasks=claude-haiku-4-5:max")})
		if m := plan.For(PhaseTasks); m.Effort != core.EffortHigh || m.Clamped != core.EffortMax {
			t.Errorf("clamp = %+v", m)
		}
	})
	t.Run("a later choice for the same phase wins", func(t *testing.T) {
		plan, _ := ResolvePlan("STANDARD", "", false, []PhaseChoice{mustChoice(t, "tasks=SIMPLE"), mustChoice(t, "tasks=claude-opus-5-5")})
		if got := plan.For(PhaseTasks).String(); got != "claude-opus-5-5" {
			t.Errorf("tasks = %s", got)
		}
	})
	t.Run("an unknown model names the flag", func(t *testing.T) {
		_, err := ResolvePlan("STANDARD", "", false, []PhaseChoice{mustChoice(t, "prd=gpt-9")})
		if CategoryOf(err) != CategoryModel || !strings.Contains(err.Error(), "--phase-model prd") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("an unknown default model", func(t *testing.T) {
		if _, err := ResolvePlan("gpt-9", "", false, nil); CategoryOf(err) != CategoryModel {
			t.Errorf("err = %v", err)
		}
	})
}

func TestParsePhaseChoice(t *testing.T) {
	for _, tc := range []struct {
		raw, phase, model string
		effort            core.Effort
		hasEffort         bool
	}{
		{" Tasks = anthropic/claude-sonnet-5-5:OFF ", PhaseTasks, "anthropic/claude-sonnet-5-5", "", true},
		{"prd=some:model", PhasePRD, "some:model", "", false}, // a colon that names no effort is part of the model
		{"prd_review=claude-opus-5-5", PhasePRDReview, "claude-opus-5-5", "", false},
		{"prd_revision=SIMPLE:low", PhasePRDRevision, "SIMPLE", core.EffortLow, true},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			c := mustChoice(t, tc.raw)
			if c.Phase != tc.phase || c.Model != tc.model || c.Effort != tc.effort || c.HasEffort != tc.hasEffort {
				t.Errorf("choice = %+v", c)
			}
		})
	}
	// A phase that is not one is refused naming the phases, in the order a
	// package is built: the review right after the PRD it reviews.
	_, err := ParsePhaseChoice("review=ADVANCED")
	if err == nil || !strings.Contains(err.Error(), "prd, prd_review, prd_revision, requirements, test_spec, tasks, architecture") {
		t.Errorf("an unknown phase: err = %v", err)
	}
	for _, bad := range []string{"tasks", "=claude-opus-5-5", "tasks=", "tasks=:high"} {
		t.Run("refuses "+bad, func(t *testing.T) {
			if _, err := ParsePhaseChoice(bad); err == nil {
				t.Errorf("%q was accepted", bad)
			}
		})
	}
}
