package specgen

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/schema"

	"github.com/agent-fox-dev/spec/afspec"
)

// marshalSchema renders a converted schema the way a provider would send it.
func marshalSchema(t *testing.T, s *schema.Schema) map[string]any {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshalling the schema: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("re-reading the schema: %v", err)
	}
	return m
}

func toolSchema(t *testing.T, doc string) (*schema.Schema, []string) {
	t.Helper()
	s, dropped, err := ToolSchema([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return s, dropped
}

// walkSchema visits every schema node reachable from s.
func walkSchema(s *schema.Schema, fn func(path string, node *schema.Schema)) {
	seen := map[*schema.Schema]bool{}
	var walk func(string, *schema.Schema)
	walk = func(path string, node *schema.Schema) {
		if node == nil || seen[node] {
			return
		}
		seen[node] = true
		fn(path, node)
		for _, name := range node.PropertyOrder {
			walk(path+"."+name, node.Properties[name])
		}
		walk(path+"[]", node.Items)
		if node.AdditionalProperties != nil {
			walk(path+"{}", node.AdditionalProperties.Schema)
		}
		for i, alt := range append(slices.Clone(node.AnyOf), node.OneOf...) {
			walk(path+"|"+strconv.Itoa(i), alt)
		}
	}
	walk("", s)
}

// propertyNames collects every property name in a marshalled schema, at every
// depth — the same walk a vendor's validator does.
func propertyNames(node any) []string {
	var out []string
	switch v := node.(type) {
	case map[string]any:
		for key, val := range v {
			if props, ok := val.(map[string]any); ok && key == "properties" {
				for name, sub := range props {
					out = append(out, name)
					out = append(out, propertyNames(sub)...)
				}
				continue
			}
			out = append(out, propertyNames(val)...)
		}
	case []any:
		for _, e := range v {
			out = append(out, propertyNames(e)...)
		}
	}
	return out
}

// The converter exists for this: the map-walking pipeline it replaced deleted
// keys by name at every depth, including inside `properties`, whose keys are
// property names. The v2 test object's `title` and `then` were deleted while
// `required` still listed them, so no argument object could satisfy the
// schema declared for submit_test_spec.
func TestToolSchema_KeepsAPropertyNamedLikeAKeyword(t *testing.T) {
	s, err := ArtifactSchema(afspec.StepTestSpec)
	if err != nil {
		t.Fatal(err)
	}
	test := s.Properties["tests"].Items
	for _, name := range []string{"title", "then", "given", "when", "id", "kind", "verifies"} {
		if test == nil || test.Properties[name] == nil {
			t.Errorf("the test object lost its %q property", name)
		}
	}
}

// Whatever the converter drops, no schema asks for a field it does not
// describe, and no property name a vendor rejects survives: Anthropic refuses
// the whole request over one, which once killed every generation step.
func TestToolSchema_LeavesEveryArtifactSchemaSatisfiable(t *testing.T) {
	for _, step := range afspec.GenerationSteps {
		t.Run(string(step), func(t *testing.T) {
			s, err := ArtifactSchema(step)
			if err != nil {
				t.Fatal(err)
			}
			walkSchema(s, func(path string, node *schema.Schema) {
				for _, name := range node.Required {
					if node.Properties[name] == nil {
						t.Errorf("%s: %q is required but not declared", path, name)
					}
				}
			})
			for _, name := range propertyNames(marshalSchema(t, s)) {
				if !undeclarableName.MatchString(name) {
					t.Errorf("declares a property named %q, which no tool schema may carry", name)
				}
			}
		})
	}
}

// $refs are resolved into what they name, $defs, metadata and the
// conditionals are dropped — afspec's validator enforces the conditionals
// and names the rule — and the constraints and descriptions survive.
func TestToolSchema_ResolvesRefsAndKeepsTheConstraints(t *testing.T) {
	for _, step := range afspec.GenerationSteps {
		s, err := ArtifactSchema(step)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(s)
		for _, kw := range []string{`"$ref"`, `"$defs"`, `"allOf"`, `"if"`, `"$id"`, `"$schema":"https://json-schema.org`} {
			if strings.Contains(string(raw), kw) {
				t.Errorf("%s: %s survived into the tool schema", step, kw)
			}
		}
		if m := marshalSchema(t, s); m["$schema"] != nil || s.Description == "" {
			t.Errorf("%s: top-level $schema %v, description %q", step, m["$schema"], s.Description)
		}
	}

	tasks, _ := ArtifactSchema(afspec.StepTasks)
	if task := tasks.Properties["tasks"].Items; task == nil || len(task.Properties) == 0 {
		t.Error("the task item schema is empty, so its $ref was not resolved")
	}
	s, _ := ArtifactSchema(afspec.StepTestSpec)
	test := s.Properties["tests"].Items
	if len(test.Properties["kind"].Enum) != 4 || test.Properties["id"].Pattern == "" ||
		s.Properties["tests"].MinItems == nil || *s.Properties["tests"].MinItems != 1 ||
		test.Properties["verifies"].UniqueItems == nil {
		t.Error("an enum, pattern, minItems or uniqueItems was lost")
	}
	if s.AdditionalProperties == nil || s.AdditionalProperties.Allowed {
		t.Error("additionalProperties:false was lost")
	}
	raw, _, _ := ToolSchema(afspec.Schemas()[afspec.TestSpecSchemaName])
	if raw.Properties["schema_version"] == nil || !raw.Properties["schema_version"].HasConst {
		t.Error("the schema_version const was lost")
	}
}

// Property order is model-visible and part of the provider's cache prefix,
// and a Go map cannot carry it.
func TestToolSchema_KeepsTheDocumentsPropertyOrder(t *testing.T) {
	s, _ := toolSchema(t, `{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"string"},"mid":{"type":"integer"}}}`)
	if want := []string{"zeta", "alpha", "mid"}; !slices.Equal(s.PropertyOrder, want) {
		t.Errorf("PropertyOrder = %v, want %v", s.PropertyOrder, want)
	}
	// The artifact's own fields are left out of the order with the rest of
	// what the handler writes.
	if ts, _ := ArtifactSchema(afspec.StepTestSpec); !slices.Equal(ts.PropertyOrder, []string{"tests"}) {
		t.Errorf("test_spec PropertyOrder = %v", ts.PropertyOrder)
	}
}

// A keyword the converter does not model rides through rather than being
// silently dropped, and the numeric ones are carried as numbers.
func TestToolSchema_CarriesEveryKeyword(t *testing.T) {
	s, _ := toolSchema(t, `{"type":"object","x-vendor-hint":"keep me","properties":{
		"n":{"type":"number","minimum":1,"maximum":9,"exclusiveMinimum":0,"exclusiveMaximum":10,"multipleOf":0.5},
		"s":{"type":"string","minLength":1,"maxLength":5,"format":"date"},
		"a":{"type":"array","maxItems":3,"uniqueItems":true,"items":{"oneOf":[{"type":"string"},{"type":"integer"}]}},
		"u":{"anyOf":[{"type":"string"},true]},
		"d":{"type":"object","additionalProperties":{"type":"string"}},
		"c":{"const":"fixed"}}}`)
	if m := marshalSchema(t, s); m["x-vendor-hint"] != "keep me" {
		t.Errorf("an unmodelled keyword was dropped: %v", m)
	}
	n, str, arr := s.Properties["n"], s.Properties["s"], s.Properties["a"]
	if *n.Minimum != 1 || *n.Maximum != 9 || *n.ExclusiveMinimum != 0 || *n.ExclusiveMaximum != 10 || *n.MultipleOf != 0.5 {
		t.Errorf("numeric bounds = %+v", n)
	}
	if *str.MinLength != 1 || *str.MaxLength != 5 || str.Format != "date" {
		t.Errorf("string bounds = %+v", str)
	}
	if *arr.MaxItems != 3 || !*arr.UniqueItems || len(arr.Items.OneOf) != 2 {
		t.Errorf("array = %+v", arr)
	}
	if u := s.Properties["u"]; len(u.AnyOf) != 2 {
		t.Errorf("anyOf = %+v", u.AnyOf)
	}
	if d := s.Properties["d"]; d.AdditionalProperties == nil || !d.AdditionalProperties.Allowed ||
		d.AdditionalProperties.Schema.Type != schema.TypeString {
		t.Errorf("a dictionary's value schema was lost: %+v", d.AdditionalProperties)
	}
	if c := s.Properties["c"]; !c.HasConst || string(c.Const) != `"fixed"` {
		t.Errorf("const = %s", c.Const)
	}
}

func TestToolSchema_MakesATypeArrayWithNullNullable(t *testing.T) {
	s, _ := toolSchema(t, `{"type":["string","null"]}`)
	if s.Type != schema.TypeString || !s.Nullable {
		t.Errorf("type=%q nullable=%v, want a nullable string", s.Type, s.Nullable)
	}
}

// A cycle is broken by an untyped schema rather than followed forever, and a
// $ref the document cannot resolve is dropped rather than left dangling.
func TestToolSchema_BreaksACircularRefAndDropsAnUnresolvedOne(t *testing.T) {
	s, _ := toolSchema(t, `{"type":"object","properties":{"node":{"$ref":"#/$defs/n"},"lost":{"$ref":"#/$defs/missing"},
		"far":{"$ref":"other.json#/x"}},
		"$defs":{"n":{"type":"object","properties":{"child":{"$ref":"#/$defs/n"}}}}}`)
	node := s.Properties["node"]
	if node == nil || node.Properties["child"] == nil {
		t.Fatal("the first level of the cycle was not resolved")
	}
	if len(node.Properties["child"].Properties) != 0 {
		t.Error("the cycle was followed instead of being broken")
	}
	for _, name := range []string{"lost", "far"} {
		if p := s.Properties[name]; p == nil || p.Type != "" || len(p.Properties) != 0 {
			t.Errorf("%s = %+v, want an untyped schema", name, p)
		}
	}
	if raw, _ := json.Marshal(s); strings.Contains(string(raw), "$ref") {
		t.Errorf("a $ref survived: %s", raw)
	}
}

func TestToolSchema_RefusesWhatIsNotOneSchemaObject(t *testing.T) {
	for name, doc := range map[string]string{
		"truncated JSON":     `{"type":`,
		"a non-object root":  `[1,2,3]`,
		"trailing data":      `{"type":"object"} {}`,
		"a duplicate key":    `{"type":"object","type":"string"}`,
		"a scalar schema":    `"string"`,
		"an empty document":  ``,
		"an unclosed object": `{"properties":{"a":{"type":"string"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ToolSchema([]byte(doc)); err == nil {
				t.Errorf("ToolSchema(%q) accepted it", doc)
			}
		})
	}
}

// A property no tool schema can declare is dropped, reported by its path so
// the caller can decide whether it has a value for it, and taken out of
// `required`: leaving it there would make the schema unsatisfiable.
func TestToolSchema_DropsAndReportsAnUndeclarableProperty(t *testing.T) {
	s, dropped := toolSchema(t, `{"type":"object",
		"properties":{"$schema":{"type":"string"},"spec_id":{"type":"string"},
			"rows":{"type":"array","items":{"type":"object",
				"properties":{"$ref_id":{"type":"string"},"n":{"type":"integer"}},
				"required":["$ref_id","n"]}}},
		"required":["$schema","spec_id","rows"],"additionalProperties":false}`)
	if s.Properties["$schema"] != nil || s.Properties["spec_id"] == nil || slices.Contains(s.Required, "$schema") {
		t.Errorf("properties %v, required %v", s.PropertyOrder, s.Required)
	}
	row := s.Properties["rows"].Items
	if row.Properties["$ref_id"] != nil || slices.Contains(row.Required, "$ref_id") || !slices.Equal(row.Required, []string{"n"}) {
		t.Errorf("the nested property survived: %+v", row)
	}
	if want := []string{"$schema", "rows[].$ref_id"}; !slices.Equal(dropped, want) {
		t.Errorf("dropped = %v, want %v", dropped, want)
	}
}
