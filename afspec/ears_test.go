package afspec

import "testing"

func TestCriterionBuilders_SetTheFieldsOfTheirPattern(t *testing.T) {
	t.Run("ubiquitous carries no condition", func(t *testing.T) {
		c := UbiquitousCriterion("REQ-1.1", "loader", "return a Spec")
		if c.Condition != nil {
			t.Errorf("condition=%v; must be absent", c.Condition)
		}
	})
	t.Run("unwanted requires a contract", func(t *testing.T) {
		c := UnwantedCriterion("REQ-1.1", "the file is missing", "loader", "return an error", "a *LoadError")
		if c.ContractText() != "a *LoadError" {
			t.Errorf("contract = %q", c.ContractText())
		}
	})
	t.Run("an empty system is omitted rather than written empty", func(t *testing.T) {
		c := UbiquitousCriterion("REQ-1.1", "", "do a thing")
		if c.System != nil {
			t.Errorf("System = %v; want nil so the schema's minLength is not violated", c.System)
		}
		if c.SystemName() != "system" {
			t.Errorf("SystemName() = %q; want the default %q", c.SystemName(), "system")
		}
	})
	t.Run("a check is its own or shared", func(t *testing.T) {
		own := UbiquitousCriterion("REQ-1.1", "", "do").WithCheck(CheckKindProperty, []string{"a table"}, "for x in table: assert f(x)")
		if own.Check.IsShared() || own.Check.KindText() != "property" || own.Check.Setup[0] != "a table" || own.Check.PseudocodeText() == "" {
			t.Errorf("own check = %+v", own.Check)
		}
		if empty := OwnCheck(CheckKindUnit, nil, ""); empty.Setup != nil || empty.Pseudocode != nil {
			t.Errorf("an empty setup or pseudocode was written: %+v", empty)
		}
		shared := own.WithSameCheckAs("PATH-1")
		if !shared.Check.IsShared() || *shared.Check.SameAs != "PATH-1" || shared.Check.Kind != nil || shared.Check.KindText() != "" {
			t.Errorf("shared check = %+v", shared.Check)
		}
		if own.Check.IsShared() {
			t.Error("WithSameCheckAs mutated the receiver")
		}
	})
}

// A criterion that names no system takes its requirement's; one that names
// its own keeps it.
func TestRequirementResolve_FillsTheSystem(t *testing.T) {
	r := Requirement{Id: "REQ-1", System: strPtr("the loader")}
	if got := r.Resolve(UbiquitousCriterion("REQ-1.1", "", "do")).RenderEARSSentence(); got != "THE the loader SHALL do" {
		t.Errorf("= %q", got)
	}
	if got := r.Resolve(UbiquitousCriterion("REQ-1.1", "own", "do")).RenderEARSSentence(); got != "THE own SHALL do" {
		t.Errorf("= %q", got)
	}
	if got := (Requirement{}).Resolve(UbiquitousCriterion("REQ-1.1", "", "do")).RenderEARSSentence(); got != "THE system SHALL do" {
		t.Errorf("= %q", got)
	}
}

func TestRenderEARSSentence_WritesEachPattern(t *testing.T) {
	cases := []struct {
		name string
		c    Criterion
		want string
	}{
		{
			"ubiquitous",
			UbiquitousCriterion("REQ-1.1", "loader", "return a Spec"),
			"THE loader SHALL return a Spec",
		},
		{
			"event_driven",
			EventDrivenCriterion("REQ-1.1", "a spec is loaded", "loader", "return a Spec"),
			"WHEN a spec is loaded, THE loader SHALL return a Spec",
		},
		{
			"event_driven with two conditions",
			EventDrivenCriterion("REQ-1.1", "a spec is loaded and it is sealed", "loader", "reject the write"),
			"WHEN a spec is loaded and it is sealed, THE loader SHALL reject the write",
		},
		{
			"state_driven",
			StateDrivenCriterion("REQ-1.1", "the spec is a draft", "loader", "allow edits"),
			"WHILE the spec is a draft, THE loader SHALL allow edits",
		},
		{
			"unwanted",
			UnwantedCriterion("REQ-1.1", "the file is missing", "loader", "return an error", "a *LoadError"),
			"IF the file is missing, THEN THE loader SHALL return an error",
		},
		{
			"optional",
			OptionalCriterion("REQ-1.1", "architecture.md is present", "loader", "read it"),
			"WHERE architecture.md is present, THE loader SHALL read it",
		},
		{
			"missing system falls back to the default",
			Criterion{Id: "REQ-1.1", Pattern: CriterionPatternUbiquitous, Action: "do a thing"},
			"THE system SHALL do a thing",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.RenderEARSSentence(); got != tc.want {
				t.Errorf("RenderEARSSentence() =\n  %q\nwant\n  %q", got, tc.want)
			}
		})
	}
}

// §11.2: a contract the coder cannot see does not exist.
func TestRenderEARS_AlwaysShowsTheContract(t *testing.T) {
	c := UnwantedCriterion("REQ-1.1", "the file is missing", "loader", "return an error", "a *LoadError naming the file")
	got := c.RenderEARS()
	want := "IF the file is missing, THEN THE loader SHALL return an error\n   → a *LoadError naming the file"
	if got != want {
		t.Errorf("RenderEARS() =\n%q\nwant\n%q", got, want)
	}

	plain := UbiquitousCriterion("REQ-1.2", "loader", "do a thing")
	if plain.RenderEARS() != plain.RenderEARSSentence() {
		t.Error("a criterion with no contract gained a contract line")
	}
}

func TestWithContract_ReturnsACopy(t *testing.T) {
	base := UbiquitousCriterion("REQ-1.1", "loader", "do a thing")
	withContract := base.WithContract("a nil error")
	if base.Contract != nil {
		t.Error("WithContract mutated the receiver")
	}
	if withContract.ContractText() != "a nil error" {
		t.Errorf("contract = %q", withContract.ContractText())
	}
}
