package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/evidence"
)

// closeOnEvidence applies, to the moves a task-form call on a package that
// requires evidence would make, what the evidence asks of them (ADR 26), and
// returns the log of the tests it ran, if it ran any. A move to pending
// removes the task's evidence, its work being done again. A move to done is
// checked after the state machine's own checks: every criterion the task owns
// (for the integration task, every path) has an evidence entry; every test of
// an implement task has a failing run or is exempt; every test, run now, is
// pass. When every task to close passes, each test's passing run is recorded
// in the progress the call writes; when one does not, the whole call is
// refused, naming every fault. --force closes a task without the checks, with
// a high warning, and leaves its evidence as it is. A package that does not
// require evidence is left as it is.
func (c *call) closeOnEvidence(name string, spec *afspec.Spec, plan *movePlan) (string, *cli.ErrorInfo) {
	if !spec.RequiresEvidence() {
		return "", nil
	}
	var closing []*afspec.PlannedTask
	for i, m := range plan.moves {
		switch m.to {
		case afspec.TaskStatePending:
			setEvidence(plan.after, m.id, nil)
		case afspec.TaskStateDone:
			if c.opts.force {
				plan.moves[i].forced = true
				plan.warnings = append(plan.warnings, plannedWarning{cli.WarnTaskEvidenceForced, "high", fmt.Sprintf(
					"task %d was closed with --force, without the checks of its evidence: none of its tests was run, and its evidence is as it was", m.id)})
				continue
			}
			task, _ := spec.GetTask(m.id)
			closing = append(closing, task)
		}
	}
	if len(closing) == 0 {
		return "", nil
	}

	var tests []string
	for _, task := range closing {
		for _, e := range spec.TaskEvidence(task.Id) {
			for _, t := range e.Tests {
				if !slices.Contains(tests, t.Test) {
					tests = append(tests, t.Test)
				}
			}
		}
	}
	var ran testRun
	if len(tests) > 0 {
		var failure *cli.ErrorInfo
		if ran, failure = c.runEvidenceTests(name, spec, tests); failure != nil {
			return "", failure
		}
	}

	var faults []string
	for _, task := range closing {
		if f := closeFaults(spec, task, ran); len(f) > 0 {
			faults = append(faults, fmt.Sprintf("task %d: %s", task.Id, strings.Join(f, "; ")))
		}
	}
	if len(faults) > 0 {
		noun := "this task"
		if len(closing) > 1 {
			noun = "these tasks"
		}
		return "", &cli.ErrorInfo{Stage: "evidence", Category: envelope.CategoryEvidence, FixHint: &cli.FixHint{Flag: "--force"},
			Message: fmt.Sprintf("%s: %s cannot close on the evidence, so no task was changed: %s; record the evidence with --evidence, "+
				"make the tests pass, or give --force to close without the checks (a high task_evidence_forced warning)",
				name, noun, strings.Join(faults, ". "))}
	}

	passed := &afspec.EvidenceRun{At: ran.at, Tree: ran.tree}
	for _, task := range closing {
		entries := spec.TaskEvidence(task.Id)
		for i := range entries {
			for j := range entries[i].Tests {
				run := *passed
				entries[i].Tests[j].Passed = &run
			}
			if entries[i].NoRed != nil {
				plan.warnings = append(plan.warnings, plannedWarning{cli.WarnEvidenceNoRed, "low", fmt.Sprintf(
					"task %d closes with %s exempt from a failing run: %s", task.Id, entries[i].Covers, *entries[i].NoRed)})
			}
		}
		setEvidence(plan.after, task.Id, entries)
	}
	plan.warnings = append(plan.warnings, ran.warnings...)
	return ran.log, nil
}

// closeFaults are what stops the task from closing on its evidence and the
// tests' statuses in ran: each criterion (for the integration task, path) it
// owns without an entry, each test of an implement task without a failing run
// that is not exempt, and each test that is not pass now.
func closeFaults(spec *afspec.Spec, task *afspec.PlannedTask, ran testRun) []string {
	entries := spec.TaskEvidence(task.Id)
	covered := map[string]bool{}
	exempt := map[string]bool{}
	for _, e := range entries {
		covered[e.Covers] = true
		for _, t := range e.Tests {
			listed, ok := exempt[t.Test]
			exempt[t.Test] = (listed || !ok) && e.NoRed != nil
		}
	}
	var unmapped []string
	for _, id := range spec.Owned(task.Id) {
		if !covered[id] {
			unmapped = append(unmapped, id)
		}
	}
	var faults []string
	if len(unmapped) > 0 {
		faults = append(faults, fmt.Sprintf("%s %s no evidence entry", strings.Join(unmapped, ", "), hasOrHave(len(unmapped))))
	}
	seen := map[string]bool{}
	for _, e := range entries {
		for _, t := range e.Tests {
			if seen[t.Test] {
				continue
			}
			seen[t.Test] = true
			if task.Kind == afspec.TaskKindImplement && t.Failed == nil && !exempt[t.Test] {
				faults = append(faults, fmt.Sprintf("%s has no failing run and is not exempt", t.Test))
			}
			if s := ran.status[t.Test]; s != evidence.Pass {
				faults = append(faults, fmt.Sprintf("%s is %s now", t.Test, s))
			}
		}
	}
	return faults
}

func hasOrHave(n int) string {
	if n == 1 {
		return "has"
	}
	return "have"
}

// setEvidence sets the evidence of the task with the given id in p, nil for
// none.
func setEvidence(p *afspec.ProgressV3Json, id int, entries []afspec.EvidenceEntry) {
	for i := range p.Tasks {
		if p.Tasks[i].Id == id {
			p.Tasks[i].Evidence = entries
		}
	}
}
