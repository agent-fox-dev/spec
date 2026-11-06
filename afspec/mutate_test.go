package afspec

import (
	"path/filepath"
	"testing"
)

func TestAddRequirement_AppendsToACopyAndRefusesADuplicate(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	req := *spec.Requirements

	added, err := AddRequirement(req, Requirement{
		Id:       "REQ-3",
		Title:    "Third",
		Criteria: []Criterion{UbiquitousCriterion("REQ-3.1", "loader", "do a thing").WithCheck(CheckKindUnit, nil, "")},
	})
	if err != nil {
		t.Fatalf("AddRequirement = %v", err)
	}
	if len(added.Requirements) != 3 || len(req.Requirements) != 2 {
		t.Errorf("added=%d original=%d; the original must not change", len(added.Requirements), len(req.Requirements))
	}

	if _, err := AddRequirement(req, Requirement{Id: "REQ-1", Title: "Dup"}); err == nil {
		t.Error("a duplicate requirement ID was accepted")
	}
}

func TestGetAndRemoveRequirement_WorkOnCopies(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	req := *spec.Requirements

	got, ok := GetRequirement(req, "REQ-2")
	if !ok || got.Title != "Cross-file validation" {
		t.Fatalf("GetRequirement = %v, %v", got, ok)
	}
	got.Title = "changed"
	got.Criteria[0].Check.Setup[0] = "changed"
	if req.Requirements[1].Title == "changed" || req.Requirements[1].Criteria[0].Check.Setup[0] == "changed" {
		t.Error("GetRequirement returned a view into the artifact")
	}

	reduced, removed := RemoveRequirement(req, "REQ-1")
	if !removed || len(reduced.Requirements) != 1 {
		t.Errorf("RemoveRequirement removed=%v len=%d", removed, len(reduced.Requirements))
	}
	if _, removed := RemoveRequirement(req, "REQ-9"); removed {
		t.Error("RemoveRequirement reported removing a requirement that does not exist")
	}
}

func TestAddCriterionAndExecutionPath_RefuseDuplicates(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)

	r, err := AddCriterion(spec.Requirements.Requirements[0],
		UbiquitousCriterion("REQ-1.4", "loader", "do another thing").WithSameCheckAs("REQ-1.1"))
	if err != nil {
		t.Fatalf("AddCriterion = %v", err)
	}
	if len(r.Criteria) != 4 || len(spec.Requirements.Requirements[0].Criteria) != 3 {
		t.Error("AddCriterion mutated the original requirement")
	}
	if _, err := AddCriterion(r, UbiquitousCriterion("REQ-1.1", "loader", "dup")); err == nil {
		t.Error("a duplicate criterion ID was accepted")
	}

	if c, ok := GetCriterion(r, "REQ-1.4"); !ok || !c.Check.IsShared() || *c.Check.SameAs != "REQ-1.1" {
		t.Errorf("GetCriterion = %+v, %v; want the criterion just added with its shared check", c, ok)
	}

	withPath, err := AddExecutionPath(*spec.Requirements, ExecutionPath{
		Id: "PATH-2", Title: "Another path",
		Steps: []PathStep{{Actor: "operator", Action: "does something"}, {Actor: "system", Action: "responds"}},
		Smoke: Smoke{RealComponents: []string{"system"}, EndState: []string{"it responded"}},
	})
	if err != nil {
		t.Fatalf("AddExecutionPath = %v", err)
	}
	if len(withPath.ExecutionPaths) != 2 {
		t.Errorf("paths = %d; want 2", len(withPath.ExecutionPaths))
	}
	if _, err := AddExecutionPath(withPath, ExecutionPath{Id: "PATH-1", Title: "Dup"}); err == nil {
		t.Error("a duplicate path ID was accepted")
	}
}

func TestAddTaskAndDependency_WorkOnCopies(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)

	added, err := AddTask(*spec.Tasks, Task{
		Id: 3, Title: "Another",
		Criteria: []string{"REQ-1.1"},
		Steps:    []string{"do it"},
	})
	if err != nil {
		t.Fatalf("AddTask = %v", err)
	}
	if len(added.Tasks) != 3 || len(spec.Tasks.Tasks) != 2 {
		t.Error("AddTask mutated the original")
	}
	if _, err := AddTask(added, Task{Id: 1}); err == nil {
		t.Error("a duplicate task ID was accepted")
	}

	withDep := AddDependency(*spec.Tasks, Dependency{Spec: "00", Reason: "uses the loader"})
	if len(withDep.Dependencies) != 1 || len(spec.Tasks.Dependencies) != 0 {
		t.Error("AddDependency mutated the original")
	}
}

func TestAddExternalAPI_RefusesADuplicatePackage(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	added, err := AddExternalAPI(*spec.Requirements, ExternalApi{
		Package: "github.com/spf13/cobra", Version: "v1.10.2", Verified: true,
		Symbols: []ExternalApiSymbol{{Name: "Command", ImportPath: "github.com/spf13/cobra", Signature: "type Command struct{}"}},
	})
	if err != nil {
		t.Fatalf("AddExternalAPI = %v", err)
	}
	if len(added.ExternalApis) != 1 {
		t.Fatalf("external APIs = %d; want 1", len(added.ExternalApis))
	}
	if _, err := AddExternalAPI(added, ExternalApi{Package: "github.com/spf13/cobra", Version: "v1", Verified: false}); err == nil {
		t.Error("a duplicate package was accepted")
	}
}

func TestExtractInterfaceSummary_ReportsContracts(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	summary := extractInterfaceSummary(spec)

	if _, present := summary["glossary"]; present {
		t.Error("the summary carries a glossary, which version 3 has no field for")
	}
	contracts, ok := summary["contracts"].([]map[string]string)
	if !ok || len(contracts) == 0 {
		t.Fatalf("contracts missing from the summary: %v", summary)
	}
	for _, c := range contracts {
		if c["criterion_id"] == "" || c["contract"] == "" {
			t.Errorf("incomplete contract entry: %v", c)
		}
	}
}

// A dependent spec sees the contracts of the specs it depends on, and nothing
// when there are none or the root cannot be read.
func TestLoadDependentInterfaces_SummarisesTheUpstreamSpecs(t *testing.T) {
	root := specRoot(t, map[string]string{
		"01_test_feature":  fixtureValidSpec,
		"02_draft_feature": fixtureDraft,
	})

	summaries := LoadDependentInterfaces("02", root)
	if len(summaries) != 1 {
		t.Fatalf("summaries = %v; want one for the upstream spec", summaries)
	}
	if _, ok := summaries[0]["contracts"]; !ok {
		t.Errorf("the summary carries no contracts: %v", summaries[0])
	}

	if got := LoadDependentInterfaces("01", root); len(got) != 0 {
		t.Errorf("a spec with no dependencies returned %v", got)
	}
	if got := LoadDependentInterfaces("02", filepath.Join(root, "nope")); got == nil || len(got) != 0 {
		t.Errorf("an unreadable root should degrade to an empty slice, got %v", got)
	}
}
