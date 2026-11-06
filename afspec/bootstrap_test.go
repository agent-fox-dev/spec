package afspec

import "testing"

func TestFinalize_ReportsEachMissingArtifact(t *testing.T) {
	spec, errs := NewBootstrapSpec("01", "test_feature").Finalize()
	if spec != nil {
		t.Error("Finalize returned a Spec despite missing artifacts")
	}
	if len(errs) != 3 {
		t.Fatalf("errors = %v; want one per missing artifact", errs)
	}
	for _, e := range errs {
		if e.Rule != "bootstrap" {
			t.Errorf("rule = %q; want bootstrap", e.Rule)
		}
	}
}

func TestFinalize_AssemblesAValidSpec(t *testing.T) {
	spec, errs := bootstrapFrom(t, "01", "test_feature").Finalize()
	if len(errs) > 0 {
		t.Fatalf("Finalize on complete artifacts = %v", errs)
	}
	if spec.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version = %d; want %d", spec.SchemaVersion, SchemaVersion)
	}
}

// The profile is carried into the spec, so a lite bootstrap is held to the
// lite rules.
func TestFinalize_CarriesTheProfile(t *testing.T) {
	b := bootstrapFrom(t, "01", "test_feature")
	b.Profile = ProfileLite
	spec, errs := b.Finalize()
	if spec != nil || len(errs) == 0 {
		t.Fatal("a lite bootstrap with a path was accepted")
	}
	b.Requirements = &RequirementsV3Json{}
	*b.Requirements = deepCopyRequirements(*loadFixture(t, fixtureValidSpec).Requirements)
	b.Requirements.ExecutionPaths = nil
	spec, errs = b.Finalize()
	if len(errs) > 0 || spec.Profile != ProfileLite {
		t.Errorf("Finalize = %v, %v; want a lite spec", spec, errs)
	}
}

func TestFinalize_RefusesArtifactsOfAnotherSpec(t *testing.T) {
	if _, errs := bootstrapFrom(t, "07", "other_feature").Finalize(); len(errs) == 0 {
		t.Fatal("artifacts declaring a different spec_id were accepted")
	}
}
