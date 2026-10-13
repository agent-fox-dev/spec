package afspec

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// fullySetSpec is the valid fixture with every optional field of the package
// set, including values that would break a Markdown table if written raw.
func fullySetSpec(t *testing.T) *Spec {
	t.Helper()
	s := loadFixture(t, fixtureValidSpec)
	str := func(v string) *string { return &v }
	s.Owner, s.Source = "team-widgets", "https://example.com/issues/7"
	s.Supersedes, s.Tags = []string{"00_old_loader"}, []string{"core", "io"}
	s.IntentHash = str("sha256:abc123")
	s.Requirements.Glossary = RequirementsV2JsonGlossary{"pipe | term": "a definition\nover two lines"}
	s.Requirements.ExternalApis = []ExternalApi{{
		Package: "example.com/lib", Version: "v1.2.3", Verified: true,
		Symbols: []ExternalApiSymbol{{Name: "Union", ImportPath: "example.com/lib/u",
			Signature: "func Union(a, b T) T", Notes: str("returns a | b")}},
	}}
	s.Tasks.Dependencies = []Dependency{{Spec: "00_bootstrap", Reason: "uses its loader"}}
	s.Tasks.TestCommands.SpecTests = str("go test ./afspec/... -run TestSpec01")
	opt := true
	s.Tasks.Tasks[0].Optional = &opt
	return s
}

// leaves collects every scalar value of v's JSON form, with its key, skipping
// the keys a render deliberately leaves out.
func leaves(t *testing.T, v any, skip map[string]bool) map[string]string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	var walk func(path, key string, x any)
	walk = func(path, key string, x any) {
		switch y := x.(type) {
		case map[string]any:
			for k, c := range y {
				if !skip[k] {
					walk(path+"/"+k, k, c)
				}
			}
		case []any:
			for i, c := range y {
				walk(fmt.Sprintf("%s/%d", path, i), key, c)
			}
		case string:
			out[path] = y
		case float64:
			out[path] = fmt.Sprint(y)
		}
	}
	walk("", "", doc)
	return out
}

// unescape undoes the table-cell escaping, so a value can be found in the
// render as it is in the files.
func unescape(render string) string {
	return strings.NewReplacer(`\|`, "|", "<br>", "\n").Replace(render)
}

// Nothing in the package is dropped by an unbudgeted render: every value of
// every artifact, and every frontmatter field, appears in it.
func TestTheRenderDropsNothingFromThePackage(t *testing.T) {
	s := fullySetSpec(t)
	// $schema and the artifacts' copies of the identity are format
	// constants and duplicates of the metadata; the pattern is the EARS
	// sentence's keyword.
	skip := map[string]bool{"$schema": true, "schema_version": true, "spec_id": true, "spec_name": true, "pattern": true}

	renders := map[string]string{"combined": s.RenderCombined()}
	ind := s.RenderIndividual()
	renders["individual"] = ind["prd"] + ind["requirements"] + ind["test_spec"] + ind["tasks"]
	for name, render := range renders {
		render = unescape(render)
		for _, artifact := range []any{s.Requirements, s.TestSpec, s.Tasks} {
			for path, value := range leaves(t, artifact, skip) {
				if !strings.Contains(render, value) {
					t.Errorf("%s: %s = %q is not in the render", name, path, value)
				}
			}
		}
		for _, want := range []string{"01_test_feature", s.Title, s.Status, s.CreatedAt, s.UpdatedAt, s.Owner,
			s.Source, "00_old_loader", "core", "io", "sha256:abc123", "Schema version:** 2",
			"verified", "_(optional)_"} {
			if !strings.Contains(render, want) {
				t.Errorf("%s: the metadata lacks %q", name, want)
			}
		}
	}
}

// A scoped render shows everything that belongs to the whole spec, and names
// by id what belongs to other tasks: nothing in the package is missing from
// it altogether.
func TestAScopedRenderMissesNothingFromThePackage(t *testing.T) {
	s := fullySetSpec(t)
	for _, task := range s.Tasks.Tasks {
		out := s.RenderIndividualScoped(task.Id)
		render := unescape(out["prd"] + out["requirements"] + out["test_spec"] + out["tasks"])
		specWide := []any{s.Requirements.ExternalApis, s.Requirements.Glossary, s.Tasks.Dependencies, s.Tasks.TestCommands}
		for _, part := range specWide {
			for path, value := range leaves(t, part, nil) {
				if !strings.Contains(render, value) {
					t.Errorf("task %d: %s = %q is not in the scoped render", task.Id, path, value)
				}
			}
		}
		var ids []string
		for _, r := range s.Requirements.Requirements {
			ids = append(ids, r.Id)
		}
		for _, p := range s.Requirements.ExecutionPaths {
			ids = append(ids, p.Id)
		}
		for _, tt := range s.TestSpec.Tests {
			ids = append(ids, tt.Id)
		}
		for _, other := range s.Tasks.Tasks {
			ids = append(ids, fmt.Sprintf("%d. %s", other.Id, other.Title))
		}
		for _, id := range append(ids, "01_test_feature", s.Requirements.Introduction) {
			if !strings.Contains(render, id) {
				t.Errorf("task %d: the scoped render does not name %q", task.Id, id)
			}
		}
	}
}

// A table cell keeps its row: a pipe or a newline in a value is escaped.
func TestTableCellsAreEscaped(t *testing.T) {
	out := fullySetSpec(t).Requirements.Render()
	for _, want := range []string{`| pipe \| term | a definition<br>over two lines |`, "returns a \\| b |"} {
		if !strings.Contains(out, want) {
			t.Errorf("the render lacks %q:\n%s", want, out)
		}
	}
	if got := code("a`b"); got != "``a`b``" {
		t.Errorf("code(a`b) = %q", got)
	}
}
