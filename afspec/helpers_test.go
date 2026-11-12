package afspec

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixtureValidSpec is the canonical positive fixture: spec 01, a complete v3
// package that satisfies every rule of §10.2.
const fixtureValidSpec = "testdata/valid_spec"

// fixtureV3Example is the worked example of the format specification, as the
// migration of fixtureV2Example writes it.
const fixtureV3Example = "testdata/v3_example"

// fixtureV2Example is the worked example of format version 2: the input of
// the migration tests.
const fixtureV2Example = "testdata/v2_example"

// fixtureWithArch is the canonical fixture with an architecture.md beside it.
const fixtureWithArch = "testdata/valid_spec_with_arch"

// fixtureDraft is spec 02, which depends on spec 01.
const fixtureDraft = "testdata/draft_spec"

// validFixtures are the checked-in packages that satisfy every rule.
var validFixtures = []string{
	fixtureValidSpec,
	fixtureV3Example,
	fixtureDraft,
	"testdata/alpha_prefix_spec",
	fixtureWithArch,
}

// contractFiles are the files of a v3 package a save writes beside the
// progress and the render.
var contractFiles = []string{"prd.md", RequirementsFileName, TasksFileName}

// loadFixture loads a spec fixture or fails the test.
func loadFixture(t *testing.T, dir string) *Spec {
	t.Helper()
	spec, err := LoadSpec(dir)
	if err != nil {
		t.Fatalf("LoadSpec(%q) = %v; want no error", dir, err)
	}
	return spec
}

// copyDir copies the files directly under src into dst, creating dst.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("ReadDir(%q) = %v", src, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// copyFixture copies a fixture into a temporary directory so a test can save
// over it without touching the checked-in files. The returned path is the
// copy's directory.
func copyFixture(t *testing.T, dir string) string {
	t.Helper()
	dst := t.TempDir()
	copyDir(t, dir, dst)
	return dst
}

// specRoot builds a spec root containing the named fixtures, each copied into
// the directory named by its key (relative to the root).
func specRoot(t *testing.T, fixtures map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for dirName, fixture := range fixtures {
		copyDir(t, fixture, filepath.Join(root, dirName))
	}
	return root
}

// editTasks rewrites the tasks.json at path through edit.
func editTasks(t *testing.T, path string, edit func(*TasksV3Json)) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var tasks TasksV3Json
	if err := json.Unmarshal(data, &tasks); err != nil {
		t.Fatal(err)
	}
	edit(&tasks)
	out, err := MarshalJSON(&tasks)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// editProgress rewrites the progress.json at path through edit, from an empty
// progress when there is none.
func editProgress(t *testing.T, path string, edit func(*ProgressV3Json)) {
	t.Helper()
	var p ProgressV3Json
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &p); err != nil {
			t.Fatal(err)
		}
	}
	edit(&p)
	out, err := MarshalJSON(&p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// activeCopy saves a copy of the canonical fixture as active, with its intent
// hash, and returns its directory.
func activeCopy(t *testing.T) string {
	t.Helper()
	dir := copyFixture(t, fixtureValidSpec)
	spec := loadFixture(t, dir)
	spec.Status = "active"
	hash, err := ComputeIntentHash(spec.PRDBody)
	if err != nil {
		t.Fatal(err)
	}
	spec.IntentHash = &hash
	if err := spec.Save(dir); err != nil {
		t.Fatalf("Save of the activated spec = %v", err)
	}
	return dir
}

// bootstrapFrom is a BootstrapSpec with the given identity holding every
// artifact of the canonical fixture.
func bootstrapFrom(t *testing.T, specID, specName string) *BootstrapSpec {
	t.Helper()
	source := loadFixture(t, fixtureValidSpec)
	b := NewBootstrapSpec(specID, specName)
	b.Requirements = source.Requirements
	b.Tasks = source.Tasks
	b.PRDBody = source.PRDBody
	return b
}

// checkTwoSpecs asserts that metas lists the valid fixture as spec 01 and the
// draft fixture as spec 02, in that order.
func checkTwoSpecs(t *testing.T, metas []SpecMeta) {
	t.Helper()
	if len(metas) != 2 {
		t.Fatalf("discovered %d specs; want the two properly named ones", len(metas))
	}
	if metas[0].SpecID != "01" || metas[1].SpecID != "02" {
		t.Errorf("specs are not in prefix order: %v", metas)
	}
	if metas[0].Status != "draft" || metas[0].SpecName != "test_feature" {
		t.Errorf("metadata for spec 01 is wrong: %+v", metas[0])
	}
}

// hasFinding reports whether a lint finding carries the given rule.
func hasFinding(findings []LintFinding, rule string) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

// mutateFixture loads the canonical fixture and applies mutate to it.
func mutateFixture(t *testing.T, mutate func(*Spec)) *Spec {
	t.Helper()
	spec := loadFixture(t, fixtureValidSpec)
	mutate(spec)
	return spec
}

// errorChecks returns the Check field of every error in the result.
func errorChecks(result ValidationResult) []string {
	checks := make([]string, 0, len(result.Errors))
	for _, e := range result.Errors {
		checks = append(checks, e.Check)
	}
	return checks
}

// hasCheck reports whether the result carries an error with the given rule.
func hasCheck(result ValidationResult, check string) bool {
	for _, e := range result.Errors {
		if e.Check == check {
			return true
		}
	}
	return false
}

// hasEntityID reports whether the result carries an error about the given id.
func hasEntityID(result ValidationResult, id string) bool {
	for _, e := range result.Errors {
		if e.EntityID == id {
			return true
		}
	}
	return false
}

// hasWarningContaining reports whether any warning message contains sub.
func hasWarningContaining(result ValidationResult, sub string) bool {
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, sub) {
			return true
		}
	}
	return false
}

// messages returns the message of every error in the result.
func messages(result ValidationResult) []string {
	out := make([]string, 0, len(result.Errors))
	for _, e := range result.Errors {
		out = append(out, e.Message)
	}
	return out
}

// firstDifference describes where two texts diverge, so a round-trip failure
// points at the offending line instead of dumping both files.
func firstDifference(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		w, g := "<missing>", "<missing>"
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			return fmt.Sprintf("line %d:\n  want: %q\n  got:  %q", i+1, w, g)
		}
	}
	return "texts are equal"
}

// ---------------------------------------------------------------------------
// Lifecycle: packages in a given status, and snapshots of a tree on disk
// ---------------------------------------------------------------------------

// packageIn builds the package root/name in the given status: the valid
// fixture, brought there by the library's own moves.
func packageIn(t *testing.T, root, name, status string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	copyDir(t, fixtureValidSpec, dir)
	step := func(s *Spec, target string) *Spec {
		t.Helper()
		next, err := s.Transition(target, dir)
		if err != nil {
			t.Fatalf("Transition(%q) = %v", target, err)
		}
		return next
	}
	spec := loadFixture(t, dir)
	switch status {
	case "draft":
	case "active":
		step(spec, "active")
	case "sealed":
		step(step(spec, "active"), "sealed")
	case "superseded":
		if _, err := step(step(spec, "active"), "sealed").Supersede("02", dir); err != nil {
			t.Fatalf("Supersede = %v", err)
		}
	case "archived":
		step(spec, "archived")
	default:
		t.Fatalf("no such status %q", status)
	}
	return dir
}

// fileState is what a file holds and when it was last written.
type fileState struct {
	data    string
	modTime time.Time
}

// treeOf records every file under dir, by its path relative to dir.
func treeOf(t *testing.T, dir string) map[string]fileState {
	t.Helper()
	out := map[string]fileState{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			out[rel] = fileState{data: "symlink to " + target}
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out[rel] = fileState{string(data), info.ModTime()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// changedFiles names the files that differ between two trees, or exist in only
// one.
func changedFiles(a, b map[string]fileState) []string {
	var out []string
	for name, x := range a {
		if y, ok := b[name]; !ok || x.data != y.data || !x.modTime.Equal(y.modTime) {
			out = append(out, name)
		}
	}
	for name := range b {
		if _, ok := a[name]; !ok {
			out = append(out, name)
		}
	}
	return out
}

func sameTree(a, b map[string]fileState) bool { return len(changedFiles(a, b)) == 0 }

// entriesOf lists the names directly under dir.
func entriesOf(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// readOnly makes dir unwritable for the rest of the test, and writable again
// when it ends so that its temporary directory can be removed. The tests that
// use it cannot fail a write as root.
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

// ---------------------------------------------------------------------------
// Task plans
// ---------------------------------------------------------------------------

// plannedState pairs a task with the state its progress entry records.
type plannedState struct {
	task  Task
	state TaskState
}

// planTask is a task with the given state. A nil deps leaves depends_on
// absent, which format v3 §8.2 reads as "the previous task"; a non-nil empty
// slice is an explicit "none".
func planTask(id int, state TaskState, deps []int) plannedState {
	return plannedState{Task{Id: id, Title: "task", Criteria: []string{"REQ-1"}, Steps: []string{"s"}, DependsOn: deps}, state}
}

// plan builds a lite package (so that it has no integration task) whose plan
// is the given tasks, in order, with their states in the progress.
func plan(tasks ...plannedState) *Spec {
	s := CreateSpec("05", "plan")
	s.Profile = ProfileLite
	s.Progress = &ProgressV3Json{}
	for _, pt := range tasks {
		s.Tasks.Tasks = append(s.Tasks.Tasks, pt.task)
		s.Progress.Tasks = append(s.Progress.Tasks, TaskProgress{Id: pt.task.Id, State: pt.state})
	}
	return s
}

// ---------------------------------------------------------------------------
// Generation
// ---------------------------------------------------------------------------

// artifactMap re-encodes a typed artifact as the decoded JSON object a model's
// tool input arrives as.
func artifactMap(t *testing.T, v any) map[string]any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// partialFrom is the canonical fixture as the generation pipeline holds it
// after the given steps.
func partialFrom(t *testing.T, steps ...GenerationStep) PartialSpec {
	t.Helper()
	source := loadFixture(t, fixtureValidSpec)
	partial := PartialSpec{SpecID: source.SpecID, SpecName: source.SpecName}
	for _, step := range steps {
		switch step {
		case StepRequirements:
			partial.Requirements = source.Requirements
		case StepTasks:
			partial.Tasks = source.Tasks
		}
	}
	return partial
}

// stepsThrough lists the generation steps up to and including step.
func stepsThrough(step GenerationStep) []GenerationStep {
	var produced []GenerationStep
	for _, s := range GenerationSteps {
		produced = append(produced, s)
		if s == step {
			break
		}
	}
	return produced
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// fullySetSpec is the valid fixture with every optional field of the package
// set, including values that would break a Markdown table if written raw.
func fullySetSpec(t *testing.T) *Spec {
	t.Helper()
	s := loadFixture(t, fixtureValidSpec)
	str := func(v string) *string { return &v }
	s.Source = "https://example.com/issues/7"
	s.SteeringVersion = "3.1.4"
	s.Supersedes = []string{"00_old_loader"}
	s.IntentHash = str("sha256:abc123")
	s.Requirements.ExternalApis = []ExternalApi{{
		Package: "example.com/lib", Version: "v1.2.3", Verified: true,
		Symbols: []ExternalApiSymbol{{Name: "Union", ImportPath: "example.com/lib/u",
			Signature: "func Union(a, b T) T", Notes: str("returns a | b")}},
	}}
	s.Requirements.ExecutionPaths[0].Smoke.Pseudocode = str("run the path; assert the end state")
	s.Requirements.Requirements[1].Criteria[0].Check = SameAs("REQ-1.1")
	s.Tasks.Dependencies = []Dependency{{Spec: "00_bootstrap", Reason: "uses its loader"}}
	s.Tasks.TestCommands.SpecTests = str("go test ./afspec/... -run TestSpec01")
	s.Tasks.Tasks[0].DoneWhen = []string{"the loader is documented"}
	return s
}

// leaves collects every scalar value of v's JSON form, with its key, skipping
// the keys a render deliberately leaves out.
func leaves(t *testing.T, v any, skip map[string]bool) map[string]string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	var walk func(path string, x any)
	walk = func(path string, x any) {
		switch y := x.(type) {
		case map[string]any:
			for k, c := range y {
				if !skip[k] {
					walk(path+"/"+k, c)
				}
			}
		case []any:
			for i, c := range y {
				walk(fmt.Sprintf("%s/%d", path, i), c)
			}
		case string:
			out[path] = y
		case float64:
			out[path] = fmt.Sprint(y)
		}
	}
	walk("", doc)
	return out
}

// unescape undoes the table-cell escaping, so a value can be found in the
// render as it is in the files.
func unescape(render string) string {
	return strings.NewReplacer(`\|`, "|", "<br>", "\n").Replace(render)
}

// activeInto activates the package in dir through the library, and returns dir.
func activeInto(t *testing.T, dir string) string {
	t.Helper()
	if _, err := loadFixture(t, dir).Transition("active", dir); err != nil {
		t.Fatalf("Transition(active) = %v", err)
	}
	return dir
}

// withRender writes the render of the package in dir to its RenderedFileName,
// as a save would, and returns dir.
func withRender(t *testing.T, dir string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, RenderedFileName), loadFixture(t, dir).RenderedBytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// scopedRender joins the parts of a scoped render in the order of a whole
// render.
func scopedRender(parts map[string]string) string {
	return parts["prd"] + parts["architecture"] + parts["requirements"] + parts["tasks"]
}

// ---------------------------------------------------------------------------
// The next package: packages of a spec root with dependencies and states
// ---------------------------------------------------------------------------

// pkgAt copies the valid fixture to root/dirName as the package id, depending
// on deps, with every task of its plan, the integration task included, in
// state.
func pkgAt(t *testing.T, root, dirName, id string, state TaskState, deps ...string) string {
	t.Helper()
	dir := filepath.Join(root, dirName)
	copyDir(t, fixtureValidSpec, dir)
	prd := filepath.Join(dir, "prd.md")
	b, err := os.ReadFile(prd)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prd, []byte(strings.Replace(string(b), `spec_id: "01"`, `spec_id: "`+id+`"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	editTasks(t, filepath.Join(dir, TasksFileName), func(tj *TasksV3Json) {
		tj.SpecId = id
		tj.Dependencies = nil
		for _, d := range deps {
			tj.Dependencies = append(tj.Dependencies, Dependency{Spec: d, Reason: "needs " + d})
		}
	})
	editProgress(t, filepath.Join(dir, ProgressFileName), func(p *ProgressV3Json) {
		p.SpecId = id
		p.Tasks = nil
		for i := 1; i <= 3; i++ {
			p.Tasks = append(p.Tasks, TaskProgress{Id: i, State: state})
		}
	})
	return dir
}
