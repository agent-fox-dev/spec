package main

import (
	"fmt"
	"slices"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/evidence"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// executeVerify is the verify form: speca <spec> --verify-evidence. It runs
// every test recorded for every done task once, on the tree as it is, and
// answers with one verdict, as the gate does, in result.evidence_check. It
// writes nothing to the package and reads it whatever its status, a package
// whose contract drifted included; only a package that cannot be read fails
// the call (ADR 26).
func executeVerify(c *call, dir string) (*Result, *cli.ErrorInfo) {
	spec, failure := load(dir)
	if failure != nil {
		return nil, failure
	}
	name := specpkg.Display(dir, c.root)
	check := envelope.EvidenceCheck{Tasks: []envelope.EvidenceCheckTask{}}

	type held struct {
		id      int
		entries []afspec.EvidenceEntry
	}
	var done []held
	var tests []string
	for _, t := range spec.Plan() {
		entries := spec.TaskEvidence(t.Id)
		if t.State != afspec.TaskStateDone || len(entries) == 0 {
			continue
		}
		done = append(done, held{t.Id, entries})
		for _, e := range entries {
			for _, test := range e.Tests {
				if !slices.Contains(tests, test.Test) {
					tests = append(tests, test.Test)
				}
			}
		}
	}

	switch ran, failure := c.verifyRun(name, spec, tests); {
	case len(done) == 0:
		check.Verdict = envelope.GateVerdictNotRun
		check.Reason = fmt.Sprintf("no done task of %s has evidence: there is nothing to check", name)
	case failure != nil:
		check.Verdict, check.Reason = envelope.GateVerdictNotRun, failure.Message
	default:
		for _, w := range ran.warnings {
			c.run.Warn(w.code, w.severity, "%s", w.message)
		}
		check.Verdict, check.Log = envelope.GateVerdictPass, ran.log
		for _, h := range done {
			task := envelope.EvidenceCheckTask{ID: h.id}
			for _, e := range h.entries {
				entry := envelope.EvidenceCheckEntry{Covers: e.Covers}
				for _, test := range e.Tests {
					status := ran.status[test.Test]
					if status != evidence.Pass {
						check.Verdict = envelope.GateVerdictFail
					}
					entry.Tests = append(entry.Tests, envelope.EvidenceCheckTest{Test: test.Test, Status: string(status)})
				}
				task.Evidence = append(task.Evidence, entry)
			}
			check.Tasks = append(check.Tasks, task)
		}
	}
	return &Result{AdminResult: envelope.AdminResult{
		SpecDir: name, SpecID: spec.SpecID, SpecName: spec.SpecName, Status: spec.Status, EvidenceCheck: &check,
	}}, nil
}

// verifyRun runs the tests, none when there are none to run.
func (c *call) verifyRun(name string, spec *afspec.Spec, tests []string) (testRun, *cli.ErrorInfo) {
	if len(tests) == 0 {
		return testRun{}, nil
	}
	return c.runEvidenceTests(name, spec, tests)
}
