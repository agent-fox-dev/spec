package main

import (
	"fmt"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// Sections is the closed set of parts a render is made of, in the order the
// format renders them (§11.1). --section takes these names.
var Sections = envelope.Sections()

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
	ReducedArchitecture = envelope.ReducedArchitecture
	ReducedTestDetail   = envelope.ReducedTestDetail
)

// Result is specr's result. Its fields are the envelope package's
// RenderResult, which a program reading the output decodes into; the methods
// are what the shell asks of a result.
type Result struct {
	envelope.RenderResult

	// writable is whether the package can take a write, as specpkg.Writable says.
	// It decides whether next suggests a call of speca, and is not written.
	writable bool
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

// Next suggests the calls to make after this render, in the order an agent makes
// them. After a whole render of a package that can take a write, with nothing in
// progress, the first call is the claim of the next ready task, with speca; the
// render of the next task that is not done follows it, and is the only entry when
// there is no claim to make. After a whole render that task is the first, and after
// a task, the one after it.
func (r *Result) Next() []cli.Next {
	after := 0
	if r.Task != nil {
		after = r.Task.ID
	}
	var next *envelope.TaskInfo
	seen := after == 0
	for i, t := range r.Tasks {
		if !seen {
			seen = t.ID == after
			continue
		}
		if t.State != string(afspec.TaskStateDone) && t.State != string(afspec.TaskStateDropped) {
			next = &r.Tasks[i]
			break
		}
	}

	var out []cli.Next
	if claim, ok := r.claim(next); ok {
		out = append(out, claim)
	}
	if next != nil {
		why := fmt.Sprintf("task %d (%s) is the first task not done; render the package scoped to it", next.ID, next.Title)
		if after != 0 {
			why = fmt.Sprintf("task %d (%s) is the next task not done", next.ID, next.Title)
		}
		out = append(out, cli.Next{Tool: "specr", Input: r.SpecDir, Flags: []string{"--task", fmt.Sprint(next.ID)}, Why: why})
	}
	return out
}

// claim is the speca call that claims the task the render suggests next, when it
// would succeed and speca would suggest the same. After a whole render that is
// the first task not done: pending and ready, on a package that can take a write,
// with no task in progress (speca's own report suggests a claim only then, and
// then for this task).
func (r *Result) claim(first *envelope.TaskInfo) (cli.Next, bool) {
	if r.Task != nil || !r.writable || first == nil || first.State != string(afspec.TaskStatePending) || !first.Ready {
		return cli.Next{}, false
	}
	for _, t := range r.Tasks {
		if t.State == string(afspec.TaskStateInProgress) {
			return cli.Next{}, false
		}
	}
	return cli.Next{Tool: "speca", Input: r.SpecDir, Flags: []string{"--task", fmt.Sprint(first.ID), "--state", string(afspec.TaskStateInProgress)},
		Why: fmt.Sprintf("task %d (%s) is the next ready task; claim it before working on it so that no other agent takes it", first.ID, first.Title)}, true
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
		res.Scope = envelope.ScopeTask
		parts = spec.RenderIndividualScoped(opts.task, budget...)
		full := spec.RenderIndividualScoped(opts.task)
		res.Reduced = reducedParts(spec, parts, full)
	case opts.sections != nil:
		res.Scope = envelope.ScopeSpec
		parts = spec.RenderIndividual()
	default:
		// The whole package is the library's combined render, byte for byte.
		res.Scope = envelope.ScopeSpec
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
