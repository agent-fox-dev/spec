package specgen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/agent-fox-dev/agentkit-go/provider/faux"
	"github.com/agent-fox-dev/agentkit-go/tools"
	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/agentrun/indextest"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/project"
)

func TestRun_WritesAValidatedActivatedPackage(t *testing.T) {
	ws := newWorkspace(t)
	a := newAuthor(t, "01", "test_feature")
	a.prd.RelevantFiles = []RelevantFile{{Path: "a.go", Why: "the loader"}}
	idx := &indextest.Index{}
	o := newOptions(ws, a)
	// Without --adversarial no review phase runs.
	o.reviewer = failingReviewer{t}
	o.Index = idx

	got, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.SpecID != "01" || got.SpecName != "test_feature" || got.SpecDir != filepath.Join(".specs", "01_test_feature") {
		t.Errorf("id/name/dir = %q/%q/%q", got.SpecID, got.SpecName, got.SpecDir)
	}
	if !got.Validation.Valid {
		t.Fatalf("the package does not validate: %+v", got.Validation.Errors)
	}
	if got.Status != "active" || got.Source != "interactive" {
		t.Errorf("status %q, source %q; want a valid package activated", got.Status, got.Source)
	}

	// One PRD phase, then the generation steps in the format's order: a step
	// that cannot see a test id cannot own it.
	if !slices.Equal(a.steps, afspec.GenerationSteps) || len(a.prdRequests) != 1 {
		t.Errorf("%d PRD phases, steps %v", len(a.prdRequests), a.steps)
	}

	dir := filepath.Join(ws.Root(), got.SpecDir)
	files := dirNames(t, dir)
	if !slices.Equal(files, []string{"prd.md", "requirements.json", "spec.md", "tasks.json", "test_spec.json"}) {
		t.Errorf("package files = %v", files)
	}
	if want := []string{"prd.md", "requirements.json", "test_spec.json", "tasks.json", "spec.md"}; !slices.Equal(got.Package.Artifacts, want) {
		t.Errorf("artifacts = %v, want %v: the run reports every file it wrote", got.Package.Artifacts, want)
	}
	// The relevant files are reported, never written into the package.
	for _, name := range files {
		if b, _ := os.ReadFile(filepath.Join(dir, name)); bytes.Contains(b, []byte("relevant_files")) {
			t.Errorf("%s carries relevant_files", name)
		}
	}
	if len(got.RelevantFiles) != 1 || got.RelevantFiles[0].Path != "a.go" {
		t.Errorf("RelevantFiles = %+v", got.RelevantFiles)
	}

	// What is reported is the verdict on the package as it was written, read
	// back from disk.
	loaded, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if loaded.Title != "Test Feature" || loaded.IntentHash == nil || *loaded.IntentHash == "" {
		t.Errorf("title %q, intent hash %v; activation computes the hash", loaded.Title, loaded.IntentHash)
	}
	if res := loaded.Validate(); res.Valid != got.Validation.Valid || len(res.Errors) != got.Validation.ErrorCount {
		t.Errorf("the reported verdict disagrees with the package on disk: %+v", res)
	}

	// The counts and the traceability are derived, not stored.
	if got.Requirements == 0 || got.Tests == 0 || got.Tasks == 0 {
		t.Errorf("counts = %+v", got.Package)
	}
	if len(got.Traceability.CriteriaUncovered) != 0 || len(got.Traceability.TestsUnowned) != 0 ||
		got.Traceability.CriteriaCovered != got.Criteria {
		t.Errorf("traceability = %+v for %d criteria", got.Traceability, got.Criteria)
	}
	if len(got.OpenQuestions) != 1 {
		t.Errorf("OpenQuestions = %+v", got.OpenQuestions)
	}
	if raw, _ := json.Marshal(got); got.Review != nil || bytes.Contains(raw, []byte(`"review"`)) {
		t.Errorf("the result has a review: %s", raw)
	}
	if want := "spec: wrote .specs/01_test_feature; 1 open question"; got.Summary() != want {
		t.Errorf("Summary() = %q, want %q", got.Summary(), want)
	}

	// Every phase is recorded in usage, with the tool calls it made.
	env := o.Run.Envelope(cli.ExitOK, got, nil)
	var names []string
	for _, p := range env.Usage.Phases {
		names = append(names, p.Name)
		if len(p.ToolCalls) == 0 || p.Scope != "test_feature" {
			t.Errorf("phase %s: tool calls %v, scope %q", p.Name, p.ToolCalls, p.Scope)
		}
	}
	if want := []string{"prd", "generate:requirements", "generate:test_spec", "generate:tasks"}; !slices.Equal(names, want) {
		t.Errorf("usage phases = %v, want %v", names, want)
	}

	// The spec tool never changes the tree, so it never invalidates the index.
	if n := idx.InvalidateCalls(); n != 0 {
		t.Errorf("Invalidate was called %d times: %v", n, idx.Events())
	}
}

func TestRun_NumbersAPackageAfterTheExistingOnes(t *testing.T) {
	ws := newWorkspace(t)
	if _, err := Run(context.Background(), newOptions(ws, newAuthor(t, "01", "first_feature"))); err != nil {
		t.Fatalf("first run: %v", err)
	}
	got, err := Run(context.Background(), newOptions(ws, newAuthor(t, "02", "second_feature")))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got.SpecID != "02" {
		t.Errorf("SpecID = %q, want the next free prefix", got.SpecID)
	}
}

// An archived spec keeps its number, so the next package never reuses it.
func TestMaxSpecNumber_CountsArchivedSpecs(t *testing.T) {
	for _, c := range []struct {
		name string
		dirs []string
		want int
	}{
		{"only archived", []string{"archive/01_a", "archive/02_b"}, 2},
		{"archived above active", []string{"02_x", "archive/05_y"}, 5},
		{"active above archived", []string{"07_x", "archive/05_y"}, 7},
		{"no archive directory", []string{"01_a", "03_c"}, 3},
		{"names without a number", []string{"notes", "_x", "x_y"}, 0},
		{"nothing at all", nil, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			for _, d := range c.dirs {
				if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if got := maxSpecNumber(root); got != c.want {
				t.Errorf("maxSpecNumber = %d, want %d", got, c.want)
			}
		})
	}
}

// A dry run leaves nothing on disk to count, so it numbers from what it has
// assigned itself.
func TestAllocateID_SkipsArchivedSpecsAndStaysIncreasing(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"archive/01_a", "archive/02_b"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e := &runEnv{specsDir: root}
	for _, want := range []string{"03", "04", "05"} {
		if got := e.allocateID(); got != want {
			t.Errorf("allocateID = %s, want %s", got, want)
		}
	}
}

func TestFormatSpecID_PadsToSortLexically(t *testing.T) {
	for n, want := range map[int]string{1: "01", 42: "42", 99: "99", 100: "100", 123: "123"} {
		if got := formatSpecID(n); got != want {
			t.Errorf("formatSpecID(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestReserveSpecDir_GivesConcurrentRunsDistinctNumbers(t *testing.T) {
	specs := filepath.Join(t.TempDir(), ".specs")
	var mu sync.Mutex
	var ids []string
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := reserveSpecDir(specs, fmt.Sprintf("spec_%c", 'a'+i), 0)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			ids = append(ids, id)
			mu.Unlock()
		}()
	}
	wg.Wait()
	slices.Sort(ids)
	if len(slices.Compact(slices.Clone(ids))) != 8 {
		t.Errorf("ids = %v, want 8 distinct numbers", ids)
	}
}

// A lock left by a run that died holding it is broken; a fresh one is
// another run's and is waited for.
func TestReserveSpecDir_BreaksAStaleLock(t *testing.T) {
	specs := t.TempDir()
	lock := filepath.Join(specs, reserveLockName)
	writeFiles(t, specs, map[string]string{reserveLockName: ""})
	old := time.Now().Add(-2 * reserveLockStale)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	id, err := reserveSpecDir(specs, "widget", 4)
	if err != nil || id != "05" {
		t.Fatalf("reserveSpecDir = %q, %v; want 05 past the floor", id, err)
	}
	if exists(lock) {
		t.Error("the lock was left behind")
	}
}

// The number is reserved on disk before the paid phases run, so a run that
// starts meanwhile cannot take it.
func TestRun_ReservesTheNumberWhileThePhasesRun(t *testing.T) {
	ws := newWorkspace(t)
	a := newAuthor(t, "01", "test_feature")
	reserved := false
	a.onGenerate = func(req artifactRequest) {
		reserved = reserved || exists(filepath.Join(ws.Root(), ".specs", req.SpecID+"_"+req.SpecName))
	}
	if _, err := Run(context.Background(), newOptions(ws, a)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !reserved {
		t.Error("the package directory did not exist while the generation phases ran")
	}
}

// A run that fails before the package is written gives the number back and
// claims no package: the result reports what the PRD phase decided, the
// summary does not say "wrote", and no artifact points at a directory.
func TestRun_AFailedGenerationStepLeavesNothingAndClaimsNoPackage(t *testing.T) {
	for _, step := range []afspec.GenerationStep{afspec.StepRequirements, afspec.StepTasks} {
		t.Run(string(step), func(t *testing.T) {
			ws := newWorkspace(t)
			a := newAuthor(t, "01", "test_feature")
			a.artifactErr[step] = errors.New("anthropic: HTTP 400: grammar too large")

			res, err := Run(context.Background(), newOptions(ws, a))
			if err == nil || res == nil {
				t.Fatalf("want a result and an error, got %v, %v", res, err)
			}
			if f := asFailure(t, err); f.Stage != string(step) {
				t.Errorf("stage = %q", f.Stage)
			}
			if names := specDirs(t, ws); names != "" {
				t.Errorf("the spec root holds %s, want the reservation removed", names)
			}
			if res.SpecDir != "" || res.Status != "" || res.Package.Artifacts == nil || len(res.Package.Artifacts) != 0 {
				t.Errorf("the result claims a package: dir %q, status %q, artifacts %#v", res.SpecDir, res.Status, res.Package.Artifacts)
			}
			if res.SpecID != "01" || res.SpecName != "test_feature" || res.Title != "Test Feature" || len(res.OpenQuestions) != 1 {
				t.Errorf("what the PRD phase decided was dropped: %+v", res.Package)
			}
			if len(res.Artifacts()) != 0 || len(res.Next()) != 0 {
				t.Errorf("artifacts %+v, next %+v; nothing was written", res.Artifacts(), res.Next())
			}
			env := cli.NewRun("spec", "test").Envelope(cli.ExitFailed, res, cli.ErrorFrom("run", err))
			if strings.Contains(env.Summary, "wrote") || !strings.Contains(env.Summary, "grammar too large") || len(env.Artifacts) != 0 {
				t.Errorf("summary %q, artifacts %+v: a failed run says what failed", env.Summary, env.Artifacts)
			}
		})
	}
}

// A package that does not validate is still written, because a spec you can
// read and fix is worth more than none: it is reported with the rules it
// breaks, not activated, and not posted to the issue. The break is a test no
// task owns, made with the per-step check bypassed, which is how a model that
// satisfies each step and still produces an inconsistent package looks.
func TestRun_WritesAnInvalidPackageAndReportsIt(t *testing.T) {
	ws := newWorkspace(t)
	a := newAuthor(t, "01", "test_feature")
	a.skipStepValidation = true
	dropOneOwnedTest(a.artifacts[afspec.StepTasks])
	o := newOptions(ws, a)
	forge := &mockForge{authenticated: true}
	commentOn(&o, forge)

	got, err := Run(context.Background(), o)
	if f := asFailure(t, err); f.Category != CategoryInvalid || f.Stage != "validate" {
		t.Fatalf("err = %v", err)
	}
	if got == nil || got.Validation.Valid || got.Status != "draft" {
		t.Fatalf("result = %+v; want an invalid package left in draft", got)
	}
	if !exists(filepath.Join(ws.Root(), got.SpecDir, "tasks.json")) {
		t.Error("the package should be on disk")
	}
	for _, e := range got.Validation.Errors {
		if e.Check == "" {
			t.Errorf("an error with no rule name: %+v", e)
		}
	}
	if w, ok := warning(o.Run, cli.WarnCommentNotPosted); !ok || !strings.Contains(w.Message, "validate") || forge.commentCalls != 0 {
		t.Errorf("warning %+v, %d comments; want the skipped comment warned about", w, forge.commentCalls)
	}

	// The report is the one spec has always written, byte for byte.
	have, err := json.MarshalIndent(map[string]any{"validation": got.Validation, "traceability": got.Traceability}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "internal", "specpkg", "testdata", "pipeline.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(have, '\n'), want) {
		t.Errorf("the report differs from internal/specpkg/testdata/pipeline.golden.json:\n%s", have)
	}
}

// A dry run produces and validates the package, against the directory name it
// would have used, and writes nothing to disk or to the forge.
func TestRun_DryRunValidatesAndWritesNothing(t *testing.T) {
	ws := newWorkspace(t)
	o := newOptions(ws, newAuthor(t, "01", "test_feature"))
	o.DryRun = true
	forge := &mockForge{authenticated: true, commentURL: "https://forge/c"}
	commentOn(&o, forge)

	got, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !got.Validation.Valid || got.Status != "draft" || got.Traceability.CriteriaCovered == 0 {
		t.Errorf("valid %v, status %q, trace %+v", got.Validation.Valid, got.Status, got.Traceability)
	}
	if exists(filepath.Join(ws.Root(), ".specs")) {
		t.Error("--dry-run created the spec root")
	}
	if slices.Contains(got.Package.Artifacts, afspec.RenderedFileName) {
		t.Errorf("artifacts = %v: a dry run wrote no render", got.Package.Artifacts)
	}
	if forge.commentCalls != 0 || len(o.Run.SideEffects()) != 0 {
		t.Errorf("%d comments, side effects %+v", forge.commentCalls, o.Run.SideEffects())
	}
}

// The architecture document is optional by the format's own definition, so
// failing to write it degrades the package instead of failing the run.
func TestRun_WritesTheArchitectureWhenAsked(t *testing.T) {
	t.Run("written", func(t *testing.T) {
		ws := newWorkspace(t)
		a := newAuthor(t, "01", "test_feature")
		a.arch = "# Architecture: test_feature\n\n## Overview\n\nIt loads specs.\n"
		o := newOptions(ws, a)
		o.Architecture = true
		got, err := Run(context.Background(), o)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		b, err := os.ReadFile(filepath.Join(ws.Root(), got.SpecDir, "architecture.md"))
		if err != nil || !strings.HasPrefix(string(b), "# Architecture") {
			t.Errorf("architecture.md = %q, %v", b, err)
		}
		if !slices.Contains(got.Package.Artifacts, "architecture.md") {
			t.Errorf("artifacts = %v", got.Package.Artifacts)
		}
	})
	t.Run("failed", func(t *testing.T) {
		o := newOptions(newWorkspace(t), newAuthor(t, "01", "test_feature"))
		o.Architecture = true
		got, err := Run(context.Background(), o)
		if err != nil || !got.Validation.Valid {
			t.Fatalf("Run: %v, valid %v", err, got.Validation.Valid)
		}
		if _, ok := warning(o.Run, cli.WarnArchitectureNotWritten); !ok {
			t.Errorf("warnings = %v", o.Run.Warnings())
		}
	})
}

// Every refusal Preflight makes comes before any phase is paid for.
func TestRun_RefusesBeforeAnyPhase(t *testing.T) {
	for _, tc := range []struct {
		name     string
		set      func(*Options)
		category string
		msg      string
	}{
		{"a name the format cannot use", func(o *Options) { o.Name = "Not Valid" }, "usage", "--name"},
		{"a name with a doubled underscore", func(o *Options) { o.Name = "widget__cache" }, "usage", "--name"},
		{"--comment on an input with no issue", func(o *Options) { o.Comment = true }, "usage", "posts the PRD back"},
		{"--comment with no forge", func(o *Options) { commentOn(o, nil) }, "auth",
			"--comment needs a forge credential: set GITHUB_TOKEN, GH_TOKEN, or GITLAB_TOKEN, or run without --comment"},
		{"--comment with no credential", func(o *Options) { commentOn(o, &mockForge{}) }, "auth", "forge credential"},
		{"no workspace", func(o *Options) { o.Workspace = nil }, agentrun.CategoryInternal, "no workspace"},
		{"no runner", func(o *Options) { o.author = nil }, agentrun.CategoryInternal, "no runner"},
		{"--adversarial with no runner", func(o *Options) { o.Adversarial = true }, agentrun.CategoryInternal, "no runner"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := newWorkspace(t)
			a := newAuthor(t, "01", "test_feature")
			o := newOptions(ws, a)
			tc.set(&o)
			res, err := Run(context.Background(), o)
			f := asFailure(t, err)
			if f.Stage != "preflight" || f.Category != tc.category || !strings.Contains(f.Error(), tc.msg) {
				t.Errorf("failure %s/%s %q, want preflight/%s mentioning %q", f.Stage, f.Category, f.Error(), tc.category, tc.msg)
			}
			if res != nil || len(a.prdRequests) != 0 || exists(filepath.Join(ws.Root(), ".specs")) {
				t.Errorf("result %+v, %d PRD phases; want nothing run or written", res, len(a.prdRequests))
			}
		})
	}
	if pkg, dir, err := GeneratePRD(context.Background(), Options{}); pkg != nil || dir != "" || err == nil {
		t.Errorf("GeneratePRD on a refused run = %+v, %q, %v", pkg, dir, err)
	}
}

func TestSourceField_RecordsProvenance(t *testing.T) {
	for _, c := range []struct {
		in   cli.Input
		want string
	}{
		{cli.Input{Kind: cli.KindText, Origin: "argument"}, "interactive"},
		{cli.Input{Kind: cli.KindStdin, Origin: "stdin"}, "interactive"},
		{cli.Input{Kind: cli.KindFile, Origin: "docs/prd.md"}, "docs/prd.md"},
		{cli.Input{Kind: cli.KindIssue, Origin: "https://github.com/a/b/issues/1"}, "https://github.com/a/b/issues/1"},
	} {
		t.Run(c.in.Kind.String(), func(t *testing.T) {
			if got := sourceField(c.in); got != c.want {
				t.Errorf("sourceField = %q, want %q", got, c.want)
			}
		})
	}
}

func TestNormalizeBody_StripsFrontmatterAndSettlesWhitespace(t *testing.T) {
	for in, want := range map[string]string{
		"---\ntitle: x\n---\n\n## Intent\n\nDo the thing.\n\n\n": "## Intent\n\nDo the thing.\n",
		"\n\n## Intent\n\nx":             "## Intent\n\nx\n",
		"---\nunterminated\n## Intent\n": "---\nunterminated\n## Intent\n",
	} {
		if got := normalizeBody(in); got != want {
			t.Errorf("normalizeBody(%q) = %q, want %q", in, got, want)
		}
	}
}

// --comment posts the finished PRD to the issue it came from, and records the
// comment as a side effect; a comment the forge refuses is a warning, not a
// failure of the run that produced the package.
func TestRun_PostsThePRDToTheIssue(t *testing.T) {
	for _, tc := range []struct {
		name    string
		forge   *mockForge
		wantURL string
		wantOK  bool
		wantWrn cli.WarnCode
	}{
		{"posted", &mockForge{authenticated: true, commentURL: "https://forge/issue/42#note_9"}, "https://forge/issue/42#note_9", true, ""},
		{"refused", &mockForge{authenticated: true, commentErr: errors.New("forbidden")}, "", false, cli.WarnCommentNotPosted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := newOptions(newWorkspace(t), newAuthor(t, "01", "test_feature"))
			commentOn(&o, tc.forge)
			pkg, dir, err := GeneratePRD(context.Background(), o)
			if err != nil || pkg == nil {
				t.Fatalf("GeneratePRD: %v", err)
			}
			if pkg.CommentURL != tc.wantURL || dir != pkg.SpecDir {
				t.Errorf("CommentURL %q, dir %q", pkg.CommentURL, dir)
			}
			if tc.forge.commentCalls != 1 || tc.forge.lastRef != testIssue {
				t.Errorf("%d comments on %+v", tc.forge.commentCalls, tc.forge.lastRef)
			}
			for _, want := range []string{"## Specification: `01_test_feature`", "The package validates.",
				"Should the loader follow symlinks?", "## Intent"} {
				if !strings.Contains(tc.forge.lastBody, want) {
					t.Errorf("the comment lacks %q:\n%s", want, tc.forge.lastBody)
				}
			}
			se := o.Run.SideEffects()
			if len(se) != 1 || se[0].Action != "comment" || se[0].Target != "acme/widgets#42" ||
				se[0].OK != tc.wantOK || se[0].Warning != tc.wantWrn {
				t.Errorf("side effects = %+v", se)
			}
			if w, ok := warning(o.Run, cli.WarnCommentNotPosted); ok != !tc.wantOK ||
				(ok && !strings.Contains(w.Message, "the PRD could not be posted")) {
				t.Errorf("warnings = %+v", o.Run.Warnings())
			}
		})
	}
}

// Every phase is granted code_search when the run has an index — the
// Options' or, when they carry none, the Runner's — and none is without one.
func TestRun_GrantsCodeSearchOnlyWithAnIndex(t *testing.T) {
	for _, tc := range []struct {
		name              string
		options, onRunner bool
		want              bool
	}{
		{"the options' index", true, true, true},
		{"the runner's index", false, true, true},
		{"no index", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := newWorkspace(t)
			idx := &indextest.Index{}
			var runnerIdx tools.Index
			if tc.onRunner {
				runnerIdx = idx
			}
			p := faux.New()
			o := newOptions(ws, nil)
			o.Runner = indexedRunner(t, ws, p, runnerIdx)
			if tc.options {
				o.Index = idx
			}
			// The unscripted model ends the PRD phase with no result; what the
			// phase was offered is all this needs.
			_, _ = Run(context.Background(), o)
			got := toolNames(firstRequest(t, p))
			if slices.Contains(got, "code_search") != tc.want {
				t.Errorf("tools = %v, want code_search %v", got, tc.want)
			}
			for _, n := range agentrun.ReadOnlyFileTools {
				if !slices.Contains(got, n) {
					t.Errorf("%s is missing from the PRD phase", n)
				}
			}
		})
	}
}

// A package is validated with the specs of the spec root, so one that passes
// its own rules and breaks a rule across specs is written, reported with the
// rule, and not activated. The upstream package's paths use the actors
// "consumer" and "afspec library".
func TestRun_ChecksThePackageAgainstTheSpecsItDependsOn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		shape    packageShape
		breakOwn bool
		want     []string
	}{
		{"sharing an actor with its dependency", packageShape{dependsOn: "01"}, false, nil},
		{"sharing no actor with its dependency", packageShape{dependsOn: "01", actor: "somebody else"}, false,
			[]string{"cross_spec_actor"}},
		{"naming its dependency by directory", packageShape{dependsOn: "01_widget_core"}, false,
			[]string{"cross_spec_unknown_dependency"}},
		// One report, the package's own findings first, as speca gives them.
		{"breaking its own rules too", packageShape{dependsOn: "01", actor: "somebody else"}, true,
			[]string{"C7", "cross_spec_actor"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := newWorkspace(t)
			if up, err := Run(context.Background(), newOptions(ws, newAuthor(t, "01", "widget_core"))); err != nil || up.Status != "active" {
				t.Fatalf("the upstream package: %v", err)
			}
			a := newAuthor(t, "02", "widget_github")
			a.shapes = map[string]packageShape{"widget_github": tc.shape}
			if tc.breakOwn {
				a.skipStepValidation = true
				dropOneOwnedTest(a.artifacts[afspec.StepTasks])
			}
			got, err := Run(context.Background(), newOptions(ws, a))
			if tc.want == nil {
				if err != nil || !got.Validation.Valid || got.Status != "active" {
					t.Fatalf("err %v, valid %v, status %q; want a valid, active package", err, got.Validation.Valid, got.Status)
				}
				return
			}
			if !isInvalid(err) {
				t.Fatalf("err = %v; want the failure of a package that does not validate", err)
			}
			if checks := checksOf(got.Validation.Errors); !slices.Equal(checks, tc.want) || got.Validation.ErrorCount != len(checks) {
				t.Errorf("errors %v (count %d), want %v", checks, got.Validation.ErrorCount, tc.want)
			}
			if e := got.Validation.Errors[len(got.Validation.Errors)-1]; e.Check == "cross_spec_actor" &&
				(e.Entity != "02" || !strings.Contains(e.Message, "spec 02 depends on spec 01")) {
				t.Errorf("the finding = %+v; want it about spec 02 and its dependency on 01", e)
			}
			loaded, err := afspec.LoadSpec(filepath.Join(ws.Root(), got.SpecDir))
			if err != nil || got.Status != "draft" || loaded.Status != "draft" {
				t.Errorf("status %q, on disk %v (%v); want draft", got.Status, loaded, err)
			}
		})
	}
}

// One unreadable spec directory is warned about; the specs that can be read
// are still the landscape every phase is shown.
func TestRun_AnUnreadableSpecDirDoesNotHideTheOthers(t *testing.T) {
	ws := newWorkspace(t)
	specs := filepath.Join(ws.Root(), ".specs")
	copyFixture(t, filepath.Join(specs, "01_test_feature"))
	if err := os.MkdirAll(filepath.Join(specs, "07_scratch"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := newAuthor(t, "08", "test_feature")
	o := newOptions(ws, a)
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if land := a.prdRequests[0].Landscape; len(land) != 1 || land[0].SpecID != "01" {
		t.Errorf("landscape = %+v, want the readable 01 spec", land)
	}
	if _, ok := warning(o.Run, cli.WarnSpecsDirUnreadable); !ok {
		t.Error("the unreadable directory was not warned about")
	}
}

// An undivided input has no interior boundary: --total-budget does not stop
// it between phases, and the one phase's ceiling bounds it.
func TestRun_TotalBudgetDoesNotStopAnUndividedInput(t *testing.T) {
	a := newAuthor(t, "01", "test_feature")
	a.prdCost = 0.30
	o := newOptions(newWorkspace(t), a)
	o.TotalBudgetUSD = 0.01
	got, err := Run(context.Background(), o)
	if err != nil || got.SpecID != "01" || !got.Validation.Valid {
		t.Errorf("err %v, result %+v; want the package written", err, got)
	}
}

func TestResolveSpecsDir_FlagThenEnvironmentThenDefault(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(t.TempDir(), "specs")
	for _, tc := range []struct {
		name, flag, env, want string
	}{
		{"the default", "", "", filepath.Join(root, DefaultSpecDirName)},
		{"a relative environment value", "", "env/specs", filepath.Join(root, "env/specs")},
		{"an absolute environment value", "", abs + "/", abs},
		{"a relative flag over the environment", "flag/specs", "env/specs", filepath.Join(root, "flag/specs")},
		{"an absolute flag", abs + "/.", "", abs},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(SpecDirEnv, tc.env)
			if got := resolveSpecsDir(Options{SpecsDir: tc.flag}, root); got != tc.want {
				t.Errorf("resolveSpecsDir = %q, want %q", got, tc.want)
			}
		})
	}
}

// The repository map is built once, before the first phase, and every phase
// of every scope carries the same map in its system prompt, where the
// provider caches it. It never reaches the envelope.
func TestRun_BuildsTheMapOnceAndEveryPhaseCarriesIt(t *testing.T) {
	ws := newWorkspace(t)
	arch := toolCall("a", ToolSubmitArchitecture, map[string]any{"document": "# Architecture\n"})
	turns := []faux.Turn{toolCall("p1", ToolSubmitPRD, prdArgs(t, "widget_core", "widget_core", "widget_gadget"))}
	turns = append(turns, generationTurns(t, "01", "widget_core")...)
	turns = append(turns, arch, toolCall("p2", ToolSubmitPRD, prdArgs(t, "widget_gadget")))
	turns = append(turns, generationTurns(t, "02", "widget_gadget")...)
	turns = append(turns, arch)
	runner, p := fauxRunner(t, ws, turns...)

	o := fileOptions(ws, &agentAuthor{runner: runner, ws: ws})
	o.Architecture = true
	o.RepoMapTokens = 1234
	o.Input.Body = "widgets: a model in internal/widget/model.go, and a gadget"
	var calls, budget int
	var inputs []string
	o.buildMap = func(_ context.Context, _ *workspace.Workspace, b int, in []string) (string, error) {
		calls++
		budget, inputs = b, in
		return sampleMap, nil
	}
	res, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 1 || budget != 1234 || !slices.Equal(inputs, []string{"internal/widget/model.go"}) {
		t.Errorf("built %d times, budget %d, input paths %v; want once, 1234, the path the idea names", calls, budget, inputs)
	}
	reqs := p.Requests()
	if want := 2 * (1 + len(afspec.GenerationSteps) + 1); len(reqs) != want {
		t.Fatalf("%d phases reached the model, want %d", len(reqs), want)
	}
	first := mapSection(blocksText(reqs[0].System))
	if !strings.Contains(first, "The map below lists the repository") || !strings.Contains(first, strings.TrimSpace(sampleMap)) {
		t.Fatalf("the first phase's map block:\n%s", first)
	}
	for i, req := range reqs {
		if sec := mapSection(blocksText(req.System)); sec != first {
			t.Errorf("phase %d's map block differs from the first phase's:\n%s", i, sec)
		}
	}
	raw, err := json.Marshal(o.Run.Envelope(0, res, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"## Repository map", "repo_map", "main.go  func Main"} {
		if bytes.Contains(raw, []byte(banned)) {
			t.Errorf("the envelope contains %q", banned)
		}
	}
}

// --repo-map-tokens 0 builds no map: no phase has a map block, and the PRD
// phase is sent exactly what it was before the map existed.
func TestRun_ZeroMapBudgetLeavesThePromptsWithoutAMap(t *testing.T) {
	ws := newWorkspace(t)
	turns := append([]faux.Turn{toolCall("p1", ToolSubmitPRD, prdArgs(t, "test_feature"))},
		generationTurns(t, "01", "test_feature")...)
	runner, p := fauxRunner(t, ws, turns...)
	o := newOptions(ws, &agentAuthor{runner: runner, ws: ws})
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := p.Requests()
	if want := 1 + len(afspec.GenerationSteps); len(reqs) != want {
		t.Fatalf("%d phases reached the model, want %d", len(reqs), want)
	}
	for i, req := range reqs {
		for _, prompt := range []string{requestUserText(req), blocksText(req.System)} {
			if strings.Contains(prompt, "## Repository map") || strings.Contains(prompt, "{{") {
				t.Errorf("phase %d has a map block or an unfilled placeholder:\n%s", i, prompt)
			}
		}
	}
	wantSystem := prdSystemPrompt() + runContextBlock(project.DetectProfile(ws.Root()).LanguageBlock(), "", "", "")
	if got := blocksText(reqs[0].System); !strings.HasPrefix(got, wantSystem) {
		t.Errorf("the PRD system prompt is not the one without a map:\n%q", got)
	}
	if got, want := requestUserText(reqs[0]), prdUserPrompt(ws.Root(), "", ""); got != want {
		t.Errorf("the PRD task = %q, want %q", got, want)
	}
}

// A map that cannot be built never fails the run: the phases go without it.
func TestRun_AMapThatCannotBeBuiltIsAWarning(t *testing.T) {
	a := newAuthor(t, "01", "test_feature")
	o := newOptions(newWorkspace(t), a)
	o.RepoMapTokens = 6000
	o.buildMap = func(context.Context, *workspace.Workspace, int, []string) (string, error) {
		return "", errors.New("walk exploded")
	}
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("a map failure failed the run: %v", err)
	}
	if a.prdRequests[0].RepoMap != "" {
		t.Errorf("the PRD phase got a map: %q", a.prdRequests[0].RepoMap)
	}
	if w, ok := warning(o.Run, cli.WarnRepoMapBuildFailed); !ok || w.Severity != "low" {
		t.Errorf("warnings = %+v", o.Run.Warnings())
	}
}

// A dry run with --adversarial runs the review, writes nothing, and reports
// the review a real run reports.
func TestRun_ADryRunReviewsAndWritesNothing(t *testing.T) {
	script := func() *scriptedReviewer {
		return &scriptedReviewer{t: t, reviews: []reviewOutcome{{severities: []string{"major"}}, {}},
			revisions: []revisionOutcome{{}}}
	}
	dry := script()
	o := adversarialOptions(t, newAuthor(t, "01", "test_feature"), dry, 2)
	o.DryRun = true
	d, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if reviews, revisions := dry.counts(); reviews != 2 || revisions != 1 {
		t.Errorf("the dry run ran %d reviews, %d revisions", reviews, revisions)
	}
	if exists(filepath.Join(o.Workspace.Root(), ".specs")) {
		t.Error("the dry run wrote a spec root")
	}
	r, err := Run(context.Background(), adversarialOptions(t, newAuthor(t, "01", "test_feature"), script(), 2))
	if err != nil {
		t.Fatal(err)
	}
	if d.Review == nil || !reflect.DeepEqual(d.Review, r.Review) {
		t.Errorf("dry review %+v, real review %+v", d.Review, r.Review)
	}
}

// The rounds run after the PRD phase and --name, and before the number is
// reserved, so an interrupted review leaves no reservation behind.
func TestRun_ReviewsThePRDBeforeReservingItsNumber(t *testing.T) {
	a := newAuthor(t, "01", "custom_name")
	a.prd.SpecName = "model_name"
	var log []string
	rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{severities: []string{"minor"}}}, revisions: []revisionOutcome{{}}}
	o := adversarialOptions(t, a, rv, 1)
	o.Name = "custom_name"
	reserved := func() string {
		return map[bool]string{true: "reserved", false: "free"}[exists(filepath.Join(o.Workspace.Root(), ".specs", "01_custom_name"))]
	}
	rv.onReview = func(req reviewRequest) { log = append(log, "review "+req.PRD.SpecName+" "+reserved()) }
	rv.onRevise = func(revisionRequest) { log = append(log, "revision "+reserved()) }
	a.onGenerate = func(req artifactRequest) {
		if req.Step == afspec.StepRequirements {
			log = append(log, "requirements "+reserved())
		}
	}
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if want := []string{"review custom_name free", "revision free", "requirements reserved"}; !slices.Equal(log, want) {
		t.Errorf("order = %v, want %v", log, want)
	}
}

// The PRD the review leaves is what prd.md, the comment, the result and every
// later phase are made from, and none of the review reaches the package or
// the comment.
func TestRun_TheReviewedPRDFeedsEverythingAfterIt(t *testing.T) {
	ws := newWorkspace(t)
	a := newAuthor(t, "01", "test_feature")
	a.arch = "# Architecture\n"
	a.prd.CodebaseNotes = "ORIGINAL-NOTES"
	revBody := a.prd.Body + "\n\nREVISED-BODY-MARK\n"
	revQuestions := []OpenQuestion{{Question: "REVISED-OQ", Decision: "d", Why: "w"}}
	revFiles := []RelevantFile{{Path: "cache.go", Why: "REVISED-RF"}}
	rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{severities: []string{"minor"}, mark: "FINDING-MARK"}},
		revisions: []revisionOutcome{{edit: func(p PRD) PRD {
			p.Body, p.OpenQuestions, p.RelevantFiles, p.CodebaseNotes = revBody, revQuestions, revFiles, "REVISED-NOTES"
			return p
		}}}}
	forge := &mockForge{authenticated: true, commentURL: "https://forge/c/1"}
	o := newOptions(ws, a)
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 1, rv
	o.Architecture = true
	commentOn(&o, forge)

	res, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Review.Rounds[0].Findings[0].Flaw, "FINDING-MARK") {
		t.Fatalf("review = %+v", res.Review)
	}
	if !reflect.DeepEqual(res.OpenQuestions, revQuestions) || !reflect.DeepEqual(res.RelevantFiles, revFiles) {
		t.Errorf("result open questions %+v, relevant files %+v", res.OpenQuestions, res.RelevantFiles)
	}
	if len(a.artifactReqs) != 3 || len(a.archReqs) != 1 {
		t.Fatalf("%d generation and %d architecture phases", len(a.artifactReqs), len(a.archReqs))
	}
	type sent struct {
		name, prd, notes string
		files            []RelevantFile
	}
	all := []sent{{"architecture", a.archReqs[0].PRD, a.archReqs[0].CodebaseNotes, a.archReqs[0].RelevantFiles}}
	for _, r := range a.artifactReqs {
		all = append(all, sent{string(r.Step), r.PRD, r.CodebaseNotes, r.RelevantFiles})
	}
	for _, s := range all {
		if !strings.Contains(s.prd, "REVISED-BODY-MARK") || s.notes != "REVISED-NOTES" || !reflect.DeepEqual(s.files, revFiles) {
			t.Errorf("%s was sent the PRD before its revision", s.name)
		}
	}
	if !strings.Contains(forge.lastBody, "REVISED-BODY-MARK") || !strings.Contains(forge.lastBody, "REVISED-OQ") ||
		strings.Contains(forge.lastBody, "FINDING-MARK") || strings.Contains(strings.ToLower(forge.lastBody), "review:") {
		t.Errorf("the comment is not the revised PRD alone:\n%s", forge.lastBody)
	}
	dir := filepath.Join(ws.Root(), res.SpecDir)
	files := dirNames(t, dir)
	if !slices.Equal(files, []string{"architecture.md", "prd.md", "requirements.json", "spec.md", "tasks.json", "test_spec.json"}) {
		t.Errorf("package files = %v", files)
	}
	for _, f := range files {
		b, _ := os.ReadFile(filepath.Join(dir, f))
		if bytes.Contains(b, []byte("FINDING-MARK")) || bytes.Contains(b, []byte("R1-F1")) {
			t.Errorf("%s carries review data", f)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "prd.md")); !bytes.Contains(b, []byte("REVISED-BODY-MARK")) {
		t.Error("prd.md does not have the revised body")
	}
}

// A review that does not finish costs the run the rest of the review, never
// the package; an interrupt stops the run with none.
func TestRun_AFailedReviewKeepsThePackageAndAnInterruptStopsTheRun(t *testing.T) {
	t.Run("failed", func(t *testing.T) {
		a := newAuthor(t, "01", "test_feature")
		rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{err: phaseErr(agentrun.PhasePRDReview, agentrun.CategoryBudget)}}}
		o := adversarialOptions(t, a, rv, 1)
		res, err := Run(context.Background(), o)
		if err != nil || res.SpecDir == "" || res.Review == nil || res.Review.StopReason != envelope.ReviewStopIncomplete {
			t.Fatalf("err %v, result %+v", err, res)
		}
		prdMD, _ := os.ReadFile(filepath.Join(o.Workspace.Root(), res.SpecDir, "prd.md"))
		if !strings.Contains(string(prdMD), strings.SplitN(a.prd.Body, "\n", 2)[0]) {
			t.Error("prd.md is not the PRD phase's PRD")
		}
	})
	t.Run("interrupted", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		a := newAuthor(t, "01", "test_feature")
		rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{err: phaseErr(agentrun.PhasePRDReview, agentrun.CategoryAborted)}},
			onReview: func(reviewRequest) { cancel() }}
		o := adversarialOptions(t, a, rv, 1)
		res, err := Run(ctx, o)
		if f := asFailure(t, err); f.Stage != agentrun.PhasePRDReview || f.Category != agentrun.CategoryAborted {
			t.Fatalf("err = %v", err)
		}
		if len(a.steps) != 0 || (res != nil && res.SpecDir != "") || exists(filepath.Join(o.Workspace.Root(), ".specs", "01_test_feature")) {
			t.Errorf("an interrupted run generated %v or reserved a package", a.steps)
		}
	})
}

// --preflight runs every check Run makes before its first model call and
// stops: nothing is written, no phase runs, and the model is never called.
func TestRunPreflight_ReportsTheChecklistWithoutRunningOrWriting(t *testing.T) {
	o := preflightOptions(t)
	p := faux.New()
	o.Runner = preflightRunner(t, p, "")
	a := o.author.(*scriptedAuthor)
	specsDir := resolveSpecsDir(o, o.Workspace.Root())

	res, err := RunPreflight(context.Background(), o)
	if err != nil {
		t.Fatalf("RunPreflight: %v", err)
	}
	if res.Stage != "preflight" || res.Package.SpecDir != "" {
		t.Errorf("stage %q, package %q", res.Stage, res.Package.SpecDir)
	}
	for _, name := range []string{"schemas_valid", "split_plan"} {
		if c, ok := findCheck(res.Preflight, name); !ok || !c.OK {
			t.Errorf("%s = %+v (present %v)", name, c, ok)
		}
	}
	if exists(specsDir) || len(a.prdRequests) != 0 || p.Calls() != 0 {
		t.Errorf("spec root written %v, %d PRD phases, %d model calls", exists(specsDir), len(a.prdRequests), p.Calls())
	}
	if env := o.Run.Envelope(0, res, nil); env.Usage != nil {
		t.Errorf("a phase was recorded: %+v", env.Usage)
	}
	if res.Estimate == nil || res.Estimate.MaxTurnsPerPhase != 10 || res.Estimate.MaxBudgetPerPhaseUSD != 2.0 {
		t.Errorf("estimate bounds = %+v", res.Estimate)
	}
}

// The entries for --name and --comment appear only when given; --dry-run
// drops split_plan and the comment entries.
func TestRunPreflight_ReportsTheConditionalChecksOnlyWhenTheyApply(t *testing.T) {
	for _, tc := range []struct {
		name    string
		set     func(*Options)
		present map[string]string // check -> detail, "*" for any non-empty one
		absent  []string
	}{
		{"by default", func(*Options) {}, map[string]string{"schemas_valid": "", "split_plan": "*"},
			[]string{"name_flag", "comment_target", "comment_credential"}},
		{"--name", func(o *Options) { o.Name = "my_spec" }, map[string]string{"name_flag": "my_spec"}, nil},
		{"--dry-run", func(o *Options) {
			o.DryRun = true
			commentOn(o, nil)
		}, map[string]string{"schemas_valid": ""}, []string{"split_plan", "comment_target", "comment_credential"}},
		{"--comment", func(o *Options) { commentOn(o, &mockForge{authenticated: true}) },
			map[string]string{"comment_target": "*", "comment_credential": ""}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := preflightOptions(t)
			tc.set(&o)
			res, err := RunPreflight(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			for name, detail := range tc.present {
				c, ok := findCheck(res.Preflight, name)
				if !ok || !c.OK || (detail == "*" && c.Detail == "") || (detail != "*" && c.Detail != detail) {
					t.Errorf("%s = %+v (present %v), want detail %q", name, c, ok, detail)
				}
			}
			for _, name := range tc.absent {
				if _, ok := findCheck(res.Preflight, name); ok {
					t.Errorf("%s is present", name)
				}
			}
		})
	}
}

// A refusal is the pipeline's own Preflight failure, verbatim, with no
// result: a partial checklist would be a second answer to "would this start".
func TestRunPreflight_RefusesAsTheRunWould(t *testing.T) {
	o := preflightOptions(t)
	o.Name = "Bad Name"
	_, want := Preflight(context.Background(), o)
	res, err := RunPreflight(context.Background(), o)
	if got := asFailure(t, err); want == nil || got.Stage != want.Stage || got.Category != want.Category || got.Error() != want.Error() {
		t.Errorf("failure = %+v, want %+v", got, want)
	}
	if res != nil {
		t.Errorf("a refusal carries a result: %+v", res)
	}

	// Two plans for one input are ambiguous: a refusal, not a checklist entry.
	o = preflightOptions(t)
	specsDir := resolveSpecsDir(o, o.Workspace.Root())
	for _, name := range []string{"one", "two"} {
		plan := newSplitPlan(o.Input, []SplitScope{{Name: name, Scope: "s"}, {Name: name + "_b", Scope: "t"}})
		if err := plan.save(specsDir); err != nil {
			t.Fatal(err)
		}
	}
	_, err = RunPreflight(context.Background(), o)
	if got := asFailure(t, err); got.Stage != "preflight" || got.Category != "usage" || !strings.Contains(got.Error(), "two unfinished splits") {
		t.Errorf("two matching plans: err = %v", err)
	}
}

// The estimate is one PRD phase and one per generation step for each package
// the plan on disk decides, one more for --architecture, and every allowed
// review and revision round: an upper bound for the review, a lower one for a
// fresh input the model may still split.
func TestRunPreflight_EstimatesThePhases(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*testing.T, *Options)
		want int
	}{
		{"a fresh input", func(*testing.T, *Options) {}, 4},
		{"--architecture", func(_ *testing.T, o *Options) { o.Architecture = true }, 5},
		{"--adversarial with three rounds", func(_ *testing.T, o *Options) { o.Adversarial, o.AdversarialRounds = true, 3 }, 10},
		{"--adversarial and --architecture", func(_ *testing.T, o *Options) {
			o.Adversarial, o.AdversarialRounds, o.Architecture = true, 3, true
		}, 11},
		{"resuming a plan with two scopes to write", func(t *testing.T, o *Options) {
			plan := newSplitPlan(o.Input, []SplitScope{{Name: "first", Scope: "a"}, {Name: "second", Scope: "b"}, {Name: "third", Scope: "c"}})
			plan.Scopes[0].Dir, plan.Scopes[0].SpecID = "01_first", "01"
			if err := plan.save(resolveSpecsDir(*o, o.Workspace.Root())); err != nil {
				t.Fatal(err)
			}
		}, 8},
		{"--adversarial-plan with three rounds", func(_ *testing.T, o *Options) { o.AdversarialPlan, o.AdversarialRounds = true, 3 }, 10},
		{"both reviews with three rounds", func(_ *testing.T, o *Options) {
			o.Adversarial, o.AdversarialPlan, o.AdversarialRounds = true, true, 3
		}, 16},
		{"the plan review over two scopes to write", func(t *testing.T, o *Options) {
			o.AdversarialPlan, o.AdversarialRounds = true, 3
			plan := newSplitPlan(o.Input, []SplitScope{{Name: "first", Scope: "a"}, {Name: "second", Scope: "b"}, {Name: "third", Scope: "c"}})
			plan.Scopes[0].Dir, plan.Scopes[0].SpecID = "01_first", "01"
			if err := plan.save(resolveSpecsDir(*o, o.Workspace.Root())); err != nil {
				t.Fatal(err)
			}
		}, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := preflightOptions(t)
			tc.set(t, &o)
			specsDir := resolveSpecsDir(o, o.Workspace.Root())
			before := dirNames(t, specsDir)
			res, err := RunPreflight(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			if res.Estimate.Phases != tc.want || res.Estimate.MaxTotalUSD != float64(tc.want)*2.0 {
				t.Errorf("estimate = %+v, want %d phases at $2", res.Estimate, tc.want)
			}
			if !slices.Equal(dirNames(t, specsDir), before) {
				t.Errorf("the spec root changed: %v -> %v", before, dirNames(t, specsDir))
			}
		})
	}
}

// phase_models names the model and effort each phase will run on, in the
// order a package is built, so a caller sees what a choice resolved to.
func TestRunPreflight_NamesEachPhasesModel(t *testing.T) {
	const gen = "requirements claude-opus-5-5/high; test_spec claude-sonnet-5-5/high; tasks claude-sonnet-5-5/high"
	for _, tc := range []struct {
		name string
		set  func(*Options)
		want string
	}{
		{"by default", func(*Options) {}, "prd claude-opus-5-5/xhigh; " + gen},
		{"--architecture", func(o *Options) { o.Architecture = true },
			"prd claude-opus-5-5/xhigh; " + gen + "; architecture claude-sonnet-5-5/high"},
		{"--adversarial", func(o *Options) { o.Adversarial, o.AdversarialRounds = true, 1 },
			"prd claude-opus-5-5/xhigh; prd_review claude-opus-5-5/xhigh; prd_revision claude-opus-5-5/xhigh; " + gen},
		{"--adversarial-plan", func(o *Options) { o.AdversarialPlan, o.AdversarialRounds = true, 1 },
			"prd claude-opus-5-5/xhigh; " + gen + "; plan_review claude-opus-5-5/high; plan_revision claude-opus-5-5/high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := preflightOptions(t)
			o.Runner = preflightRunner(t, faux.New(), "ADVANCED")
			tc.set(&o)
			res, err := RunPreflight(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			if c, ok := findCheck(res.Preflight, "phase_models"); !ok || !c.OK || c.Detail != tc.want {
				t.Errorf("phase_models = %+v, want %q", c, tc.want)
			}
		})
	}
}

// symbol_backend says which backend the symbol tools use; a detection that
// fails leaves the check out and the rest in place.
func TestRunPreflight_ReportsTheSymbolBackend(t *testing.T) {
	t.Run("detected", func(t *testing.T) {
		res, err := RunPreflight(context.Background(), preflightOptions(t))
		if err != nil {
			t.Fatal(err)
		}
		c, ok := findCheck(res.Preflight, "symbol_backend")
		if !ok || !c.OK || (c.Detail != agentrun.SymbolBackendTreeSitter && c.Detail != agentrun.SymbolBackendGoOnly) {
			t.Errorf("symbol_backend = %+v (present %v)", c, ok)
		}
	})
	t.Run("detection fails", func(t *testing.T) {
		orig := agentrun.DetectSymbolBackend
		agentrun.DetectSymbolBackend = func(*workspace.Workspace) (string, error) { return "", errors.New("detection failed") }
		t.Cleanup(func() { agentrun.DetectSymbolBackend = orig })
		res, err := RunPreflight(context.Background(), preflightOptions(t))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := findCheck(res.Preflight, "symbol_backend"); ok {
			t.Errorf("symbol_backend present after a failed detection: %+v", res.Preflight)
		}
		if _, ok := findCheck(res.Preflight, "schemas_valid"); !ok {
			t.Errorf("other checks missing: %+v", res.Preflight)
		}
	})
}

// code_search_index says whether the run has an index and, when not, why. The
// fallback is search_files, so it never refuses the run.
func TestRunPreflight_ReportsTheCodeSearchIndex(t *testing.T) {
	for _, tc := range []struct {
		name   string
		index  tools.Index
		reason string
		want   string
	}{
		{"built", &indextest.Index{}, "", "built"},
		{"not built", nil, "", "unavailable: index not built"},
		{"unavailable for a reason", nil, "unsupported platform", "unavailable: unsupported platform"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := preflightOptions(t)
			o.Index, o.IndexUnavailable = tc.index, tc.reason
			res, err := RunPreflight(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			if c, ok := findCheck(res.Preflight, "code_search_index"); !ok || !c.OK || c.Detail != tc.want {
				t.Errorf("code_search_index = %+v (present %v), want %q", c, ok, tc.want)
			}
		})
	}
}

func reviewOf(rounds, findings, fixed int) *Review {
	return reviewWithSteering(rounds, findings, fixed, 0)
}

// reviewWithSteering is a review whose round holds findings findings, the
// first steering of them with the steering rubric.
func reviewWithSteering(rounds, findings, fixed, steering int) *Review {
	round := ReviewRound{Round: 1, Findings: []Finding{}}
	for i := range findings {
		rubric := "prd"
		if i < steering {
			rubric = "steering"
		}
		round.Findings = append(round.Findings, Finding{ID: fmt.Sprintf("R1-F%d", i+1), Rubric: rubric})
	}
	return &Review{RoundsRun: rounds, RoundsAllowed: 5, StopReason: "converged", FindingsCount: findings,
		FixedCount: fixed, ContestedCount: findings - fixed, Rounds: []ReviewRound{round}}
}

func TestResult_Summary(t *testing.T) {
	const dir = ".specs/07_widget_cache"
	two := []OpenQuestion{{Question: "a"}, {Question: "b"}}
	for _, tc := range []struct {
		name string
		r    Result
		want string
	}{
		{"nothing written", Result{}, ""},
		{"one package", Result{SpecResult: envelope.SpecResult{Package: Package{SpecDir: dir, OpenQuestions: two}}},
			"spec: wrote .specs/07_widget_cache; 2 open questions"},
		{"one open question", Result{SpecResult: envelope.SpecResult{Package: Package{SpecDir: dir, OpenQuestions: two[:1]}}},
			"spec: wrote .specs/07_widget_cache; 1 open question"},
		{"follow-on packages", Result{SpecResult: envelope.SpecResult{
			Package:       Package{SpecDir: dir, OpenQuestions: two[:1]},
			FollowOnSpecs: []Package{{SpecDir: ".specs/08_b", OpenQuestions: two}}}},
			"spec: wrote .specs/07_widget_cache (+1 follow-on packages); 3 open questions across 2 packages (per package in split[])"},
		{"a split", Result{SpecResult: envelope.SpecResult{
			Package: Package{OpenQuestions: two[:1]},
			Split:   []ScopeReport{{Status: ScopeDone}, {Status: ScopeDone}, {Status: ScopeFailed}}}},
			"spec: split 2 of 3 scopes; 1 open question"},
		{"a preflight", Result{SpecResult: envelope.SpecResult{Stage: "preflight", Preflight: make([]cli.PreflightCheck, 3)}},
			"spec: preflight passed (3 checks)"},
		{"a review", Result{SpecResult: envelope.SpecResult{Package: Package{SpecDir: dir, OpenQuestions: two, Review: reviewWithSteering(2, 9, 7, 1)}}},
			"spec: wrote .specs/07_widget_cache; 2 open questions; review: 2 rounds, 9 findings (1 steering), 7 fixed"},
		{"a review with no steering finding", Result{SpecResult: envelope.SpecResult{Package: Package{SpecDir: dir, Review: reviewOf(2, 9, 7)}}},
			"spec: wrote .specs/07_widget_cache; review: 2 rounds, 9 findings, 7 fixed"},
		{"one round with one finding", Result{SpecResult: envelope.SpecResult{Package: Package{SpecDir: dir, Review: reviewOf(1, 1, 1)}}},
			"spec: wrote .specs/07_widget_cache; review: 1 round, 1 finding, 1 fixed"},
		{"a review that found nothing", Result{SpecResult: envelope.SpecResult{Package: Package{SpecDir: dir, Review: reviewOf(1, 0, 0)}}},
			"spec: wrote .specs/07_widget_cache; review: 1 round, 0 findings"},
		{"reviews totalled over the packages", Result{SpecResult: envelope.SpecResult{
			Package:       Package{SpecDir: dir, Review: reviewWithSteering(2, 3, 2, 1)},
			FollowOnSpecs: []Package{{SpecDir: ".specs/08_b", Review: reviewWithSteering(1, 2, 1, 2)}, {SpecDir: ".specs/09_c"}}}},
			"spec: wrote .specs/07_widget_cache (+2 follow-on packages); review: 3 rounds, 5 findings (3 steering), 3 fixed"},
		{"a plan review", Result{SpecResult: envelope.SpecResult{Package: Package{SpecDir: dir, PlanReview: reviewOf(2, 5, 4)}}},
			"spec: wrote .specs/07_widget_cache; plan review: 2 rounds, 5 findings, 4 fixed"},
		{"both reviews", Result{SpecResult: envelope.SpecResult{Package: Package{SpecDir: dir, Review: reviewOf(1, 2, 2), PlanReview: reviewOf(2, 5, 4)}}},
			"spec: wrote .specs/07_widget_cache; review: 1 round, 2 findings, 2 fixed; plan review: 2 rounds, 5 findings, 4 fixed"},
		{"plan reviews totalled over the packages", Result{SpecResult: envelope.SpecResult{
			Package:       Package{SpecDir: dir, PlanReview: reviewOf(2, 3, 2)},
			FollowOnSpecs: []Package{{SpecDir: ".specs/08_b", PlanReview: reviewOf(1, 2, 1)}, {SpecDir: ".specs/09_c", PlanReview: reviewOf(1, 0, 0)}}}},
			"spec: wrote .specs/07_widget_cache (+2 follow-on packages); plan review: 4 rounds, 5 findings, 3 fixed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.Summary(); got != tc.want {
				t.Errorf("Summary() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The review clause comes before the envelope's high-warning clause, and a
// long summary is cut to the envelope's cap with that clause kept.
func TestResult_SummaryFitsTheEnvelope(t *testing.T) {
	run := cli.NewRun("spec", "test")
	run.Warn(cli.WarnPRDReviewIncomplete, "high", "the review stopped")

	short := &Result{}
	short.SpecDir, short.Review = ".specs/07_widget_cache", reviewOf(2, 9, 7)
	if got := run.Envelope(cli.ExitOK, short, nil).Summary; !strings.Contains(got, "7 fixed; 1 high-severity warning") {
		t.Errorf("summary = %q", got)
	}
	long := &Result{}
	long.SpecDir, long.Review = ".specs/07_"+strings.Repeat("very_long_name_", 12), reviewOf(2, 9, 7)
	if got := run.Envelope(cli.ExitOK, long, nil).Summary; utf8.RuneCountInString(got) > 200 ||
		!strings.HasSuffix(got, "; 1 high-severity warning") {
		t.Errorf("%d runes: %q", utf8.RuneCountInString(got), got)
	}
}

// A result is resumable while its split plan is on disk, and a preflight
// never is.
func TestResult_IsResumableWhileItsPlanIsOnDisk(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    Result
		want bool
	}{
		{"no plan", Result{}, false},
		{"a plan", Result{SpecResult: envelope.SpecResult{SplitPlan: ".specs/x.split.json"}}, true},
		{"a preflight", Result{SpecResult: envelope.SpecResult{Stage: "preflight", SplitPlan: ".specs/x.split.json"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.Resumable(); got != tc.want {
				t.Errorf("Resumable() = %v, want %v", got, tc.want)
			}
		})
	}
}

// In a split the top level describes the first package only, so split[]
// carries each written package's own state.
func TestResult_SplitReportCarriesEachPackagesState(t *testing.T) {
	r := &Result{}
	r.Package = Package{SpecDir: ".specs/02_a", Validation: ValidationReport{Valid: true},
		OpenQuestions: []OpenQuestion{{Question: "q"}}}
	r.FollowOnSpecs = []Package{{SpecDir: ".specs/03_b", Validation: ValidationReport{ErrorCount: 2},
		OpenQuestions: []OpenQuestion{{Question: "x"}, {Question: "y"}},
		Traceability:  TraceReport{CriteriaUncovered: []string{"C-1"}, PathsUncovered: []string{"P-1"}}}}
	r.setSplit([]ScopeReport{{Name: "a", SpecDir: ".specs/02_a"}, {Name: "b", SpecDir: ".specs/03_b"}, {Name: "c"}})
	a, b, c := r.Split[0], r.Split[1], r.Split[2]
	if a.Valid == nil || !*a.Valid || a.OpenQuestionsCount != 1 || a.TraceGaps != 0 {
		t.Errorf("scope a = %+v", a)
	}
	if b.Valid == nil || *b.Valid || b.ErrorCount != 2 || b.OpenQuestionsCount != 2 || b.TraceGaps != 2 {
		t.Errorf("scope b = %+v", b)
	}
	if c.Valid != nil {
		t.Errorf("an unwritten scope has no state: %+v", c)
	}
}

// steeringAt is a steering file at version, with one directive.
func steeringAt(version string) string {
	return "---\nversion: " + version + "\namendments:\n  - version: " + version +
		"\n    date: 2026-11-01\n    change: The baseline.\n---\n# Steering\n\nUse make check.\n"
}

// A run records the steering's version in every package it writes when the
// steering is versioned, and leaves the field out otherwise.
func TestRun_RecordsTheSteeringVersionInEveryPackage(t *testing.T) {
	for _, tc := range []struct {
		name, steering, want string
	}{
		{"versioned", steeringAt("1.2.0"), "1.2.0"},
		{"unversioned", "# Steering\n\nUse make check.\n", ""},
		{"malformed", strings.Replace(steeringAt("1.2.0"), "version: 1.2.0\namendments", "version: 01.2.0\namendments", 1), ""},
		{"nothing to say", "---\nversion: 1.2.0\namendments:\n  - version: 1.2.0\n    date: 2026-11-01\n    change: x\n---\n# Steering\n", ""},
	} {
		for _, split := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/split %v", tc.name, split), func(t *testing.T) {
				ws := newWorkspace(t)
				writeFiles(t, ws.Root(), map[string]string{".specs/steering.md": tc.steering})
				a := newAuthor(t, "01", "test_feature")
				if split {
					a = splitAuthor(t)
					a.prd.RecommendedSplit = threeScopes()[:2]
				}
				if _, err := Run(context.Background(), fileOptions(ws, a)); err != nil {
					t.Fatalf("Run: %v", err)
				}
				dirs := strings.Split(specDirs(t, ws), ",")
				dirs = slices.DeleteFunc(dirs, func(d string) bool { return d == "steering.md" })
				if want := map[bool]int{false: 1, true: 2}[split]; len(dirs) != want {
					t.Fatalf("packages %v; want %d", dirs, want)
				}
				for _, d := range dirs {
					dir := filepath.Join(ws.Root(), ".specs", d)
					prd, err := os.ReadFile(filepath.Join(dir, "prd.md"))
					if err != nil {
						t.Fatal(err)
					}
					has := strings.Contains(string(prd), "steering_version:")
					if tc.want == "" && has || tc.want != "" && !strings.Contains(string(prd), `steering_version: "`+tc.want+`"`) {
						t.Errorf("%s: prd.md frontmatter does not record %q:\n%s", d, tc.want, prd[:300])
					}
					if loaded, err := afspec.LoadSpec(dir); err != nil || !loaded.Validate().Valid {
						t.Errorf("%s does not validate: %v", d, err)
					}
				}
			})
		}
	}
}

// A malformed record is one high warning at preflight, before any phase, and
// the run goes on with the directives.
func TestRun_WarnsOnceAboutAMalformedSteeringRecord(t *testing.T) {
	ws := newWorkspace(t)
	// Two rules broken: a key that is not allowed, and a date not in YYYY-MM-DD.
	writeFiles(t, ws.Root(), map[string]string{".specs/steering.md": strings.Replace(
		strings.Replace(steeringAt("1.2.0"), "date: 2026-11-01", "date: 1 Nov 2026", 1), "version: 1.2.0\n", "version: 1.2.0\nowner: me\n", 1)})
	a := newAuthor(t, "01", "test_feature")
	o := newOptions(ws, a)
	var atFirstPhase []string
	a.onWritePRD = func() {
		for _, w := range o.Run.Envelope(cli.ExitOK, nil, nil).Warnings {
			atFirstPhase = append(atFirstPhase, string(w.Code))
		}
	}

	got, err := Run(context.Background(), o)
	if err != nil || !got.Validation.Valid {
		t.Fatalf("Run: %v; valid %v — the run must go on as it would under an unversioned steering", err, got != nil && got.Validation.Valid)
	}
	var malformed []cli.Warning
	for _, w := range o.Run.Envelope(cli.ExitOK, got, nil).Warnings {
		if w.Code == cli.WarnSteeringMalformed {
			malformed = append(malformed, w)
		}
	}
	if len(malformed) != 1 || malformed[0].Severity != "high" || malformed[0].Stage != "preflight" {
		t.Fatalf("steering_malformed warnings %+v; want one, high, at preflight", malformed)
	}
	if m := malformed[0].Message; !strings.Contains(m, "only version and amendments") || !strings.Contains(m, "YYYY-MM-DD") {
		t.Errorf("message %q does not name both broken rules", m)
	}
	if !slices.Contains(atFirstPhase, string(cli.WarnSteeringMalformed)) {
		t.Errorf("warnings at the first phase %v; want steering_malformed raised before it", atFirstPhase)
	}
	if !strings.Contains(a.prdRequests[0].Steering, "Use make check.") || a.prdRequests[0].SteeringVersion != "" {
		t.Errorf("the PRD phase got steering %q, version %q; want the directives and no version",
			a.prdRequests[0].Steering, a.prdRequests[0].SteeringVersion)
	}
	for _, r := range a.artifactReqs {
		if !strings.Contains(r.Steering, "Use make check.") {
			t.Errorf("%s was not given the directives", r.Step)
		}
	}
}

// Every escalation of every round reaches the package's open questions, in
// round order and then in the reviser's, once each, whatever a later revision
// did to the open questions.
func TestRun_HandsEveryEscalationToTheCaller(t *testing.T) {
	escalated := func(q string) Disposition {
		return Disposition{Disposition: "escalated", Question: q, Options: []string{"A.", "B."}, Decision: "A for " + q, Reason: "unsure of " + q}
	}
	const existing = "Should the loader follow symlinks?" // the PRD phase's open question
	for _, tc := range []struct {
		name    string
		replace bool
		want    []string
	}{
		{"a later revision that keeps the open questions", false, []string{existing, "Q1?", "Q2?"}},
		{"a later revision that replaces them", true, []string{"REPLACED?", "Q1?", "Q2?", existing}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			second := revisionOutcome{answers: map[int]Disposition{0: escalated("Should  the loader\nfollow symlinks?")}}
			if tc.replace {
				second.edit = func(p PRD) PRD {
					p.Body += "\nrevised in round 2\n"
					p.OpenQuestions = []OpenQuestion{{Question: "REPLACED?", Decision: "d", Why: "w"}}
					return p
				}
				second.answers[0] = escalated(existing)
			}
			rv := &scriptedReviewer{t: t,
				reviews:   []reviewOutcome{{severities: []string{"major", "major"}}, {severities: []string{"major"}}},
				revisions: []revisionOutcome{{answers: map[int]Disposition{0: escalated("Q1?"), 1: escalated("Q2?")}}, second},
			}
			got, err := Run(context.Background(), adversarialOptions(t, newAuthor(t, "01", "test_feature"), rv, 2))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			var questions []string
			for _, q := range got.OpenQuestions {
				questions = append(questions, q.Question)
			}
			if !slices.Equal(questions, tc.want) {
				t.Fatalf("open questions %q; want %q", questions, tc.want)
			}
			if q := got.OpenQuestions[1]; q.Decision != "A for Q1?" || q.Why != "unsure of Q1?" {
				t.Errorf("the escalation reached the caller as %+v; want its decision and its reason", q)
			}
			if want := fmt.Sprintf("%d open questions", len(tc.want)); !strings.Contains(got.Summary(), want) {
				t.Errorf("summary %q does not count %s", got.Summary(), want)
			}
		})
	}
}

// Nothing checks alignment with the steering where the steering says nothing
// or the PRD is not reviewed; where it says something, the review is given
// the directives and told to check against them.
func TestRun_ChecksTheSteeringOnlyWhenItSaysSomethingAndThePRDIsReviewed(t *testing.T) {
	for _, tc := range []struct {
		name, steering string
		adversarial    bool
		block          bool
	}{
		{"a steering that says something", "# Steering\n\nDIRECTIVE: use make check.\n", true, true},
		{"no steering", "", true, false},
		{"a bare heading", "# Steering\n", true, false},
		{"no review", "# Steering\n\nDIRECTIVE: use make check.\n", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{}, {}}}
			o := adversarialOptions(t, newAuthor(t, "01", "test_feature"), rv, 2)
			o.Adversarial = tc.adversarial
			if tc.steering != "" {
				writeFiles(t, o.Workspace.Root(), map[string]string{".specs/steering.md": tc.steering})
			}
			got, err := Run(context.Background(), o)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !tc.adversarial {
				if reviews, _ := rv.counts(); got.Review != nil || reviews != 0 {
					t.Errorf("review %+v after %d reviews; want none without --adversarial", got.Review, reviews)
				}
				return
			}
			for _, round := range got.Review.Rounds {
				for _, f := range round.Findings {
					if f.Rubric == "steering" {
						t.Errorf("%s carries the steering rubric", f.ID)
					}
				}
			}
			req := rv.reviewReqs[0]
			prompt := prdReviewSystemPrompt() + runContextBlock(req.Profile.LanguageBlock(), req.Steering, req.SteeringVersion, req.RepoMap)
			if has := strings.Contains(prompt, "--- BEGIN STEERING ---") && strings.Contains(prompt, "DIRECTIVE: use make check."); has != tc.block {
				t.Errorf("the review prompt carries the steering block %v; want %v", has, tc.block)
			}
			if !strings.Contains(prompt, "rubric") || !strings.Contains(prompt, "every directive") {
				t.Error("the review is not told to check the PRD against every directive")
			}
		})
	}
}

// planOptions is newOptions with the plan review on and rv as its model half.
func planOptions(t *testing.T, a author, rv reviewer, rounds int) Options {
	t.Helper()
	o := newOptions(newWorkspace(t), a)
	o.AdversarialPlan, o.AdversarialRounds, o.reviewer = true, rounds, rv
	return o
}

// phasesRun are the names of the phases a run recorded, in order.
func phasesRun(o Options, got *Result) []string {
	var names []string
	for _, p := range o.Run.Envelope(cli.ExitOK, got, nil).Usage.Phases {
		names = append(names, p.Name)
	}
	return names
}

// writtenTaskTitle is the title of task 1 in the tasks.json a run wrote.
func writtenTaskTitle(t *testing.T, o Options, got *Result) string {
	t.Helper()
	loaded, err := afspec.LoadSpec(filepath.Join(o.Workspace.Root(), got.SpecDir))
	if err != nil {
		t.Fatal(err)
	}
	return loaded.Tasks.Tasks[0].Title
}

// The plan is reviewed after the tasks phase and before the architecture
// phase, which is given the revised plan, and the package holds it.
func TestRun_ReviewsThePlanBetweenTheTasksAndTheArchitecture(t *testing.T) {
	a := newAuthor(t, "01", "test_feature")
	a.arch = "# Architecture: test_feature\n"
	rv := &scriptedReviewer{t: t, planReviews: []reviewOutcome{{severities: []string{"major"}}}, planRevisions: []revisionOutcome{{}}}
	o := planOptions(t, a, rv, 1)
	o.Architecture = true
	got, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"prd", "generate:requirements", "generate:test_spec", "generate:tasks", "plan_review", "plan_revision", "architecture"}
	if names := phasesRun(o, got); !slices.Equal(names, want) {
		t.Errorf("phases = %v; want %v", names, want)
	}
	if title := a.archReqs[0].Partial.Tasks.Tasks[0].Title; title != "revised in round 1" {
		t.Errorf("the architecture was given task 1 titled %q; want the revised plan", title)
	}
	if title := writtenTaskTitle(t, o, got); title != "revised in round 1" {
		t.Errorf("the written plan has task 1 titled %q; want the revised plan", title)
	}
}

// The two review flags are independent, and a run with neither runs the
// phases it always ran.
func TestRun_TheTwoReviewsAreIndependent(t *testing.T) {
	gen := []string{"prd", "generate:requirements", "generate:test_spec", "generate:tasks"}
	for _, tc := range []struct {
		name      string
		prd, plan bool
		want      []string
	}{
		{"neither", false, false, gen},
		{"the PRD review", true, false, []string{"prd", "prd_review", "generate:requirements", "generate:test_spec", "generate:tasks"}},
		{"the plan review", false, true, append(slices.Clone(gen), "plan_review")},
		{"both", true, true, []string{"prd", "prd_review", "generate:requirements", "generate:test_spec", "generate:tasks", "plan_review"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{}}, planReviews: []reviewOutcome{{}}}
			o := newOptions(newWorkspace(t), newAuthor(t, "01", "test_feature"))
			o.Adversarial, o.AdversarialPlan, o.AdversarialRounds, o.reviewer = tc.prd, tc.plan, 1, rv
			if !tc.prd && !tc.plan {
				o.reviewer = failingReviewer{t}
			}
			got, err := Run(context.Background(), o)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if names := phasesRun(o, got); !slices.Equal(names, tc.want) {
				t.Errorf("phases = %v; want %v", names, tc.want)
			}
			if (got.Review != nil) != tc.prd || (got.PlanReview != nil) != tc.plan {
				t.Errorf("review %v, plan review %v; want %v and %v", got.Review != nil, got.PlanReview != nil, tc.prd, tc.plan)
			}
		})
	}
}

// A dry run reviews the plan and writes nothing.
func TestRun_ADryRunReviewsThePlanAndWritesNothing(t *testing.T) {
	rv := &scriptedReviewer{t: t, planReviews: []reviewOutcome{{severities: []string{"major"}}}, planRevisions: []revisionOutcome{{}}}
	o := planOptions(t, newAuthor(t, "01", "test_feature"), rv, 1)
	o.DryRun = true
	got, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if names := phasesRun(o, got); !slices.Contains(names, "plan_review") || !slices.Contains(names, "plan_revision") {
		t.Errorf("phases = %v; want the plan's two", names)
	}
	if got.PlanReview == nil || !got.Validation.Valid {
		t.Errorf("plan review %+v, valid %v", got.PlanReview, got.Validation.Valid)
	}
	if exists(filepath.Join(o.Workspace.Root(), ".specs")) {
		t.Error("a dry run created the spec root")
	}
}

// A plan that does not validate is not reviewed: one high warning, no plan
// phase, and the run fails on the errors it would have failed on anyway.
func TestRun_DoesNotReviewAPlanThatDoesNotValidate(t *testing.T) {
	run := func(plan bool) (*Result, error, Options) {
		a := newAuthor(t, "01", "test_feature")
		a.breakScope = "test_feature"
		o := newOptions(newWorkspace(t), a)
		o.AdversarialPlan, o.reviewer = plan, failingReviewer{t}
		got, err := Run(context.Background(), o)
		return got, err, o
	}
	got, err, o := run(true)
	_, errOff, _ := run(false)
	if err == nil || errOff == nil || err.Error() != errOff.Error() {
		t.Fatalf("errors %v and %v; want the same failure with the flag on and off", err, errOff)
	}
	var skipped []cli.Warning
	for _, w := range o.Run.Envelope(cli.ExitFailed, got, nil).Warnings {
		if w.Code == cli.WarnPlanReviewSkipped {
			skipped = append(skipped, w)
		}
	}
	if len(skipped) != 1 || skipped[0].Severity != "high" || !strings.Contains(skipped[0].Message, "test_feature") ||
		!strings.Contains(skipped[0].Message, "1 error") {
		t.Errorf("warnings %+v; want one high plan_review_skipped naming the scope and the error count", skipped)
	}
	if got.PlanReview != nil || slices.Contains(phasesRun(o, got), "plan_review") {
		t.Errorf("plan review %+v, phases %v; want none", got.PlanReview, phasesRun(o, got))
	}
}

// The package is written from the last complete plan, and the architecture
// is given the same plan.
func TestRun_WritesTheLastCompletePlan(t *testing.T) {
	failed := errors.New("the revision failed")
	for _, tc := range []struct {
		name      string
		reviews   []reviewOutcome
		revisions []revisionOutcome
		want      string
	}{
		{"two finished revisions", []reviewOutcome{{severities: []string{"major"}}, {severities: []string{"major"}}}, []revisionOutcome{{}, {}}, "revised in round 2"},
		{"one finished and one not", []reviewOutcome{{severities: []string{"major"}}, {severities: []string{"major"}}}, []revisionOutcome{{}, {err: failed}}, "revised in round 1"},
		{"none finished", []reviewOutcome{{severities: []string{"major"}}}, []revisionOutcome{{err: failed}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAuthor(t, "01", "test_feature")
			a.arch = "# Architecture: test_feature\n"
			want := tc.want
			if want == "" {
				want = a.artifacts[afspec.StepTasks]["tasks"].([]any)[0].(map[string]any)["title"].(string)
			}
			rv := &scriptedReviewer{t: t, planReviews: tc.reviews, planRevisions: tc.revisions}
			o := planOptions(t, a, rv, 2)
			o.Architecture = true
			got, err := Run(context.Background(), o)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if title := writtenTaskTitle(t, o, got); title != want {
				t.Errorf("the written plan has task 1 titled %q; want %q", title, want)
			}
			if title := a.archReqs[0].Partial.Tasks.Tasks[0].Title; title != want {
				t.Errorf("the architecture was given task 1 titled %q; want %q", title, want)
			}
		})
	}
}

// A plan phase that fails changes neither whether the run fails nor its exit
// code; an interrupt still aborts the run.
func TestRun_AFailedPlanPhaseDoesNotFailTheRun(t *testing.T) {
	failed := errors.New("the plan review failed")
	t.Run("everything else succeeds", func(t *testing.T) {
		rv := &scriptedReviewer{t: t, planReviews: []reviewOutcome{{err: failed}}}
		o := planOptions(t, newAuthor(t, "01", "test_feature"), rv, 1)
		got, err := Run(context.Background(), o)
		if err != nil || !got.Validation.Valid {
			t.Fatalf("Run: %v", err)
		}
		if got.PlanReview == nil || got.PlanReview.StopReason != envelope.ReviewStopIncomplete {
			t.Errorf("plan review %+v; want it incomplete", got.PlanReview)
		}
	})
	t.Run("the package does not validate", func(t *testing.T) {
		run := func(plan bool) error {
			a := newAuthor(t, "01", "test_feature")
			a.shapes = map[string]packageShape{"test_feature": {dependsOn: "42"}} // a dependency the root lacks
			rv := &scriptedReviewer{t: t, planReviews: []reviewOutcome{{err: failed}}}
			o := newOptions(newWorkspace(t), a)
			o.AdversarialPlan, o.reviewer = plan, rv
			_, err := Run(context.Background(), o)
			return err
		}
		on, off := run(true), run(false)
		if on == nil || off == nil || agentrun.CategoryOf(on) != agentrun.CategoryOf(off) || on.Error() != off.Error() {
			t.Errorf("errors %v and %v; want the same failure", on, off)
		}
	})
	t.Run("an interrupt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		rv := &scriptedReviewer{t: t, planReviews: []reviewOutcome{{err: phaseErr(agentrun.PhasePlanReview, agentrun.CategoryAborted)}},
			onPlanReview: func(planReviewRequest) { cancel() }}
		_, err := Run(ctx, planOptions(t, newAuthor(t, "01", "test_feature"), rv, 1))
		if f := asFailure(t, err); f.Stage != agentrun.PhasePlanReview || f.Category != agentrun.CategoryAborted {
			t.Errorf("err = %v; want aborted at plan_review", err)
		}
	})
}

// The plan reviewer is given the steering and told to check the plan against
// it, and a steering finding is recorded beside the others; with a steering
// that says nothing there is nothing to check.
func TestRun_ChecksThePlanAgainstTheSteeringOnlyWhenItSaysSomething(t *testing.T) {
	for _, tc := range []struct {
		name, steering string
		block          bool
	}{
		{"a steering that says something", "# Steering\n\nDIRECTIVE: use make check.\n", true},
		{"no steering", "", false},
		{"a bare heading", "# Steering\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outcome := reviewOutcome{severities: []string{"major", "major"}, rubrics: []string{"zero_decision", "steering"}}
			if !tc.block {
				outcome = reviewOutcome{severities: []string{"major"}}
			}
			rv := &scriptedReviewer{t: t, planReviews: []reviewOutcome{outcome}, planRevisions: []revisionOutcome{{}}}
			o := planOptions(t, newAuthor(t, "01", "test_feature"), rv, 1)
			if tc.steering != "" {
				writeFiles(t, o.Workspace.Root(), map[string]string{".specs/steering.md": tc.steering})
			}
			got, err := Run(context.Background(), o)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			req := rv.planReviewReqs[0]
			prompt := planReviewSystemPrompt() + runContextBlock(req.Profile.LanguageBlock(), req.Steering, req.SteeringVersion, req.RepoMap)
			if has := strings.Contains(prompt, "DIRECTIVE: use make check."); has != tc.block {
				t.Errorf("the plan review prompt carries the steering %v; want %v", has, tc.block)
			}
			if !strings.Contains(prompt, "check the plan against every directive") {
				t.Error("the plan reviewer is not told to check the plan against the directives")
			}
			var rubrics []string
			for _, f := range got.PlanReview.Rounds[0].Findings {
				rubrics = append(rubrics, f.Rubric)
			}
			if has := slices.Contains(rubrics, "steering"); has != tc.block {
				t.Errorf("rubrics %v; a steering finding recorded %v, want %v", rubrics, has, tc.block)
			}
		})
	}
}

// Every package of a run with the plan review carries its record, with the
// fields of the PRD review's.
func TestRun_EveryPackageCarriesItsPlanReview(t *testing.T) {
	rv := &scriptedReviewer{t: t, planReviews: []reviewOutcome{{severities: []string{"major"}}, {}, {}}, planRevisions: []revisionOutcome{{}}}
	o := fileOptions(newWorkspace(t), splitAuthor(t))
	o.AdversarialPlan, o.reviewer = true, rv
	got, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, p := range append([]Package{got.Package}, got.FollowOnSpecs...) {
		r := p.PlanReview
		if r == nil || r.RoundsAllowed != 1 || r.StopReason != envelope.ReviewStopRoundsExhausted || len(r.Rounds) != 1 {
			t.Errorf("%s: plan review %+v", p.SpecDir, r)
			continue
		}
		if sum := r.FixedCount + r.ContestedCount + r.EscalatedCount + r.DeclinedCount + r.UnansweredCount; sum != r.FindingsCount {
			t.Errorf("%s: the counts sum to %d of %d findings", p.SpecDir, sum, r.FindingsCount)
		}
	}
	if !strings.Contains(got.Summary(), "plan review: 3 rounds, 1 finding, 1 fixed") {
		t.Errorf("summary %q", got.Summary())
	}
}
