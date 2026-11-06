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
	for _, absent := range []string{"profile:", "source:", "supersedes:", "updated_at:", "owner:", "tags:"} {
		if strings.Contains(empty, absent) {
			t.Errorf("field %q was written:\n%s", absent, empty)
		}
	}
	if !strings.Contains(empty, "intent_hash: null") {
		t.Errorf("intent_hash is not written as null:\n%s", empty)
	}

	set := base
	set.Profile, set.Source = ProfileLite, "docs/prd.md"
	set.Supersedes = []string{"04"}
	out := string(renderPRD(&set))
	for _, want := range []string{`profile: "lite"`, `source: "docs/prd.md"`, `supersedes: ["04"]`} {
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
// after schema_version, where renderPRD writes profile and steering_version.
func withFrontmatterLine(t *testing.T, dir, line string) {
	t.Helper()
	path := filepath.Join(dir, "prd.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const after = "schema_version: 3\n"
	if !strings.Contains(string(data), after) {
		t.Fatalf("%s has no %q", path, after)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), after, after+line+"\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// profile and steering_version are optional: each round-trips where it is set,
// in its place, and is never written where it is not.
func TestOptionalFrontmatter_RoundTripsInItsPlace(t *testing.T) {
	for _, tc := range []struct {
		name, line                string
		wantProfile, wantSteering string
	}{
		{"steering set", `steering_version: "2.0.1"`, "", "2.0.1"},
		{"profile set", `profile: "full"`, "full", ""},
		{"absent", "", "", ""},
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
			if spec.SteeringVersion != tc.wantSteering || spec.Profile != tc.wantProfile {
				t.Errorf("SteeringVersion = %q, Profile = %q; want %q and %q", spec.SteeringVersion, spec.Profile, tc.wantSteering, tc.wantProfile)
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
			if tc.line == "" && (strings.Contains(string(got), "steering_version") || strings.Contains(string(got), "profile")) {
				t.Errorf("an absent optional field was written:\n%s", got)
			}
		})
	}

	set := string(renderPRD(&Spec{SpecID: "05", SpecName: "f", Title: "F", Status: "draft", SchemaVersion: SchemaVersion,
		Profile: ProfileLite, SteeringVersion: "1.2.3", Source: "docs/x.md", PRDBody: "# F\n"}))
	schema, profile, sv, source := strings.Index(set, "schema_version:"), strings.Index(set, "profile:"),
		strings.Index(set, "steering_version:"), strings.Index(set, "source:")
	if !(schema < profile && profile < sv && sv < source) {
		t.Errorf("the optional fields are not in the order profile, steering_version, source:\n%s", set)
	}
}

// A version 2 frontmatter's updated_at, owner and tags are not fields of
// version 3: they are ignored on a load and never written back.
func TestParsePRD_IgnoresTheFieldsOfVersion2(t *testing.T) {
	fm, _, err := parsePRD([]byte("---\nspec_id: \"01\"\nspec_name: \"x\"\nstatus: \"draft\"\nupdated_at: \"2026-01-01T00:00:00Z\"\nowner: \"me\"\ntags: [\"a\"]\n---\n# X\n"), "prd.md")
	if err != nil {
		t.Fatal(err)
	}
	if fm.Raw["owner"] != "me" || len(fm.Extensions) != 0 {
		t.Errorf("Raw = %v, Extensions = %v", fm.Raw, fm.Extensions)
	}
	out := string(renderPRD(&Spec{SpecID: fm.SpecID, SpecName: fm.SpecName, Status: fm.Status, SchemaVersion: SchemaVersion, PRDBody: "# X\n"}))
	for _, gone := range []string{"updated_at", "owner", "tags"} {
		if strings.Contains(out, gone) {
			t.Errorf("%s was written back:\n%s", gone, out)
		}
	}
}
