package envelope_test

import (
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
)

// Sections hands out its own slice, so one caller cannot reorder another's.
func TestSections_IsAFreshSliceInRenderOrder(t *testing.T) {
	a := envelope.Sections()
	if strings.Join(a, ",") != "prd,architecture,requirements,test_spec,tasks" {
		t.Fatalf("sections = %v", a)
	}
	a[0] = "changed"
	if envelope.Sections()[0] != envelope.SectionPRD {
		t.Error("Sections shares its backing array")
	}
}
