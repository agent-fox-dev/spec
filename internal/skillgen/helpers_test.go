package main

import (
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// tree builds an in-memory skills tree. Every key is a path under the source
// root, such as "af-spec/SKILL.md" or "_shared/references/speca-specr.md".
func tree(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for path, data := range files {
		fsys[path] = &fstest.MapFile{Data: []byte(data)}
	}
	return fsys
}

// skillFile returns a SKILL.md with a valid frontmatter for name and the body
// given, so that a test varies only what it is about.
func skillFile(name, body string) string {
	return "---\nname: " + name + "\ndescription: " + name + " does one thing, described in a sentence.\n---\n\n" + body
}

// names returns the skills' names, for comparing against a want list.
func names(src *source) []string {
	got := make([]string, 0, len(src.skills))
	for _, sk := range src.skills {
		got = append(got, sk.name)
	}
	return got
}

// lines renders problems the way the render writes them, one to a line, so a
// test can assert on the text an author would read.
func lines(problems []problem) []string {
	got := make([]string, 0, len(problems))
	for _, p := range problems {
		got = append(got, p.String())
	}
	return got
}

// refNames returns the base names of a skill's included reference files, sorted.
func refNames(sk *skill) []string {
	got := make([]string, 0, len(sk.refs))
	for name := range sk.refs {
		got = append(got, name)
	}
	sort.Strings(got)
	return got
}

// onlyProblem fails unless there is exactly one problem, and returns its text.
func onlyProblem(t *testing.T, problems []problem) string {
	t.Helper()
	if len(problems) != 1 {
		t.Fatalf("want exactly one problem, got %d: %s", len(problems), strings.Join(lines(problems), " | "))
	}
	return problems[0].String()
}
