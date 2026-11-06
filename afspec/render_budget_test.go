package afspec

import (
	"strings"
	"testing"
)

func TestRenderCombined_DropsTheArchitectureFirst(t *testing.T) {
	spec := loadFixture(t, fixtureWithArch)
	out := spec.RenderCombined(WithMaxTokens(EstimateTokens(spec.RenderCombined()) - 1))
	if strings.Contains(out, "# Architecture") {
		t.Error("the architecture section survived a budget it could not fit in")
	}
	if !strings.Contains(out, "# Requirements") || !strings.Contains(out, "- setup:") {
		t.Error("the first level dropped more than the architecture section")
	}
}

// Past the architecture, the budget slims the checks; a spec without an
// architecture goes straight to that.
func TestRenderCombined_SlimsTheChecksWhenTheBudgetIsTight(t *testing.T) {
	for _, fixture := range []string{fixtureWithArch, fixtureValidSpec} {
		t.Run(fixture, func(t *testing.T) {
			spec := loadFixture(t, fixture)
			out := spec.RenderCombined(WithMaxTokens(1))
			if strings.Contains(out, "# Architecture") || strings.Contains(out, "- setup:") || strings.Contains(out, "**Setup:**") {
				t.Error("the tightest budget kept the architecture or the setup")
			}
			for _, section := range []string{"# PRD", "# Requirements", "# Tasks"} {
				if !strings.Contains(out, section) {
					t.Errorf("section %q is missing", section)
				}
			}
			for _, r := range spec.Requirements.Requirements {
				for _, c := range r.Criteria {
					if !strings.Contains(out, "["+c.Id+"]") || !strings.Contains(out, "Check: "+c.Check.KindText()) {
						t.Errorf("criterion %s or its check kind is missing from the slim render", c.Id)
					}
				}
			}
		})
	}
}

// Slimming may drop context but never what a check is or what a smoke check
// asserts.
func TestRenderRequirementsSlim_KeepsTheKindAndTheEndState(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	slim := renderRequirements(spec.Requirements, true)

	for _, p := range spec.Requirements.ExecutionPaths {
		for _, line := range p.Smoke.EndState {
			if !strings.Contains(slim, line) {
				t.Errorf("path %s: an end-state line is missing from the slim render", p.Id)
			}
		}
		for _, c := range p.Smoke.RealComponents {
			if !strings.Contains(slim, c) {
				t.Errorf("path %s: a real component is missing from the slim render", p.Id)
			}
		}
	}
	if strings.Contains(slim, "```") {
		t.Error("the slim render kept the pseudocode it is meant to drop")
	}
	if EstimateTokens(slim) >= EstimateTokens(spec.Requirements.Render()) {
		t.Error("the slim render is not smaller than the full one")
	}
}

func TestRenderIndividual_AppliesTheBudgetInLevels(t *testing.T) {
	spec := loadFixture(t, fixtureWithArch)

	out := spec.RenderIndividual(WithMaxTokens(sumMapTokens(spec.RenderIndividual()) - 1))
	if _, present := out["architecture"]; present {
		t.Error("architecture was not dropped at the first level")
	}
	if !strings.Contains(out["requirements"], "- setup:") {
		t.Error("the first level slimmed the checks as well")
	}

	out = spec.RenderIndividual(WithMaxTokens(1))
	if strings.Contains(out["requirements"], "- setup:") {
		t.Error("the checks were not slimmed at the second level")
	}
}

func TestRenderIndividualScoped_AppliesTheBudgetInLevels(t *testing.T) {
	spec := loadFixture(t, fixtureWithArch)
	full := spec.RenderIndividualScoped(1)
	if _, present := full["architecture"]; !present {
		t.Fatal("the unbudgeted scoped render has no architecture")
	}

	out := spec.RenderIndividualScoped(1, WithMaxTokens(sumMapTokens(full)-1))
	if _, present := out["architecture"]; present {
		t.Error("architecture was not dropped at the first level")
	}
	if out["requirements"] != full["requirements"] {
		t.Error("the first level changed the requirements as well")
	}

	out = spec.RenderIndividualScoped(1, WithMaxTokens(1))
	if strings.Contains(out["requirements"], "- setup:") || !strings.Contains(out["requirements"], "**REQ-2:** Cross-file validation (other task)") {
		t.Errorf("the second level did not slim the task's checks and keep the others listed:\n%s", out["requirements"])
	}
}

func TestWithMaxTokens_ZeroMeansUnlimited(t *testing.T) {
	spec := loadFixture(t, fixtureWithArch)
	if spec.RenderCombined(WithMaxTokens(0)) != spec.RenderCombined() {
		t.Error("WithMaxTokens(0) changed the output; it should disable the budget")
	}
}
