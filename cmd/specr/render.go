package main

import (
	"fmt"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// Sections is the closed set of parts a render is made of, in the order the
// format renders them (§11.1). --section takes these names.
var Sections = []string{"prd", "architecture", "requirements", "test_spec", "tasks"}

// sectionHeading is each section's top-level heading, the one
// afspec.RenderCombined gives it, so every form of the render reads alike.
var sectionHeading = map[string]string{
	"prd":          "PRD",
	"architecture": "Architecture",
	"requirements": "Requirements",
	"test_spec":    "Test Specification",
	"tasks":        "Tasks",
}

// What --max-tokens can drop, in the order it drops it.
const (
	ReducedArchitecture = "architecture"
	ReducedTestDetail   = "test_detail"
)

// Result is specr's result: what was rendered, the plan's tasks, and the
// Markdown itself, last, so a reader meets what it is before what it says.
type Result struct {
	SpecDir   string     `json:"spec_dir" description:"the package directory, relative to --dir when inside it; runnable as specr's input"`
	SpecID    string     `json:"spec_id" description:"the spec id, from the PRD frontmatter"`
	SpecName  string     `json:"spec_name" description:"the spec name, from the PRD frontmatter"`
	Title     string     `json:"title" description:"the spec title, from the PRD frontmatter"`
	Status    string     `json:"status" description:"the package's lifecycle status: draft, active, sealed, superseded or archived"`
	Valid     bool       `json:"valid" description:"whether the package passes the format's validation; an invalid package still renders in full, and a spec_invalid warning names the failing rules"`
	Scope     string     `json:"scope" description:"what was rendered: spec (the whole package) or task (the package scoped to one task)"`
	Task      *TaskInfo  `json:"task,omitempty" description:"the task the render is scoped to; present exactly when scope is task"`
	Sections  []string   `json:"sections" description:"the sections the Markdown holds, in order; a subset of prd, architecture, requirements, test_spec, tasks"`
	MaxTokens int        `json:"max_tokens,omitempty" description:"the --max-tokens budget the render was held to; absent when there was none"`
	Reduced   []string   `json:"reduced,omitempty" description:"the detail --max-tokens dropped to fit, in the order it is dropped: architecture (the whole section), test_detail (each test's given, when, real components and pseudocode); absent when nothing was dropped"`
	Tokens    int        `json:"tokens" description:"the estimated token count of markdown (bytes / 4)"`
	Tasks     []TaskInfo `json:"tasks" description:"every task of the plan, in order: the ids --task takes"`
	// MarkdownFile is where --markdown-file wrote the Markdown.
	MarkdownFile string `json:"markdown_file,omitempty" description:"the absolute path --markdown-file wrote the Markdown to"`
	Markdown     string `json:"markdown" description:"the rendered Markdown"`
}

// TaskInfo is one task of the plan, by id, without its body.
type TaskInfo struct {
	ID        int    `json:"id" description:"the task id, as --task takes it"`
	Title     string `json:"title" description:"the task title"`
	Kind      string `json:"kind" description:"implement, or integration for the plan's last task"`
	State     string `json:"state" description:"pending, in_progress, done or dropped"`
	Optional  bool   `json:"optional,omitempty" description:"true when the task may be skipped"`
	DependsOn []int  `json:"depends_on,omitempty" description:"the tasks that must be done first"`
}

// Summary is the envelope's one sentence.
func (r *Result) Summary() string {
	what := "whole"
	if r.Task != nil {
		what = fmt.Sprintf("scoped to task %d of %d", r.Task.ID, len(r.Tasks))
	}
	sections := strings.Join(r.Sections, ", ")
	if sections == "" {
		sections = "no sections"
	}
	s := fmt.Sprintf("specr: rendered %s_%s %s (%s, ~%d tokens)", r.SpecID, r.SpecName, what, sections, r.Tokens)
	if len(r.Reduced) > 0 {
		s += "; dropped " + strings.Join(r.Reduced, ", ") + " to fit"
	}
	return s
}

// Artifacts names the file --markdown-file wrote.
func (r *Result) Artifacts() []cli.Artifact {
	if r.MarkdownFile == "" {
		return nil
	}
	return []cli.Artifact{{Kind: cli.ArtifactMarkdownFile, Path: r.MarkdownFile}}
}

// Next suggests rendering the next task that is not done: after a whole
// render, the first; after a task, the next one after it.
func (r *Result) Next() []cli.Next {
	after := 0
	if r.Task != nil {
		after = r.Task.ID
	}
	seen := after == 0
	for _, t := range r.Tasks {
		if !seen {
			seen = t.ID == after
			continue
		}
		if t.State == string(afspec.TaskStateDone) || t.State == string(afspec.TaskStateDropped) {
			continue
		}
		why := fmt.Sprintf("task %d (%s) is the first task not done; render the package scoped to it", t.ID, t.Title)
		if after != 0 {
			why = fmt.Sprintf("task %d (%s) is the next task not done", t.ID, t.Title)
		}
		return []cli.Next{{Tool: "specr", Input: r.SpecDir, Flags: []string{"--task", fmt.Sprint(t.ID)}, Why: why}}
	}
	return nil
}

// renderOptions are the flags that shape the render.
type renderOptions struct {
	// task scopes the render to one task; 0 renders the whole package.
	task int
	// sections selects what the Markdown holds; nil means every section.
	sections []string
	// maxTokens is the budget; 0 means none. It is never combined with
	// sections.
	maxTokens int
}

// render renders spec as the options ask, using the afspec render
// functions, and records in res what was rendered and what was dropped.
// Rendering cannot fail; what can go wrong is reported as a warning.
func render(spec *afspec.Spec, opts renderOptions, res *Result, run *cli.Run) {
	var budget []afspec.RenderOption
	if opts.maxTokens > 0 {
		budget = append(budget, afspec.WithMaxTokens(opts.maxTokens))
		res.MaxTokens = opts.maxTokens
	}

	res.Sections = []string{}
	var parts map[string]string
	switch {
	case opts.task > 0:
		res.Scope = "task"
		parts = spec.RenderIndividualScoped(opts.task, budget...)
		full := spec.RenderIndividualScoped(opts.task)
		res.Reduced = reducedParts(spec, parts, full)
	case opts.sections != nil:
		res.Scope = "spec"
		parts = spec.RenderIndividual()
	default:
		// The whole package is the library's combined render, byte for byte.
		res.Scope = "spec"
		res.Markdown = spec.RenderCombined(budget...)
		res.Reduced = reducedCombined(spec, res.Markdown)
		for _, key := range Sections {
			if key == "architecture" && (spec.Architecture == "" || has(res.Reduced, ReducedArchitecture)) {
				continue
			}
			res.Sections = append(res.Sections, key)
		}
	}

	if parts != nil {
		var b strings.Builder
		if res.Task != nil {
			fmt.Fprintf(&b, "> The package %s, scoped to task %d: %s. What the task owns is rendered in full; "+
				"requirements, tests and tasks it does not own are listed by id and title only.\n\n",
				code(res.SpecID+"_"+res.SpecName), res.Task.ID, res.Task.Title)
		}
		for _, key := range Sections {
			if opts.sections != nil && !has(opts.sections, key) {
				continue
			}
			body, ok := parts[key]
			if !ok {
				if key == "architecture" && opts.sections != nil {
					run.Warn(cli.WarnSectionAbsent, "low", "--section architecture asked for, and the package has no architecture.md")
				}
				continue
			}
			fmt.Fprintf(&b, "# %s\n\n%s\n", sectionHeading[key], body)
			res.Sections = append(res.Sections, key)
		}
		res.Markdown = b.String()
	}

	res.Tokens = afspec.EstimateTokens(res.Markdown)
	if opts.maxTokens > 0 && res.Tokens > opts.maxTokens {
		run.Warn(cli.WarnOverBudget, "low", "the render is ~%d tokens, over --max-tokens %d, with everything droppable dropped",
			res.Tokens, opts.maxTokens)
	}
}

// reducedCombined says what a budgeted RenderCombined dropped, by comparing
// it with the library's own render at each level of detail.
func reducedCombined(spec *afspec.Spec, got string) []string {
	if got == spec.RenderCombined() {
		return nil
	}
	if spec.Architecture != "" {
		noArch := *spec
		noArch.Architecture = ""
		if got == noArch.RenderCombined() {
			return []string{ReducedArchitecture}
		}
		return []string{ReducedArchitecture, ReducedTestDetail}
	}
	return []string{ReducedTestDetail}
}

// reducedParts says what a budgeted per-section render dropped, against the
// same render without a budget.
func reducedParts(spec *afspec.Spec, got, full map[string]string) []string {
	var out []string
	if _, ok := got["architecture"]; spec.Architecture != "" && !ok {
		out = append(out, ReducedArchitecture)
	}
	if got["test_spec"] != full["test_spec"] {
		out = append(out, ReducedTestDetail)
	}
	return out
}

// taskInfos lists the plan's tasks without their bodies.
func taskInfos(spec *afspec.Spec) []TaskInfo {
	out := []TaskInfo{}
	if spec.Tasks == nil {
		return out
	}
	for _, t := range spec.Tasks.Tasks {
		out = append(out, taskInfo(t))
	}
	return out
}

func taskInfo(t afspec.Task) TaskInfo {
	return TaskInfo{
		ID: t.Id, Title: t.Title, Kind: string(t.Kind), State: string(t.State),
		Optional: t.IsOptional(), DependsOn: append([]int(nil), t.DependsOn...),
	}
}

func has(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// code renders s as a Markdown code span.
func code(s string) string {
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	return fence + s + fence
}
