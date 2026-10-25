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
		`"introduction"`, `"glossary"`, `"requirements"`, `"execution_paths"`} {
		i := strings.Index(out, key)
		if i < 0 {
			t.Fatalf("key %s is missing", key)
		}
		if i < last {
			t.Errorf("key %s is out of schema order", key)
		}
		last = i
	}
}

func TestMarshalJSON_SortsMapKeys(t *testing.T) {
	req := RequirementsV2Json{Glossary: RequirementsV2JsonGlossary{"zebra": "z", "alpha": "a", "middle": "m"}}
	data, err := MarshalJSON(&req)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if strings.Index(out, "alpha") > strings.Index(out, "middle") ||
		strings.Index(out, "middle") > strings.Index(out, "zebra") {
		t.Errorf("glossary keys are not sorted:\n%s", out)
	}
}

// Spec text containing <, > or & must round-trip, and encoding/json escapes
// them by default.
func TestMarshalJSON_DoesNotEscapeHTML(t *testing.T) {
	req := RequirementsV2Json{Introduction: `write {"error": <message>} when a && b`}
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
// array such as the integration task's "criteria": [] is preserved here and
// dropped by encoding/json's omitempty, and dropping it would break round-trip
// fidelity.
func TestMarshalJSON_IsStableUnderDecodeAndReencode(t *testing.T) {
	first, err := MarshalJSON(loadFixture(t, fixtureValidSpec).Tasks)
	if err != nil {
		t.Fatal(err)
	}

	var decoded TasksV2Json
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
	if decoded.Tasks[2].Criteria == nil {
		t.Error(`the integration task's explicit "criteria": [] decoded as absent`)
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
