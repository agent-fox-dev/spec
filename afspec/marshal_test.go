package afspec

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMarshalJSON_OrdersFieldsBySchema(t *testing.T) {
	data, err := MarshalJSON(loadFixture(t, fixtureValidSpec).Requirements)
	if err != nil {
		t.Fatalf("MarshalJSON = %v", err)
	}
	out := string(data)
	last := -1
	for _, key := range []string{`"$schema"`, `"spec_id"`, `"spec_name"`, `"schema_version"`,
		`"requirements"`, `"system"`, `"criteria"`, `"check"`, `"execution_paths"`, `"smoke"`} {
		i := strings.Index(out, key)
		if i < 0 {
			t.Fatalf("key %s is missing", key)
		}
		if i < last {
			t.Errorf("key %s is out of schema order", key)
		}
		last = i
	}
	// Inside a criterion the check comes last, after the contract.
	if strings.Index(out, `"contract": "a non-nil`) > strings.Index(out, `"check": {`) {
		t.Error("the check is written before the contract")
	}
}

// Spec text containing <, > or & must round-trip, and encoding/json escapes
// them by default.
func TestMarshalJSON_DoesNotEscapeHTML(t *testing.T) {
	req := RequirementsV3Json{Requirements: []Requirement{{Title: `write {"error": <message>} when a && b`}}}
	data, err := MarshalJSON(&req)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if strings.Contains(out, `\u003c`) || strings.Contains(out, `\u0026`) {
		t.Errorf("angle brackets or ampersands were escaped:\n%s", out)
	}
	if !strings.Contains(out, "<message>") {
		t.Errorf("the literal text is missing:\n%s", out)
	}
}

// The output decodes with encoding/json and re-marshals to the same bytes. The
// two marshallers do not agree field for field on purpose: an explicitly empty
// array such as "execution_paths": [] is preserved here and dropped by
// encoding/json's omitempty, and dropping it would break round-trip fidelity.
func TestMarshalJSON_IsStableUnderDecodeAndReencode(t *testing.T) {
	spec := loadFixture(t, fixtureValidSpec)
	spec.Requirements.ExecutionPaths = []ExecutionPath{}
	first, err := MarshalJSON(spec.Requirements)
	if err != nil {
		t.Fatal(err)
	}

	var decoded RequirementsV3Json
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("our output does not decode: %v", err)
	}
	second, err := MarshalJSON(&decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) {
		t.Errorf("marshal → unmarshal → marshal is not stable:\n%s", firstDifference(string(first), string(second)))
	}
	if decoded.ExecutionPaths == nil {
		t.Error(`the explicit "execution_paths": [] decoded as absent`)
	}
}

// A nil execution_paths is left out, which is how a lite package is written.
func TestMarshalJSON_OmitsANilExecutionPaths(t *testing.T) {
	data, err := MarshalJSON(&RequirementsV3Json{Requirements: []Requirement{}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "execution_paths") {
		t.Errorf("a nil execution_paths was written:\n%s", data)
	}
}

func TestMarshalJSON_WritesNilAsNull(t *testing.T) {
	data, err := MarshalJSON(nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "null\n" {
		t.Errorf("= %q; want %q", data, "null\n")
	}
}
