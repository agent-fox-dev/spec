package main

import (
	"fmt"
	"math"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/gate"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// gateReport is the form of speca <spec> --gate-report [--since <commit>]: the
// gate runs recorded for the package in dir since the commit, summed. A run
// counts by the commit that was checked out when it ran, not by its time
// (ADR 22). It reads the package whatever its status, and writes nothing.
func gateReport(c *call, dir string) (*Result, *cli.ErrorInfo) {
	spec, failure := load(dir)
	if failure != nil {
		return nil, failure
	}
	top, err := gate.Top(c.root)
	if err != nil {
		return nil, usageInfo(fmt.Errorf("--gate-report counts runs by commit, and %v", err), nil)
	}
	since, refusal := sinceCommit(top, c.opts.since)
	if refusal != nil {
		return nil, refusal
	}
	in, err := gate.Since(top, since)
	if err != nil {
		return nil, &cli.ErrorInfo{Stage: "git", Category: "internal", Message: fmt.Sprintf("the commits since %s could not be listed: %v", since, err)}
	}
	runs, err := gate.ReadRuns(top)
	if err != nil {
		return nil, &cli.ErrorInfo{Stage: "load", Category: "input", Message: fmt.Sprintf("the run log could not be read: %v", err)}
	}
	report := sumRuns(runs, spec.SpecID, in, gateBudget(c, false))
	report.Since = since
	return &Result{AdminResult: envelope.AdminResult{
		SpecDir: specpkg.Display(dir, c.root), SpecID: spec.SpecID, SpecName: spec.SpecName, Status: spec.Status, GateReport: &report,
	}}, nil
}

// sinceCommit is the commit the runs are counted from: --since, or the merge
// base of HEAD and the base branch.
func sinceCommit(top string, since givenFlag) (string, *cli.ErrorInfo) {
	if since.given {
		id, err := gate.Commit(top, since.value)
		if err != nil {
			return "", usageInfo(fmt.Errorf("--since %v", err), nil)
		}
		return id, nil
	}
	base, err := gate.BaseBranch(top)
	if err != nil {
		return "", usageInfo(fmt.Errorf("there is no base branch to count from (%v): give --since <commit>", err), nil)
	}
	id, err := gate.MergeBase(top, base)
	if err != nil {
		return "", usageInfo(fmt.Errorf("%v: give --since <commit>", err), nil)
	}
	return id, nil
}

// sumRuns sums the runs of the package specID that ended in pass or fail at a
// commit in the set, in the order they were logged.
// A last full run that recorded no budget is measured against budget.
func sumRuns(runs []gate.Run, specID string, in map[string]bool, budget int) envelope.GateReport {
	r := envelope.GateReport{Slowest: []envelope.GateUnit{}}
	var lastFull *gate.Run
	for _, run := range runs {
		if run.SpecID != specID || !in[run.Commit] ||
			(run.Verdict != envelope.GateVerdictPass && run.Verdict != envelope.GateVerdictFail) {
			continue
		}
		var counts *envelope.GateRuns
		switch run.Level {
		case envelope.GateLevelFull:
			counts = &r.Full
			if r.BaselineFullSeconds == nil {
				d := run.Duration()
				r.BaselineFullSeconds = &d
			}
			lastFull = &run
		case envelope.GateLevelTask:
			counts = &r.Task
		default:
			continue
		}
		counts.Runs++
		counts.Seconds = math.Round((counts.Seconds+run.Seconds)*1000) / 1000
		if run.Reused != nil {
			counts.Reused++
		}
	}
	if lastFull != nil {
		d := lastFull.Duration()
		r.LastFullSeconds = &d
		if lastFull.Slowest != nil {
			r.Slowest = lastFull.Slowest
		}
		// A run logged before budgets were recorded is measured now.
		b := gate.MeasureBudget(d, budget)
		if lastFull.Budget != nil {
			b = *lastFull.Budget
		}
		cost := gate.TestCost(b, lastFull.Growth)
		r.TestCost = &cost
	}
	return r
}
