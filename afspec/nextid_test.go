package afspec

import "testing"

func TestNextIDs_FollowTheHighestSuffixInUse(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)

	if got := NextRequirementID(*spec.Requirements); got != "REQ-3" {
		t.Errorf("NextRequirementID = %q; want REQ-3", got)
	}
	if got := NextCriterionID(spec.Requirements.Requirements[0]); got != "REQ-1.4" {
		t.Errorf("NextCriterionID = %q; want REQ-1.4", got)
	}
	if got := NextExecutionPathID(*spec.Requirements); got != "PATH-2" {
		t.Errorf("NextExecutionPathID = %q; want PATH-2", got)
	}
	if got := NextTaskID(*spec.Tasks); got != 3 {
		t.Errorf("NextTaskID = %d; want 3, the integration task's id", got)
	}
}

func TestNextIDs_StartAtOneWhenEmpty(t *testing.T) {
	spec := CreateSpec("05", "empty")

	if got := NextRequirementID(*spec.Requirements); got != "REQ-1" {
		t.Errorf("NextRequirementID = %q; want REQ-1", got)
	}
	if got := NextExecutionPathID(*spec.Requirements); got != "PATH-1" {
		t.Errorf("NextExecutionPathID = %q; want PATH-1", got)
	}
	if got := NextTaskID(*spec.Tasks); got != 1 {
		t.Errorf("NextTaskID = %d; want 1", got)
	}
	if got := NextCriterionID(Requirement{Id: "REQ-1"}); got != "REQ-1.1" {
		t.Errorf("NextCriterionID = %q; want REQ-1.1", got)
	}
}

func TestNextRequirementID_IgnoresMalformedIDs(t *testing.T) {
	req := RequirementsV3Json{
		Requirements: []Requirement{
			{Id: "REQ-2"},
			{Id: "garbage"},
			{Id: "05-REQ-7"},
			{Id: "REQ-notanumber"},
		},
	}
	if got := NextRequirementID(req); got != "REQ-3" {
		t.Errorf("NextRequirementID = %q; want REQ-3", got)
	}
}
