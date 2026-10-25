package repomap

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/workspace"
)

// openWS opens a workspace at dir with the ignore engine's global layer pinned
// to empty, so a developer's own core.excludesFile cannot hide a fixture file.
func openWS(t *testing.T, dir string) *workspace.Workspace {
	t.Helper()
	ws, err := workspace.Open(dir, workspace.Options{Ignore: workspace.NoGlobalExcludes()})
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// newWS writes files (slash path -> content) under a fresh temp directory and
// returns a workspace rooted there.
func newWS(t *testing.T, files map[string]string) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return openWS(t, dir)
}

// fixtureWS copies testdata/fixture into a fresh temp directory and returns a
// workspace over it. Copying keeps the walk independent of the ignore files of
// the repository the fixture is committed in.
func fixtureWS(t *testing.T) *workspace.Workspace {
	t.Helper()
	src := filepath.Join("testdata", "fixture")
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return openWS(t, dst)
}

// build is Build with a background context that fails the test on an error.
func build(t *testing.T, ws *workspace.Workspace, budget int, inputs []string) string {
	t.Helper()
	got, err := Build(context.Background(), ws, budget, inputs)
	if err != nil {
		t.Fatalf("Build(budget %d, inputs %v): %v", budget, inputs, err)
	}
	return got
}

// goAt returns a Go source file whose statements sit on the given 1-based
// lines, so a test can pin the line numbers the map must show.
func goAt(stmts map[int]string) string {
	last := 1
	for ln := range stmts {
		if ln > last {
			last = ln
		}
	}
	lines := make([]string, last)
	lines[0] = "package x"
	for ln, s := range stmts {
		lines[ln-1] = s
	}
	return strings.Join(lines, "\n") + "\n"
}

// lineFor returns the map line that mentions name, or "" when none does.
func lineFor(m, name string) string {
	for _, l := range strings.Split(m, "\n") {
		if strings.Contains(l, name) {
			return l
		}
	}
	return ""
}

// bigTree is a fixture of 13 files across 4 directories.
func bigTree() map[string]string {
	files := map[string]string{}
	for _, d := range []string{"cmd/tool", "internal/alpha", "internal/beta", "internal/beta/deep"} {
		for i := 0; i < 3; i++ {
			files[fmt.Sprintf("%s/file%d.go", d, i)] = goAt(map[int]string{
				3: fmt.Sprintf("func Exported%d() {}", i),
				5: fmt.Sprintf("func private%d() {}", i),
				7: fmt.Sprintf("type Kind%d struct{}", i),
			})
		}
	}
	files["internal/alpha/file0_test.go"] = goAt(map[int]string{3: "func TestOnly(t int) {}"})
	return files
}

// levelsTree is a four-level tree (app/svc/core/deep) of exported and
// unexported declarations, with one test file per directory. The deepest
// directory's exported declarations are named DeepExported so a test can tell
// them from the shallower ones. files is the number of source files per
// directory and perFile the number of declarations of each kind in each.
func levelsTree(files, perFile int) map[string]string {
	out := map[string]string{}
	for _, dir := range []string{"app", "app/svc", "app/svc/core", "app/svc/core/deep"} {
		prefix := "Exported"
		if strings.HasSuffix(dir, "deep") {
			prefix = "DeepExported"
		}
		stem := path.Base(dir)
		for f := 0; f < files; f++ {
			stmts := map[int]string{}
			for i := 0; i < perFile; i++ {
				stmts[3+2*i] = fmt.Sprintf("func %s%d_%d() {}", prefix, f, i)
				stmts[4+2*i] = fmt.Sprintf("func helper%d_%d() {}", f, i)
			}
			out[fmt.Sprintf("%s/%s_handler_%02d.go", dir, stem, f)] = goAt(stmts)
		}
		stmts := map[int]string{}
		for i := 0; i < perFile; i++ {
			stmts[3+2*i] = fmt.Sprintf("func TestSomething%d(t int) {}", i)
		}
		out[fmt.Sprintf("%s/%s_test.go", dir, stem)] = goAt(stmts)
	}
	return out
}
