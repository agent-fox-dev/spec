package main

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

func ptr[T any](v T) *T { return &v }

// report16 is the result of the report form for a package in .specs. A draft and
// an active package can take a write, as the report form says of a package whose
// intent has not drifted.
func report16(status string, done, total int, next *envelope.TaskInfo, valid bool) *Result {
	return &Result{AdminResult: envelope.AdminResult{
		SpecDir: ".specs/16_indexed_code_search", SpecID: "16", SpecName: "indexed_code_search", Status: status,
		Valid: ptr(valid), TaskCounts: &envelope.TaskCounts{Done: done, Total: total}, NextTask: next,
	}, writable: status == "draft" || status == "active"}
}

// TS-10-45: the report's sentence names the directory, the status, the tasks
// done, the next task and whether the package validates.
func TestReportSummary_TS10_45(t *testing.T) {
	next := &envelope.TaskInfo{ID: 4, Title: "t", State: "pending", Ready: true}
	if got, want := report16("active", 3, 7, next, true).Summary(), "speca: 16_indexed_code_search active, 3 of 7 tasks done; next task 4"; got != want {
		t.Errorf("Summary = %q; want %q", got, want)
	}

	invalid := report16("active", 3, 7, next, false)
	invalid.FailingRules = []string{"C3", "C7"}
	if got := invalid.Summary(); !strings.Contains(got, "does not validate") || !strings.HasPrefix(got, "speca: 16_indexed_code_search active, 3 of 7 tasks done; next task 4") {
		t.Errorf("an invalid package: Summary = %q", got)
	}
	if got := report16("active", 3, 7, next, true).Summary(); strings.Contains(got, "validate") {
		t.Errorf("a valid package's Summary mentions validation: %q", got)
	}

	done := report16("sealed", 7, 7, nil, true).Summary()
	if want := "speca: 16_indexed_code_search sealed, 7 of 7 tasks done"; done != want {
		t.Errorf("no next task: Summary = %q; want %q", done, want)
	}
	empty := report16("draft", 0, 0, nil, true)
	empty.Tasks = []envelope.TaskInfo{}
	if got, want := empty.Summary(), "speca: 16_indexed_code_search draft, 0 of 0 tasks done"; got != want {
		t.Errorf("no tasks: Summary = %q; want %q", got, want)
	}

	long := report16("active", 3, 7, next, false)
	long.SpecDir = ".specs/16_" + strings.Repeat("long_", 60)
	if got := envelopeSummary(t, long); utf8.RuneCountInString(got) > 200 || !strings.HasPrefix(got, "speca: 16_long_") {
		t.Errorf("the envelope's summary is %d characters: %q", utf8.RuneCountInString(got), got)
	}
}

// envelopeSummary is the summary of the envelope the run builds around r.
func envelopeSummary(t *testing.T, r *Result) string {
	t.Helper()
	return cli.NewRun(tool, "v").Envelope(cli.ExitOK, r, nil).Summary
}

// TS-10-46: the verdict's sentence says whether the package validates, and if
// not how many errors and which rules.
func TestVerdictSummary_TS10_46(t *testing.T) {
	valid := &Result{AdminResult: envelope.AdminResult{SpecDir: ".specs/16_indexed_code_search", Valid: ptr(true), Validation: &envelope.ValidationReport{Valid: true}}}
	if got, want := valid.Summary(), "speca: 16_indexed_code_search validates"; got != want {
		t.Errorf("Summary = %q; want %q", got, want)
	}

	errs := []envelope.ValidationItem{{Check: "C3"}, {Check: "C7"}, {Check: "cross_spec_actor"}}
	invalid := &Result{AdminResult: envelope.AdminResult{SpecDir: ".specs/16_indexed_code_search", Valid: ptr(false),
		Validation: &envelope.ValidationReport{ErrorCount: 3, Errors: errs}}}
	want := "speca: 16_indexed_code_search does not validate (3 errors: C3, C7, cross_spec_actor)"
	if got := invalid.Summary(); got != want {
		t.Errorf("Summary = %q; want %q", got, want)
	}
	if got := envelopeSummary(t, invalid); got != want || utf8.RuneCountInString(got) > 200 {
		t.Errorf("the envelope's summary = %q", got)
	}

	// A rule is named once however many errors break it, and one error is not "1 errors".
	twice := &Result{AdminResult: envelope.AdminResult{SpecDir: ".specs/16_x", Valid: ptr(false),
		Validation: &envelope.ValidationReport{ErrorCount: 2, Errors: []envelope.ValidationItem{{Check: "C4"}, {Check: "C4"}}}}}
	if got, want := twice.Summary(), "speca: 16_x does not validate (2 errors: C4)"; got != want {
		t.Errorf("Summary = %q; want %q", got, want)
	}
	once := &Result{AdminResult: envelope.AdminResult{SpecDir: ".specs/16_x", Valid: ptr(false),
		Validation: &envelope.ValidationReport{ErrorCount: 1, Errors: []envelope.ValidationItem{{Check: "C4"}}}}}
	if got, want := once.Summary(), "speca: 16_x does not validate (1 error: C4)"; got != want {
		t.Errorf("Summary = %q; want %q", got, want)
	}
}

func packages(statuses ...string) []envelope.PackageInfo {
	var out []envelope.PackageInfo
	for i, s := range statuses {
		p := envelope.PackageInfo{SpecDir: fmt.Sprintf(".specs/%02d_p", i+1)}
		if s == "" {
			p.LoadError = "cannot read prd.md"
		} else {
			p.Status = s
		}
		out = append(out, p)
	}
	return out
}

// TS-10-47: the list's sentence counts the packages, then each status that
// has one, in lifecycle order, then the packages that did not load.
func TestListSummary_TS10_47(t *testing.T) {
	nine := &Result{AdminResult: envelope.AdminResult{Packages: packages("sealed", "active", "draft", "active", "active", "sealed", "draft", "active", "active")}}
	if got, want := nine.Summary(), "speca: 9 packages (2 draft, 5 active, 2 sealed)"; got != want {
		t.Errorf("Summary = %q; want %q", got, want)
	}

	broken := &Result{AdminResult: envelope.AdminResult{Packages: packages("draft", "", "")}}
	if got, want := broken.Summary(), "speca: 3 packages (1 draft, 2 unreadable)"; got != want {
		t.Errorf("Summary = %q; want %q", got, want)
	}

	all := &Result{AdminResult: envelope.AdminResult{Packages: packages("archived", "superseded", "sealed", "active", "draft")}}
	if got, want := all.Summary(), "speca: 5 packages (1 draft, 1 active, 1 sealed, 1 superseded, 1 archived)"; got != want {
		t.Errorf("Summary = %q; want %q", got, want)
	}

	if got, want := (&Result{AdminResult: envelope.AdminResult{Packages: []envelope.PackageInfo{}}}).Summary(), "speca: 0 packages"; got != want {
		t.Errorf("an empty list: Summary = %q; want %q", got, want)
	}
	if got, want := (&Result{AdminResult: envelope.AdminResult{Packages: packages("active")}}).Summary(), "speca: 1 package (1 active)"; got != want {
		t.Errorf("one package: Summary = %q; want %q", got, want)
	}
}

// TS-10-48: the report suggests rendering the next task with specr, and says
// whether it is under way or the next ready one. Where the task is ready and the
// package can take a write, the claim of the task (speca's task form, spec 11)
// comes before the render, which is still there.
func TestReportNext_TS10_48(t *testing.T) {
	for name, tc := range map[string]struct {
		next  *envelope.TaskInfo
		why   string
		claim bool
	}{
		"in progress": {&envelope.TaskInfo{ID: 4, Title: "Fix it", State: "in_progress"}, "in progress", false},
		"ready":       {&envelope.TaskInfo{ID: 5, Title: "Add it", State: "pending", Ready: true}, "next ready task", true},
	} {
		got := report16("active", 3, 7, tc.next, true).Next()
		if tc.claim {
			if len(got) != 2 || got[0].Tool != "speca" || !slices.Equal(got[0].Flags, []string{"--task", "5", "--state", "in_progress"}) {
				t.Errorf("%s: Next = %+v; want the claim of task 5 first", name, got)
				continue
			}
			got = got[1:]
		}
		if len(got) != 1 || got[0].Tool != "specr" || got[0].Input != ".specs/16_indexed_code_search" ||
			!slices.Equal(got[0].Flags, []string{"--task", fmt.Sprint(tc.next.ID)}) {
			t.Errorf("%s: Next = %+v", name, got)
			continue
		}
		if !strings.Contains(got[0].Why, tc.why) || !strings.Contains(got[0].Why, tc.next.Title) {
			t.Errorf("%s: why = %q; want it to say %q and name the task", name, got[0].Why, tc.why)
		}
	}
}

// TS-10-49: the list suggests the first active package with a ready task and
// none in progress.
func TestListNext_TS10_49(t *testing.T) {
	task := func(id int, state string) *envelope.TaskInfo {
		return &envelope.TaskInfo{ID: id, Title: "t", State: state, Ready: state == "pending"}
	}
	pkg := func(n int, status string, inProgress int, next *envelope.TaskInfo) envelope.PackageInfo {
		return envelope.PackageInfo{SpecDir: fmt.Sprintf(".specs/%02d_p", n), Status: status,
			TaskCounts: &envelope.TaskCounts{InProgress: inProgress}, NextTask: next}
	}
	packages := []envelope.PackageInfo{
		pkg(1, "draft", 0, task(1, "pending")),
		pkg(2, "active", 1, task(2, "in_progress")),
		pkg(3, "active", 0, task(3, "pending")),
		pkg(4, "active", 0, task(1, "pending")),
	}
	writable := map[string]bool{".specs/01_p": true, ".specs/02_p": true, ".specs/03_p": true, ".specs/04_p": true}
	r := &Result{AdminResult: envelope.AdminResult{Packages: packages}, writableDirs: writable}
	got := r.Next()
	if len(got) != 2 || got[0].Tool != "speca" || got[0].Input != ".specs/03_p" || !slices.Equal(got[0].Flags, []string{"--task", "3", "--state", "in_progress"}) ||
		got[1].Tool != "specr" || got[1].Input != ".specs/03_p" || !slices.Equal(got[1].Flags, []string{"--task", "3"}) {
		t.Errorf("Next = %+v; want the claim of .specs/03_p's task 3, then specr .specs/03_p --task 3", got)
	}

	// A package that cannot take a write, whose intent drifted, is skipped for the next.
	writable[".specs/03_p"] = false
	if got := r.Next(); len(got) != 2 || got[0].Input != ".specs/04_p" {
		t.Errorf("with .specs/03_p unable to take a write: Next = %+v; want the claim of .specs/04_p", got)
	}

	// An archived package is not work to do, whatever its status says.
	archived := pkg(1, "active", 0, task(1, "pending"))
	archived.Archived = true
	if got := (&Result{AdminResult: envelope.AdminResult{Packages: []envelope.PackageInfo{archived}}}).Next(); len(got) != 0 {
		t.Errorf("an archived package: Next = %+v", got)
	}
}

// TS-10-50: no call is suggested when none would help.
func TestNoNextCall_TS10_50(t *testing.T) {
	verdict := &Result{AdminResult: envelope.AdminResult{SpecDir: ".specs/16_x", Valid: ptr(true), Validation: &envelope.ValidationReport{Valid: true}}}
	// Even a verdict carrying a next_task would not suggest one: it has no tasks.
	verdict.NextTask = &envelope.TaskInfo{ID: 1}
	idle := &Result{AdminResult: envelope.AdminResult{Packages: []envelope.PackageInfo{
		{SpecDir: ".specs/01_p", Status: "active", TaskCounts: &envelope.TaskCounts{InProgress: 1}, NextTask: &envelope.TaskInfo{ID: 2, State: "in_progress"}},
		{SpecDir: ".specs/02_p", Status: "active", TaskCounts: &envelope.TaskCounts{Done: 3, Total: 3}},
		{SpecDir: ".specs/03_p", LoadError: "cannot read prd.md"},
		{SpecDir: ".specs/04_p", Status: "sealed", TaskCounts: &envelope.TaskCounts{}},
	}}}
	for name, r := range map[string]*Result{
		"the verdict":              verdict,
		"a report without a task":  report16("sealed", 7, 7, nil, true),
		"a list without a package": idle,
		"an empty list":            {AdminResult: envelope.AdminResult{Packages: []envelope.PackageInfo{}}},
	} {
		if got := r.Next(); len(got) != 0 {
			t.Errorf("%s: Next = %+v; want none", name, got)
		}
	}
}

// TS-10-55: Result is an AdminResult with a sentence and a next call, and no
// envelope speca writes is retryable or resumable. Result may hold state of its
// own beside the embedded AdminResult (the task form's marks), none of it
// exported, so none of it written.
func TestResultAndEnvelopeFlags_TS10_55(t *testing.T) {
	typ := reflect.TypeFor[Result]()
	if typ.NumField() < 1 || !typ.Field(0).Anonymous || typ.Field(0).Type != reflect.TypeFor[envelope.AdminResult]() {
		t.Errorf("Result is %v; want a struct whose first field embeds envelope.AdminResult", typ)
	}
	for i := 1; i < typ.NumField(); i++ {
		if typ.Field(i).IsExported() {
			t.Errorf("Result.%s is exported: it would be written beside the AdminResult", typ.Field(i).Name)
		}
	}
	for _, m := range []string{"Summary", "Next"} {
		if _, ok := reflect.TypeFor[*Result]().MethodByName(m); !ok {
			t.Errorf("*Result has no %s method", m)
		}
	}

	report := report16("active", 3, 7, &envelope.TaskInfo{ID: 4}, true)
	verdict := &Result{AdminResult: envelope.AdminResult{SpecDir: ".specs/16_x", Valid: ptr(true), Validation: &envelope.ValidationReport{Valid: true}}}
	list := &Result{AdminResult: envelope.AdminResult{Packages: packages("active")}}
	usage := &cli.ErrorInfo{Stage: "usage", Category: "usage", Message: "m"}
	load := &cli.ErrorInfo{Stage: "load", Category: "input", Message: "m"}

	for name, c := range map[string]struct {
		code    int
		result  *Result
		failure *cli.ErrorInfo
	}{
		"report":  {cli.ExitOK, report, nil},
		"verdict": {cli.ExitOK, verdict, nil},
		"list":    {cli.ExitOK, list, nil},
		"usage":   {cli.ExitUsage, nil, usage},
		"load":    {cli.ExitFailed, nil, load},
	} {
		var result any
		if c.result != nil {
			result = c.result
		}
		var out bytes.Buffer
		cli.Emit(&out, cli.NewRun(tool, "v").Envelope(c.code, result, c.failure))
		env, err := envelope.DecodeSpeca(out.Bytes())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if env.Tool != "speca" || (c.failure == nil) != env.OK {
			t.Errorf("%s: tool %q ok %v", name, env.Tool, env.OK)
		}
		if env.Error != nil && (env.Error.Retryable || env.Error.Resumable) {
			t.Errorf("%s: retryable %v resumable %v", name, env.Error.Retryable, env.Error.Resumable)
		}
		if env.Summary == "" {
			t.Errorf("%s: no summary", name)
		}
	}
}
