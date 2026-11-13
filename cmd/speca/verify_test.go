package main

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// doneWithEvidence lays out the repository of the verify form's tests: tasks 1
// and 2 done on their evidence (T1 to T3, then T4 and T5), and the integration
// task in progress with its own (S1).
func doneWithEvidence(t *testing.T) *evidenceRepo {
	t.Helper()
	r := newEvidenceRepo(t, true)
	r.recordAll(t)
	r.answer(t, map[string]string{"T1": "pass", "T2": "pass", "T3": "pass"})
	steps := []func() (envelope.AdminEnvelope, int){
		func() (envelope.AdminEnvelope, int) { return r.move(t, "1", "done") },
		func() (envelope.AdminEnvelope, int) { return r.move(t, "2", "in_progress") },
		func() (envelope.AdminEnvelope, int) {
			r.answer(t, map[string]string{"T4": "fail", "T5": "fail"})
			return r.record(t, 2, r.file(t, entry{Covers: "REQ-2.1", Tests: []string{"T4"}}, entry{Covers: "REQ-2.2", Tests: []string{"T5"}}))
		},
		func() (envelope.AdminEnvelope, int) {
			r.answer(t, map[string]string{"T4": "pass", "T5": "pass"})
			return r.move(t, "2", "done")
		},
		func() (envelope.AdminEnvelope, int) { return r.move(t, "3", "in_progress") },
		func() (envelope.AdminEnvelope, int) {
			r.answer(t, map[string]string{"S1": "pass"})
			return r.record(t, 3, r.file(t, entry{Covers: "PATH-1", Tests: []string{"S1"}}))
		},
	}
	for i, step := range steps {
		if env, code := step(); code != cli.ExitOK {
			t.Fatalf("step %d: code %d, error %+v", i+1, code, env.Error)
		}
	}
	r.forget(t)
	return r
}

func (r *evidenceRepo) verify(t *testing.T, extra ...string) (envelope.AdminEnvelope, int) {
	t.Helper()
	env, code, _ := run(t, append([]string{"--dir", r.root, "05", "--verify-evidence"}, extra...)...)
	return env, code
}

// The verify form runs every test recorded for every done task once, on the
// tree as it is, and answers with one verdict and each test's status, writing
// nothing to the package.
func TestVerifyEvidence_RunsEveryRecordedTestOfEveryDoneTask(t *testing.T) {
	r := doneWithEvidence(t)
	all := map[string]string{"T1": "pass", "T2": "pass", "T3": "pass", "T4": "pass", "T5": "pass", "S1": "pass"}
	r.answer(t, all)
	before := treeBytes(t, r.dir)

	env, code := r.verify(t)
	check := result(t, env).EvidenceCheck
	if code != cli.ExitOK || check == nil || check.Verdict != "pass" {
		t.Fatalf("code %d, error %+v, evidence_check %+v; want pass", code, env.Error, check)
	}
	if len(check.Tasks) != 2 || check.Tasks[0].ID != 1 || check.Tasks[1].ID != 2 {
		t.Fatalf("tasks %+v; want the two done tasks", check.Tasks)
	}
	for _, task := range check.Tasks {
		for _, e := range task.Evidence {
			for _, test := range e.Tests {
				if test.Status != "pass" {
					t.Errorf("task %d %s %s: %s; want pass", task.ID, e.Covers, test.Test, test.Status)
				}
			}
		}
	}
	if covers := check.Tasks[1].Evidence; len(covers) != 2 || covers[0].Covers != "REQ-2.1" || covers[0].Tests[0].Test != "T4" {
		t.Errorf("task 2's entries %+v; want REQ-2.1 with T4, then REQ-2.2", covers)
	}
	asked := r.askedTests(t)
	slices.Sort(asked)
	if want := []string{"T1", "T2", "T3", "T4", "T5"}; !slices.Equal(asked, want) {
		t.Errorf("the tests run %v; want each of the done tasks' once, and not S1", asked)
	}

	all["T5"] = "fail"
	r.answer(t, all)
	env, code = r.verify(t)
	check = result(t, env).EvidenceCheck
	if code != cli.ExitOK || check.Verdict != "fail" || check.Tasks[1].Evidence[1].Tests[0].Status != "fail" {
		t.Errorf("code %d, evidence_check %+v; want fail, T5 fail", code, check)
	}
	if !maps.Equal(before, treeBytes(t, r.dir)) {
		t.Error("the verify form wrote to the package")
	}
}

// When nothing can be checked, the verdict is not_run, with the reason, and
// the call is still a success.
func TestVerifyEvidence_AnswersNotRunWhenNothingCanBeChecked(t *testing.T) {
	missing, open := "no-such-program-xyz {tests}", `run-tests "open {tests}`
	for name, tc := range map[string]struct {
		setup func(t *testing.T) (*evidenceRepo, []string)
		says  []string
	}{
		"no done task has evidence": {func(t *testing.T) (*evidenceRepo, []string) {
			return newEvidenceRepo(t, true), nil
		}, []string{"no done task"}},
		"no evidence_tests and no go.mod": {func(t *testing.T) (*evidenceRepo, []string) {
			r := doneWithEvidence(t)
			r.setCommand(t, nil)
			return r, nil
		}, []string{"evidence_tests", "go.mod"}},
		"a program not found": {func(t *testing.T) (*evidenceRepo, []string) {
			r := doneWithEvidence(t)
			r.setCommand(t, &missing)
			return r, nil
		}, []string{"no-such-program-xyz", "could not start"}},
		"a command that cannot be split": {func(t *testing.T) (*evidenceRepo, []string) {
			r := doneWithEvidence(t)
			r.setCommand(t, &open)
			return r, nil
		}, []string{"quote"}},
		"a command past --timeout": {func(t *testing.T) (*evidenceRepo, []string) {
			r := doneWithEvidence(t)
			slow := filepath.Join(r.bin, "slow")
			writeFile(t, slow, "#!/bin/sh\nsleep 60\n")
			if err := os.Chmod(slow, 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := slow + " {tests}"
			r.setCommand(t, &cmd)
			return r, []string{"--timeout", "300ms"}
		}, []string{"timed out"}},
	} {
		t.Run(name, func(t *testing.T) {
			r, extra := tc.setup(t)
			before := treeBytes(t, r.dir)
			env, code := r.verify(t, extra...)
			check := result(t, env).EvidenceCheck
			if code != cli.ExitOK || env.Error != nil || check == nil || check.Verdict != "not_run" {
				t.Fatalf("code %d, error %+v, evidence_check %+v; want not_run", code, env.Error, check)
			}
			saysAll(t, []string{name}, check.Reason, tc.says...)
			if !maps.Equal(before, treeBytes(t, r.dir)) {
				t.Error("the verify form wrote to the package")
			}
			if strings.TrimSpace(env.Summary) == "" {
				t.Error("no summary")
			}
		})
	}
}
