package specpkg

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// makePackage creates dir with the one file every package has, a prd.md. The
// resolver looks no further.
func makePackage(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prd.md"), []byte("---\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sameDir fails unless got and want are one directory, whatever symlinks the
// temporary directory goes through.
func sameDir(t *testing.T, got, want string) {
	t.Helper()
	g, gerr := filepath.EvalSymlinks(got)
	w, werr := filepath.EvalSymlinks(want)
	if gerr != nil || werr != nil || g != w {
		t.Errorf("resolved %q; want %q", got, want)
	}
}

// TS-10-9: a reference is a path first, then a name, a number or a bare name,
// and an archived package is reached by its path only.
func TestLocateResolvesPathsNamesAndNumbers_TS10_9(t *testing.T) {
	root := t.TempDir()
	specs := filepath.Join(root, ".specs")
	makePackage(t, filepath.Join(specs, "01_a"))
	makePackage(t, filepath.Join(specs, "09_b"))
	makePackage(t, filepath.Join(specs, "archive", "09_old"))
	want := filepath.Join(specs, "09_b")

	t.Chdir(root)
	for _, ref := range []string{
		want,                            // absolute path
		filepath.Join(".specs", "09_b"), // relative to the working directory
		filepath.Join(".specs", "09_b", "prd.md"), // a file in it
		"09_b", "09", "9", "b", // under the spec root
	} {
		got, err := Locate("speca", ref, root, specs)
		if err != nil {
			t.Errorf("%s: %v", ref, err)
			continue
		}
		sameDir(t, got, want)
	}

	// A path relative to --dir, from a working directory that is not it.
	t.Chdir(t.TempDir())
	got, err := Locate("speca", filepath.Join(".specs", "09_b"), root, specs)
	if err != nil {
		t.Fatal(err)
	}
	sameDir(t, got, want)

	archived := filepath.Join(specs, "archive", "09_old")
	got, err = Locate("specr", archived, root, specs)
	if err != nil {
		t.Fatal(err)
	}
	sameDir(t, got, archived)
}

// TS-10-10: a refusal is a usage error that begins with the tool that was
// called, and names the archived package a reference would have matched.
func TestLocateRefusesWithAUsageErrorNamingTheTool_TS10_10(t *testing.T) {
	root := t.TempDir()
	specs := filepath.Join(root, ".specs")
	makePackage(t, filepath.Join(specs, "09_b"))
	makePackage(t, filepath.Join(specs, "09_c"))
	makePackage(t, filepath.Join(specs, "archive", "05_old"))
	if err := os.MkdirAll(filepath.Join(specs, "07_empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tool := range []string{"specr", "speca"} {
		for name, ref := range map[string]string{
			"stdin":         "-",
			"URL":           "https://example.com/x",
			"multi-line":    "09_b\n09_c",
			"unknown":       "nothing_here",
			"ambiguous":     "09",
			"archived":      "05",
			"not a package": filepath.Join(specs, "07_empty"),
			"the spec root": specs,
		} {
			_, err := Locate(tool, ref, root, specs)
			var usage *cli.UsageError
			if !errors.As(err, &usage) || usage.Category() != "usage" {
				t.Errorf("%s %s: err = %v; want a *cli.UsageError", tool, name, err)
				continue
			}
			if !strings.HasPrefix(err.Error(), tool) {
				t.Errorf("%s %s: message %q does not begin with the tool", tool, name, err)
			}
			switch name {
			case "archived":
				if !strings.Contains(err.Error(), "05_old") {
					t.Errorf("%s: message %q does not name the archived package", tool, err)
				}
			case "ambiguous":
				if !strings.Contains(err.Error(), "09_b, 09_c") {
					t.Errorf("%s: message %q does not name both packages", tool, err)
				}
			case "unknown":
				if !strings.Contains(err.Error(), "09_b, 09_c") {
					t.Errorf("%s: message %q does not list what the spec root holds", tool, err)
				}
			}
		}
	}

	if _, err := Locate("speca", "09", root, filepath.Join(root, "missing")); err == nil ||
		!strings.Contains(err.Error(), "could not be read") {
		t.Errorf("an unreadable spec root: err = %v", err)
	}
}

// TS-10-11: List holds the NN_name directories and no other entry, in the
// order of their numbers, and Display shows a path relative to --dir when it
// is inside it.
func TestListAndDisplay_TS10_11(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"01_a", "10_b", "misc", "archive"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "20_file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := List(root)
	if err != nil || !slices.Equal(got, []string{"01_a", "10_b"}) {
		t.Errorf("List = %v, %v; want [01_a 10_b]", got, err)
	}
	if _, err := List(filepath.Join(root, "missing")); err == nil {
		t.Error("List of a missing root = nil error")
	}

	// 100 follows 99, though it sorts before it as text; a tie on the number
	// is broken by the whole name.
	numeric := t.TempDir()
	for _, name := range []string{"100_x", "99_y", "02_b", "02_a", "002_c"} {
		if err := os.MkdirAll(filepath.Join(numeric, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := List(numeric); !slices.Equal(got, []string{"002_c", "02_a", "02_b", "99_y", "100_x"}) {
		t.Errorf("List = %v; want 002_c 02_a 02_b 99_y 100_x", got)
	}

	repo := filepath.Join(root, "repo")
	inside := filepath.Join(repo, ".specs", "01_a")
	if got := Display(inside, repo); got != filepath.Join(".specs", "01_a") {
		t.Errorf("Display(inside) = %q", got)
	}
	outside := filepath.Join(root, "elsewhere", "01_a")
	if got := Display(outside, repo); got != outside {
		t.Errorf("Display(outside) = %q; want the absolute path", got)
	}
	if got := Display(repo, repo); got != "." {
		t.Errorf("Display(the root itself) = %q", got)
	}
}

// TS-10-12: one TaskInfo per task in plan order, with depends_on copied and
// ready from IsReady.
func TestTaskInfosListsThePlanWithReadiness_TS10_12(t *testing.T) {
	yes := true
	plan := &afspec.TasksV2Json{Tasks: []afspec.Task{
		{Id: 1, Title: "first", Kind: afspec.TaskKindImplement, State: afspec.TaskStateDone},
		{Id: 2, Title: "second", Kind: afspec.TaskKindImplement, State: afspec.TaskStatePending, DependsOn: []int{1}},
		{Id: 3, Title: "third", Kind: afspec.TaskKindImplement, State: afspec.TaskStatePending, DependsOn: []int{2}},
		{Id: 4, Title: "fourth", Kind: afspec.TaskKindIntegration, State: afspec.TaskStateInProgress, Optional: &yes, DependsOn: []int{1, 2}},
	}}
	infos := TaskInfos(&afspec.Spec{Tasks: plan})
	if len(infos) != 4 {
		t.Fatalf("TaskInfos = %+v", infos)
	}
	for i, info := range infos {
		task := plan.Tasks[i]
		if info.ID != task.Id || info.Title != task.Title || info.Kind != string(task.Kind) ||
			info.State != string(task.State) || info.Optional != task.IsOptional() {
			t.Errorf("entry %d = %+v; want task %+v", i, info, task)
		}
		if want := task.Id == 2; info.Ready != want {
			t.Errorf("task %d: ready = %v; want %v", task.Id, info.Ready, want)
		}
	}
	if !slices.Equal(infos[3].DependsOn, []int{1, 2}) {
		t.Errorf("depends_on = %v", infos[3].DependsOn)
	}
	infos[3].DependsOn[0] = 99
	if plan.Tasks[3].DependsOn[0] != 1 {
		t.Error("mutating a TaskInfo's depends_on changed the plan")
	}

	if got := TaskInfos(&afspec.Spec{}); got == nil || len(got) != 0 {
		t.Errorf("a spec without tasks gives %#v; want an empty, non-nil list", got)
	}
}

// A package whose format version is neither absent nor 2 is legacy: the v2
// types read none of its tests or tasks.
func TestLegacyIsAFormatVersionOtherThanTwo(t *testing.T) {
	for version, want := range map[int]bool{0: false, afspec.SchemaVersion: false, 1: true, 3: true} {
		if got := Legacy(&afspec.Spec{SchemaVersion: version}); got != want {
			t.Errorf("Legacy(version %d) = %v; want %v", version, got, want)
		}
	}
}

// brokenSpec is the v2 example with its plan and tests damaged in every way
// the report names: a test no task owns, a criterion and a path no test
// verifies, and a term in several criteria with no glossary entry.
func brokenSpec(t *testing.T) *afspec.Spec {
	t.Helper()
	spec, err := afspec.LoadSpec(filepath.Join("..", "..", "afspec", "testdata", "v2_example"))
	if err != nil {
		t.Fatal(err)
	}
	spec.Tasks.Tasks[0].Tests = []string{"TS-09-1", "TS-09-2"}
	req := &spec.Requirements.Requirements[1]
	req.Criteria = append(req.Criteria, afspec.Criterion{Id: "09-REQ-2.3", Pattern: afspec.CriterionPatternUbiquitous, Action: "keep the `Frobnicate` stable"})
	spec.Requirements.ExecutionPaths = append(spec.Requirements.ExecutionPaths, afspec.ExecutionPath{
		Id: "09-PATH-2", Title: "An unverified path", Steps: spec.Requirements.ExecutionPaths[0].Steps})
	for i := range spec.Requirements.Requirements[0].Criteria {
		spec.Requirements.Requirements[0].Criteria[i].Action += " using `Frobnicate`"
	}
	return spec
}

// TS-10-13: the converters moved here give the report spec's result has
// always held, and specgen no longer carries a copy. The golden file was
// captured from specgen's own converters before they moved.
func TestConvertersGiveTheReportSpecAlwaysWrote_TS10_13(t *testing.T) {
	spec := brokenSpec(t)
	got, err := json.MarshalIndent(map[string]any{
		"validation":   ReportOf(spec.Validate()),
		"traceability": TraceOf(spec),
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "report.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(got, '\n'), want) {
		t.Errorf("the report differs from testdata/report.golden.json:\n%s", got)
	}

	files, err := filepath.Glob(filepath.Join("..", "..", "specgen", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("specgen sources: %v, %v", files, err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil &&
				slices.Contains([]string{"reportOf", "itemOf", "traceOf", "unownedTests"}, fn.Name.Name) {
				t.Errorf("%s still defines %s", file, fn.Name.Name)
			}
		}
	}
}

// TS-10-14: the rule of an entry is its Check, or its Category when the Check
// is empty, and each is named once, in the order it first appears.
func TestRulesNamesEachRuleOnceInOrder_TS10_14(t *testing.T) {
	entries := []afspec.ValidationEntry{
		{Check: "C3"}, {Check: "C7"}, {Check: "C3"},
		{Category: "schema"},
		{Category: "cross_spec_actor", Check: "cross_spec_actor"},
	}
	if got := Rules(entries); !slices.Equal(got, []string{"C3", "C7", "schema", "cross_spec_actor"}) {
		t.Errorf("Rules = %v", got)
	}
	if got := Rules(nil); len(got) != 0 {
		t.Errorf("Rules(nil) = %v; want none", got)
	}
}
