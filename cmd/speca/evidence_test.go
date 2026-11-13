package main

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/gate"
)

// evidenceRepo is a repository with one package, 05_ev, that requires
// evidence: task 1 owns REQ-1 (REQ-1.1 to REQ-1.3), task 2 owns REQ-2, and
// the integration task 3 owns PATH-1. Its evidence command is a stub that
// answers for each test it is given the status the test set with answer, and
// notes every test it was asked in a file outside the repository.
type evidenceRepo struct {
	root, dir, bin string
	answers, asked string
}

// newEvidenceRepo lays the repository out, in a git work tree when inGit, with
// the tasks in the given states, and activates the package with its contract
// hash.
func newEvidenceRepo(t *testing.T, inGit bool, states ...string) *evidenceRepo {
	t.Helper()
	r := &evidenceRepo{root: t.TempDir(), bin: t.TempDir()}
	t.Setenv(gate.StateEnv, t.TempDir())
	r.answers, r.asked = filepath.Join(r.bin, "answers"), filepath.Join(r.bin, "asked")
	writeFile(t, r.answers, "")
	r.dir = addPackage(t, r.root, "05_ev", "05", "ev")
	if len(states) == 0 {
		states = []string{"in_progress", "pending", "pending"}
	}
	task := func(id int, criteria string, deps []int) map[string]any {
		m := planTask(id, states[id-1], deps)
		m["criteria"] = []any{criteria}
		return m
	}
	setTasks(t, r.dir, task(1, "REQ-1", nil), task(2, "REQ-2", []int{}), integrationTask(states[2]))
	setEvidenceCommand(t, r.dir, r.stub(t)+" {tests}")
	if inGit {
		gitInit(t, r.root)
	}
	activateWithContract(t, r.dir)
	return r
}

// stub writes the evidence command: for each test it is given it notes the
// test and prints a result line with the status answers holds for it, or none.
func (r *evidenceRepo) stub(t *testing.T) string {
	t.Helper()
	path := filepath.Join(r.bin, "run-tests")
	body := `#!/bin/sh
for a in "$@"; do
  echo "$a" >> '` + r.asked + `'
  s=$(awk -v t="$a" '$1 == t { print $2 }' '` + r.answers + `')
  if [ -n "$s" ]; then printf '{"test":"%s","status":"%s"}\n' "$a" "$s"; fi
done
`
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// answer sets the status the stub gives each test.
func (r *evidenceRepo) answer(t *testing.T, statuses map[string]string) {
	t.Helper()
	var b strings.Builder
	for _, test := range slices.Sorted(maps.Keys(statuses)) {
		b.WriteString(test + " " + statuses[test] + "\n")
	}
	writeFile(t, r.answers, b.String())
}

// askedTests are the tests the stub was asked, in order, since the last
// forget.
func (r *evidenceRepo) askedTests(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(r.asked)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(b))
}

func (r *evidenceRepo) forget(t *testing.T) {
	t.Helper()
	if err := os.Remove(r.asked); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

// entry is one entry of an evidence file.
type entry struct {
	Covers string   `json:"covers"`
	Tests  []string `json:"tests"`
	NoRed  string   `json:"no_red,omitempty"`
}

// file writes an evidence file of the entries outside the repository and
// returns its path.
func (r *evidenceRepo) file(t *testing.T, entries ...entry) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"evidence": entries})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "map.json")
	writeFile(t, path, string(b))
	return path
}

// record runs speca 05 --task <task> --evidence <file> [extra] in the
// repository.
func (r *evidenceRepo) record(t *testing.T, task int, file string, extra ...string) (envelope.AdminEnvelope, int) {
	t.Helper()
	env, code, _ := run(t, append([]string{"--dir", r.root, "05", "--task", strconv.Itoa(task), "--evidence", file}, extra...)...)
	return env, code
}

// evidenceOnDisk is the evidence progress.json holds for the task.
func (r *evidenceRepo) evidenceOnDisk(t *testing.T, task int) []afspec.EvidenceEntry {
	t.Helper()
	return loadSpec(t, r.dir).TaskEvidence(task)
}

// runOf finds the recorded test of the evidence.
func testOf(entries []afspec.EvidenceEntry, covers, test string) *afspec.EvidenceTest {
	for _, e := range entries {
		if e.Covers != covers {
			continue
		}
		for i := range e.Tests {
			if e.Tests[i].Test == test {
				return &e.Tests[i]
			}
		}
	}
	return nil
}

// refusedAtEvidence checks that the call was refused at stage evidence with
// exit 1, and that the package is as it was.
func refusedAtEvidence(t *testing.T, env envelope.AdminEnvelope, code int, before map[string]string, after map[string]string) *envelope.ErrorInfo {
	t.Helper()
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "evidence" {
		t.Fatalf("code %d, error %+v; want exit 1 at stage evidence", code, env.Error)
	}
	if !maps.Equal(before, after) {
		t.Error("the package changed")
	}
	return env.Error
}

// A first call records, for every test, the failing run with its time and the
// tree it ran on, and writes progress.json alone; a second call keeps the
// failing run of a test still listed and drops the tests no longer listed.
func TestEvidence_RecordsTheFailingRunOfEachTest(t *testing.T) {
	r := newEvidenceRepo(t, true)
	r.answer(t, map[string]string{"T1": "fail", "T2": "fail"})
	tree, err := gate.ReadTree(r.root)
	if err != nil {
		t.Fatal(err)
	}
	before := treeBytes(t, r.dir)
	updatedBefore := loadSpec(t, r.dir).UpdatedAt

	env, code := r.record(t, 1, r.file(t, entry{Covers: "REQ-1.1", Tests: []string{"T1"}}, entry{Covers: "REQ-1.2", Tests: []string{"T2"}}))
	if code != cli.ExitOK || env.Error != nil {
		t.Fatalf("code %d, error %+v", code, env.Error)
	}
	ev := r.evidenceOnDisk(t, 1)
	for _, c := range []struct{ covers, test string }{{"REQ-1.1", "T1"}, {"REQ-1.2", "T2"}} {
		got := testOf(ev, c.covers, c.test)
		if got == nil || got.Failed == nil || got.Failed.At == "" || got.Failed.Tree == nil || *got.Failed.Tree != tree.ID || got.Passed != nil {
			t.Errorf("%s %s recorded as %+v; want a failed run on tree %s and no passed run", c.covers, c.test, got, tree.ID)
		}
	}
	after := treeBytes(t, r.dir)
	for name := range before {
		if name != afspec.ProgressFileName && before[name] != after[name] {
			t.Errorf("%s changed; want progress.json alone written", name)
		}
	}
	if loadSpec(t, r.dir).UpdatedAt == updatedBefore {
		t.Error("updated_at was not set")
	}
	res := result(t, env)
	if want := []envelope.Change{{Kind: "evidence", ID: 1}}; !slices.Equal(res.Changes, want) {
		t.Errorf("changes %+v; want %+v", res.Changes, want)
	}
	if len(res.Tasks[0].Evidence) != 2 {
		t.Errorf("the result's task 1 has evidence %+v; want the two entries written", res.Tasks[0].Evidence)
	}
	first := *testOf(ev, "REQ-1.1", "T1").Failed

	r.answer(t, map[string]string{"T1": "pass", "T3": "fail"})
	if env, code := r.record(t, 1, r.file(t, entry{Covers: "REQ-1.1", Tests: []string{"T1", "T3"}})); code != cli.ExitOK {
		t.Fatalf("the second call: code %d, error %+v", code, env.Error)
	}
	ev = r.evidenceOnDisk(t, 1)
	if got := testOf(ev, "REQ-1.1", "T1"); got == nil || got.Failed == nil || got.Failed.At != first.At {
		t.Errorf("T1 recorded as %+v; want its first failed run kept", got)
	}
	if got := testOf(ev, "REQ-1.1", "T3"); got == nil || got.Failed == nil {
		t.Errorf("T3 recorded as %+v; want a failed run", got)
	}
	if len(ev) != 1 || testOf(ev, "REQ-1.2", "T2") != nil {
		t.Errorf("evidence %+v; want T2 and its entry dropped", ev)
	}

	// Outside a git work tree a run has no tree.
	outside := newEvidenceRepo(t, false)
	outside.answer(t, map[string]string{"T1": "fail"})
	if env, code := outside.record(t, 1, outside.file(t, entry{Covers: "REQ-1.1", Tests: []string{"T1"}})); code != cli.ExitOK {
		t.Fatalf("outside git: code %d, error %+v", code, env.Error)
	}
	if got := testOf(outside.evidenceOnDisk(t, 1), "REQ-1.1", "T1"); got == nil || got.Failed == nil || got.Failed.Tree != nil {
		t.Errorf("outside git, T1 recorded as %+v; want a failed run without a tree", got)
	}
}

// A test every entry of which is exempt, and every test of the integration
// task, must pass or fail, and gets no failing run; a test one entry of which
// is not exempt must fail.
func TestEvidence_AsksAnExemptOrSmokeTestOnlyToRun(t *testing.T) {
	for name, tc := range map[string]struct {
		task    int
		entries []entry
		status  string
		refused bool
	}{
		"exempt, passing": {1, []entry{{Covers: "REQ-1.1", Tests: []string{"T1"}, NoRed: "already reported"}}, "pass", false},
		"exempt, failing": {1, []entry{{Covers: "REQ-1.1", Tests: []string{"T1"}, NoRed: "already reported"}}, "fail", false},
		"exempt in one entry only, passing": {1, []entry{
			{Covers: "REQ-1.1", Tests: []string{"T1"}, NoRed: "already reported"}, {Covers: "REQ-1.2", Tests: []string{"T1"}}}, "pass", true},
		"a smoke test of the integration task, passing": {3, []entry{{Covers: "PATH-1", Tests: []string{"T1"}}}, "pass", false},
	} {
		t.Run(name, func(t *testing.T) {
			states := []string{"in_progress", "pending", "pending"}
			if tc.task == 3 {
				states = []string{"done", "done", "in_progress"}
			}
			r := newEvidenceRepo(t, true, states...)
			r.answer(t, map[string]string{"T1": tc.status})
			before := treeBytes(t, r.dir)
			env, code := r.record(t, tc.task, r.file(t, tc.entries...))
			if tc.refused {
				refusedAtEvidence(t, env, code, before, treeBytes(t, r.dir))
				return
			}
			if code != cli.ExitOK {
				t.Fatalf("code %d, error %+v", code, env.Error)
			}
			if got := testOf(r.evidenceOnDisk(t, tc.task), tc.entries[0].Covers, "T1"); got == nil || got.Failed != nil {
				t.Errorf("T1 recorded as %+v; want it recorded without a failed run", got)
			}
		})
	}
}

// A test that had to fail and did not, and an exempt test that neither passed
// nor failed, refuse the call, which names every one of them with its status
// and writes nothing.
func TestEvidence_RefusesATestThatDidNotFailFirst(t *testing.T) {
	for name, tc := range map[string]struct {
		entries []entry
		answers map[string]string
		says    []string
	}{
		"passing":           {[]entry{{Covers: "REQ-1.1", Tests: []string{"T1"}}}, map[string]string{"T1": "pass"}, []string{"T1", "pass", "without the implementation", "no_red"}},
		"not built":         {[]entry{{Covers: "REQ-1.1", Tests: []string{"T1"}}}, map[string]string{"T1": "error"}, []string{"T1", "error", "not a failing test"}},
		"missing":           {[]entry{{Covers: "REQ-1.1", Tests: []string{"T1"}}}, map[string]string{}, []string{"T1", "missing"}},
		"skipped":           {[]entry{{Covers: "REQ-1.1", Tests: []string{"T1"}}}, map[string]string{"T1": "skipped"}, []string{"T1", "skipped"}},
		"exempt, not built": {[]entry{{Covers: "REQ-1.1", Tests: []string{"T1"}, NoRed: "x"}}, map[string]string{"T1": "error"}, []string{"T1", "error"}},
		"exempt, missing":   {[]entry{{Covers: "REQ-1.1", Tests: []string{"T1"}, NoRed: "x"}}, map[string]string{}, []string{"T1", "missing"}},
		"exempt, skipped":   {[]entry{{Covers: "REQ-1.1", Tests: []string{"T1"}, NoRed: "x"}}, map[string]string{"T1": "skipped"}, []string{"T1", "skipped"}},
		"two at once": {[]entry{{Covers: "REQ-1.1", Tests: []string{"T1", "T2", "T3"}}}, map[string]string{"T1": "pass", "T2": "fail", "T3": "error"},
			[]string{"T1", "pass", "T3", "error"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := newEvidenceRepo(t, true)
			r.answer(t, tc.answers)
			before := treeBytes(t, r.dir)
			env, code := r.record(t, 1, r.file(t, tc.entries...))
			e := refusedAtEvidence(t, env, code, before, treeBytes(t, r.dir))
			saysAll(t, []string{name}, e.Message, tc.says...)
			if strings.Contains(e.Message, "T2") {
				t.Errorf("message %q names T2, which failed as it had to", e.Message)
			}
		})
	}
}

// A covers the task does not own, and a covers given twice, refuse the call,
// which names every one of them before any test runs.
func TestEvidence_RefusesACoversTheTaskDoesNotOwn(t *testing.T) {
	for name, tc := range map[string]struct {
		entries []entry
		names   []string
	}{
		"owned by task 2 only":        {[]entry{{Covers: "REQ-2.1", Tests: []string{"T1"}}}, []string{"REQ-2.1"}},
		"no such criterion":           {[]entry{{Covers: "REQ-9.9", Tests: []string{"T1"}}}, []string{"REQ-9.9"}},
		"a path on an implement task": {[]entry{{Covers: "PATH-1", Tests: []string{"T1"}}}, []string{"PATH-1"}},
		"given twice":                 {[]entry{{Covers: "REQ-1.1", Tests: []string{"T1"}}, {Covers: "REQ-1.1", Tests: []string{"T2"}}}, []string{"REQ-1.1"}},
		"two at once":                 {[]entry{{Covers: "REQ-2.1", Tests: []string{"T1"}}, {Covers: "REQ-9.9", Tests: []string{"T2"}}}, []string{"REQ-2.1", "REQ-9.9"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := newEvidenceRepo(t, true)
			r.answer(t, map[string]string{"T1": "fail", "T2": "fail"})
			before := treeBytes(t, r.dir)
			env, code := r.record(t, 1, r.file(t, tc.entries...))
			e := refusedAtEvidence(t, env, code, before, treeBytes(t, r.dir))
			saysAll(t, []string{name}, e.Message, tc.names...)
			if asked := r.askedTests(t); len(asked) != 0 {
				t.Errorf("the tests %v ran; want none", asked)
			}
		})
	}
}

// A call that cannot hold, or whose file is not an evidence file, is a usage
// error, and nothing runs.
func TestEvidence_RefusesAMalformedCallBeforeReadingThePackage(t *testing.T) {
	r := newEvidenceRepo(t, true)
	good := r.file(t, entry{Covers: "REQ-1.1", Tests: []string{"T1"}})
	raw := func(content string) string {
		path := filepath.Join(t.TempDir(), "map.json")
		writeFile(t, path, content)
		return path
	}
	tests := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "T" + strconv.Itoa(i)
		}
		return out
	}
	many := make([]entry, 11)
	for i := range many {
		many[i] = entry{Covers: "REQ-1.1", Tests: tests(50)}
		for j := range many[i].Tests {
			many[i].Tests[j] = "E" + strconv.Itoa(i*50+j)
		}
	}
	for name, argv := range map[string][]string{
		"without --task":          {"05", "--evidence", good},
		"with --state":            {"05", "--task", "1", "--state", "in_progress", "--evidence", good},
		"two tasks":               {"05", "--task", "1,2", "--evidence", good},
		"next":                    {"05", "--task", "next", "--evidence", good},
		"all":                     {"05", "--task", "all", "--evidence", good},
		"no file":                 {"05", "--task", "1", "--evidence", filepath.Join(t.TempDir(), "none.json")},
		"too large":               {"05", "--task", "1", "--evidence", raw(`{"evidence":[]}` + strings.Repeat(" ", 1<<20))},
		"not JSON":                {"05", "--task", "1", "--evidence", raw("evidence: []")},
		"an unknown key":          {"05", "--task", "1", "--evidence", raw(`{"evidence":[{"covers":"REQ-1.1","tests":["T1"],"why":"x"}]}`)},
		"no evidence key":         {"05", "--task", "1", "--evidence", raw(`{}`)},
		"an entry without covers": {"05", "--task", "1", "--evidence", raw(`{"evidence":[{"tests":["T1"]}]}`)},
		"no tests":                {"05", "--task", "1", "--evidence", r.file(t, entry{Covers: "REQ-1.1", Tests: []string{}})},
		"51 tests":                {"05", "--task", "1", "--evidence", r.file(t, entry{Covers: "REQ-1.1", Tests: tests(51)})},
		"an empty identifier":     {"05", "--task", "1", "--evidence", r.file(t, entry{Covers: "REQ-1.1", Tests: []string{""}})},
		"an identifier of 501":    {"05", "--task", "1", "--evidence", r.file(t, entry{Covers: "REQ-1.1", Tests: []string{strings.Repeat("a", 501)}})},
		"a line break":            {"05", "--task", "1", "--evidence", r.file(t, entry{Covers: "REQ-1.1", Tests: []string{"a\nb"}})},
		"an empty no_red":         {"05", "--task", "1", "--evidence", raw(`{"evidence":[{"covers":"REQ-1.1","tests":["T1"],"no_red":""}]}`)},
		"a no_red of 501":         {"05", "--task", "1", "--evidence", r.file(t, entry{Covers: "REQ-1.1", Tests: []string{"T1"}, NoRed: strings.Repeat("a", 501)})},
		"501 distinct tests":      {"05", "--task", "1", "--evidence", r.file(t, many...)},
		"two JSON values":         {"05", "--task", "1", "--evidence", raw(`{"evidence":[]} {}`)},
	} {
		t.Run(name, func(t *testing.T) {
			env, code, _ := run(t, append([]string{"--dir", r.root}, argv...)...)
			if code != cli.ExitUsage || env.Error == nil || env.Error.Category != envelope.CategoryUsage {
				t.Fatalf("code %d, error %+v; want a usage error", code, env.Error)
			}
			if asked := r.askedTests(t); len(asked) != 0 {
				t.Errorf("the tests %v ran; want none", asked)
			}
		})
	}
}

// A package that does not require evidence, and a task that is not in
// progress, take no evidence: the refusal says why, and nothing runs.
func TestEvidence_RefusesAPackageOrTaskThatTakesNoEvidence(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(t *testing.T, r *evidenceRepo)
		says  string
	}{
		"a draft": {func(t *testing.T, r *evidenceRepo) {
			editFile(t, filepath.Join(r.dir, "prd.md"), `status: "active"`, `status: "draft"`)
		}, "draft"},
		"an active package without contract_hash": {func(t *testing.T, r *evidenceRepo) {
			spec := loadSpec(t, r.dir)
			spec.ContractHash = nil
			if err := spec.Save(r.dir); err != nil {
				t.Fatal(err)
			}
		}, "contract_hash"},
		"a pending task": {func(t *testing.T, r *evidenceRepo) {
			setTaskState(t, r.dir, 1, "pending")
		}, "in_progress"},
		"a done task": {func(t *testing.T, r *evidenceRepo) {
			setTaskState(t, r.dir, 1, "done")
		}, "in_progress"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newEvidenceRepo(t, true)
			r.answer(t, map[string]string{"T1": "fail"})
			tc.setup(t, r)
			before := treeBytes(t, r.dir)
			env, code := r.record(t, 1, r.file(t, entry{Covers: "REQ-1.1", Tests: []string{"T1"}}))
			e := refusedAtEvidence(t, env, code, before, treeBytes(t, r.dir))
			saysAll(t, []string{name}, e.Message, tc.says)
			if asked := r.askedTests(t); len(asked) != 0 {
				t.Errorf("the tests %v ran; want none", asked)
			}
		})
	}
}

// --dry-run runs the tests and every check and writes nothing: its result is
// the real call's, and so is its refusal.
func TestEvidence_ADryRunWritesNothing(t *testing.T) {
	r := newEvidenceRepo(t, true)
	r.answer(t, map[string]string{"T1": "fail"})
	before := treeBytes(t, r.dir)
	file := r.file(t, entry{Covers: "REQ-1.1", Tests: []string{"T1"}})

	env, code := r.record(t, 1, file, "--dry-run")
	if code != cli.ExitOK || !result(t, env).DryRun {
		t.Fatalf("code %d, error %+v; want a dry run", code, env.Error)
	}
	if !maps.Equal(before, treeBytes(t, r.dir)) {
		t.Error("the dry run wrote to the package")
	}
	got := result(t, env).Tasks[0].Evidence
	if len(got) != 1 || got[0].Covers != "REQ-1.1" || got[0].Tests[0].Failed == nil {
		t.Errorf("the dry run reported %+v; want T1 with the failed run the call would record", got)
	}
	if asked := r.askedTests(t); !slices.Equal(asked, []string{"T1"}) {
		t.Errorf("the dry run asked %v; want T1 run", asked)
	}

	r.answer(t, map[string]string{"T1": "pass"})
	dry, dryCode := r.record(t, 1, file, "--dry-run")
	real, realCode := r.record(t, 1, file)
	if dryCode != cli.ExitFailed || realCode != cli.ExitFailed || dry.Error.Message != real.Error.Message {
		t.Errorf("the dry run's refusal %+v; want the real call's, %+v", dry.Error, real.Error)
	}
}

// Where the state directory cannot be written, the command runs without a
// log, the result names none, and a low warning says so.
func TestEvidence_RunsWithoutALogWhereTheStateDirectoryCannotBeWritten(t *testing.T) {
	r := newEvidenceRepo(t, true)
	blocked := filepath.Join(t.TempDir(), "a-file")
	writeFile(t, blocked, "")
	t.Setenv(gate.StateEnv, blocked)
	r.answer(t, map[string]string{"T1": "fail"})

	env, code := r.record(t, 1, r.file(t, entry{Covers: "REQ-1.1", Tests: []string{"T1"}}))
	if code != cli.ExitOK {
		t.Fatalf("code %d, error %+v", code, env.Error)
	}
	if got := testOf(r.evidenceOnDisk(t, 1), "REQ-1.1", "T1"); got == nil || got.Failed == nil {
		t.Errorf("T1 recorded as %+v; want a failed run", got)
	}
	if log := result(t, env).EvidenceLog; log != "" {
		t.Errorf("the result names the log %q; want none", log)
	}
	var found bool
	for _, w := range env.Warnings {
		found = found || (w.Code == "evidence_log_not_written" && w.Severity == "low")
	}
	if !found {
		t.Errorf("warnings %+v; want a low evidence_log_not_written", env.Warnings)
	}
}

// An evidence command still running at --timeout is ended with every process
// it started, and the call fails, writing nothing.
func TestEvidence_EndsACommandAndItsProcessesAtTheTimeout(t *testing.T) {
	r := newEvidenceRepo(t, true)
	pids := filepath.Join(r.bin, "pids")
	slow := filepath.Join(r.bin, "slow")
	writeFile(t, slow, "#!/bin/sh\nsleep 60 & echo $$ $! > '"+pids+"'; wait\n")
	if err := os.Chmod(slow, 0o755); err != nil {
		t.Fatal(err)
	}
	// The command is part of the contract, so the package is activated again
	// with it.
	spec := loadSpec(t, r.dir)
	cmd := slow + " {tests}"
	spec.Tasks.TestCommands.EvidenceTests = &cmd
	spec.ContractHash, _ = spec.ComputeContractHash()
	if err := spec.Save(r.dir); err != nil {
		t.Fatal(err)
	}
	before := treeBytes(t, r.dir)

	start := time.Now()
	env, code := r.record(t, 1, r.file(t, entry{Covers: "REQ-1.1", Tests: []string{"T1"}}), "--timeout", "300ms")
	if took := time.Since(start); took > 20*time.Second {
		t.Errorf("the call took %v", took)
	}
	e := refusedAtEvidence(t, env, code, before, treeBytes(t, r.dir))
	if e.Category != "evidence" || !strings.Contains(e.Message, "timed out") {
		t.Errorf("error %+v; want category evidence, saying it timed out", e)
	}
	b, err := os.ReadFile(pids)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range strings.Fields(string(b)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			t.Fatal(err)
		}
		alive := func() bool { return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }
		for deadline := time.Now().Add(5 * time.Second); alive() && time.Now().Before(deadline); {
			time.Sleep(20 * time.Millisecond)
		}
		if alive() {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("process %d the command started is still running", pid)
		}
	}
}

// setCommand sets the package's evidence command, nil for none, and activates
// it again with that contract.
func (r *evidenceRepo) setCommand(t *testing.T, command *string) {
	t.Helper()
	spec := loadSpec(t, r.dir)
	spec.Tasks.TestCommands.EvidenceTests = command
	spec.ContractHash, _ = spec.ComputeContractHash()
	if err := spec.Save(r.dir); err != nil {
		t.Fatal(err)
	}
}

// When the evidence command cannot be started, neither the evidence form nor
// a close goes ahead: the refusal names why, and nothing is written.
func TestEvidence_RefusesWhenTheTestsCannotRun(t *testing.T) {
	missing, open := "no-such-program-xyz {tests}", `run-tests "open {tests}`
	for name, tc := range map[string]struct {
		command *string
		says    []string
	}{
		"no evidence_tests and no go.mod": {nil, []string{"evidence_tests", "tasks.json"}},
		"a program not found":             {&missing, []string{"no-such-program-xyz", "could not start"}},
		"a command that cannot be split":  {&open, []string{`run-tests \"open {tests}`, "quote"}},
	} {
		for _, call := range []string{"--evidence", "--state done"} {
			t.Run(name+", "+call, func(t *testing.T) {
				r := newEvidenceRepo(t, true)
				r.recordAll(t)
				r.answer(t, map[string]string{"T1": "pass", "T2": "pass", "T3": "pass"})
				r.setCommand(t, tc.command)
				before := treeBytes(t, r.dir)
				var env envelope.AdminEnvelope
				var code int
				if call == "--evidence" {
					env, code = r.record(t, 1, r.file(t, entry{Covers: "REQ-1.1", Tests: []string{"T1"}}))
				} else {
					env, code = r.move(t, "1", "done")
				}
				e := refusedAtEvidence(t, env, code, before, treeBytes(t, r.dir))
				if e.Category != "evidence" {
					t.Errorf("category %q; want evidence", e.Category)
				}
				saysAll(t, []string{name}, e.Message, tc.says...)
			})
		}
	}
}
