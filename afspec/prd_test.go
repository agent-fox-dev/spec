package afspec

import (
	"strings"
	"testing"
)

func TestRenderPRD_WritesOptionalFieldsOnlyWhenSet(t *testing.T) {
	base := Spec{
		SpecID: "05", SpecName: "feature", Title: "Feature", Status: "draft",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
		SchemaVersion: SchemaVersion, PRDBody: "# Feature\n",
	}
	empty := string(renderPRD(&base))
	for _, absent := range []string{"owner:", "source:", "supersedes:", "tags:"} {
		if strings.Contains(empty, absent) {
			t.Errorf("empty optional field %q was written:\n%s", absent, empty)
		}
	}
	if !strings.Contains(empty, "intent_hash: null") {
		t.Errorf("intent_hash is not written as null:\n%s", empty)
	}

	set := base
	set.Owner, set.Source = "someone", "docs/prd.md"
	set.Supersedes, set.Tags = []string{"04"}, []string{"a", "b"}
	out := string(renderPRD(&set))
	for _, want := range []string{`owner: "someone"`, `source: "docs/prd.md"`, `supersedes: ["04"]`, `tags: ["a", "b"]`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestParsePRD_RefusesMalformedFrontmatter(t *testing.T) {
	for _, tc := range []struct {
		name, content, wantInError string
	}{
		{"no opening delimiter", "spec_id: \"01\"\n---\n# Body\n", ""},
		{"no closing delimiter", "---\nspec_id: \"01\"\n# Body\n", ""},
		{"no spec_id", "---\ntitle: \"x\"\n---\n# Body\n", "spec_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parsePRD([]byte(tc.content), "prd.md")
			if err == nil {
				t.Fatal("a malformed prd.md was accepted")
			}
			if !strings.Contains(err.Error(), tc.wantInError) {
				t.Errorf("the error does not name %q: %v", tc.wantInError, err)
			}
		})
	}
}
