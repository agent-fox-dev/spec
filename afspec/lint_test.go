package afspec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunLintSpecs_PassesAValidSpec(t *testing.T) {
	root := specRoot(t, map[string]string{"01_test_feature": fixtureValidSpec})

	result, err := RunLintSpecs(root, true)
	if err != nil {
		t.Fatalf("RunLintSpecs = %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d on a valid spec; findings: %v", result.ExitCode, result.Findings)
	}
}

func TestRunLintSpecs_FailsOnABrokenRule(t *testing.T) {
	root := specRoot(t, map[string]string{"01_test_feature": fixtureValidSpec})
	// Orphan a test so that C7 fires.
	editTasks(t, filepath.Join(root, "01_test_feature", "tasks.json"), func(tasks *TasksV2Json) {
		tasks.Tasks[0].Tests = []string{"TS-01-1"}
	})

	result, err := RunLintSpecs(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode == 0 {
		t.Fatal("linting a spec with orphaned tests exited 0")
	}
	if !hasFinding(result.Findings, "C7") {
		t.Errorf("no C7 finding: %v", result.Findings)
	}
}

// A package that does not load is one finding, and the others are still
// linted.
func TestRunLintSpecs_ReportsAPackageThatDoesNotLoad(t *testing.T) {
	root := specRoot(t, map[string]string{
		"01_test_feature": fixtureValidSpec,
		"02_broken":       "testdata/malformed_json",
	})
	result, err := RunLintSpecs(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode == 0 || !hasFinding(result.Findings, "load-failure") {
		t.Errorf("findings = %v; want a load-failure error", result.Findings)
	}
	for _, f := range result.Findings {
		if f.SpecName == "test_feature" && f.Severity == "error" {
			t.Errorf("the valid spec has an error: %+v", f)
		}
	}
}

// A fully implemented spec is one with every task done or dropped; it is
// skipped unless every spec is asked for.
func TestRunLintSpecs_SkipsAFullyImplementedSpecUnlessAll(t *testing.T) {
	root := specRoot(t, map[string]string{"01_test_feature": fixtureValidSpec})
	// Break the plan and mark every task done.
	editTasks(t, filepath.Join(root, "01_test_feature", "tasks.json"), func(tasks *TasksV2Json) {
		tasks.Tasks[0].Tests = []string{"TS-01-1"}
		for i := range tasks.Tasks {
			tasks.Tasks[i].State = TaskStateDone
		}
	})

	result, err := RunLintSpecs(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 {
		t.Errorf("a fully implemented spec produced findings: %v", result.Findings)
	}

	result, err = RunLintSpecs(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) == 0 {
		t.Error("linting all specs did not lint the fully implemented one")
	}
}

func TestDiscoverLintSpecs(t *testing.T) {
	root := specRoot(t, map[string]string{
		"01_test_feature":  fixtureValidSpec,
		"02_draft_feature": fixtureDraft,
		"03_no_reqs":       "testdata/missing_req",
		"not_a_spec":       fixtureValidSpec,
	})
	if err := os.Remove(filepath.Join(root, "02_draft_feature", "tasks.json")); err != nil {
		t.Fatal(err)
	}

	infos, err := DiscoverLintSpecs(root, "")
	if err != nil {
		t.Fatalf("DiscoverLintSpecs = %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("infos = %+v; want the two named packages with a requirements.json", infos)
	}
	first, second := infos[0], infos[1]
	if first.Name != "test_feature" || first.Prefix != 1 || !first.HasTasks || !first.HasPRD {
		t.Errorf("infos[0] = %+v", first)
	}
	if second.Name != "draft_feature" || second.HasTasks {
		t.Errorf("infos[1] = %+v; want draft_feature without tasks", second)
	}

	filtered, err := DiscoverLintSpecs(root, "draft_feature")
	if err != nil || len(filtered) != 1 || filtered[0].Name != "draft_feature" {
		t.Errorf("filtered = %+v, %v; want draft_feature alone", filtered, err)
	}

	for name, args := range map[string][2]string{
		"an unknown name": {root, "nope"},
		"an empty root":   {t.TempDir(), ""},
		"a missing root":  {filepath.Join(root, "missing"), ""},
	} {
		if _, err := DiscoverLintSpecs(args[0], args[1]); err == nil {
			t.Errorf("%s: DiscoverLintSpecs = nil error", name)
		}
	}
}

func TestSortFindings_OrdersBySpecFileAndSeverity(t *testing.T) {
	findings := []LintFinding{
		{SpecName: "02", Severity: "warning", Rule: "scope"},
		{SpecName: "01", File: "tasks.json", Severity: "error", Rule: "C7"},
		{SpecName: "01", File: "requirements.json", Severity: "novel", Rule: "x"},
		{SpecName: "01", File: "requirements.json", Severity: "hint", Rule: "y"},
		{SpecName: "01", File: "requirements.json", Severity: "warning", Rule: "vague"},
	}
	sorted := SortFindings(findings)
	var rules []string
	for _, f := range sorted {
		rules = append(rules, f.Rule)
	}
	// An unknown severity sorts as a hint; the sort is stable among equals.
	want := []string{"vague", "x", "y", "C7", "scope"}
	for i := range want {
		if rules[i] != want[i] {
			t.Fatalf("order = %v; want %v", rules, want)
		}
	}
	if findings[0].SpecName != "02" {
		t.Error("SortFindings reordered its input")
	}
}

func TestComputeExitCode_FailsOnlyOnAnError(t *testing.T) {
	for _, tc := range []struct {
		name     string
		findings []LintFinding
		want     int
	}{
		{"no findings", nil, 0},
		{"warnings alone", []LintFinding{{Severity: "warning"}, {Severity: "hint"}}, 0},
		{"an error", []LintFinding{{Severity: "warning"}, {Severity: "error"}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ComputeExitCode(tc.findings); got != tc.want {
				t.Errorf("ComputeExitCode = %d; want %d", got, tc.want)
			}
		})
	}
}
