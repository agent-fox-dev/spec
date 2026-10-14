package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// envelope is the part of the stdout object the tests read.
type envelope struct {
	OK        bool              `json:"ok"`
	Status    string            `json:"status"`
	ExitCode  int               `json:"exit_code"`
	Summary   string            `json:"summary"`
	Error     *cli.ErrorInfo    `json:"error"`
	Warnings  []cli.Warning     `json:"warnings"`
	Result    *Result           `json:"result"`
	Artifacts []json.RawMessage `json:"artifacts"`
	Next      []cli.Next        `json:"next"`
}

// newRepo lays out a repository with two packages under .specs: the v2
// example as 09_agent_mode, and the fixture with an architecture.md as
// 01_test_feature. It returns the repository root.
func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	copyDir(t, filepath.Join("..", "..", "afspec", "testdata", "v2_example"), filepath.Join(root, ".specs", "09_agent_mode"))
	copyDir(t, filepath.Join("..", "..", "afspec", "testdata", "valid_spec_with_arch"), filepath.Join(root, ".specs", "01_test_feature"))
	return root
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// run runs specr and decodes the one JSON object it printed.
func run(t *testing.T, argv ...string) (envelope, int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Main(argv, &stdout, &stderr)
	var env envelope
	dec := json.NewDecoder(&stdout)
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("stdout is not a JSON object (%v):\n%s", err, stdout.String())
	}
	if dec.More() {
		t.Fatalf("stdout holds more than one JSON object")
	}
	if code != env.ExitCode {
		t.Fatalf("exit code %d, envelope says %d", code, env.ExitCode)
	}
	return env, code, stderr.String()
}

func load(t *testing.T, root, name string) *afspec.Spec {
	t.Helper()
	spec, err := afspec.LoadSpec(filepath.Join(root, ".specs", name))
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

// The whole package is the library's combined render, byte for byte.
func TestWholePackageIsRenderCombined(t *testing.T) {
	root := newRepo(t)
	env, code, _ := run(t, "--dir", root, "01_test_feature")
	if code != cli.ExitOK || !env.OK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	r := env.Result
	if want := load(t, root, "01_test_feature").RenderCombined(); r.Markdown != want {
		t.Errorf("markdown differs from RenderCombined")
	}
	if r.Scope != "spec" || r.Task != nil || r.SpecDir != filepath.Join(".specs", "01_test_feature") {
		t.Errorf("scope %q task %+v dir %q", r.Scope, r.Task, r.SpecDir)
	}
	if got := strings.Join(r.Sections, ","); got != "prd,architecture,requirements,test_spec,tasks" {
		t.Errorf("sections = %s", got)
	}
	if r.Tokens != afspec.EstimateTokens(r.Markdown) || !r.Valid || len(r.Tasks) != 3 {
		t.Errorf("tokens %d valid %v tasks %d", r.Tokens, r.Valid, len(r.Tasks))
	}
	if len(env.Next) != 1 || strings.Join(env.Next[0].Flags, " ") != "--task 1" || env.Next[0].Input != r.SpecDir {
		t.Errorf("next = %+v", env.Next)
	}
}

// A task-scoped render is the library's scoped render, each section under
// its heading, with what the task owns in full.
func TestTaskScopedRender(t *testing.T) {
	root := newRepo(t)
	env, code, _ := run(t, "--dir", root, "9", "--task", "2")
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	r := env.Result
	if r.Scope != "task" || r.Task == nil || r.Task.ID != 2 || r.Task.Title == "" {
		t.Fatalf("scope %q task %+v", r.Scope, r.Task)
	}
	parts := load(t, root, "09_agent_mode").RenderIndividualScoped(2)
	for _, key := range []string{"prd", "requirements", "test_spec", "tasks"} {
		if !strings.Contains(r.Markdown, "# "+sectionHeading[key]+"\n\n"+parts[key]) {
			t.Errorf("section %s is not the scoped render", key)
		}
	}
	for _, want := range []string{"scoped to task 2", "09-REQ-1:** JSON-only stdout in agent mode (other task)", "TS-09-4"} {
		if !strings.Contains(r.Markdown, want) {
			t.Errorf("markdown lacks %q", want)
		}
	}
	if len(env.Next) != 1 || strings.Join(env.Next[0].Flags, " ") != "--task 3" {
		t.Errorf("next = %+v", env.Next)
	}
}

// next skips the tasks that are done.
func TestNextSkipsDoneTasks(t *testing.T) {
	root := newRepo(t)
	spec := load(t, root, "09_agent_mode")
	spec.Tasks = spec.Tasks.CompleteTaskStates([]int{1})
	if err := spec.Save(spec.Dir); err != nil {
		t.Fatal(err)
	}
	env, _, _ := run(t, "--dir", root, "09")
	if len(env.Next) != 1 || strings.Join(env.Next[0].Flags, " ") != "--task 2" {
		t.Errorf("next = %+v", env.Next)
	}
	if env.Result.Tasks[0].State != "done" {
		t.Errorf("tasks[0] = %+v", env.Result.Tasks[0])
	}
	env, _, _ = run(t, "--dir", root, "09", "--task", "3")
	if len(env.Next) != 0 {
		t.Errorf("after the last task, next = %+v", env.Next)
	}
}

// Every shape of reference names the same package.
func TestReferenceForms(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)
	for _, ref := range []string{
		filepath.Join(root, ".specs", "09_agent_mode"),
		filepath.Join(".specs", "09_agent_mode"),
		filepath.Join(".specs", "09_agent_mode", "tasks.json"),
		"09_agent_mode", "09", "9", "agent_mode",
	} {
		env, code, _ := run(t, ref)
		if code != cli.ExitOK || env.Result.SpecDir != filepath.Join(".specs", "09_agent_mode") {
			t.Errorf("%s: code %d, %+v", ref, code, env.Error)
		}
	}
}

// A reference that names nothing is a usage error that lists what is there,
// and points at an archived package that matches.
func TestUnknownReference(t *testing.T) {
	root := newRepo(t)
	copyDir(t, filepath.Join(root, ".specs", "09_agent_mode"), filepath.Join(root, ".specs", "archive", "07_old_thing"))
	env, code, stderr := run(t, "--dir", root, "old_thing")
	if code != cli.ExitUsage || env.Error == nil || env.Error.Category != "usage" || env.Result != nil {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	for _, want := range []string{"01_test_feature, 09_agent_mode", filepath.Join(".specs", "archive", "07_old_thing")} {
		if !strings.Contains(env.Error.Message, want) {
			t.Errorf("message lacks %q: %s", want, env.Error.Message)
		}
	}
	if !strings.Contains(stderr, env.Error.Message) {
		t.Errorf("stderr lacks the message: %s", stderr)
	}
	for _, ref := range []string{"-", "https://github.com/acme/widgets/issues/1", ".specs"} {
		if _, code, _ := run(t, "--dir", root, ref); code != cli.ExitUsage {
			t.Errorf("%s: code %d", ref, code)
		}
	}
}

// Two packages with one number are refused by name.
func TestAmbiguousReference(t *testing.T) {
	root := newRepo(t)
	copyDir(t, filepath.Join(root, ".specs", "09_agent_mode"), filepath.Join(root, ".specs", "09_other"))
	env, code, _ := run(t, "--dir", root, "9")
	if code != cli.ExitUsage || !strings.Contains(env.Error.Message, "09_agent_mode, 09_other") {
		t.Errorf("code %d: %+v", code, env.Error)
	}
}

// An unknown task is a usage error whose fix hint lists the task ids.
func TestUnknownTask(t *testing.T) {
	root := newRepo(t)
	env, code, _ := run(t, "--dir", root, "9", "--task", "7")
	if code != cli.ExitUsage || env.Error.FixHint == nil || env.Error.FixHint.Flag != "--task" ||
		strings.Join(env.Error.FixHint.Valid, ",") != "1,2,3" {
		t.Errorf("code %d: %+v", code, env.Error)
	}
}

// --section renders only the sections named, in the package's order.
func TestSection(t *testing.T) {
	root := newRepo(t)
	env, code, _ := run(t, "--dir", root, "1", "--section", "tasks,prd")
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	r := env.Result
	parts := load(t, root, "01_test_feature").RenderIndividual()
	if want := "# PRD\n\n" + parts["prd"] + "\n# Tasks\n\n" + parts["tasks"] + "\n"; r.Markdown != want {
		t.Errorf("markdown:\n%s", r.Markdown)
	}
	if strings.Join(r.Sections, ",") != "prd,tasks" {
		t.Errorf("sections = %v", r.Sections)
	}

	env, _, _ = run(t, "--dir", root, "9", "--section", "architecture")
	if len(env.Warnings) != 1 || env.Warnings[0].Code != cli.WarnSectionAbsent || env.Result.Sections == nil {
		t.Errorf("warnings %+v sections %v", env.Warnings, env.Result.Sections)
	}
	if _, code, _ := run(t, "--dir", root, "9", "--section", "intro"); code != cli.ExitUsage {
		t.Errorf("unknown section: code %d", code)
	}
}

// --max-tokens drops the architecture first, then the tests' detail, and
// says which; past that, it says the render is still over.
func TestMaxTokens(t *testing.T) {
	root := newRepo(t)
	spec := load(t, root, "01_test_feature")
	noArch := *spec
	noArch.Architecture = ""
	level1 := afspec.EstimateTokens(noArch.RenderCombined())

	env, _, _ := run(t, "--dir", root, "1", "--max-tokens", strconv.Itoa(level1))
	if r := env.Result; strings.Join(r.Reduced, ",") != ReducedArchitecture || has(r.Sections, "architecture") || r.MaxTokens != level1 {
		t.Errorf("reduced %v sections %v max %d", r.Reduced, r.Sections, r.MaxTokens)
	}
	if len(env.Warnings) != 0 || !strings.Contains(env.Summary, "dropped architecture") {
		t.Errorf("warnings %+v summary %s", env.Warnings, env.Summary)
	}

	env, _, _ = run(t, "--dir", root, "1", "--max-tokens", "10")
	if strings.Join(env.Result.Reduced, ",") != "architecture,test_detail" {
		t.Errorf("reduced %v", env.Result.Reduced)
	}
	if len(env.Warnings) != 1 || env.Warnings[0].Code != cli.WarnOverBudget {
		t.Errorf("warnings %+v", env.Warnings)
	}

	env, _, _ = run(t, "--dir", root, "1", "--task", "1", "--max-tokens", "10")
	if strings.Join(env.Result.Reduced, ",") != "architecture,test_detail" {
		t.Errorf("scoped: reduced %v", env.Result.Reduced)
	}

	env, _, _ = run(t, "--dir", root, "1", "--max-tokens", "1000000")
	if env.Result.Reduced != nil || env.Result.Markdown != spec.RenderCombined() {
		t.Errorf("a budget that fits dropped %v", env.Result.Reduced)
	}

	for _, argv := range [][]string{{"--max-tokens", "-1"}, {"--max-tokens", "10", "--section", "prd"}, {"--task", "-2"}} {
		if _, code, _ := run(t, append([]string{"--dir", root, "1"}, argv...)...); code != cli.ExitUsage {
			t.Errorf("%v: code %d", argv, code)
		}
	}
}

// --markdown-file writes exactly result.markdown and lists the file.
func TestMarkdownFile(t *testing.T) {
	root := newRepo(t)
	path := filepath.Join(t.TempDir(), "out", "task.md")
	env, code, _ := run(t, "--dir", root, "9", "--task", "1", "--markdown-file", path)
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != env.Result.Markdown || env.Result.MarkdownFile != path {
		t.Fatalf("file %q (%v), result %q", path, err, env.Result.MarkdownFile)
	}
	if len(env.Artifacts) != 1 || !strings.Contains(string(env.Artifacts[0]), `"markdown_file"`) {
		t.Errorf("artifacts = %s", env.Artifacts)
	}

	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	env, code, _ = run(t, "--dir", root, "9", "--markdown-file", filepath.Join(blocked, "x.md"))
	if code != cli.ExitFailed || env.Error.Stage != "write" || env.Error.Category != "disk" {
		t.Errorf("code %d: %+v", code, env.Error)
	}
	for _, bad := range []string{"-", t.TempDir()} {
		if _, code, _ := run(t, "--dir", root, "9", "--markdown-file", bad); code != cli.ExitUsage {
			t.Errorf("--markdown-file %s: code %d", bad, code)
		}
	}
}

// A package that cannot be read fails at stage load; one that reads but
// does not validate renders, with a warning.
func TestBrokenPackages(t *testing.T) {
	root := newRepo(t)
	broken := filepath.Join(root, ".specs", "05_broken")
	copyDir(t, filepath.Join("..", "..", "afspec", "testdata", "malformed_json"), broken)
	env, code, _ := run(t, "--dir", root, "5")
	if code != cli.ExitFailed || env.Error.Stage != "load" || env.Error.Category != "input" {
		t.Errorf("code %d: %+v", code, env.Error)
	}

	tasks := filepath.Join(root, ".specs", "09_agent_mode", "tasks.json")
	b, err := os.ReadFile(tasks)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tasks, bytes.Replace(b, []byte(`"TS-09-6"`), []byte(`"TS-09-99"`), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	env, code, _ = run(t, "--dir", root, "9")
	if code != cli.ExitOK || env.Result.Valid || len(env.Warnings) != 1 || env.Warnings[0].Code != cli.WarnSpecInvalid {
		t.Errorf("code %d valid %v warnings %+v", code, env.Result.Valid, env.Warnings)
	}
}

// -h prints the help to stderr only; a bare invocation into a pipe also
// writes a usage envelope; --version prints the build.
func TestPersonPaths(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"-h"}, &stdout, &stderr); code != cli.ExitOK || stdout.Len() != 0 ||
		!strings.Contains(stderr.String(), "specr <spec> --task N") {
		t.Errorf("-h: code %d stdout %q", code, stdout.String())
	}
	env, code, stderr2 := run(t)
	if code != cli.ExitUsage || env.Error.Message != noInputMessage || !strings.Contains(stderr2, "Usage:") {
		t.Errorf("bare: code %d %+v", code, env.Error)
	}
	stdout.Reset()
	if code := Main([]string{"--version"}, &stdout, &stderr); code != cli.ExitOK || !strings.HasPrefix(stdout.String(), "specr ") {
		t.Errorf("--version: %d %q", code, stdout.String())
	}
	if _, code, _ := run(t, "9", "10"); code != cli.ExitUsage {
		t.Errorf("two inputs: code %d", code)
	}
}

// The Markdown reaches stdout unescaped: a model reading the raw JSON sees
// <, > and & as they are, not as unicode escapes.
func TestMarkdownIsNotHTMLEscaped(t *testing.T) {
	root := newRepo(t)
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--dir", root, "1"}, &stdout, &stderr); code != cli.ExitOK {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(stdout.String(), "&LoadError") {
		t.Errorf("the test's pseudocode lost its & on stdout")
	}
	for _, esc := range []string{"3c", "3e", "26"} {
		if strings.Contains(stdout.String(), "\\u00"+esc) {
			t.Errorf("stdout escapes HTML (u00%s)", esc)
		}
	}
}
