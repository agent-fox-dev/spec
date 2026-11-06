package afspec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// frontmatterExtensionLines are the extension keys added to the fixture's
// PRD frontmatter: a string, a list and a map.
const frontmatterExtensionLines = "x-jira: \"ABC-1\"\nx-labels: [\"a\", \"b\"]\nx-meta: {\"k\": 1}\n"

// withExtensions writes extension keys on objects of every kind in the
// package at dir: the root of an artifact, a requirement, a criterion, a
// check, a path step, a smoke check, a task, the test commands, a progress
// entry and the PRD frontmatter.
func withExtensions(t *testing.T, dir string) {
	t.Helper()
	editJSON(t, filepath.Join(dir, "requirements.json"), func(m map[string]any) {
		m["x-team"] = "core"
		req := m["requirements"].([]any)[0].(map[string]any)
		req["x-jira"] = "ABC-1"
		criterion := req["criteria"].([]any)[1].(map[string]any)
		criterion["x-risk"] = map[string]any{"level": "high", "score": 3.0}
		criterion["check"].(map[string]any)["x-flaky"] = false
		path := m["execution_paths"].([]any)[0].(map[string]any)
		path["steps"].([]any)[0].(map[string]any)["x-note"] = true
		path["smoke"].(map[string]any)["x-env"] = "ci"
	})
	editJSON(t, filepath.Join(dir, "tasks.json"), func(m map[string]any) {
		m["tasks"].([]any)[1].(map[string]any)["x-estimate"] = 2.5
		m["test_commands"].(map[string]any)["x-ci"] = "make ci"
	})
	editJSON(t, filepath.Join(dir, "progress.json"), func(m map[string]any) {
		m["tasks"].([]any)[0].(map[string]any)["x-by"] = "agent-7"
	})
	prdPath := filepath.Join(dir, "prd.md")
	prd, err := os.ReadFile(prdPath)
	if err != nil {
		t.Fatal(err)
	}
	head, body, ok := strings.Cut(string(prd)[4:], "\n---\n")
	if !ok {
		t.Fatal("the fixture PRD has no frontmatter")
	}
	if err := os.WriteFile(prdPath, []byte("---\n"+head+"\n"+frontmatterExtensionLines+"---\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func editJSON(t *testing.T, path string, edit func(map[string]any)) {
	t.Helper()
	m := readJSON(t, path)
	edit(m)
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return m
}

func readFrontmatter(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "prd.md"))
	if err != nil {
		t.Fatal(err)
	}
	head, _, _ := strings.Cut(string(b)[4:], "\n---\n")
	var m map[string]any
	if err := yaml.Unmarshal([]byte(head), &m); err != nil {
		t.Fatalf("the saved frontmatter is not YAML: %v\n%s", err, head)
	}
	return m
}

func loadAndSave(t *testing.T, dir string) *Spec {
	t.Helper()
	s, err := LoadSpec(dir)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := s.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return s
}

// A save writes every extension key back on the object it was loaded from,
// after that object's own fields, and a second load and save changes no byte.
func TestSave_KeepsExtensionKeysOnTheirObjects(t *testing.T) {
	dir := copyFixture(t, fixtureValidSpec)
	withExtensions(t, dir)
	loadAndSave(t, dir)

	req := readJSON(t, filepath.Join(dir, "requirements.json"))
	first := req["requirements"].([]any)[0].(map[string]any)
	criterion := first["criteria"].([]any)[1].(map[string]any)
	risk, _ := criterion["x-risk"].(map[string]any)
	path := req["execution_paths"].([]any)[0].(map[string]any)
	step := path["steps"].([]any)[0].(map[string]any)
	tasks := readJSON(t, filepath.Join(dir, "tasks.json"))
	progress := readJSON(t, filepath.Join(dir, "progress.json"))
	fm := readFrontmatter(t, dir)
	for name, ok := range map[string]bool{
		"requirements root":   req["x-team"] == "core",
		"requirement":         first["x-jira"] == "ABC-1",
		"criterion":           risk["level"] == "high" && risk["score"] == 3.0,
		"check":               criterion["check"].(map[string]any)["x-flaky"] == false,
		"path step":           step["x-note"] == true,
		"smoke check":         path["smoke"].(map[string]any)["x-env"] == "ci",
		"task":                tasks["tasks"].([]any)[1].(map[string]any)["x-estimate"] == 2.5,
		"test commands":       tasks["test_commands"].(map[string]any)["x-ci"] == "make ci",
		"progress entry":      progress["tasks"].([]any)[0].(map[string]any)["x-by"] == "agent-7",
		"frontmatter string":  fm["x-jira"] == "ABC-1",
		"frontmatter list":    len(fm["x-labels"].([]any)) == 2,
		"frontmatter map":     fm["x-meta"] != nil,
		"no key on the other": tasks["tasks"].([]any)[0].(map[string]any)["x-estimate"] == nil,
	} {
		if !ok {
			t.Errorf("%s: the extension key was not kept where it was", name)
		}
	}

	raw, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`"depends_on": \[\s*1\s*\],?\s*"x-estimate": 2\.5\s*}`).Match(raw) {
		t.Errorf("the extension key does not follow the task's own fields:\n%s", raw)
	}

	before := snapshot(t, dir)
	loadAndSave(t, dir)
	if after := snapshot(t, dir); after != before {
		t.Error("a second load and save of a package with extension keys changed its files")
	}
	if !strings.Contains(before, frontmatterExtensionLines[:len("x-jira: \"ABC-1\"\n")]) {
		t.Errorf("the frontmatter key is not written as it was read:\n%s", before)
	}
}

// snapshot is the content of a package's files, in one string.
func snapshot(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	for _, f := range []string{"prd.md", "requirements.json", "tasks.json", "progress.json"} {
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString(f + "\n" + string(data))
	}
	return b.String()
}

// A task move keeps the moved task's extension keys; an object a mutation
// removed takes its keys with it.
func TestSave_ExtensionKeysFollowTheirObjectThroughAMutation(t *testing.T) {
	dir := copyFixture(t, fixtureValidSpec)
	withExtensions(t, dir)
	s, err := LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := s.TransitionTask(1, string(TaskStateInProgress))
	if err != nil {
		t.Fatal(err)
	}
	s.Progress = moved
	s.Requirements.Requirements = s.Requirements.Requirements[1:] // drops REQ-1 and its keys
	s.Tasks.Tasks[0].Criteria = []string{"REQ-2"}
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}

	tasks := readJSON(t, filepath.Join(dir, "tasks.json"))
	if got := tasks["tasks"].([]any)[1].(map[string]any)["x-estimate"]; got != 2.5 {
		t.Errorf("task 2's extension key after a move of task 1 = %v", got)
	}
	progress := readJSON(t, filepath.Join(dir, "progress.json"))
	if got := progress["tasks"].([]any)[0].(map[string]any)["x-by"]; got != "agent-7" {
		t.Errorf("the moved task's progress entry lost its key: %v", got)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "requirements.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"x-jira", "x-risk", "x-flaky"} {
		if strings.Contains(string(raw), gone) {
			t.Errorf("%s outlived the requirement it was on", gone)
		}
	}
	if !strings.Contains(string(raw), `"x-team": "core"`) {
		t.Error("the root's extension key was lost")
	}
}

// Extension keys are allowed by every schema: the package validates, the raw
// files validate against the bundled schemas, and a key that is neither the
// format's nor an extension is still reported.
func TestValidateSchema_AllowsExtensionKeysAndReportsOtherKeys(t *testing.T) {
	dir := copyFixture(t, fixtureValidSpec)
	withExtensions(t, dir)
	editJSON(t, filepath.Join(dir, "tasks.json"), func(m map[string]any) {
		m["tasks"].([]any)[0].(map[string]any)["done-when"] = []any{"x"}
	})
	s, err := LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	var unknown []string
	for _, e := range s.ValidateSchema().Errors {
		if strings.Contains(e.Message, "x-") {
			t.Errorf("an extension key was reported: %+v", e)
		}
		if e.Check == "unknown_field" {
			unknown = append(unknown, e.Message)
		}
	}
	if len(unknown) != 1 || !strings.Contains(unknown[0], "done-when") {
		t.Errorf("unknown_field errors = %q, want the one for done-when", unknown)
	}

	for file, schema := range map[string]string{
		"requirements.json": RequirementsSchemaName,
		"progress.json":     ProgressSchemaName,
	} {
		if errs := ValidateArtifactSchema(readJSON(t, filepath.Join(dir, file)), schema, file); len(errs) > 0 {
			t.Errorf("%s with extension keys does not validate against its schema: %v", file, errs)
		}
	}
	errs := ValidateArtifactSchema(readJSON(t, filepath.Join(dir, "tasks.json")), TasksSchemaName, "tasks.json")
	if len(errs) == 0 {
		t.Error("a key that is not an extension key validated against the tasks schema")
	}
	for _, e := range errs {
		if strings.Contains(e.Message, "x-") {
			t.Errorf("the tasks schema refused an extension key: %+v", e)
		}
	}
	if errs := ValidateArtifactSchema(readFrontmatter(t, dir), PRDFrontmatterSchemaName, "prd.md"); len(errs) > 0 {
		t.Errorf("the frontmatter with extension keys does not validate against its schema: %v", errs)
	}
}

func TestUnknownFields_DoesNotListExtensionKeys(t *testing.T) {
	raw := map[string]any{"x-team": "core", "tasks": []any{map[string]any{"id": 1.0, "x-estimate": 2.0, "done-when": "x"}}}
	got := UnknownFields(raw, &TasksV3Json{})
	if len(got) != 1 || got[0] != "/tasks/0/done-when" {
		t.Errorf("UnknownFields = %q, want only /tasks/0/done-when", got)
	}
}

// The generator writes no extension keys: one in a model's artifact is
// refused by its path.
func TestDecodeArtifact_RefusesExtensionKeys(t *testing.T) {
	content := readJSON(t, filepath.Join(fixtureValidSpec, "requirements.json"))
	content["requirements"].([]any)[0].(map[string]any)["x-jira"] = "ABC-1"
	_, err := DecodeArtifact(StepRequirements, content)
	if err == nil || !strings.Contains(err.Error(), "/requirements/id=REQ-1/x-jira") ||
		!strings.Contains(err.Error(), "extension key") {
		t.Errorf("DecodeArtifact = %v, want a refusal naming the extension key", err)
	}
}
