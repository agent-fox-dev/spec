package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// lifecycle is the five lifecycle statuses in the order of a package's life:
// the order the list's summary counts them in, and allowed_status names them.
var lifecycle = []string{
	string(afspec.PrdFrontmatterV2JsonStatusDraft),
	string(afspec.PrdFrontmatterV2JsonStatusActive),
	string(afspec.PrdFrontmatterV2JsonStatusSealed),
	string(afspec.PrdFrontmatterV2JsonStatusSuperseded),
	string(afspec.PrdFrontmatterV2JsonStatusArchived),
}

// Result is speca's result. Its fields are the envelope package's
// AdminResult, which a program reading the output decodes into; the methods
// are what the shell asks of a result.
type Result struct {
	envelope.AdminResult

	// What follows is state the shell asks of a result; none of it is written.

	// taskForm marks the result of the task form, whose summary names the
	// changes.
	taskForm bool
	// callNext, where nextSet, is the next of a dry run: the one call that makes
	// the changes, or none. It replaces what the report would suggest, since the
	// result is that of a package that has not changed.
	callNext []cli.Next
	nextSet  bool
	// writable is whether the package the report is of can take a write, as
	// specpkg.Writable says. The report suggests a claim only if it can.
	writable bool
	// writableDirs holds, for each package of a list, whether it can take a
	// write, which an entry of the list does not say. The list suggests no claim
	// for one that cannot.
	writableDirs map[string]bool
	// lifecycle holds the status call the report suggests for the package, if one
	// is, which comes before any suggestion about its tasks. Only the report form
	// sets it.
	lifecycle []cli.Next
	// sentence is the status form's own summary, which it makes where it knows
	// what it did.
	sentence string
	// resumable is true of the failure to move a package under archive/ after
	// its status was written: running the same call again moves it.
	resumable bool
}

// Resumable says whether running the call again continues the work. The shell
// asks the result, and reports it in the error.
func (r *Result) Resumable() bool { return r.resumable }

// Summary is the envelope's one sentence. The fields a form fills say which
// form it was: packages for the list, validation for the verdict, and
// otherwise it is the report.
func (r *Result) Summary() string {
	switch {
	case r.sentence != "":
		return r.sentence
	case r.taskForm:
		return r.taskSummary()
	case r.Packages != nil:
		return r.listSummary()
	case r.Validation != nil:
		return r.verdictSummary()
	default:
		return r.reportSummary()
	}
}

// dirName is the name of the package's directory, the one an agent calls it by.
func (r *Result) dirName() string { return filepath.Base(r.SpecDir) }

func (r *Result) reportSummary() string {
	done, total := 0, 0
	if c := r.TaskCounts; c != nil {
		done, total = c.Done, c.Total
	}
	s := fmt.Sprintf("%s: %s %s, %d of %d tasks done", tool, r.dirName(), r.Status, done, total)
	if r.NextTask != nil {
		s += fmt.Sprintf("; next task %d", r.NextTask.ID)
	}
	if r.Valid != nil && !*r.Valid {
		s += "; does not validate"
		if len(r.FailingRules) > 0 {
			s += " (" + strings.Join(r.FailingRules, ", ") + ")"
		}
	}
	return s
}

func (r *Result) verdictSummary() string {
	s := fmt.Sprintf("%s: %s", tool, r.dirName())
	if r.Validation.Valid && r.Validation.ErrorCount == 0 {
		return s + " validates"
	}
	noun := "errors"
	if r.Validation.ErrorCount == 1 {
		noun = "error"
	}
	return s + fmt.Sprintf(" does not validate (%d %s: %s)", r.Validation.ErrorCount, noun,
		strings.Join(specpkg.Rules(entriesOf(r.Validation.Errors)), ", "))
}

// entriesOf turns reported findings back into the entries Rules reads.
func entriesOf(items []envelope.ValidationItem) []afspec.ValidationEntry {
	entries := make([]afspec.ValidationEntry, len(items))
	for i, it := range items {
		entries[i] = afspec.ValidationEntry{Check: it.Check}
	}
	return entries
}

// taskSummary names what the task form did: the one task and its states, the
// tasks and where they went, or that none changed; and, under --dry-run, that
// nothing was written.
func (r *Result) taskSummary() string {
	s := fmt.Sprintf("%s: %s", tool, r.dirName())
	switch n := len(r.Changes); n {
	case 0:
		s += " no task changed"
	case 1:
		c := r.Changes[0]
		s += fmt.Sprintf(" task %d %s → %s", c.ID, c.From, c.To)
	default:
		ids := make([]string, n)
		for i, c := range r.Changes {
			ids[i] = fmt.Sprint(c.ID)
		}
		s += fmt.Sprintf(" tasks %s → %s", strings.Join(ids, ", "), r.Changes[0].To)
	}
	if r.DryRun {
		s += " (dry run, nothing written)"
	}
	return s
}

// refused is the result of a call that was refused or failed after the package
// was read: the report of the package as it is, so that the caller sees its
// tasks without another call. It has no sentence of its own, so the envelope's
// summary is the refusal, and no next: a suggestion after a refused claim could
// point one agent at the task another agent holds.
type refused struct{ *Result }

func (refused) Summary() string  { return "" }
func (refused) Next() []cli.Next { return nil }

func (r *Result) listSummary() string {
	noun := "packages"
	if len(r.Packages) == 1 {
		noun = "package"
	}
	counts := map[string]int{}
	unreadable := 0
	for _, p := range r.Packages {
		if p.LoadError != "" {
			unreadable++
			continue
		}
		counts[p.Status]++
	}
	var parts []string
	for _, status := range lifecycle {
		if n := counts[status]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, status))
		}
	}
	if unreadable > 0 {
		parts = append(parts, fmt.Sprintf("%d unreadable", unreadable))
	}
	s := fmt.Sprintf("%s: %d %s", tool, len(r.Packages), noun)
	if len(parts) > 0 {
		s += " (" + strings.Join(parts, ", ") + ")"
	}
	return s
}

// Next suggests only calls that exist: rendering the task to work on with
// specr. The verdict suggests none, and neither does a report or a list that
// has no task to name.
func (r *Result) Next() []cli.Next {
	switch {
	case r.nextSet:
		return r.callNext
	case r.Packages != nil:
		return r.listNext()
	case r.Validation != nil:
		return nil
	default:
		return r.reportNext()
	}
}

// reportNext is the report's suggestions: the status call, if there is one,
// then the calls about the next task.
func (r *Result) reportNext() []cli.Next {
	return append(slices.Clone(r.lifecycle), r.taskNext()...)
}

// taskNext is the claim of the next task, where it can be claimed, and the
// render of it.
func (r *Result) taskNext() []cli.Next {
	t := r.NextTask
	if t == nil {
		return nil
	}
	why := fmt.Sprintf("task %d (%s) is the next ready task; render the package scoped to it", t.ID, t.Title)
	if t.State == string(afspec.TaskStateInProgress) {
		why = fmt.Sprintf("task %d (%s) is in progress; render the package scoped to it", t.ID, t.Title)
	}
	render := cli.Next{Tool: "specr", Input: r.SpecDir, Flags: []string{"--task", fmt.Sprint(t.ID)}, Why: why}
	// A ready task is claimed before it is worked on, so the claim comes first,
	// for a package that can take it: a draft, or an active one whose intent
	// still matches its hash. A task already in progress has been claimed.
	if t.State == string(afspec.TaskStatePending) && t.Ready && r.writable {
		return []cli.Next{claimOf(r.SpecDir, *t), render}
	}
	return []cli.Next{render}
}

// claimOf is the call that claims task t of the package at dir: speca's own task
// form, which moves it to in_progress.
func claimOf(dir string, t envelope.TaskInfo) cli.Next {
	return cli.Next{Tool: tool, Input: dir, Flags: []string{"--task", fmt.Sprint(t.ID), "--state", string(afspec.TaskStateInProgress)},
		Why: fmt.Sprintf("task %d (%s) is the next ready task and nothing is in progress; claim it before working on it", t.ID, t.Title)}
}

// listNext names the first package, in list order, that is waiting for
// someone: an active one, not under archive/, with no task in progress and a
// ready task. With none in progress, a package's next task is its first ready
// one.
func (r *Result) listNext() []cli.Next {
	for _, p := range r.Packages {
		if p.Archived || p.LoadError != "" || p.Status != string(afspec.PrdFrontmatterV2JsonStatusActive) ||
			p.TaskCounts == nil || p.TaskCounts.InProgress > 0 || p.NextTask == nil || !p.NextTask.Ready || !r.writableDirs[p.SpecDir] {
			continue
		}
		why := fmt.Sprintf("%s is active with no task in progress; task %d (%s) is the next ready task", p.SpecDir, p.NextTask.ID, p.NextTask.Title)
		return []cli.Next{
			claimOf(p.SpecDir, *p.NextTask),
			{Tool: "specr", Input: p.SpecDir, Flags: []string{"--task", fmt.Sprint(p.NextTask.ID)}, Why: why},
		}
	}
	return nil
}
