package afspec

import "testing"

func TestComputeCoverage_CoversEverythingInACompleteSpec(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	report := spec.Requirements.ComputeCoverage()

	if len(report.Uncovered) != 0 {
		t.Errorf("uncovered = %v; the canonical fixture covers everything", report.Uncovered)
	}
	if len(report.Covered) != 5 {
		t.Errorf("covered = %v; want the four criteria and the one path", report.Covered)
	}
}

// A criterion without a check, one whose shared check resolves nowhere, and
// a path without a smoke check are the gaps, in artifact order; a shared
// check that resolves to a criterion's own check or to a path covers.
func TestComputeCoverage_ListsTheGapsInArtifactOrder(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	reqs := spec.Requirements
	reqs.Requirements[0].Criteria[1].Check = Check{}
	reqs.Requirements[0].Criteria[2].Check = SameAs("REQ-1.2")
	reqs.Requirements[1].Criteria[0].Check = SameAs("PATH-1")
	reqs.ExecutionPaths[0].Smoke.EndState = nil

	report := reqs.ComputeCoverage()
	if len(report.Covered) != 1 || report.Covered[0] != "REQ-1.1" {
		t.Errorf("covered = %v; want just REQ-1.1", report.Covered)
	}
	want := []string{"REQ-1.2", "REQ-1.3", "REQ-2.1", "PATH-1"}
	if len(report.Uncovered) != len(want) {
		t.Fatalf("uncovered = %v; want %v", report.Uncovered, want)
	}
	for i, id := range want {
		if report.Uncovered[i] != id {
			t.Errorf("uncovered[%d] = %q; want %q", i, report.Uncovered[i], id)
		}
	}
}

// §8.5: the matrix comes from the checks and task.criteria, so it cannot
// drift from the artifacts.
func TestComputeTraceability_DerivesLinksFromChecksAndTasks(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	spec.Requirements.Requirements[0].Criteria[2].Check = SameAs("REQ-1.1")
	matrix := spec.ComputeTraceability()

	if len(matrix.Links) != 5 {
		t.Fatalf("links = %d; want four criteria plus one path", len(matrix.Links))
	}

	byID := map[string]TraceLink{}
	for _, l := range matrix.Links {
		byID[l.ID] = l
	}

	link := byID["REQ-1.1"]
	if link.Kind != "criterion" || link.Check != "unit" {
		t.Errorf("link = %+v; want a criterion with a unit check", link)
	}
	if len(link.Tasks) != 1 || link.Tasks[0] != 1 {
		t.Errorf("tasks = %v; want the owning task", link.Tasks)
	}
	if !link.Covered || !link.Owned {
		t.Errorf("covered=%v owned=%v; both should hold", link.Covered, link.Owned)
	}
	if shared := byID["REQ-1.3"]; shared.Check != "same as REQ-1.1" || !shared.Covered {
		t.Errorf("shared link = %+v", shared)
	}

	path := byID["PATH-1"]
	if path.Kind != "path" || path.Check != "smoke" || !path.Covered {
		t.Errorf("path link = %+v; want the smoke check", path)
	}
	if len(path.Tasks) != 1 || path.Tasks[0] != 3 || !path.Owned {
		t.Errorf("path tasks = %v; want the integration task", path.Tasks)
	}
}

func TestComputeTraceability_MarksACoveredButUnownedCriterion(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	spec.Tasks.Tasks[0].Criteria = []string{"REQ-1.1", "REQ-1.2"}

	for _, link := range spec.ComputeTraceability().Links {
		if link.ID != "REQ-1.3" {
			continue
		}
		if !link.Covered {
			t.Error("the criterion still carries its check, so it is covered")
		}
		if link.Owned {
			t.Error("no task owns REQ-1.3 any more, so the criterion is not owned")
		}
		return
	}
	t.Fatal("REQ-1.3 is missing from the matrix")
}

// A lite package has no integration task, so a path (which it must not have)
// would be owned by nothing.
func TestComputeTraceability_OwnsAPathByTheIntegrationTask(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	spec.Profile = ProfileLite
	for _, l := range spec.ComputeTraceability().Links {
		if l.Kind == "path" && (l.Owned || len(l.Tasks) != 0) {
			t.Errorf("a path of a lite package is owned: %+v", l)
		}
	}
}

func TestComputeTraceability_IsEmptyWithoutRequirements(t *testing.T) {
	if got := len(CreateSpec("05", "empty").ComputeTraceability().Links); got != 0 {
		t.Errorf("links = %d on a scaffold; want none", got)
	}
	var bare Spec
	if got := len(bare.ComputeTraceability().Links); got != 0 {
		t.Errorf("links = %d on a spec with no requirements; want none", got)
	}
}
