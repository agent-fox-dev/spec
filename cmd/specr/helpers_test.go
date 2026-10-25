package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// example is the directory name of afspec's v2 example in a newRepo: three
// tasks, task 2 depending on 1 and task 3 on 1 and 2, none done.
const example = "09_agent_mode"

func TestMain(m *testing.M) {
	code := m.Run()
	if builds.dir != "" {
		_ = os.RemoveAll(builds.dir)
	}
	os.Exit(code)
}

// newRepo lays out a repository with two packages under .specs: the v2
// example as 09_agent_mode, and the fixture with an architecture.md as
// 01_test_feature. It returns the repository root.
func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	copyDir(t, filepath.Join("..", "..", "afspec", "testdata", "v2_example"), filepath.Join(root, ".specs", example))
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

// run runs specr and decodes the one JSON object it printed with the public
// decoder, so the tests read the output the way a program would.
func run(t *testing.T, argv ...string) (envelope.RenderEnvelope, int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Main(argv, &stdout, &stderr)
	env, err := envelope.DecodeSpecr(stdout.Bytes())
	if err != nil {
		t.Fatalf("stdout is not one envelope (%v):\n%s", err, stdout.String())
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

// editTasks rewrites the plan of the package in dir: edit is given each task as
// a JSON object.
func editTasks(t *testing.T, dir string, edit func(tasks []map[string]any)) {
	t.Helper()
	path := filepath.Join(dir, "tasks.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	var tasks []map[string]any
	for _, task := range m["tasks"].([]any) {
		tasks = append(tasks, task.(map[string]any))
	}
	edit(tasks)
	if b, err = json.MarshalIndent(m, "", "  "); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// plan gives the tasks of the example the states, and their depends_on. A nil
// deps keeps the example's dependencies; a nil entry leaves the key out, so the
// task depends on the one before it; an empty list says it depends on none.
func plan(t *testing.T, dir string, states []string, deps [][]int) {
	t.Helper()
	editTasks(t, dir, func(tasks []map[string]any) {
		for i, task := range tasks {
			task["state"] = states[i]
			switch {
			case deps == nil:
			case deps[i] == nil:
				delete(task, "depends_on")
			default:
				task["depends_on"] = deps[i]
			}
		}
	})
}

// exampleWith is a newRepo whose example has its tasks in the states, with the
// dependencies plan reads from deps.
func exampleWith(t *testing.T, states []string, deps [][]int) string {
	t.Helper()
	root := newRepo(t)
	plan(t, filepath.Join(root, ".specs", example), states, deps)
	return root
}

// setStatus gives the package a lifecycle status by the text of its frontmatter.
func setStatus(t *testing.T, dir, status string) {
	t.Helper()
	path := filepath.Join(dir, "prd.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`status: "draft"`)) {
		t.Fatalf("prd.md of %s is not a draft", dir)
	}
	if err := os.WriteFile(path, bytes.Replace(b, []byte(`status: "draft"`), []byte(`status: "`+status+`"`), 1), 0o644); err != nil {
		t.Fatal(err)
	}
}

// activate moves the package to active through the library, which records the
// hash of its intent, and, if drift, changes the intent after it.
func activate(t *testing.T, dir string, drift bool) {
	t.Helper()
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spec.Transition("active", dir); err != nil {
		t.Fatal(err)
	}
	if drift {
		path := filepath.Join(dir, "prd.md")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, bytes.Replace(b, []byte("Coding agents drive"), []byte("Coding agents might drive"), 1), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// unsetHash makes the active package record no intent hash.
func unsetHash(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "prd.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	i := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "intent_hash:") })
	if i < 0 {
		t.Fatalf("no intent_hash in prd.md:\n%s", b)
	}
	lines[i] = "intent_hash: null"
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

// prepareStatus puts the example of root in one of the lifecycle states a test
// names: draft (as written), active, unhashed (active with no intent hash),
// drifted (active with its intent changed after), sealed, superseded or
// archived.
func prepareStatus(t *testing.T, root, status string) {
	t.Helper()
	dir := filepath.Join(root, ".specs", example)
	switch status {
	case "draft":
	case "active":
		activate(t, dir, false)
	case "unhashed":
		activate(t, dir, false)
		unsetHash(t, dir)
	case "drifted":
		activate(t, dir, true)
	case "sealed", "superseded", "archived":
		setStatus(t, dir, status)
	default:
		t.Fatalf("unknown status %q", status)
	}
}

// byTool returns the entries of next that call tool.
func byTool(next []envelope.Next, tool string) []envelope.Next {
	var out []envelope.Next
	for _, n := range next {
		if n.Tool == tool {
			out = append(out, n)
		}
	}
	return out
}

// calls is next as one line per entry: the tool and its flags.
func calls(next []envelope.Next) []string {
	out := []string{}
	for _, n := range next {
		out = append(out, strings.Join(append([]string{n.Tool}, n.Flags...), " "))
	}
	return out
}

// claims lists the tasks next claims, as the flags of the entries that do.
func claims(next []envelope.Next) [][]string {
	var out [][]string
	for _, n := range byTool(next, "speca") {
		if len(n.Flags) == 4 && n.Flags[2] == "--state" && n.Flags[3] == "in_progress" {
			out = append(out, n.Flags)
		}
	}
	return out
}

// treeOf records what every file under dir holds and when it was written.
func treeOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out[rel] = info.ModTime().String() + "\x00" + string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// newLegacyPackage adds a package in format version 1 as 07_legacy: the v2
// example with its frontmatter's version set back and a v1 task plan, whose
// task_groups the v2 types do not read.
func newLegacyPackage(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".specs", "07_legacy")
	copyDir(t, filepath.Join("..", "..", "afspec", "testdata", "v2_example"), dir)
	prd := filepath.Join(dir, "prd.md")
	b, err := os.ReadFile(prd)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prd, bytes.Replace(b, []byte("schema_version: 2"), []byte("schema_version: 1"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	v1 := `{"spec_id": "09", "spec_name": "agent_mode", "schema_version": 1,
  "task_groups": [{"id": 1, "kind": "tests", "title": "Write tests",
    "subtasks": [{"id": "1.1", "title": "Write a test", "state": "done"}]}]}`
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
}

// siblingLine fails the test unless help carries the sentence naming the three
// commands on a line of its own, after the title line and before Usage:, with a
// blank line on each side.
func siblingLine(t *testing.T, what, help string) {
	t.Helper()
	usage := strings.Index(help, "Usage:")
	if usage < 0 {
		t.Errorf("%s: the help has no Usage: line:\n%s", what, help)
		return
	}
	before := help[:usage]
	if !strings.Contains(before, "\n\n"+cli.SiblingCommands+"\n\n") {
		t.Errorf("%s: the sentence is not on a line of its own, with a blank line on each side, before Usage::\n%s", what, before)
	}
	if title, _, _ := strings.Cut(help, "\n"); title == "" || strings.Index(before, cli.SiblingCommands) < len(title) {
		t.Errorf("%s: the title line %q does not come before the sentence:\n%s", what, title, before)
	}
}

// The real speca is built once for the tests that run the calls specr
// suggests. Its Main is in a package main, which a test cannot import.
var builds struct {
	mu   sync.Mutex
	dir  string
	path string
}

// buildSpeca returns the path of the real speca, built on first use. Building
// takes seconds, so -short skips what needs it.
func buildSpeca(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("-short: building speca takes seconds")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go tool is not on the path, so speca cannot be built")
	}
	builds.mu.Lock()
	defer builds.mu.Unlock()
	if builds.path != "" {
		return builds.path
	}
	if builds.dir, err = os.MkdirTemp("", "specr-builds"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(builds.dir, "speca")
	if out, err := exec.Command(goTool, "build", "-o", path, "github.com/agent-fox-dev/spec/cmd/speca").CombinedOutput(); err != nil {
		t.Fatalf("building speca: %v\n%s", err, out)
	}
	builds.path = path
	return path
}

// speca runs the built command and decodes its envelope; a refusal exits
// non-zero and says so in the envelope.
func speca(t *testing.T, bin string, argv ...string) (envelope.AdminEnvelope, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, argv...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("speca %v: %v", argv, err)
		}
		code = exit.ExitCode()
	}
	env, err := envelope.DecodeSpeca(stdout.Bytes())
	if err != nil {
		t.Fatalf("speca %v: stdout is not one envelope (%v):\nstdout %s\nstderr %s", argv, err, stdout.String(), stderr.String())
	}
	return env, code
}

// taskState is the state of task id in the example, read from disk.
func taskState(t *testing.T, root string, id int) string {
	t.Helper()
	for _, task := range load(t, root, example).Tasks.Tasks {
		if task.Id == id {
			return string(task.State)
		}
	}
	t.Fatalf("no task %d", id)
	return ""
}
