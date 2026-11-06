package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// reportLine is the one shape every line the render writes to stderr takes.
var reportLine = regexp.MustCompile(`^[^ :]+: \S`)

// listing returns the paths under root, relative and sorted, so a test can say
// a directory is exactly as it was.
func listing(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel != "." {
			found = append(found, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(found)
	return found
}

func TestMain_ReportsEveryProblemOfARunAndWritesNothing(t *testing.T) {
	source := t.TempDir()
	writeTree(t, source, map[string]string{
		"af-prd/SKILL.md":  skillFile("af-prd", "Read [references/gone.md](references/gone.md).\n"),
		"af-spec/SKILL.md": "---\nname: af-impl\ndescription: a name that is not its directory's.\n---\n\n# af-spec\n",
		// {{invoke af-fix}} is 17 bytes and the /af-fix it renders to is 7, so
		// the source body is 11 over the ceiling to render 1 over it.
		"af-impl/SKILL.md": docOf(paddedFrontmatter, bodyOfBytes("Run {{invoke af-fix}} next.\n", bodyLimit+11)),
	})
	out := t.TempDir()
	writeTree(t, out, map[string]string{"from-a-previous-run.txt": "kept\n"})
	before := listing(t, out)

	var stdout, stderr bytes.Buffer
	code := Main([]string{"--source", source, "--out", out}, &stdout, &stderr)

	if code == exitOK {
		t.Fatalf("code %d, want non-zero", code)
	}
	report := strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n")
	for _, line := range report {
		if !reportLine.MatchString(line) {
			t.Errorf("line %q is not of the form <source path>: <problem>", line)
		}
	}
	// Every problem of the run is reported, not the first: the broken link, the
	// name, the unknown token and the body over the ceiling are all named.
	for _, want := range []string{
		"af-prd/SKILL.md: links references/gone.md",
		`af-spec/SKILL.md: name "af-impl" is not the name of its directory`,
		"af-impl/SKILL.md: holds {{invoke af-fix}}",
		"af-impl/SKILL.md: body is 24,577 bytes for claude",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("the report does not name %q:\n%s", want, stderr.String())
		}
	}
	if got := listing(t, out); !slices.Equal(got, before) {
		t.Errorf("the output directory changed: %v, want %v", got, before)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want the report on stderr alone", stdout.String())
	}
}

func TestMain_WarnsAboutAReferenceFileNothingLinksAndLeavesTheCodeZero(t *testing.T) {
	source := t.TempDir()
	writeTree(t, source, map[string]string{
		"af-impl/SKILL.md":                  skillFile("af-impl", "Read [references/landing.md](references/landing.md).\n"),
		"af-impl/references/landing.md":     "# landing\n",
		"af-impl/references/old.md":         "# a file nothing links\n",
		"_shared/references/speca-specr.md": "# linked by nobody either\n",
	})
	out := t.TempDir()

	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--source", source, "--out", out}, &stdout, &stderr); code != exitOK {
		t.Fatalf("code %d, want 0; stderr %q", code, stderr.String())
	}
	for _, want := range []string{
		source + "/af-impl/references/old.md: linked by nothing; not rendered",
		source + "/_shared/references/speca-specr.md: linked by nothing; not rendered",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("the report does not warn %q:\n%s", want, stderr.String())
		}
	}
	if strings.Contains(stderr.String(), "landing.md: linked by nothing") {
		t.Error("the file the skill links was warned about")
	}
}

func TestMain_RefusesAnEmptySourceOrOutputDirectory(t *testing.T) {
	for _, argv := range [][]string{{"--source", ""}, {"--out", ""}} {
		var stdout, stderr bytes.Buffer
		if code := Main(argv, &stdout, &stderr); code != exitUsage {
			t.Errorf("Main(%v) = %d, want %d", argv, code, exitUsage)
		}
	}
}
