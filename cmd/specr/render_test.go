package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// The whole package is the library's combined render, byte for byte, with
// every section the package has.
func TestRender_TheWholePackageIsTheCombinedRender(t *testing.T) {
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
	if got := strings.Join(r.Sections, ","); got != "prd,architecture,requirements,tasks" {
		t.Errorf("sections = %s", got)
	}
	if r.Tokens != afspec.EstimateTokens(r.Markdown) || !r.Valid || len(r.Tasks) != 3 {
		t.Errorf("tokens %d valid %v tasks %d; want the two tasks of the plan and the integration task", r.Tokens, r.Valid, len(r.Tasks))
	}
	if !strings.Contains(r.Markdown, "### 3. "+afspec.IntegrationTaskTitle+" (integration)") || strings.Contains(r.Markdown, "pending") {
		t.Error("the render lacks the integration task, or shows a task state")
	}
}

// A task-scoped render is the library's scoped render, each section under
// its heading, with what the task owns in full and the rest by id and title.
func TestRender_ScopedToATask(t *testing.T) {
	root := newRepo(t)
	env, code, _ := run(t, "--dir", root, "9", "--task", "2")
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	r := env.Result
	if r.Scope != "task" || r.Task == nil || r.Task.ID != 2 || r.Task.Title == "" {
		t.Fatalf("scope %q task %+v", r.Scope, r.Task)
	}
	parts := load(t, root, example).RenderIndividualScoped(2)
	for _, key := range []string{"prd", "requirements", "tasks"} {
		if !strings.Contains(r.Markdown, "# "+sectionHeading[key]+"\n\n"+parts[key]) {
			t.Errorf("section %s is not the scoped render", key)
		}
	}
	for _, want := range []string{"scoped to task 2", "REQ-1:** JSON-only stdout in agent mode (other task)",
		"r = run(env, 'spec status 99_missing')", "- 4. " + afspec.IntegrationTaskTitle + " (integration)"} {
		if !strings.Contains(r.Markdown, want) {
			t.Errorf("markdown lacks %q", want)
		}
	}
}

// A render scoped to the derived integration task, task N+1, is the library's
// scoped render of it: every path in full, the contracts to confirm, and the
// task's computed content.
func TestRender_ScopedToTheIntegrationTask(t *testing.T) {
	root := newRepo(t)
	env, code, _ := run(t, "--dir", root, "9", "--task", fmt.Sprint(integrationTask))
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	r := env.Result
	if r.Scope != "task" || r.Task == nil || r.Task.ID != integrationTask || r.Task.Kind != "integration" || r.Task.Title != afspec.IntegrationTaskTitle {
		t.Fatalf("scope %q task %+v", r.Scope, r.Task)
	}
	parts := load(t, root, example).RenderIndividualScoped(integrationTask)
	for _, key := range []string{"prd", "requirements", "tasks"} {
		if !strings.Contains(r.Markdown, "# "+sectionHeading[key]+"\n\n"+parts[key]) {
			t.Errorf("section %s is not the scoped render", key)
		}
	}
	for _, want := range []string{"scoped to task 4", "## Contracts", "PATH-1:** Agent lists specs and parses the result (included below)",
		"### PATH-1:", "### 4. " + afspec.IntegrationTaskTitle + " (integration)", "**Depends on:** 1, 2, 3", "Run the smoke check of PATH-1",
		"- 3. Run the agent-mode commands end to end (implement)"} {
		if !strings.Contains(r.Markdown, want) {
			t.Errorf("markdown lacks %q", want)
		}
	}
}

// --section renders only the sections named, in the package's order; asking
// for an architecture the package does not have is a warning.
func TestRender_OnlyTheSectionsNamed(t *testing.T) {
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
}

// --max-tokens drops the architecture first, then the checks' detail, and says
// which; past that, it warns that the render is still over.
func TestRender_MaxTokensDropsDetailInOrder(t *testing.T) {
	root := newRepo(t)
	spec := load(t, root, "01_test_feature")
	noArch := *spec
	noArch.Architecture = ""
	level1 := afspec.EstimateTokens(noArch.RenderCombined())

	for _, tc := range []struct {
		name    string
		argv    []string
		reduced string
		warned  bool
	}{
		{"a budget that fits drops nothing", []string{"1", "--max-tokens", "1000000"}, "", false},
		{"the architecture goes first", []string{"1", "--max-tokens", strconv.Itoa(level1)}, "architecture", false},
		{"then the checks' detail, and still over", []string{"1", "--max-tokens", "10"}, "architecture,check_detail", true},
		{"a package with no architecture drops the checks' detail", []string{"9", "--max-tokens", "10"}, "check_detail", true},
		{"a task render drops the same", []string{"1", "--task", "1", "--max-tokens", "10"}, "architecture,check_detail", true},
		{"a render of the integration task drops the same", []string{"1", "--task", "3", "--max-tokens", "10"}, "architecture,check_detail", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, code, _ := run(t, append([]string{"--dir", root}, tc.argv...)...)
			if code != cli.ExitOK {
				t.Fatalf("code %d: %+v", code, env.Error)
			}
			r := env.Result
			if got := strings.Join(r.Reduced, ","); got != tc.reduced {
				t.Errorf("reduced = %q, want %q", got, tc.reduced)
			}
			if has(r.Reduced, ReducedArchitecture) && has(r.Sections, "architecture") {
				t.Errorf("the architecture was dropped yet listed in sections %v", r.Sections)
			}
			if max, _ := strconv.Atoi(tc.argv[len(tc.argv)-1]); r.MaxTokens != max {
				t.Errorf("max_tokens = %d", r.MaxTokens)
			}
			want := 0
			if tc.warned {
				want = 1
			}
			if len(env.Warnings) != want || (tc.warned && env.Warnings[0].Code != cli.WarnOverBudget) {
				t.Errorf("warnings %+v; want an over-budget warning: %v", env.Warnings, tc.warned)
			}
			if tc.reduced != "" && !strings.Contains(env.Summary, "dropped "+strings.ReplaceAll(tc.reduced, ",", ", ")+" to fit") {
				t.Errorf("summary %q does not say what was dropped", env.Summary)
			}
		})
	}
	env, _, _ := run(t, "--dir", root, "1", "--max-tokens", "1000000")
	if env.Result.Markdown != spec.RenderCombined() {
		t.Error("a budget that fits changed the render")
	}
}

// next suggests the calls an agent makes after a render, in order: the speca
// call that claims the task or records it done, where the package can take it
// and speca would suggest the same, and then the render of the next task that
// is not done or dropped.
func TestNext_SuggestsTheClaimAndTheNextRender(t *testing.T) {
	const (
		claim1 = "speca --task 1 --state in_progress"
		claim2 = "speca --task 2 --state in_progress"
		claim4 = "speca --task 4 --state in_progress"
		done1  = "speca --task 1 --state done"
		done3  = "speca --task 3 --state done"
		done4  = "speca --task 4 --state done"
		task1  = "specr --task 1"
		task2  = "specr --task 2"
		task3  = "specr --task 3"
		task4  = "specr --task 4"
	)
	pending := []string{"pending", "pending", "pending"}
	type tc struct {
		name   string
		status string
		states []string
		deps   [][]int
		argv   []string
		want   []string
		// why holds words each entry's why must say, by position; nil checks none.
		why [][]string
		// renderWhy, if set, is the exact why of the render suggestion, given the
		// id and title of the task it names.
		renderWhy string
	}
	cases := []tc{
		// After a whole render.
		{name: "a fresh plan: the claim of task 1, then its render", states: pending, argv: []string{"09"},
			want:      []string{claim1, task1},
			why:       [][]string{{"task 1", "next ready task", "claim", "no other agent"}},
			renderWhy: "task %d (%s) is the first task not done; render the package scoped to it"},
		{name: "a task done is skipped", states: []string{"done", "pending", "pending"}, argv: []string{"09"},
			want: []string{claim2, task2}},
		{name: "a --section render offers the same", states: []string{"done", "pending", "pending"}, argv: []string{"09", "--section", "tasks"},
			want: []string{claim2, task2}},
		{name: "a --max-tokens render offers the same", states: []string{"done", "pending", "pending"}, argv: []string{"09", "--max-tokens", "2000"},
			want: []string{claim2, task2}},
		{name: "a task in progress elsewhere withholds the claim", states: []string{"pending", "in_progress", "pending"},
			deps: [][]int{nil, {}, {}}, argv: []string{"09"}, want: []string{task1}},
		{name: "the first task in progress is rendered, not claimed", states: []string{"in_progress", "pending", "pending"},
			argv: []string{"09"}, want: []string{task1}},
		{name: "a first task that is not ready is not claimed", states: pending, deps: [][]int{{2}, nil, nil},
			argv: []string{"09"}, want: []string{task1}},
		{name: "a plan whose tasks are all done or dropped claims the integration task", states: []string{"done", "dropped", "done"},
			argv: []string{"09"}, want: []string{claim4, task4},
			why: [][]string{{"task 4", "next ready task", "claim", "no other agent"}}},
		{name: "a finished plan suggests nothing", states: []string{"done", "dropped", "done", "done"}, argv: []string{"09"},
			want: []string{}},
		// After a task render.
		{name: "a ready task is claimed though another is in progress", states: []string{"pending", "pending", "in_progress"},
			deps: [][]int{{}, nil, {}}, argv: []string{"09", "--task", "1"}, want: []string{claim1, task2},
			why: [][]string{{"claim", "task 1", "before working on it"}}},
		{name: "a task in progress is recorded done", states: []string{"in_progress", "pending", "pending"},
			argv: []string{"09", "--task", "1"}, want: []string{done1, task2},
			why:       [][]string{{"done", "task 1", "finished", "tests pass"}},
			renderWhy: "task %d (%s) is the next task not done"},
		{name: "a task that is not ready has no call", states: pending, argv: []string{"09", "--task", "2"},
			want: []string{task3}},
		{name: "a task done has no call", states: []string{"done", "dropped", "pending"}, argv: []string{"09", "--task", "1"},
			want: []string{task3}},
		{name: "a task dropped has no call", states: []string{"done", "dropped", "pending"}, argv: []string{"09", "--task", "2"},
			want: []string{task3}},
		{name: "after the last task of the plan the integration task is rendered", states: pending, argv: []string{"09", "--task", "3"},
			want: []string{task4}},
		{name: "after the integration task there is no render", states: pending, argv: []string{"09", "--task", "4"},
			want: []string{}},
		{name: "the last task of the plan in progress is recorded done, then the integration task rendered",
			states: []string{"done", "done", "in_progress"}, argv: []string{"09", "--task", "3"}, want: []string{done3, task4}},
		{name: "the integration task is claimed once every task is done or dropped", states: []string{"done", "dropped", "done"},
			argv: []string{"09", "--task", "4"}, want: []string{claim4},
			why: [][]string{{"claim", "task 4", "before working on it"}}},
		{name: "the integration task is not claimed while a task of the plan is open", states: []string{"done", "done", "pending"},
			argv: []string{"09", "--task", "4"}, want: []string{}},
		{name: "the integration task in progress has only its done call", states: []string{"done", "done", "done", "in_progress"},
			argv: []string{"09", "--task", "4"}, want: []string{done4}},
	}
	// A package that cannot take a write is offered no call of speca, and keeps
	// its render suggestion.
	for _, status := range []string{"sealed", "superseded", "archived", "drifted"} {
		cases = append(cases,
			tc{name: status + ": a whole render", status: status, states: pending, argv: []string{"09"}, want: []string{task1}},
			tc{name: status + ": a pending task", status: status, states: pending, argv: []string{"09", "--task", "1"}, want: []string{task2}},
			tc{name: status + ": a task in progress", status: status, states: []string{"in_progress", "pending", "pending"},
				argv: []string{"09", "--task", "1"}, want: []string{task2}})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := newRepo(t)
			if c.status != "" {
				prepareStatus(t, root, c.status)
			}
			plan(t, filepath.Join(root, ".specs", example), c.states, c.deps)
			env, code, _ := run(t, append([]string{"--dir", root}, c.argv...)...)
			if code != cli.ExitOK {
				t.Fatalf("code %d: %+v", code, env.Error)
			}
			if got := calls(env.Next); !slices.Equal(got, c.want) {
				t.Fatalf("next = %v, want %v", got, c.want)
			}
			for i, n := range env.Next {
				if n.Input != env.Result.SpecDir {
					t.Errorf("next[%d] names %q, not the package %q", i, n.Input, env.Result.SpecDir)
				}
				id, err := strconv.Atoi(n.Flags[1])
				if err != nil {
					t.Fatalf("next[%d] names no task: %+v", i, n)
				}
				title := env.Result.Tasks[id-1].Title
				if n.Tool == "specr" && c.renderWhy != "" {
					if want := fmt.Sprintf(c.renderWhy, id, title); n.Why != want {
						t.Errorf("next[%d].why = %q, want %q", i, n.Why, want)
					}
				}
				if i >= len(c.why) {
					continue
				}
				// The title of the task named is said too.
				for _, w := range append(c.why[i], title) {
					if !strings.Contains(n.Why, w) {
						t.Errorf("next[%d].why = %q; want it to say %q", i, n.Why, w)
					}
				}
			}
		})
	}
}

// A task that takes handoffs is rendered with each, the sending package's
// what and the sentences of its criteria that depend on it; a sending package
// that cannot be read is said to be unreadable.
func TestRender_ShowsTheHandoffsATaskTakes(t *testing.T) {
	root := handoffRepo(t)
	env, code, _ := run(t, "--dir", root, "02", "--task", "1")
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	sender := load(t, root, "01_one")
	var sentence string
	for _, r := range sender.Requirements.Requirements {
		for _, c := range r.Criteria {
			if c.Id == "REQ-1.1" {
				sentence = r.Resolve(c).RenderEARSSentence()
			}
		}
	}
	for _, want := range []string{"01_one", "H-1", "after-landing hooks", sentence} {
		if !strings.Contains(env.Result.Markdown, want) {
			t.Errorf("the render of task 1 lacks %q", want)
		}
	}
	if env, _, _ := run(t, "--dir", root, "02", "--task", "2"); strings.Contains(env.Result.Markdown, "after-landing hooks") {
		t.Error("task 2, which takes no handoff, is rendered with one")
	}

	if err := os.Chmod(filepath.Join(root, ".specs", "01_one", "requirements.json"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, ".specs", "01_one", "requirements.json"), 0o644) })
	env, code, _ = run(t, "--dir", root, "02", "--task", "1")
	if code != cli.ExitOK || !strings.Contains(env.Result.Markdown, "could not be read") {
		t.Errorf("code %d; want the render to say the sending package could not be read", code)
	}
}

// The integration task lists each inbound and outbound handoff with its
// contracts, and gets one rendered step per inbound handoff, which the
// package's spec.md does not hold.
func TestRender_TheIntegrationTaskConfirmsTheHandoffs(t *testing.T) {
	root := handoffRepo(t)
	id := "3" // the plain example has two tasks, so its integration task is 3
	env, code, _ := run(t, "--dir", root, "02", "--task", id)
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	md := env.Result.Markdown
	contracts := md[strings.Index(md, "## Contracts"):]
	contracts = contracts[:strings.Index(contracts[3:], "\n## ")+3]
	for _, want := range []string{"Inbound", "`01_one` H-1", "after-landing hooks", "taken by task 1", "Outbound", "H-1", "the fork hook", "to `03`"} {
		if !strings.Contains(contracts, want) {
			t.Errorf("the contracts lack %q:\n%s", want, contracts)
		}
	}
	steps := md[strings.Index(md, "**Steps:**"):]
	if !strings.Contains(steps, "01") || !strings.Contains(steps, "H-1") || !strings.Contains(steps, "file:line") {
		t.Errorf("no rendered step confirms 01's H-1 by file:line:\n%s", steps)
	}
	if strings.Contains(load(t, root, "02_two").RenderCombined(), "file:line that production code wires") {
		t.Error("the package's own render holds the inbound step")
	}

	env, _, _ = run(t, "--dir", root, "01", "--task", id)
	contracts = env.Result.Markdown[strings.Index(env.Result.Markdown, "## Contracts"):]
	if strings.Contains(contracts[:strings.Index(contracts[3:], "\n## ")+3], "Inbound") {
		t.Error("01, which nothing hands anything to, lists inbound handoffs")
	}
}
