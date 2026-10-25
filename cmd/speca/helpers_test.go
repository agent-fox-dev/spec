package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// The four statuses the status form moves a package to, in the order of the
// lifecycle, written out so that changing them is a change to a test.
var wantStatusTargets = []string{"active", "sealed", "superseded", "archived"}

// The four task states, in the order of a task's life.
var wantTaskStates = []string{"pending", "in_progress", "done", "dropped"}

// fixture is a package under afspec/testdata.
func fixture(name string) string { return filepath.Join("..", "..", "afspec", "testdata", name) }

// newRepo lays out a repository with two packages under .specs: the v2
// example as 09_agent_mode, and the fixture with an architecture.md as
// 01_test_feature. It returns the repository root.
func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	copyDir(t, fixture("v2_example"), pkgDir(root, "09_agent_mode"))
	copyDir(t, fixture("valid_spec_with_arch"), pkgDir(root, "01_test_feature"))
	return root
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, to)
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

func pkgDir(root, name string) string { return filepath.Join(root, ".specs", name) }

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// addPackage copies the v2 example into .specs/dirName (which may be under
// archive/) under the given id and name. The copy loads, and validates only
// when it keeps the example's id and name; the tests that add siblings care how
// they load, not that they validate.
func addPackage(t *testing.T, root, dirName, id, name string) string {
	t.Helper()
	dir := pkgDir(root, dirName)
	copyDir(t, fixture("v2_example"), dir)
	editFile(t, filepath.Join(dir, "prd.md"), `spec_id: "09"`, `spec_id: "`+id+`"`)
	editFile(t, filepath.Join(dir, "prd.md"), `spec_name: "agent_mode"`, `spec_name: "`+name+`"`)
	editJSON(t, filepath.Join(dir, "tasks.json"), func(m map[string]any) {
		m["spec_id"], m["spec_name"] = id, name
		m["dependencies"] = []any{}
	})
	return dir
}

// packageWith adds a package in the given lifecycle status, by the text of its
// frontmatter: a refusal needs the status and nothing the library would compute.
func packageWith(t *testing.T, root, dirName, id, name, status string) string {
	t.Helper()
	dir := addPackage(t, root, dirName, id, name)
	if status != "draft" {
		setStatus(t, dir, status)
	}
	return dir
}

// activePackage adds a package, activates it through the library, which records
// the hash of its intent, and gives it the plan.
func activePackage(t *testing.T, root, dirName, id, name string, tasks ...map[string]any) string {
	t.Helper()
	dir := addPackage(t, root, dirName, id, name)
	activate(t, dir)
	if len(tasks) > 0 {
		setTasks(t, dir, tasks...)
	}
	return dir
}

// addLegacyPackage adds a package in format version 1 as 07_legacy: the v2
// example with its frontmatter's version set back and a v1 task plan, whose
// task_groups the v2 types do not read.
func addLegacyPackage(t *testing.T, root string) {
	t.Helper()
	legacy := addPackage(t, root, "07_legacy", "07", "legacy")
	editFile(t, filepath.Join(legacy, "prd.md"), "schema_version: 2", "schema_version: 1")
	writeFile(t, filepath.Join(legacy, "tasks.json"), `{"spec_id": "07", "spec_name": "legacy", "schema_version": 1,
  "task_groups": [{"id": 1, "kind": "tests", "title": "Write tests", "subtasks": [{"id": "1.1", "title": "Write a test", "state": "done"}]}]}`)
}

// editFile replaces the one occurrence of old in path with new.
func editFile(t *testing.T, path, old, new string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), old) {
		t.Fatalf("%s does not contain %q", path, old)
	}
	writeFile(t, path, strings.Replace(string(b), old, new, 1))
}

// editJSON reads the JSON object in path, applies edit to it and writes it back.
func editJSON(t *testing.T, path string, edit func(m map[string]any)) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	if b, err = json.MarshalIndent(m, "", "  "); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(b))
}

// setDependencies makes the package depend on the given spec ids, in order.
func setDependencies(t *testing.T, dir string, ids ...string) {
	t.Helper()
	editJSON(t, filepath.Join(dir, "tasks.json"), func(m map[string]any) {
		deps := []any{}
		for _, id := range ids {
			deps = append(deps, map[string]any{"spec": id, "reason": "needs " + id})
		}
		m["dependencies"] = deps
	})
}

// setActors gives every step of every execution path of the package this actor.
func setActors(t *testing.T, dir, actor string) {
	t.Helper()
	editJSON(t, filepath.Join(dir, "requirements.json"), func(m map[string]any) {
		for _, p := range m["execution_paths"].([]any) {
			for _, step := range p.(map[string]any)["steps"].([]any) {
				step.(map[string]any)["actor"] = actor
			}
		}
	})
}

// setGlossaryTerm defines a term in the package's glossary.
func setGlossaryTerm(t *testing.T, dir, term, definition string) {
	t.Helper()
	editJSON(t, filepath.Join(dir, "requirements.json"), func(m map[string]any) {
		m["glossary"].(map[string]any)[term] = definition
	})
}

// planTask is one task of a plan written by setTasks. A nil deps leaves
// depends_on out, which format v2 §8.3 reads as the previous task.
func planTask(id int, state string, deps []int) map[string]any {
	task := map[string]any{"id": id, "kind": "implement", "title": fmt.Sprintf("Task %d", id),
		"criteria": []any{}, "tests": []any{}, "steps": []any{}, "state": state}
	if deps != nil {
		task["depends_on"] = deps
	}
	return task
}

// planWithOpenTasks is a plan with a pending task and an optional one in progress.
func planWithOpenTasks() []map[string]any {
	optional := planTask(2, "in_progress", []int{})
	optional["optional"] = true
	return []map[string]any{planTask(1, "pending", nil), optional}
}

// setTasks replaces the package's plan.
func setTasks(t *testing.T, dir string, tasks ...map[string]any) {
	t.Helper()
	editJSON(t, filepath.Join(dir, "tasks.json"), func(m map[string]any) {
		plan := []any{}
		for _, task := range tasks {
			plan = append(plan, task)
		}
		m["tasks"] = plan
	})
}

func setStatus(t *testing.T, dir, status string) {
	t.Helper()
	editFile(t, filepath.Join(dir, "prd.md"), `status: "draft"`, `status: "`+status+`"`)
}

// activate moves the package to active through the library, which records the
// hash of its Intent section.
func activate(t *testing.T, dir string) {
	t.Helper()
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spec.Transition("active", dir); err != nil {
		t.Fatal(err)
	}
}

// driftIntent edits the ## Intent section, so that an active package no longer
// matches its intent_hash.
func driftIntent(t *testing.T, dir string) {
	t.Helper()
	editFile(t, filepath.Join(dir, "prd.md"), "Coding agents drive", "Coding agents might drive")
}

// removeIntent renames the ## Intent heading, so that the package has none.
func removeIntent(t *testing.T, dir string) {
	t.Helper()
	editFile(t, filepath.Join(dir, "prd.md"), "## Intent", "## Purpose")
}

// breakValidity makes the example fail validation without touching its intent:
// a test the plan names is not in the test specification, which breaks the
// cross-file rules C6, C7 and C9.
func breakValidity(t *testing.T, dir string) {
	t.Helper()
	editFile(t, filepath.Join(dir, "tasks.json"), `"TS-09-6"`, `"TS-09-99"`)
}

// withFrontmatterKey adds a key of its own to the frontmatter of prd.md, which
// the schema does not allow and a write would drop.
func withFrontmatterKey(t *testing.T, dir string) {
	t.Helper()
	editFile(t, filepath.Join(dir, "prd.md"), "schema_version: 2", "schema_version: 2\nbadkey: \"x\"")
}

// addUnknownKey gives tasks.json a key the schema does not allow, as a leaked
// version 1 traceability or a newer format's field would be.
func addUnknownKey(t *testing.T, dir string) {
	t.Helper()
	editJSON(t, filepath.Join(dir, "tasks.json"), func(m map[string]any) { m["traceability"] = []any{} })
}

// run runs speca and decodes the one JSON object it printed with the public
// decoder, so the tests read the output the way a program would.
func run(t *testing.T, argv ...string) (envelope.AdminEnvelope, int, string) {
	t.Helper()
	env, _, code, stderr := runRaw(t, argv...)
	return env, code, stderr
}

// runRaw is run that also returns the members of the envelope's result as they
// were written, so that a test can tell a field that is absent from one that is
// empty.
func runRaw(t *testing.T, argv ...string) (envelope.AdminEnvelope, map[string]json.RawMessage, int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Main(argv, &stdout, &stderr)
	env, err := envelope.DecodeSpeca(stdout.Bytes())
	if err != nil {
		t.Fatalf("stdout is not one envelope (%v):\n%s", err, stdout.String())
	}
	if code != env.ExitCode {
		t.Fatalf("exit code %d, envelope says %d", code, env.ExitCode)
	}
	var top struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &top); err != nil {
		t.Fatal(err)
	}
	return env, top.Result, code, stderr.String()
}

// rawRun is runRaw without the exit code and stderr.
func rawRun(t *testing.T, argv ...string) (envelope.AdminEnvelope, map[string]json.RawMessage) {
	t.Helper()
	env, raw, _, _ := runRaw(t, argv...)
	return env, raw
}

// rawPackages runs speca and returns the members of each entry of packages as
// they were written.
func rawPackages(t *testing.T, argv ...string) (envelope.AdminEnvelope, []map[string]json.RawMessage) {
	t.Helper()
	env, raw := rawRun(t, argv...)
	var packages []map[string]json.RawMessage
	if err := json.Unmarshal(raw["packages"], &packages); err != nil {
		t.Fatalf("packages = %s (%v); error %+v", raw["packages"], err, env.Error)
	}
	return env, packages
}

// result is the envelope's result, or a stop for the test that finds none.
func result(t *testing.T, env envelope.AdminEnvelope) *envelope.AdminResult {
	t.Helper()
	if env.Result == nil {
		t.Fatalf("the call has no result; error: %+v", env.Error)
	}
	return env.Result
}

// succeeds runs the call in root and fails the test unless it exits 0 with a
// result.
func succeeds(t *testing.T, root string, argv ...string) (envelope.AdminEnvelope, *envelope.AdminResult) {
	t.Helper()
	env, code, _ := run(t, append([]string{"--dir", root}, argv...)...)
	if code != cli.ExitOK || !env.OK || env.Result == nil {
		t.Fatalf("%v: code %d, error %+v", argv, code, env.Error)
	}
	return env, env.Result
}

// followed runs the call a next entry names, from the repository, and returns
// its envelope. It is how an agent takes a suggestion.
func followed(t *testing.T, root string, n envelope.Next) (envelope.AdminEnvelope, int) {
	t.Helper()
	env, code, _ := run(t, append([]string{"--dir", root, n.Input}, n.Flags...)...)
	return env, code
}

// refusedAsUsage runs the call and fails the test unless it is a usage error
// that wrote nothing, with no result, the message on stderr and saying each of
// want.
func refusedAsUsage(t *testing.T, root string, argv []string, want ...string) *cli.ErrorInfo {
	t.Helper()
	before := snapshot(t, root)
	env, code, stderr := run(t, argv...)
	e := env.Error
	if code != cli.ExitUsage || e == nil || e.Stage != "usage" || e.Category != "usage" || env.Result != nil || env.OK {
		t.Errorf("%v: code %d: %+v", argv, code, e)
		return e
	}
	if e.Retryable || e.Resumable {
		t.Errorf("%v: retryable %v, resumable %v", argv, e.Retryable, e.Resumable)
	}
	saysAll(t, argv, e.Message, want...)
	if e.Message == "" || !strings.Contains(stderr, e.Message) {
		t.Errorf("%v: stderr %q does not hold the message %q", argv, stderr, e.Message)
	}
	if !maps.Equal(before, snapshot(t, root)) {
		t.Errorf("%v: the repository changed", argv)
	}
	return e
}

// refusedAt runs the call in root and fails the test unless it is exit 1 at the
// stage, in the category, with no result and nothing written. It returns the
// error.
func refusedAt(t *testing.T, root string, stage, category string, argv ...string) *cli.ErrorInfo {
	t.Helper()
	before := snapshot(t, root)
	env, code, _ := run(t, append([]string{"--dir", root}, argv...)...)
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != stage || env.Error.Category != category || env.Result != nil || env.OK {
		t.Errorf("%v: code %d, error %+v, result %v; want exit 1 at stage %s, category %s, no result", argv, code, env.Error, env.Result != nil, stage, category)
		return env.Error
	}
	if !maps.Equal(before, snapshot(t, root)) {
		t.Errorf("%v: the repository changed", argv)
	}
	return env.Error
}

// interrupted runs the call with .specs made read-only, so that the status is
// written and the move under archive/ is not, and returns the envelope.
func interrupted(t *testing.T, root string, argv ...string) envelope.AdminEnvelope {
	t.Helper()
	readOnly(t, filepath.Join(root, ".specs"))
	env, code, _ := run(t, append([]string{"--dir", root}, argv...)...)
	if code != cli.ExitFailed {
		t.Fatalf("%v: code %d, error %+v; want exit 1", argv, code, env.Error)
	}
	return env
}

// readOnly makes the directory unwritable for the rest of the test, and
// writable again when it ends so that its temporary directory can be removed.
// It skips as root, who is stopped by nothing.
func readOnly(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("a read-only directory stops nothing as root")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
}

// writable undoes readOnly before the test ends.
func writable(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

// failWith replaces a library call of the command for the rest of the test.
func failWith[T any](t *testing.T, call *T, replacement T) {
	t.Helper()
	was := *call
	t.Cleanup(func() { *call = was })
	*call = replacement
}

func saysAll(t *testing.T, argv []string, message string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(message, w) {
			t.Errorf("%v: message %q does not say %q", argv, message, w)
		}
	}
}

func warningCodes(env envelope.AdminEnvelope) []string {
	var out []string
	for _, w := range env.Warnings {
		out = append(out, string(w.Code))
	}
	return out
}

// warningsOf lists the warnings of the code.
func warningsOf(env envelope.AdminEnvelope, code string) []envelope.Warning {
	var out []envelope.Warning
	for _, w := range env.Warnings {
		if string(w.Code) == code {
			out = append(out, w)
		}
	}
	return out
}

// oneWarning returns the only warning with the code, or fails the test.
func oneWarning(t *testing.T, env envelope.AdminEnvelope, code, severity string) envelope.Warning {
	t.Helper()
	ws := warningsOf(env, code)
	if len(ws) != 1 || ws[0].Severity != severity {
		t.Fatalf("warnings %+v; want one %s warning of severity %s", env.Warnings, code, severity)
	}
	return ws[0]
}

// checks lists the rule of each finding.
func checks(items []envelope.ValidationItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Check)
	}
	return out
}

// ownChecks are the rules the package breaks on its own, read straight from
// the library, in the order it reports them.
func ownChecks(t *testing.T, dir string) (errs, warns []string) {
	t.Helper()
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	v := spec.Validate()
	for _, e := range v.Errors {
		errs = append(errs, e.Check)
	}
	for _, w := range v.Warnings {
		warns = append(warns, w.Check)
	}
	return errs, warns
}

func dirs(packages []envelope.PackageInfo) []string {
	var out []string
	for _, p := range packages {
		out = append(out, filepath.ToSlash(p.SpecDir))
	}
	return out
}

func kinds(changes []envelope.Change) []string {
	out := make([]string, len(changes))
	for i, c := range changes {
		out[i] = c.Kind
	}
	return out
}

// changed lists the ids of a result's changes.
func changed(r *envelope.AdminResult) []int {
	var out []int
	for _, c := range r.Changes {
		out = append(out, c.ID)
	}
	return out
}

func changeFor(r *envelope.AdminResult, id int) *envelope.Change {
	for i := range r.Changes {
		if r.Changes[i].ID == id {
			return &r.Changes[i]
		}
	}
	return nil
}

// statusCalls lists the suggestions in next that change a package's status.
func statusCalls(next []envelope.Next) []envelope.Next {
	var out []envelope.Next
	for _, n := range next {
		if slices.Contains(n.Flags, "--status") {
			out = append(out, n)
		}
	}
	return out
}

// claim is the call that claims task id of the package at dir.
func claim(dir string, id int) envelope.Next {
	return envelope.Next{Tool: "speca", Input: dir, Flags: []string{"--task", fmt.Sprint(id), "--state", "in_progress"}}
}

// sameCall reports whether n is a call to the tool with the flags for the input.
func sameCall(n, want envelope.Next) bool {
	return n.Tool == want.Tool && n.Input == want.Input && slices.Equal(n.Flags, want.Flags)
}

// hasCall reports whether next holds the call.
func hasCall(next []envelope.Next, want envelope.Next) bool {
	return slices.ContainsFunc(next, func(n envelope.Next) bool { return sameCall(n, want) })
}

// snapshot records every file and directory under root with its mode, size and
// modification time.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out[rel] = info.Mode().String() + " " + strconv.FormatInt(info.Size(), 10) + " " + strconv.FormatInt(info.ModTime().UnixNano(), 10)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// treeBytes records what every file under dir holds and when it was written.
func treeBytes(t *testing.T, dir string) map[string]string {
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

// normalized is the stdout of a call without the two members that say when it
// ran, so that two calls can be compared.
func normalized(t *testing.T, stdout []byte) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(stdout, &m); err != nil {
		t.Fatalf("%v:\n%s", err, stdout)
	}
	delete(m, "duration_ms")
	delete(m, "started_at")
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func loadSpec(t *testing.T, dir string) *afspec.Spec {
	t.Helper()
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func statusOnDisk(t *testing.T, dir string) string { t.Helper(); return loadSpec(t, dir).Status }

// supersedesOf is the supersedes list of the package, read from disk.
func supersedesOf(t *testing.T, dir string) []string { t.Helper(); return loadSpec(t, dir).Supersedes }

// onDisk loads the package and returns the state of each of its tasks, by id.
func onDisk(t *testing.T, dir string) map[int]string {
	t.Helper()
	out := map[int]string{}
	for _, task := range loadSpec(t, dir).Tasks.Tasks {
		out[task.Id] = string(task.State)
	}
	return out
}

// updatedAtIn is the updated_at of the prd.md in dir, which must be a UTC time
// in RFC 3339.
func updatedAtIn(t *testing.T, dir string) time.Time {
	t.Helper()
	raw := loadSpec(t, dir).UpdatedAt
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil || !strings.HasSuffix(raw, "Z") {
		t.Fatalf("updated_at %q is not a UTC time in RFC 3339 (%v)", raw, err)
	}
	return ts
}

// plan is a plan in memory, from tasks made by planTask.
func plan(tasks ...map[string]any) *afspec.TasksV2Json {
	p := &afspec.TasksV2Json{}
	for _, m := range tasks {
		task := afspec.Task{Id: m["id"].(int), State: afspec.TaskState(m["state"].(string)), Title: m["title"].(string)}
		if d, ok := m["depends_on"]; ok {
			task.DependsOn = d.([]int)
		}
		p.Tasks = append(p.Tasks, task)
	}
	return p
}

func ids(moves []move) []int {
	var out []int
	for _, m := range moves {
		out = append(out, m.id)
	}
	return out
}

func ptr[T any](v T) *T { return &v }

// roundTripFunc is an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// specrBinary is the real specr command, built once per run of the package's
// tests: its Main is in a package main, which a test cannot import, and the
// end-to-end test must not stand a double in its place.
var specrBinary struct {
	once sync.Once
	dir  string
	path string
	err  error
}

// specr returns the path of the built specr. Building takes seconds, so -short
// skips what needs it.
func specr(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("-short: building specr takes seconds")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go tool is not on the path, so specr cannot be built")
	}
	specrBinary.once.Do(func() {
		if specrBinary.dir, specrBinary.err = os.MkdirTemp("", "speca-test-specr"); specrBinary.err != nil {
			return
		}
		specrBinary.path = filepath.Join(specrBinary.dir, "specr")
		if out, err := exec.Command(goTool, "build", "-o", specrBinary.path, "github.com/agent-fox-dev/spec/cmd/specr").CombinedOutput(); err != nil {
			specrBinary.err = fmt.Errorf("building specr: %v\n%s", err, out)
		}
	})
	if specrBinary.err != nil {
		t.Fatal(specrBinary.err)
	}
	return specrBinary.path
}

// runSpecr runs the built specr with the call a next entry names, from root,
// and decodes its envelope.
func runSpecr(t *testing.T, bin, root string, n envelope.Next) envelope.RenderEnvelope {
	t.Helper()
	cmd := exec.Command(bin, append([]string{n.Input}, n.Flags...)...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("specr %s %s: %v\n%s", n.Input, strings.Join(n.Flags, " "), err, stderr.String())
	}
	env, err := envelope.DecodeSpecr(stdout.Bytes())
	if err != nil || env.Result == nil {
		t.Fatalf("specr's output: %v\n%s", err, stdout.String())
	}
	return env
}

// siblingLine fails the test unless help carries the sentence naming the three
// commands on a line of its own, after the title line and before Usage:, with a
// blank line on each side, so that an agent that finds one command knows the
// other two.
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

// intentHashOf is the intent_hash the package on disk records.
func intentHashOf(t *testing.T, dir string) string {
	t.Helper()
	spec := loadSpec(t, dir)
	if spec.IntentHash == nil {
		t.Fatalf("%s has no intent_hash", dir)
	}
	return *spec.IntentHash
}

// refusedWithReport runs the call and fails the test unless it is exit 1 at
// the stage, in the category, with nothing written, and with the result the
// report of the package as it is on disk: no changes and no next, so that the
// caller sees its tasks without another call and is pointed at nothing another
// agent may hold. The summary is the refusal. It returns the envelope.
func refusedWithReport(t *testing.T, root, stage, category string, argv ...string) envelope.AdminEnvelope {
	t.Helper()
	before := snapshot(t, root)
	env, code, _ := run(t, append([]string{"--dir", root}, argv...)...)
	e := env.Error
	if code != cli.ExitFailed || e == nil || e.Stage != stage || e.Category != category || env.OK {
		t.Fatalf("%v: code %d: %+v; want exit 1 at stage %s, category %s", argv, code, e, stage, category)
	}
	if e.Retryable || e.Resumable {
		t.Errorf("%v: retryable %v, resumable %v", argv, e.Retryable, e.Resumable)
	}
	r := env.Result
	if r == nil || len(r.Tasks) == 0 || r.Tasks[0].ID != 1 || r.Valid == nil || r.TaskCounts == nil || len(r.Changes) != 0 || len(env.Next) != 0 {
		t.Errorf("%v: result %+v, next %+v; want the report of the package on disk, without changes or next", argv, r, env.Next)
	}
	if env.Summary != e.Message && !strings.HasPrefix(e.Message, env.Summary) {
		t.Errorf("%v: summary %q is not the refusal %q", argv, env.Summary, e.Message)
	}
	if !maps.Equal(before, snapshot(t, root)) {
		t.Errorf("%v: the repository changed", argv)
	}
	return env
}

// report16 is the result of the report form for a package in .specs. A draft and
// an active package can take a write, as the report form says of a package whose
// intent has not drifted.
func report16(status string, done, total int, next *envelope.TaskInfo, valid bool) *Result {
	return &Result{AdminResult: envelope.AdminResult{
		SpecDir: ".specs/16_indexed_code_search", SpecID: "16", SpecName: "indexed_code_search", Status: status,
		Valid: ptr(valid), TaskCounts: &envelope.TaskCounts{Done: done, Total: total}, NextTask: next,
	}, writable: status == "draft" || status == "active"}
}

// listOf is a list result of packages with these statuses; "" is one that did
// not load.
func listOf(statuses ...string) *Result {
	out := []envelope.PackageInfo{}
	for i, s := range statuses {
		p := envelope.PackageInfo{SpecDir: fmt.Sprintf(".specs/%02d_p", i+1)}
		if s == "" {
			p.LoadError = "cannot read prd.md"
		} else {
			p.Status = s
		}
		out = append(out, p)
	}
	return &Result{AdminResult: envelope.AdminResult{Packages: out}}
}

// verdictOf is a verdict result with these rules broken.
func verdictOf(dir string, rules ...string) *Result {
	v := &envelope.ValidationReport{Valid: len(rules) == 0, ErrorCount: len(rules)}
	for _, r := range rules {
		v.Errors = append(v.Errors, envelope.ValidationItem{Check: r})
	}
	return &Result{AdminResult: envelope.AdminResult{SpecDir: dir, Valid: ptr(v.Valid), Validation: v}}
}

// envelopeSummary is the summary of the envelope the run builds around r.
func envelopeSummary(r *Result) string {
	return cli.NewRun(tool, "v").Envelope(cli.ExitOK, r, nil).Summary
}

func TestMain(m *testing.M) {
	code := m.Run()
	if specrBinary.dir != "" {
		_ = os.RemoveAll(specrBinary.dir)
	}
	os.Exit(code)
}
