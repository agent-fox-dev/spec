package envelope

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// keysOf marshals v and returns its top-level JSON members.
func keysOf(t *testing.T, v any) map[string]json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func sortedKeys(m map[string]json.RawMessage) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func ptr[T any](v T) *T { return &v }

// adminForms are one AdminResult for each form of speca.
func adminForms() (report, verdict, list AdminResult) {
	identity := AdminResult{
		SpecDir: ".specs/09_agent_mode", SpecID: "09", SpecName: "agent_mode", Title: "Agent mode",
		Status: "active", CreatedAt: "2026-10-22T07:23:01Z", UpdatedAt: "2026-10-22T07:23:01Z",
	}
	report = identity
	report.Valid = ptr(false)
	report.FailingRules = []string{"C3"}
	report.Tasks = []TaskInfo{{ID: 1, Title: "t", Kind: "implement", State: "pending", Ready: true}}
	report.TaskCounts = &TaskCounts{Pending: 1, Total: 1}
	report.NextTask = &report.Tasks[0]
	report.AllowedStatus = []string{"sealed"}
	report.DependsOn = []string{"08"}
	report.Dependents = []string{}

	verdict = identity
	verdict.Valid = ptr(true)
	verdict.Validation = &ValidationReport{Valid: true}
	verdict.Traceability = &TraceReport{CriteriaCovered: 3}

	list = AdminResult{Packages: []PackageInfo{{SpecDir: ".specs/01_a", SpecID: "01", Valid: ptr(true), TaskCounts: &TaskCounts{}}}}
	return report, verdict, list
}

// TS-10-51: every field is described, valid is a pointer, and a form's JSON
// holds the fields that form produces and no others.
func TestAdminResultDocumentsEveryFieldAndOmitsWhatAFormDoesNotProduce_TS10_51(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeFor[AdminResult](), reflect.TypeFor[TaskCounts](), reflect.TypeFor[PackageInfo](), reflect.TypeFor[TaskInfo](), reflect.TypeFor[Change](),
	} {
		for f := range typ.Fields() {
			if f.Tag.Get("description") == "" {
				t.Errorf("%s.%s has no description tag", typ.Name(), f.Name)
			}
			if name, _, _ := strings.Cut(f.Tag.Get("json"), ","); name == "" || name == "-" {
				t.Errorf("%s.%s has no json name", typ.Name(), f.Name)
			}
		}
	}

	field, _ := reflect.TypeFor[AdminResult]().FieldByName("Valid")
	if field.Type != reflect.TypeFor[*bool]() {
		t.Errorf("AdminResult.Valid is %s; want *bool", field.Type)
	}
	if got := keysOf(t, AdminResult{}); len(got) != 0 {
		t.Errorf("the zero AdminResult writes %v; want nothing", sortedKeys(got))
	}

	report, verdict, list := adminForms()

	got := keysOf(t, report)
	for _, k := range []string{"tasks", "allowed_status", "depends_on", "dependents", "task_counts", "next_task", "failing_rules", "valid"} {
		if _, ok := got[k]; !ok {
			t.Errorf("report lacks %s", k)
		}
	}
	for _, k := range []string{"validation", "traceability", "packages"} {
		if _, ok := got[k]; ok {
			t.Errorf("report has %s", k)
		}
	}
	if string(got["valid"]) != "false" {
		t.Errorf("a package that does not validate writes valid = %s; want false, present", got["valid"])
	}
	empty := report
	empty.Tasks, empty.AllowedStatus, empty.DependsOn, empty.TaskCounts = []TaskInfo{}, []string{}, []string{}, &TaskCounts{}
	empty.NextTask, empty.FailingRules = nil, nil
	got = keysOf(t, empty)
	for _, k := range []string{"tasks", "allowed_status", "depends_on", "dependents"} {
		if string(got[k]) != "[]" {
			t.Errorf("an empty %s writes %s; want []", k, got[k])
		}
	}
	nilSlices := report
	nilSlices.Tasks, nilSlices.AllowedStatus, nilSlices.DependsOn, nilSlices.Dependents = nil, nil, nil, nil
	for _, k := range []string{"tasks", "allowed_status", "depends_on", "dependents"} {
		if _, ok := keysOf(t, nilSlices)[k]; ok {
			t.Errorf("a nil %s is written", k)
		}
	}

	got = keysOf(t, verdict)
	for _, k := range []string{"tasks", "task_counts", "next_task", "allowed_status", "depends_on", "dependents", "failing_rules", "packages"} {
		if _, ok := got[k]; ok {
			t.Errorf("the verdict has %s", k)
		}
	}
	for _, k := range []string{"spec_dir", "valid", "validation", "traceability"} {
		if _, ok := got[k]; !ok {
			t.Errorf("the verdict lacks %s", k)
		}
	}

	if got := keysOf(t, AdminResult{Packages: []PackageInfo{}}); len(got) != 1 || string(got["packages"]) != "[]" {
		t.Errorf("an empty list writes %v; want only packages: []", got)
	}
	if got := keysOf(t, list); sortedKeys(got)[0] != "packages" || len(got) != 1 {
		t.Errorf("the list writes %v; want only packages", sortedKeys(got))
	}
}

// TS-10-52: DecodeSpeca reads the envelope of every form, and refuses what is
// not one.
func TestDecodeSpecaReadsEveryForm_TS10_52(t *testing.T) {
	report, verdict, list := adminForms()
	for name, res := range map[string]AdminResult{"report": report, "verdict": verdict, "list": list} {
		in := AdminEnvelope{Tool: "speca", Version: "v", OK: true, Status: "done", Summary: "s", Result: &res}
		b, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		out, err := DecodeSpeca(b)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out.Tool != "speca" || !out.OK || !reflect.DeepEqual(out.Result, in.Result) {
			t.Errorf("%s: decoded %+v; want %+v", name, out.Result, in.Result)
		}
	}

	failed := AdminEnvelope{Tool: "speca", ExitCode: ExitUsage, Status: "usage",
		Error: &ErrorInfo{Stage: "usage", Category: CategoryUsage, Message: "m"}}
	b, err := json.Marshal(failed)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeSpeca(b)
	if err != nil || out.Result != nil || out.Error == nil || out.Error.Stage != "usage" || out.ExitCode != ExitUsage {
		t.Errorf("an error envelope decoded to %+v, %v", out, err)
	}

	for _, bad := range []string{"not json", `{"tool":"speca"} {"tool":"speca"}`, `{"result":{"valid":"yes"}}`} {
		if _, err := DecodeSpeca([]byte(bad)); err == nil {
			t.Errorf("DecodeSpeca(%q) = nil error", bad)
		}
	}
}

// TS-10-53: ready, TaskCounts and PackageInfo have the documented JSON.
func TestTaskCountsPackageInfoAndReadyHaveTheDocumentedShape_TS10_53(t *testing.T) {
	if got := keysOf(t, TaskInfo{ID: 1, Title: "t", Kind: "implement", State: "pending", Ready: true}); string(got["ready"]) != "true" {
		t.Errorf("a ready task writes ready = %s; want true", got["ready"])
	}
	if got := keysOf(t, TaskInfo{ID: 1, Title: "t", Kind: "implement", State: "pending"}); got["ready"] != nil {
		t.Errorf("a task that is not ready writes ready = %s; want it absent", got["ready"])
	}

	want := []string{"dropped", "done", "in_progress", "pending", "total"}
	slices.Sort(want)
	if got := sortedKeys(keysOf(t, TaskCounts{})); !slices.Equal(got, want) {
		t.Errorf("TaskCounts writes %v; want %v", got, want)
	}

	failed := PackageInfo{SpecDir: ".specs/03_x", LoadError: "cannot read prd.md"}
	if got := sortedKeys(keysOf(t, failed)); !slices.Equal(got, []string{"load_error", "spec_dir"}) {
		t.Errorf("a package that did not load writes %v; want spec_dir and load_error", got)
	}
	if got := keysOf(t, PackageInfo{SpecDir: "x", Archived: true}); string(got["archived"]) != "true" {
		t.Errorf("an archived package writes archived = %s", got["archived"])
	}
	if got := keysOf(t, PackageInfo{SpecDir: "x"}); got["archived"] != nil {
		t.Error("a live package writes archived")
	}

	loaded := PackageInfo{SpecDir: ".specs/01_a", SpecID: "01", SpecName: "a", Title: "A", Status: "draft",
		Valid: ptr(false), TaskCounts: &TaskCounts{Done: 1, Total: 2}, NextTask: &TaskInfo{ID: 2, Title: "t", Kind: "implement", State: "pending", Ready: true}}
	b, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	var back PackageInfo
	if err := json.Unmarshal(b, &back); err != nil || !reflect.DeepEqual(back, loaded) {
		t.Errorf("round trip = %+v, %v; want %+v", back, err, loaded)
	}
	if got := keysOf(t, loaded); string(got["valid"]) != "false" {
		t.Errorf("an invalid package writes valid = %s; want false, present", got["valid"])
	}
}

// exportedBeforeSpeca are the exported names of the package before speca's
// types joined it. None may go.
var exportedBeforeSpeca = strings.Fields(`Artifact ArtifactComment ArtifactKind ArtifactMarkdownFile ArtifactSpecPackage
CategoryAPI CategoryAborted CategoryAuth CategoryBudget CategoryDisk CategoryInput CategoryInternal CategoryInvalidSpec
CategoryMaxTurns CategoryModel CategoryNoResult CategoryRefusal CategoryUsage Decode DecodeSpec DecodeSpecr
DispositionAccepted DispositionRejected DispositionUnanswered Envelope ErrorInfo Estimate ExitFailed ExitOK ExitUsage
FixHint InputInfo ModelInfo Next OpenQuestion Package PhaseInfo PhaseModelInfo PreflightCheck ReducedArchitecture
ReducedTestDetail RelevantFile RenderEnvelope RenderResult RetryableFor Review ReviewFinding ReviewRound
ReviewStopIncomplete ReviewStopNoFindings ReviewStopNoSeriousFindings ReviewStopRoundsExhausted SameInputPlaceholder
ScopeDone ScopeFailed ScopeInvalid ScopePending ScopeReport ScopeSpec ScopeTask SectionArchitecture SectionPRD
SectionRequirements SectionTasks SectionTestSpec Sections SeverityCritical SeverityHigh SeverityLow SeverityMajor
SeverityMinor SideEffect SpecEnvelope SpecResult StagePreflight StatusFor TaskInfo TraceReport UsageInfo ValidationItem
ValidationReport WarnActivationFailed WarnArchitectureNotWritten WarnCode WarnCodeSearchUnavailable WarnCommentNotPosted
WarnCommentsTruncated WarnCommentsUnreadable WarnEffortClamped WarnInputLooksLikePath WarnInputTruncated
WarnNameFlagIgnored WarnOutputNotWritten WarnOverBudget WarnPRDReviewIncomplete WarnProjectLanguageUnknown
WarnRelevantFilesUnavailable WarnRepoMapBuildFailed WarnScopeCountMismatch WarnScopeRenamed WarnSectionAbsent
WarnSpecInvalid WarnSpecsDirUnreadable WarnSplitPlanForeign WarnSplitPlanNotRemoved WarnSplitPlanStale
WarnSplitPlanUnreadable WarnSplitPlanUpdateFailed WarnToolErrors Warning`)

// exportedNames are the exported package-level names a file declares.
func exportedNames(f *ast.File) []string {
	var out []string
	add := func(id *ast.Ident) {
		if id.IsExported() {
			out = append(out, id.Name)
		}
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				add(d.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					add(spec.Name)
				case *ast.ValueSpec:
					for _, id := range spec.Names {
						add(id)
					}
				}
			}
		}
	}
	return out
}

// TS-10-54: the package still imports only the standard library, and keeps
// every name it exported.
func TestEnvelopeImportsOnlyTheStandardLibraryAndKeepsItsNames_TS10_54(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	exported := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if first, _, _ := strings.Cut(path, "/"); strings.Contains(first, ".") {
				t.Errorf("%s imports %s, which is not in the standard library", file, path)
			}
		}
		for _, name := range exportedNames(f) {
			exported[name] = true
		}
	}
	for _, name := range exportedBeforeSpeca {
		if !exported[name] {
			t.Errorf("%s is no longer exported", name)
		}
	}
}

// TS-11-51: changes and dry_run round-trip through DecodeSpeca and are left out
// when empty; a change leaves out what it does not set.
func TestChangesAndDryRunRoundTrip_TS11_51(t *testing.T) {
	changes := []Change{
		{Kind: "task", ID: 2, From: "pending", To: "in_progress"},
		{Kind: "task", ID: 3, From: "dropped", To: "pending", Forced: true},
	}
	with := AdminResult{SpecDir: ".specs/09_x", Changes: changes, DryRun: true}
	b, err := json.Marshal(AdminEnvelope{Tool: "speca", Result: &with})
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeSpeca(b)
	if err != nil || got.Result == nil || !reflect.DeepEqual(got.Result.Changes, changes) || !got.Result.DryRun {
		t.Fatalf("decoded %+v, %v; want changes %+v and dry_run", got.Result, err, changes)
	}

	without := keysOf(t, AdminResult{SpecDir: ".specs/09_x", Changes: []Change{}})
	for _, k := range []string{"changes", "dry_run"} {
		if _, ok := without[k]; ok {
			t.Errorf("a result with no changes and no dry run writes %s", k)
		}
	}

	if got := sortedKeys(keysOf(t, Change{Kind: "task", ID: 4})); !slices.Equal(got, []string{"id", "kind"}) {
		t.Errorf("a change with only a kind and an id writes %v", got)
	}
	if got := sortedKeys(keysOf(t, changes[1])); !slices.Equal(got, []string{"forced", "from", "id", "kind", "to"}) {
		t.Errorf("a forced change writes %v", got)
	}
	if got := keysOf(t, changes[0]); got["forced"] != nil {
		t.Errorf("a change that was not forced writes forced = %s", got["forced"])
	}
}

// TS-12-55: a change writes spec and added only for the supersedes kind, and
// both are tagged omitempty and described.
func TestChangeWritesSpecAndAddedOnlyForSupersedes_TS12_55(t *testing.T) {
	for _, c := range []Change{
		{Kind: "status", From: "active", To: "sealed"},
		{Kind: "moved", From: ".specs/03_x", To: ".specs/archive/03_x"},
	} {
		got := sortedKeys(keysOf(t, c))
		if !slices.Equal(got, []string{"from", "kind", "to"}) {
			t.Errorf("a %s change writes %v; want from, kind and to only", c.Kind, got)
		}
	}
	supersedes := Change{Kind: "supersedes", Spec: "12", Added: "03"}
	if got := sortedKeys(keysOf(t, supersedes)); !slices.Equal(got, []string{"added", "kind", "spec"}) {
		t.Errorf("a supersedes change writes %v; want added, kind and spec", got)
	}
	if got := keysOf(t, supersedes); string(got["spec"]) != `"12"` || string(got["added"]) != `"03"` {
		t.Errorf("a supersedes change writes spec %s and added %s; want \"12\" and \"03\"", got["spec"], got["added"])
	}

	typ := reflect.TypeOf(Change{})
	for field, key := range map[string]string{"Spec": "spec", "Added": "added"} {
		f, ok := typ.FieldByName(field)
		if !ok {
			t.Fatalf("Change has no field %s", field)
		}
		if tag := f.Tag.Get("json"); tag != key+",omitempty" {
			t.Errorf("%s has json tag %q; want %q", field, tag, key+",omitempty")
		}
		if f.Tag.Get("description") == "" {
			t.Errorf("%s has no description tag", field)
		}
	}
	// The kinds are named where a reader of the type looks.
	for _, kind := range []string{"status", "moved", "supersedes"} {
		if !changeDocMentions(t, kind) {
			t.Errorf("the documentation of Change does not name the kind %s", kind)
		}
	}
}

// changeDocMentions reports whether the comment on the Change type names word.
func changeDocMentions(t *testing.T, word string) bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "admin_result.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Doc != nil {
			for _, spec := range g.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.Name == "Change" {
					return strings.Contains(g.Doc.Text(), word)
				}
			}
		}
	}
	return false
}

// TS-12-57: every import of the package's code is in the standard library.
func TestEnvelopeImportsOnlyTheStandardLibrary_TS12_57(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			checked++
			if first, _, _ := strings.Cut(path, "/"); strings.Contains(first, ".") {
				t.Errorf("%s imports %s, which is not in the standard library", file, path)
			}
		}
	}
	if checked == 0 {
		t.Error("no import was checked")
	}
}
