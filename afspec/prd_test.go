package afspec

import (
	"os"
	"path/filepath"
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

// withFrontmatterLine adds a line to the frontmatter of the prd.md in dir, right
// after schema_version, where renderPRD writes steering_version.
func withFrontmatterLine(t *testing.T, dir, line string) {
	t.Helper()
	path := filepath.Join(dir, "prd.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const after = "schema_version: 2\n"
	if !strings.Contains(string(data), after) {
		t.Fatalf("%s has no %q", path, after)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), after, after+line+"\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// steering_version is optional: it round-trips where it is set, after
// schema_version and before owner, and is never written where it is not.
func TestSteeringVersion_RoundTripsInItsPlace(t *testing.T) {
	for _, tc := range []struct {
		name, line, want string
	}{
		{"set", `steering_version: "2.0.1"`, "2.0.1"},
		{"absent", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := copyFixture(t, fixtureValidSpec)
			if tc.line != "" {
				withFrontmatterLine(t, dir, tc.line)
			}
			want, err := os.ReadFile(filepath.Join(dir, "prd.md"))
			if err != nil {
				t.Fatal(err)
			}
			spec := loadFixture(t, dir)
			if spec.SteeringVersion != tc.want {
				t.Errorf("SteeringVersion = %q; want %q", spec.SteeringVersion, tc.want)
			}
			if v := spec.Validate(); !v.Valid {
				t.Errorf("the package does not validate: %v", v.Errors)
			}

			out := t.TempDir()
			if err := spec.Save(out); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(out, "prd.md"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("prd.md is not byte-identical after a round trip:\n%s", firstDifference(string(want), string(got)))
			}
			if tc.want == "" && strings.Contains(string(got), "steering_version") {
				t.Errorf("an absent steering_version was written:\n%s", got)
			}
			if again := loadFixture(t, out); again.SteeringVersion != tc.want {
				t.Errorf("reloaded SteeringVersion = %q; want %q", again.SteeringVersion, tc.want)
			}
		})
	}

	set := string(renderPRD(&Spec{SpecID: "05", SpecName: "f", Title: "F", Status: "draft", SchemaVersion: SchemaVersion,
		SteeringVersion: "1.2.3", Owner: "someone", PRDBody: "# F\n"}))
	sv, schema, owner := strings.Index(set, "steering_version:"), strings.Index(set, "schema_version:"), strings.Index(set, "owner:")
	if !(schema < sv && sv < owner) {
		t.Errorf("steering_version is not between schema_version and owner:\n%s", set)
	}
}
