package specgen

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
)

// TS-10-13: the report in spec's result is the one it has always been. The
// pipeline runs over a package with an unowned test, and the JSON of its
// validation and traceability is compared, byte for byte, with the golden file
// captured from the converters this package had before they moved to
// internal/specpkg.
func TestTheResultsReportIsTheOneSpecAlwaysWrote_TS10_13(t *testing.T) {
	ws := newWorkspace(t)
	a := newAuthor(t, "01", "test_feature")
	a.skipStepValidation = true
	dropOneOwnedTest(a.artifacts[afspec.StepTasks])

	got, _ := Run(context.Background(), newOptions(ws, a))
	if got == nil {
		t.Fatal("the run produced no result")
	}
	have, err := json.MarshalIndent(map[string]any{"validation": got.Validation, "traceability": got.Traceability}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "internal", "specpkg", "testdata", "pipeline.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(have, '\n'), want) {
		t.Errorf("the result's report differs from internal/specpkg/testdata/pipeline.golden.json:\n%s", have)
	}
}
