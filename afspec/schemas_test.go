package afspec

import "testing"

func TestSchemas_ReturnsTheFourV3Schemas(t *testing.T) {
	schemas := Schemas()
	want := []string{PRDFrontmatterSchemaName, RequirementsSchemaName, TasksSchemaName, ProgressSchemaName}
	if len(schemas) != len(want) {
		t.Errorf("bundled schemas = %d; want %d", len(schemas), len(want))
	}
	for _, name := range want {
		if len(schemas[name]) == 0 {
			t.Errorf("schema %q is not bundled", name)
		}
	}
}
