package main

import (
	"slices"
	"strings"
	"testing"
)

func TestReadSource_TakesEveryDirectoryButTheSharedPoolAsASkill(t *testing.T) {
	fsys := tree(map[string]string{
		"af-prd/SKILL.md":             skillFile("af-prd", "# af-prd\n\nThe body of af-prd.\n"),
		"af-spec/SKILL.md":            skillFile("af-spec", "# af-spec\n\nThe body of af-spec.\n"),
		"_shared/references/speca.md": "# speca\n",
		"notes.md":                    "a loose file, not a skill\n",
	})

	src, problems := readSource(fsys, "skills")
	if len(problems) != 0 {
		t.Fatalf("problems: %s", strings.Join(lines(problems), " | "))
	}
	if got := names(src); !slices.Equal(got, []string{"af-prd", "af-spec"}) {
		t.Errorf("skills = %v, want [af-prd af-spec]", got)
	}
	sk := src.skills[1]
	if sk.path != "skills/af-spec/SKILL.md" || sk.dir != "skills/af-spec" {
		t.Errorf("paths = %q, %q", sk.path, sk.dir)
	}
	if !strings.Contains(sk.frontmatter, "name: af-spec") || strings.Contains(sk.frontmatter, "---") {
		t.Errorf("frontmatter = %q", sk.frontmatter)
	}
	if sk.body != "\n# af-spec\n\nThe body of af-spec.\n" {
		t.Errorf("body = %q, want the text after the closing --- line", sk.body)
	}
}

func TestReadSource_IncludesALinkedFileAndWhatItLinks(t *testing.T) {
	fsys := tree(map[string]string{
		"af-spec/SKILL.md": skillFile("af-spec",
			"Read [references/tasks.md](references/tasks.md) and [references/speca-specr.md](references/speca-specr.md).\n"),
		"af-spec/references/tasks.md":       "Then [references/verdict.md](references/verdict.md).\n",
		"af-spec/references/verdict.md":     "# the verdict\n",
		"_shared/references/speca-specr.md": "# speca and specr\n",
		"_shared/references/prd-shape.md":   "# the PRD's shape, which no skill links\n",
	})

	src, problems := readSource(fsys, "skills")
	if len(problems) != 0 {
		t.Fatalf("problems: %s", strings.Join(lines(problems), " | "))
	}
	sk := src.skills[0]
	want := []string{"speca-specr.md", "tasks.md", "verdict.md"}
	if got := refNames(sk); !slices.Equal(got, want) {
		t.Errorf("included = %v, want %v", got, want)
	}
	if from := sk.refs["speca-specr.md"].from; from != "skills/_shared/references/speca-specr.md" {
		t.Errorf("speca-specr.md came from %q, want the shared pool", from)
	}
	if from := sk.refs["tasks.md"].from; from != "skills/af-spec/references/tasks.md" {
		t.Errorf("tasks.md came from %q, want the skill's own directory", from)
	}
	if _, included := sk.refs["prd-shape.md"]; included {
		t.Error("prd-shape.md is included although nothing links it")
	}
}

func TestReadSource_RefusesALinkThatResolvesInNeitherDirectory(t *testing.T) {
	fsys := tree(map[string]string{
		"af-spec/SKILL.md": skillFile("af-spec", "Read [references/tasks.md](references/tasks.md).\n"),
	})

	_, problems := readSource(fsys, "skills")
	want := "skills/af-spec/SKILL.md: links references/tasks.md, which is in neither " +
		"skills/af-spec/references/ nor skills/_shared/references/"
	if got := onlyProblem(t, problems); got != want {
		t.Errorf("problem =\n  %s\nwant\n  %s", got, want)
	}
}

func TestReadSource_RefusesALinkFromAReferenceFileThatResolvesNowhere(t *testing.T) {
	fsys := tree(map[string]string{
		"af-spec/SKILL.md":            skillFile("af-spec", "Read [references/tasks.md](references/tasks.md).\n"),
		"af-spec/references/tasks.md": "Then [references/gone.md](references/gone.md).\n",
	})

	_, problems := readSource(fsys, "skills")
	got := onlyProblem(t, problems)
	if !strings.HasPrefix(got, "skills/af-spec/references/tasks.md: links references/gone.md") {
		t.Errorf("problem = %s, want it to name the reference file that links gone.md", got)
	}
}

func TestReadSource_RefusesOneReferenceNameInBothDirectories(t *testing.T) {
	fsys := tree(map[string]string{
		"af-spec/SKILL.md":                  skillFile("af-spec", "Read [references/speca-specr.md](references/speca-specr.md).\n"),
		"af-spec/references/speca-specr.md": "the skill's own\n",
		"_shared/references/speca-specr.md": "the shared one\n",
	})

	_, problems := readSource(fsys, "skills")
	got := onlyProblem(t, problems)
	for _, want := range []string{
		"speca-specr.md",
		"skills/af-spec/references/",
		"skills/_shared/references/",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("problem %q does not name %q", got, want)
		}
	}
}

func TestReadSource_RefusesASkillWhoseFrontmatterCannotBeRead(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		want  string
	}{
		"no SKILL.md": {
			files: map[string]string{"af-x/references/a.md": "# a\n"},
			want:  "skills/af-x: holds no SKILL.md",
		},
		"no frontmatter": {
			files: map[string]string{"af-x/SKILL.md": "# af-x\n\nStraight into the body.\n"},
			want:  "skills/af-x/SKILL.md: has no frontmatter",
		},
		"frontmatter never closes": {
			files: map[string]string{"af-x/SKILL.md": "---\nname: af-x\n\n# af-x\n"},
			want:  "skills/af-x/SKILL.md: has no frontmatter",
		},
		"frontmatter is not YAML": {
			files: map[string]string{"af-x/SKILL.md": "---\nname: [unclosed\n---\n\n# af-x\n"},
			want:  "skills/af-x/SKILL.md: frontmatter is not YAML",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, problems := readSource(tree(tc.files), "skills")
			if got := onlyProblem(t, problems); !strings.HasPrefix(got, tc.want) {
				t.Errorf("problem = %s, want it to start with %s", got, tc.want)
			}
		})
	}
}
