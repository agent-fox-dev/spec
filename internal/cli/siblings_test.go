package cli

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TS-13-2: the sentence naming the three commands is written in one non-test Go
// file, the one that declares SiblingCommands, so no command can drift from it.
func TestTheSiblingSentenceIsWrittenInOneFile_TS13_2(t *testing.T) {
	root := filepath.Join("..", "..")
	var hits []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".specs", "docs", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), SiblingCommands) {
			rel, _ := filepath.Rel(root, path)
			hits = append(hits, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || !strings.HasPrefix(hits[0], "internal/cli/") {
		t.Errorf("the sentence is written in %v; want internal/cli alone", hits)
	}
}

// sentences splits the prose of a doc comment (its code blocks, which are
// indented, left out) into sentences.
func sentences(doc string) []string {
	var prose []string
	for _, line := range strings.Split(doc, "\n") {
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			prose = append(prose, line)
		}
	}
	flat := strings.Join(strings.Fields(strings.Join(prose, " ")), " ")
	return regexp.MustCompile(`\.\s+`).Split(flat, -1)
}

// TS-13-7: the package doc comment of each command has one sentence that names
// the other two.
func TestEachCommandsDocNamesTheOtherTwo_TS13_7(t *testing.T) {
	commands := []string{"spec", "specr", "speca"}
	for _, name := range commands {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "..", "cmd", name, "main.go"), nil, parser.ParseComments|parser.PackageClauseOnly)
		if err != nil {
			t.Fatal(err)
		}
		var doc string
		if file.Doc != nil {
			doc = file.Doc.Text()
		}
		var others []*regexp.Regexp
		for _, other := range commands {
			if other != name {
				others = append(others, regexp.MustCompile(`\b`+other+`\b`))
			}
		}
		found := false
		for _, s := range sentences(doc) {
			all := true
			for _, re := range others {
				all = all && re.MatchString(s)
			}
			found = found || all
		}
		if !found {
			t.Errorf("the package doc of cmd/%s has no sentence that names the other two commands:\n%s", name, doc)
		}
	}
}
