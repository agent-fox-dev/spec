package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/evidence"
	"github.com/agent-fox-dev/spec/internal/gate"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// The limits of an evidence file.
const (
	maxEvidenceFileBytes = 1 << 20
	maxTestsPerEntry     = 50
	maxTestsPerFile      = 500
	maxIdentifierChars   = 500
	maxNoRedChars        = 500
)

// fileEntry is one entry of an evidence file as it is read: a criterion (for
// the integration task, a path), its tests and, for an exemption, the reason.
type fileEntry struct {
	Covers *string  `json:"covers"`
	Tests  []string `json:"tests"`
	NoRed  *string  `json:"no_red"`
}

// readEvidenceFile reads and checks the evidence file at path before anything
// of the package is read. Anything wrong with it is a usage error, naming
// every fault found.
func readEvidenceFile(path string) ([]fileEntry, *cli.ErrorInfo) {
	usage := func(format string, args ...any) *cli.ErrorInfo {
		return usageInfo(fmt.Errorf("--evidence %s: "+format, append([]any{path}, args...)...), nil)
	}
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return nil, usage("cannot be read: %v", err)
	case info.IsDir():
		return nil, usage("is a directory, not an evidence file")
	case info.Size() > maxEvidenceFileBytes:
		return nil, usage("is %d bytes, and an evidence file is at most %d (1 MiB)", info.Size(), maxEvidenceFileBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, usage("cannot be read: %v", err)
	}
	var file struct {
		Evidence *[]fileEntry `json:"evidence"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return nil, usage("is not an evidence file, the JSON object {\"evidence\": [{\"covers\", \"tests\", \"no_red\"}]}: %v", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, usage("holds more than the one JSON object of an evidence file")
	}
	if file.Evidence == nil {
		return nil, usage("has no evidence list: an evidence file is {\"evidence\": [{\"covers\", \"tests\", \"no_red\"}]}")
	}
	var faults []string
	distinct := map[string]bool{}
	for i, e := range *file.Evidence {
		at := fmt.Sprintf("entry %d", i+1)
		if e.Covers == nil || *e.Covers == "" {
			faults = append(faults, at+" has no covers: the id of the criterion (or path) its tests cover")
		}
		if n := len(e.Tests); n < 1 || n > maxTestsPerEntry {
			faults = append(faults, fmt.Sprintf("%s has %d tests, and an entry has 1 to %d", at, n, maxTestsPerEntry))
		}
		for _, test := range e.Tests {
			switch {
			case test == "" || len([]rune(test)) > maxIdentifierChars:
				faults = append(faults, fmt.Sprintf("%s has a test identifier of %d characters, and one has 1 to %d", at, len([]rune(test)), maxIdentifierChars))
			case strings.ContainsAny(test, "\r\n"):
				faults = append(faults, fmt.Sprintf("%s has a test identifier with a line break, %q", at, test))
			}
			distinct[test] = true
		}
		if e.NoRed != nil && (*e.NoRed == "" || len([]rune(*e.NoRed)) > maxNoRedChars) {
			faults = append(faults, fmt.Sprintf("%s has a no_red of %d characters, and a reason has 1 to %d", at, len([]rune(*e.NoRed)), maxNoRedChars))
		}
	}
	if len(distinct) > maxTestsPerFile {
		faults = append(faults, fmt.Sprintf("it names %d distinct tests, and an evidence file names at most %d", len(distinct), maxTestsPerFile))
	}
	if len(faults) > 0 {
		return nil, usage("%s", strings.Join(faults, "; "))
	}
	return *file.Evidence, nil
}

// executeEvidence is the evidence form: speca <spec> --task N --evidence
// <file>. It runs every test of the file once, and records each test's
// failing run, the one that has to happen before the task's implementation
// exists (ADR 26). The file is read first, then the package; a call that is
// refused writes nothing.
func executeEvidence(c *call, dir string) (*Result, *cli.ErrorInfo) {
	entries, failure := readEvidenceFile(c.opts.evidence.value)
	if failure != nil {
		return nil, failure
	}
	spec, failure := load(dir)
	if failure != nil {
		return nil, failure
	}
	name := specpkg.Display(dir, c.root)
	if specpkg.Legacy(spec) {
		return nil, usageInfo(fmt.Errorf("%s is format version %d, and speca records evidence for format version %d only",
			name, spec.SchemaVersion, afspec.SchemaVersion), nil)
	}
	if refusal := unwritable(name, spec); refusal != nil {
		return c.refused(dir, spec, refusal)
	}
	wanted, _ := parseTaskSpec(c.opts.tasks) // checked with the flags: one id
	if refusal := checkNamedTasks(name, spec, wanted); refusal != nil {
		return nil, refusal
	}
	id := wanted.ids[0]
	task, _ := spec.GetTask(id)
	if refusal := takesEvidence(name, spec, task); refusal != nil {
		return c.refused(dir, spec, refusal)
	}
	if refusal := checkCovers(name, spec, task, entries); refusal != nil {
		return c.refused(dir, spec, refusal)
	}
	if unknown := progressUnknownKeys(spec.Validate().Errors); len(unknown) > 0 {
		return c.refused(dir, spec, &cli.ErrorInfo{Stage: "validate", Category: envelope.CategoryInvalidSpec,
			Message: fmt.Sprintf("%s holds keys the schema does not allow, and a save would delete them: %s; remove them yourself", name, strings.Join(unknown, ", "))})
	}

	var tests []string
	for _, e := range entries {
		for _, t := range e.Tests {
			if !slices.Contains(tests, t) {
				tests = append(tests, t)
			}
		}
	}
	ran, failure := c.runEvidenceTests(name, spec, tests)
	if failure != nil {
		return c.refused(dir, spec, failure)
	}

	recorded, refusal := judgeFirstRun(name, spec, task, entries, ran)
	if refusal != nil {
		return c.refused(dir, spec, refusal)
	}
	spec.Progress = spec.WithTaskEvidence(id, recorded)
	spec.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	spec.Progress.UpdatedAt = spec.UpdatedAt
	validity := validateWritten(spec, afspec.ProgressFileName)
	if !c.opts.dryRun {
		if failure := writeProgress(spec, name); failure != nil {
			return c.onDisk(dir, failure)
		}
	}
	for _, w := range ran.warnings {
		c.run.Warn(w.code, w.severity, "%s", w.message)
	}
	r := c.reportWith(dir, spec, validity)
	r.evidenceForm, r.DryRun, r.EvidenceLog = true, c.opts.dryRun, ran.log
	r.Changes = []envelope.Change{{Kind: "evidence", ID: id}}
	if c.opts.dryRun {
		r.nextSet = true
		r.callNext = []cli.Next{{Tool: tool, Input: r.SpecDir, Flags: []string{"--task", strconv.Itoa(id), "--evidence", c.opts.evidence.value},
			Why: "the same call without --dry-run, which records this evidence"}}
	}
	return r, nil
}

// takesEvidence refuses a package that does not require evidence, and a task
// that is not in progress, saying why.
func takesEvidence(name string, spec *afspec.Spec, task *afspec.PlannedTask) *cli.ErrorInfo {
	refuse := func(format string, args ...any) *cli.ErrorInfo {
		return &cli.ErrorInfo{Stage: "evidence", Category: envelope.CategoryEvidence, Message: fmt.Sprintf(format, args...)}
	}
	switch {
	case spec.Status == string(afspec.PrdFrontmatterV3JsonStatusDraft):
		return refuse("%s is a draft: evidence is recorded for the tasks of an active package activated with a contract_hash, so activate it first", name)
	case !spec.RequiresEvidence():
		return refuse("%s has no contract_hash: it was activated before evidence was recorded, and its tasks close as they did without it", name)
	case task.State != afspec.TaskStateInProgress:
		return refuse("%s: task %d is %s, and evidence is recorded for a task in progress, once its tests are written and before it is implemented "+
			"(claim it with --task %d --state in_progress)", name, task.Id, task.State, task.Id)
	}
	return nil
}

// checkCovers refuses an evidence file one of whose covers the task does not
// own, or names in two entries, naming every such covers.
func checkCovers(name string, spec *afspec.Spec, task *afspec.PlannedTask, entries []fileEntry) *cli.ErrorInfo {
	owned := spec.Owned(task.Id)
	var faults []string
	seen := map[string]int{}
	for _, e := range entries {
		seen[*e.Covers]++
		switch {
		case seen[*e.Covers] == 2:
			faults = append(faults, fmt.Sprintf("%s is given in more than one entry", *e.Covers))
		case seen[*e.Covers] == 1 && !slices.Contains(owned, *e.Covers):
			faults = append(faults, fmt.Sprintf("task %d does not own %s", task.Id, *e.Covers))
		}
	}
	if len(faults) == 0 {
		return nil
	}
	return &cli.ErrorInfo{Stage: "evidence", Category: envelope.CategoryEvidence,
		Message: fmt.Sprintf("%s: the evidence of task %d is refused, and nothing was written: %s (it owns %s)",
			name, task.Id, strings.Join(faults, "; "), strings.Join(owned, ", "))}
}

// testRun is what one run of a package's tests found, and what it would say
// if the call goes ahead.
type testRun struct {
	status   map[string]evidence.Status
	at       string
	tree     *string
	log      string
	warnings []plannedWarning
}

// runEvidenceTests runs the tests once through the package's evidence command,
// or the built-in Go runner, in --dir, under --timeout, with its log in the
// gate's state directory. It reads the tree id first, since the tests may
// change the tree. A run that could not reach its end is the failure.
func (c *call) runEvidenceTests(name string, spec *afspec.Spec, tests []string) (testRun, *cli.ErrorInfo) {
	run := testRun{at: time.Now().UTC().Format(time.RFC3339)}
	repo := c.root
	if tree, err := gate.ReadTree(c.root); err == nil {
		repo, run.tree = tree.Top, &tree.ID
	}
	runner := evidence.Runner{Dir: c.root, Timeout: c.opts.timeout.value}
	if spec.Tasks != nil && spec.Tasks.TestCommands.EvidenceTests != nil {
		runner.Command = *spec.Tasks.TestCommands.EvidenceTests
	}
	if store, err := gate.OpenStore(repo); err == nil {
		runner.LogDir = store.LogDir()
	} else {
		run.warnings = append(run.warnings, plannedWarning{cli.WarnEvidenceLogNotWritten, "low", fmt.Sprintf(
			"the state directory could not be written (%v): the tests ran, and no log of them was kept", err)})
	}
	res, err := runner.Run(tests)
	if err != nil {
		return testRun{}, &cli.ErrorInfo{Stage: "evidence", Category: envelope.CategoryEvidence,
			Message: fmt.Sprintf("%s: the tests could not be run, and nothing was written: %v", name, err)}
	}
	run.status, run.log = res.Status, res.Log
	return run, nil
}

// judgeFirstRun applies the rules of the evidence form to what the run found,
// and returns the task's evidence as the file and the run make it. A test
// that already has a failing run keeps it, whatever its status now. A test
// every entry of which is exempt, and every test of the integration task,
// must pass or fail and gets no failing run. Every other test must fail, and
// the run is recorded as its failing run. Every test that breaks its rule is
// named in the refusal.
func judgeFirstRun(name string, spec *afspec.Spec, task *afspec.PlannedTask, entries []fileEntry, ran testRun) ([]afspec.EvidenceEntry, *cli.ErrorInfo) {
	failedBefore := map[string]*afspec.EvidenceRun{}
	for _, e := range spec.TaskEvidence(task.Id) {
		for _, t := range e.Tests {
			if t.Failed != nil {
				failedBefore[t.Test] = t.Failed
			}
		}
	}
	exempt := map[string]bool{}
	for _, e := range entries {
		for _, t := range e.Tests {
			listed, ok := exempt[t]
			exempt[t] = (listed || !ok) && e.NoRed != nil
		}
	}
	integration := task.Kind == afspec.TaskKindIntegration

	var faults []string
	failed := map[string]*afspec.EvidenceRun{}
	judged := map[string]bool{}
	for _, e := range entries {
		for _, t := range e.Tests {
			if judged[t] {
				continue
			}
			judged[t] = true
			status := ran.status[t]
			switch {
			case failedBefore[t] != nil:
				failed[t] = failedBefore[t]
			case exempt[t] || integration:
				if status != evidence.Pass && status != evidence.Fail {
					faults = append(faults, statusFault(t, status)+" (a test that need not fail first must still pass or fail)")
				}
			case status == evidence.Fail:
				failed[t] = &afspec.EvidenceRun{At: ran.at, Tree: ran.tree}
			default:
				faults = append(faults, statusFault(t, status))
			}
		}
	}
	if len(faults) > 0 {
		return nil, &cli.ErrorInfo{Stage: "evidence", Category: envelope.CategoryEvidence,
			Message: fmt.Sprintf("%s: the evidence of task %d is refused, and nothing was written: %s", name, task.Id, strings.Join(faults, "; "))}
	}
	out := make([]afspec.EvidenceEntry, len(entries))
	for i, e := range entries {
		out[i] = afspec.EvidenceEntry{Covers: *e.Covers, NoRed: e.NoRed}
		for _, t := range e.Tests {
			out[i].Tests = append(out[i].Tests, afspec.EvidenceTest{Test: t, Failed: failed[t]})
		}
	}
	return out, nil
}

// statusFault says what is wrong with a test that should have failed and has
// the status it has.
func statusFault(test string, status evidence.Status) string {
	switch status {
	case evidence.Pass:
		return fmt.Sprintf("%s is pass: it passes without the implementation; if the behaviour already existed, give its entry no_red with the reason", test)
	case evidence.Error:
		return fmt.Sprintf("%s is error: a build or collection failure is not a failing test", test)
	}
	return fmt.Sprintf("%s is %s", test, status)
}
