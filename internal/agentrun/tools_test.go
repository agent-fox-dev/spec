package agentrun

import (
	"slices"
	"testing"
)

// code_search joins a grant only when the run has an index, and never grows
// the shared read-only list.
func TestWithCodeSearch(t *testing.T) {
	before := slices.Clone(ReadOnlyFileTools)
	if got := WithCodeSearch(ReadOnlyFileTools, false); !slices.Equal(got, ReadOnlyFileTools) {
		t.Errorf("without an index = %v", got)
	}
	got := WithCodeSearch(ReadOnlyFileTools, true)
	if !slices.Equal(got, append(slices.Clone(before), ToolCodeSearch)) {
		t.Errorf("with an index = %v", got)
	}
	got[0] = "changed"
	if !slices.Equal(ReadOnlyFileTools, before) {
		t.Errorf("the shared list changed: %v", ReadOnlyFileTools)
	}
}
