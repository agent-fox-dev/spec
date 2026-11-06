package main

import (
	"strings"
	"testing"
)

// metaTree builds a one-skill tree whose frontmatter is the text given, so a
// test varies only the keys it is about.
func metaTree(dir, frontmatter string) map[string]string {
	return map[string]string{dir + "/SKILL.md": "---\n" + frontmatter + "\n---\n\n# body\n"}
}

func TestReadSource_RefusesANameThatBreaksTheAgentSkillsRule(t *testing.T) {
	for name, tc := range map[string]struct {
		dir, frontmatter string
		wantProblem      bool
	}{
		"missing":            {"af-spec", "description: a sentence about it.", true},
		"differs from dir":   {"af-spec", "name: af-impl\ndescription: a sentence about it.", true},
		"uppercase":          {"Af-Spec", "name: Af-Spec\ndescription: a sentence about it.", true},
		"double hyphen":      {"af--spec", "name: af--spec\ndescription: a sentence about it.", true},
		"leading hyphen":     {"-af-spec", "name: -af-spec\ndescription: a sentence about it.", true},
		"trailing hyphen":    {"af-spec-", "name: af-spec-\ndescription: a sentence about it.", true},
		"underscore":         {"af_spec", "name: af_spec\ndescription: a sentence about it.", true},
		"65 characters":      {strings.Repeat("a", 65), "name: " + strings.Repeat("a", 65) + "\ndescription: a sentence about it.", true},
		"one character":      {"a", "name: a\ndescription: a sentence about it.", false},
		"64 characters":      {strings.Repeat("a", 64), "name: " + strings.Repeat("a", 64) + "\ndescription: a sentence about it.", false},
		"digits and hyphens": {"af-spec-2", "name: af-spec-2\ndescription: a sentence about it.", false},
	} {
		t.Run(name, func(t *testing.T) {
			_, problems := readSource(tree(metaTree(tc.dir, tc.frontmatter)), "skills")
			if tc.wantProblem {
				if got := onlyProblem(t, problems); !strings.Contains(got, "name") {
					t.Errorf("problem %q does not name the rule broken", got)
				}
				return
			}
			if len(problems) != 0 {
				t.Errorf("problems: %s", strings.Join(lines(problems), " | "))
			}
		})
	}
}

func TestReadSource_RefusesADescriptionThatIsMissingEmptyOrTooLong(t *testing.T) {
	for name, tc := range map[string]struct {
		frontmatter string
		wantProblem bool
	}{
		"missing":         {"name: af-spec", true},
		"empty":           {"name: af-spec\ndescription: \"\"", true},
		"only whitespace": {"name: af-spec\ndescription: \"   \"", true},
		"1024 characters": {"name: af-spec\ndescription: " + strings.Repeat("d", descriptionLimit), false},
		"1025 characters": {"name: af-spec\ndescription: " + strings.Repeat("d", descriptionLimit+1), true},
	} {
		t.Run(name, func(t *testing.T) {
			_, problems := readSource(tree(metaTree("af-spec", tc.frontmatter)), "skills")
			if tc.wantProblem {
				if got := onlyProblem(t, problems); !strings.Contains(got, "description") {
					t.Errorf("problem %q does not name the description", got)
				}
				return
			}
			if len(problems) != 0 {
				t.Errorf("problems: %s", strings.Join(lines(problems), " | "))
			}
		})
	}
}

func TestReadSource_SaysTheLimitWhenADescriptionIsTooLong(t *testing.T) {
	_, problems := readSource(tree(metaTree("af-spec", "name: af-spec\ndescription: "+strings.Repeat("d", descriptionLimit+1))), "skills")
	got := onlyProblem(t, problems)
	for _, want := range []string{"skills/af-spec/SKILL.md:", "1025", "1,024"} {
		if !strings.Contains(got, want) {
			t.Errorf("problem %q does not say %q", got, want)
		}
	}
}
