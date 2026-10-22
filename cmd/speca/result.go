package main

import (
	"fmt"
	"path/filepath"
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
}

// Summary is the envelope's one sentence. The fields a form fills say which
// form it was: packages for the list, validation for the verdict, and
// otherwise it is the report.
func (r *Result) Summary() string {
	switch {
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
	case r.Packages != nil:
		return r.listNext()
	case r.Validation != nil:
		return nil
	default:
		return r.reportNext()
	}
}

func (r *Result) reportNext() []cli.Next {
	t := r.NextTask
	if t == nil {
		return nil
	}
	why := fmt.Sprintf("task %d (%s) is the next ready task; render the package scoped to it", t.ID, t.Title)
	if t.State == string(afspec.TaskStateInProgress) {
		why = fmt.Sprintf("task %d (%s) is in progress; render the package scoped to it", t.ID, t.Title)
	}
	return []cli.Next{{Tool: "specr", Input: r.SpecDir, Flags: []string{"--task", fmt.Sprint(t.ID)}, Why: why}}
}

// listNext names the first package, in list order, that is waiting for
// someone: an active one, not under archive/, with no task in progress and a
// ready task. With none in progress, a package's next task is its first ready
// one.
func (r *Result) listNext() []cli.Next {
	for _, p := range r.Packages {
		if p.Archived || p.LoadError != "" || p.Status != string(afspec.PrdFrontmatterV2JsonStatusActive) ||
			p.TaskCounts == nil || p.TaskCounts.InProgress > 0 || p.NextTask == nil {
			continue
		}
		why := fmt.Sprintf("%s is active with no task in progress; task %d (%s) is the next ready task", p.SpecDir, p.NextTask.ID, p.NextTask.Title)
		return []cli.Next{{Tool: "specr", Input: p.SpecDir, Flags: []string{"--task", fmt.Sprint(p.NextTask.ID)}, Why: why}}
	}
	return nil
}
