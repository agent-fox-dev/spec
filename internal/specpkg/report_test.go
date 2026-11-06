package specpkg

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The validation report and the traceability of a package damaged in every
// way the report names are exactly the ones spec has always written.
// UPDATE_GOLDEN=1 go test -run '^TestReportOfAndTraceOf_MatchTheGolden$' ./internal/specpkg regenerates the golden.
func TestReportOfAndTraceOf_MatchTheGolden(t *testing.T) {
	spec := brokenSpec(t)
	got, err := json.MarshalIndent(map[string]any{
		"validation":   ReportOf(spec.Validate()),
		"traceability": TraceOf(spec),
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "report.golden.json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(got, '\n'), want) {
		t.Errorf("the report differs from testdata/report.golden.json:\n%s", got)
	}
}
