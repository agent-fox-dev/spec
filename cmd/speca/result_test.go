package main

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/agent-fox-dev/spec/envelope"
)

func TestSummary(t *testing.T) {
	next := &envelope.TaskInfo{ID: 4, Title: "t", State: "pending", Ready: true}
	invalid := report16("active", 3, 7, next, false)
	invalid.FailingRules = []string{"C3", "C7"}
	task := func(id int, from, to string) envelope.Change {
		return envelope.Change{Kind: "task", ID: id, From: from, To: to}
	}
	taskForm := func(changes []envelope.Change, dry bool) *Result {
		return &Result{AdminResult: envelope.AdminResult{SpecDir: ".specs/11_x", Changes: changes, DryRun: dry}, taskForm: true}
	}

	for name, tc := range map[string]struct {
		r    *Result
		want string
	}{
		// The report names the directory, the status, the tasks done, the next
		// task and whether the package validates.
		"a report":                        {report16("active", 3, 7, next, true), "speca: 16_indexed_code_search active, 3 of 7 tasks done; next task 4"},
		"a report that does not validate": {invalid, "speca: 16_indexed_code_search active, 3 of 7 tasks done; next task 4; does not validate (C3, C7)"},
		"a report with no next task":      {report16("sealed", 7, 7, nil, true), "speca: 16_indexed_code_search sealed, 7 of 7 tasks done"},
		"a report with no tasks":          {report16("draft", 0, 0, nil, true), "speca: 16_indexed_code_search draft, 0 of 0 tasks done"},
		"a report with no task counts":    {&Result{AdminResult: envelope.AdminResult{SpecDir: ".specs/16_x", Status: "draft"}}, "speca: 16_x draft, 0 of 0 tasks done"},

		// The verdict says whether the package validates, and if not how many
		// errors and which rules, each once; one error is not "1 errors".
		"a verdict that validates": {verdictOf(".specs/16_indexed_code_search"), "speca: 16_indexed_code_search validates"},
		"a verdict with errors":    {verdictOf(".specs/16_indexed_code_search", "C3", "C7", "cross_spec_actor"), "speca: 16_indexed_code_search does not validate (3 errors: C3, C7, cross_spec_actor)"},
		"a rule broken twice":      {verdictOf(".specs/16_x", "C4", "C4"), "speca: 16_x does not validate (2 errors: C4)"},
		"a verdict with one error": {verdictOf(".specs/16_x", "C4"), "speca: 16_x does not validate (1 error: C4)"},

		// The list counts the packages, then each status that has one, in
		// lifecycle order, then the packages that did not load.
		"a list":                 {listOf("sealed", "active", "draft", "active", "active", "sealed", "draft", "active", "active"), "speca: 9 packages (2 draft, 5 active, 2 sealed)"},
		"a list with unreadable": {listOf("draft", "", ""), "speca: 3 packages (1 draft, 2 unreadable)"},
		"a list of every status": {listOf("archived", "superseded", "sealed", "active", "draft"), "speca: 5 packages (1 draft, 1 active, 1 sealed, 1 superseded, 1 archived)"},
		"an empty list":          {listOf(), "speca: 0 packages"},
		"a list of one":          {listOf("active"), "speca: 1 package (1 active)"},

		// The task form names one change, several or none, and the dry run.
		"one task moved":          {taskForm([]envelope.Change{task(2, "pending", "in_progress")}, false), "speca: 11_x task 2 pending → in_progress"},
		"several tasks moved":     {taskForm([]envelope.Change{task(2, "in_progress", "done"), task(3, "in_progress", "done")}, false), "speca: 11_x tasks 2, 3 → done"},
		"no task moved":           {taskForm(nil, false), "speca: 11_x no task changed"},
		"one task moved, dry run": {taskForm([]envelope.Change{task(2, "pending", "in_progress")}, true), "speca: 11_x task 2 pending → in_progress (dry run, nothing written)"},
		"several tasks, dry run":  {taskForm([]envelope.Change{task(2, "in_progress", "done"), task(3, "in_progress", "done")}, true), "speca: 11_x tasks 2, 3 → done (dry run, nothing written)"},
		"no task moved, dry run":  {taskForm(nil, true), "speca: 11_x no task changed (dry run, nothing written)"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.r.Summary(); got != tc.want {
				t.Errorf("Summary = %q; want %q", got, tc.want)
			}
		})
	}
}

// The envelope keeps a summary within its limit, the package's name first.
func TestSummary_IsShortenedInTheEnvelope(t *testing.T) {
	long := report16("active", 3, 7, &envelope.TaskInfo{ID: 4}, false)
	long.SpecDir = ".specs/16_" + strings.Repeat("long_", 60)
	if got := envelopeSummary(long); utf8.RuneCountInString(got) > 200 || !strings.HasPrefix(got, "speca: 16_long_") {
		t.Errorf("the envelope's summary is %d characters: %q", utf8.RuneCountInString(got), got)
	}
}

// For any list of changes the task form's summary begins with the prefix and
// the package, ends with the dry-run suffix exactly under a dry run, and says no
// task changed exactly when the list is empty.
func TestSummary_OfTheTaskFormForAnyChanges(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 40))
	const suffix = " (dry run, nothing written)"
	for range 400 {
		n := rng.IntN(7)
		to := wantTaskStates[rng.IntN(len(wantTaskStates))]
		var changes []envelope.Change
		for _, id := range rng.Perm(20)[:n] {
			changes = append(changes, envelope.Change{Kind: "task", ID: id + 1, From: wantTaskStates[rng.IntN(len(wantTaskStates))], To: to})
		}
		dry := rng.IntN(2) == 0
		r := &Result{AdminResult: envelope.AdminResult{SpecDir: ".specs/12_dir", Changes: changes, DryRun: dry}, taskForm: true}
		s := r.Summary()
		if !strings.HasPrefix(s, "speca: 12_dir ") || strings.HasSuffix(s, suffix) != dry || strings.Contains(s, "no task changed") != (n == 0) {
			t.Fatalf("%d changes, dry run %v: Summary = %q", n, dry, s)
		}
	}
}

// The report suggests rendering the next task with specr, and says whether it is
// under way or the next ready one. Where the task is ready and the package can
// take a write, the claim of the task comes before the render.
func TestNext_OfTheReport(t *testing.T) {
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

// The list suggests the claim and the render of the first active package, not
// under archive/, that can take a write and has a ready task and none in
// progress.
func TestNext_OfTheList(t *testing.T) {
	task := func(id int, state string) *envelope.TaskInfo {
		return &envelope.TaskInfo{ID: id, Title: "t", State: state, Ready: state == "pending"}
	}
	pkg := func(n int, status string, inProgress int, next *envelope.TaskInfo) envelope.PackageInfo {
		return envelope.PackageInfo{SpecDir: fmt.Sprintf(".specs/%02d_p", n), Status: status,
			TaskCounts: &envelope.TaskCounts{InProgress: inProgress}, NextTask: next}
	}
	archived := pkg(5, "active", 0, task(1, "pending"))
	archived.Archived = true
	packages := []envelope.PackageInfo{
		archived,
		pkg(1, "draft", 0, task(1, "pending")),
		pkg(2, "active", 1, task(2, "in_progress")),
		pkg(3, "active", 0, task(3, "pending")),
		pkg(4, "active", 0, task(1, "pending")),
	}
	writable := map[string]bool{".specs/01_p": true, ".specs/02_p": true, ".specs/03_p": true, ".specs/04_p": true, ".specs/05_p": true}
	r := &Result{AdminResult: envelope.AdminResult{Packages: packages}, writableDirs: writable}
	got := r.Next()
	if len(got) != 2 || !sameCall(got[0], claim(".specs/03_p", 3)) ||
		!sameCall(got[1], envelope.Next{Tool: "specr", Input: ".specs/03_p", Flags: []string{"--task", "3"}}) {
		t.Errorf("Next = %+v; want the claim of .specs/03_p's task 3, then specr .specs/03_p --task 3", got)
	}

	// A package that cannot take a write is skipped.
	writable[".specs/03_p"] = false
	if got := r.Next(); len(got) != 2 || got[0].Input != ".specs/04_p" {
		t.Errorf("with .specs/03_p unable to take a write: Next = %+v; want the claim of .specs/04_p", got)
	}
}

// No call is suggested when none would help.
func TestNext_NoneWhenNothingWouldHelp(t *testing.T) {
	verdict := verdictOf(".specs/16_x")
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
		"an empty list":            listOf(),
	} {
		if got := r.Next(); len(got) != 0 {
			t.Errorf("%s: Next = %+v; want none", name, got)
		}
	}
}
