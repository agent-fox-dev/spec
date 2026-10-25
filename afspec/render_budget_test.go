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
	if !strings.Contains(out, "# Requirements") || !strings.Contains(out, "**Pseudocode:**") {
		t.Error("the first level dropped more than the architecture section")
	}
}

// Past the architecture, the budget slims the tests; a spec without an
// architecture goes straight to that.
func TestRenderCombined_SlimsTheTestsWhenTheBudgetIsTight(t *testing.T) {
	for _, fixture := range []string{fixtureWithArch, fixtureValidSpec} {
		t.Run(fixture, func(t *testing.T) {
			spec := loadFixture(t, fixture)
			out := spec.RenderCombined(WithMaxTokens(1))
			if strings.Contains(out, "# Architecture") || strings.Contains(out, "**Pseudocode:**") {
				t.Error("the tightest budget kept the architecture or the pseudocode")
			}
			for _, section := range []string{"# PRD", "# Requirements", "# Test Specification", "# Tasks"} {
				if !strings.Contains(out, section) {
					t.Errorf("section %q is missing", section)
				}
			}
			for _, test := range spec.TestSpec.Tests {
				if !strings.Contains(out, test.Id) {
					t.Errorf("test %s is missing from the slim render", test.Id)
				}
			}
		})
	}
}

// Slimming may drop context but never what a test asserts or what it verifies.
func TestRenderTestSpecSlim_KeepsVerifiesAndThen(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	slim := renderTestSpecSlim(spec.TestSpec)

	for _, test := range spec.TestSpec.Tests {
		if !strings.Contains(slim, test.Id) {
			t.Errorf("test %s is missing from the slim render", test.Id)
		}
		for _, ref := range test.Verifies {
			if !strings.Contains(slim, ref) {
				t.Errorf("test %s: verifies %s is missing from the slim render", test.Id, ref)
			}
		}
		for _, then := range test.Then {
			if !strings.Contains(slim, then) {
				t.Errorf("test %s: a Then clause is missing from the slim render", test.Id)
			}
		}
	}
	if strings.Contains(slim, "**Pseudocode:**") {
		t.Error("the slim render kept the pseudocode it is meant to drop")
	}
	if EstimateTokens(slim) >= EstimateTokens(spec.TestSpec.Render()) {
		t.Error("the slim render is not smaller than the full one")
	}
}

func TestRenderIndividual_AppliesTheBudgetInLevels(t *testing.T) {
	spec := loadFixture(t, fixtureWithArch)

	out := spec.RenderIndividual(WithMaxTokens(sumMapTokens(spec.RenderIndividual()) - 1))
	if _, present := out["architecture"]; present {
		t.Error("architecture was not dropped at the first level")
	}
	if !strings.Contains(out["test_spec"], "**Pseudocode:**") {
		t.Error("the first level slimmed the tests as well")
	}

	out = spec.RenderIndividual(WithMaxTokens(1))
	if strings.Contains(out["test_spec"], "**Pseudocode:**") {
		t.Error("the test spec was not slimmed at the second level")
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
	if out["test_spec"] != full["test_spec"] {
		t.Error("the first level changed the tests as well")
	}

	out = spec.RenderIndividualScoped(1, WithMaxTokens(1))
	if strings.Contains(out["test_spec"], "**Pseudocode:**") || !strings.Contains(out["test_spec"], "## Other tests") {
		t.Errorf("the second level did not slim the task's tests and keep the others listed:\n%s", out["test_spec"])
	}
}

func TestWithMaxTokens_ZeroMeansUnlimited(t *testing.T) {
	spec := loadFixture(t, fixtureWithArch)
	if spec.RenderCombined(WithMaxTokens(0)) != spec.RenderCombined() {
		t.Error("WithMaxTokens(0) changed the output; it should disable the budget")
	}
}
