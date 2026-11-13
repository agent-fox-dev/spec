package specrun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/agent-fox-dev/aflib/envelope"
	"github.com/agent-fox-dev/aflib/issuex"
)

// example is the worked example of format v3 the fixture's packages copy, by
// an absolute path.
var example, _ = filepath.Abs(filepath.Join("..", "..", "afspec", "testdata", "v3_example"))

// fakeClaude is the fake claude, built once per run of the package's tests.
var fakeClaude struct {
	once sync.Once
	dir  string
	err  error
}

// buildFakeClaude builds the fake claude into a directory of its own.
func buildFakeClaude() {
	if fakeClaude.dir, fakeClaude.err = os.MkdirTemp("", "specrun-fake-claude"); fakeClaude.err != nil {
		return
	}
	out, err := exec.Command("go", "build", "-o", filepath.Join(fakeClaude.dir, "claude"),
		"github.com/agent-fox-dev/spec/internal/specrun/testdata/fakeclaude").CombinedOutput()
	if err != nil {
		fakeClaude.err = fmt.Errorf("building the fake claude: %v\n%s", err, out)
	}
}

// fakeBin is the directory of the fake claude, which TestMain built.
func fakeBin(t *testing.T) string {
	t.Helper()
	if fakeClaude.err != nil || fakeClaude.dir == "" {
		t.Fatalf("the fake claude was not built: %v", fakeClaude.err)
	}
	return fakeClaude.dir
}

// TestMain builds the fake claude, puts it first on PATH and gives the test
// process an empty HOME, as its working directory too, so that no git a test
// starts reads the user's configuration and nothing runs in this repository. Every fixture's specrun runs on a host of its own (see
// fixture.host), so the fixture tests run side by side.
func TestMain(m *testing.M) {
	code := func() int {
		home, err := os.MkdirTemp("", "specrun-test-home")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer os.RemoveAll(home)
		fakeClaude.once.Do(buildFakeClaude)
		if fakeClaude.err != nil {
			fmt.Fprintln(os.Stderr, fakeClaude.err)
			return 1
		}
		_ = os.Setenv("PATH", fakeClaude.dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		_ = os.Setenv("HOME", home)
		// No git a test starts runs maintenance or gc in the background:
		// they cost processes and repositories this small never need them.
		for k, v := range map[string]string{"GIT_CONFIG_COUNT": "2",
			"GIT_CONFIG_KEY_0": "maintenance.auto", "GIT_CONFIG_VALUE_0": "false",
			"GIT_CONFIG_KEY_1": "gc.auto", "GIT_CONFIG_VALUE_1": "0"} {
			_ = os.Setenv(k, v)
		}
		// A run that is given the process's own host by mistake finds no
		// repository and no token, instead of the developer's.
		for _, v := range []string{"GITHUB_TOKEN", "GH_TOKEN", "GITLAB_TOKEN"} {
			_ = os.Unsetenv(v)
		}
		if err := os.Chdir(home); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return m.Run()
	}()
	for _, dir := range []string{fakeClaude.dir, template.dir} {
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
	}
	os.Exit(code)
}

// fixture is a repository with a bare origin and a branch main, holding
// packages under .specs, with HOME, XDG_STATE_HOME and XDG_CACHE_HOME of its
// own and the fake claude first on PATH. The test's working directory is the
// repository.
type fixture struct {
	root, origin, home, state, cache string
	record                           string // what the fake claude was started with, one line per start
	script                           string // the fake claude's plan
	scratch                          string // a directory for the test's own files
	forge                            *memForge
	// env is the variables the fixture's specrun and its sessions see over
	// the test process's own, and newForge how its specrun makes its forge.
	env      map[string]string
	newForge func(root string) (issuex.Client, issuex.Repo, error)
	// now is the clock of the fixture's specrun, and builtSkill the skill
	// hash it was built with.
	now        func() time.Time
	builtSkill string
}

// template is a bare origin and its clone on a branch main with one commit
// pushed, origin/HEAD set, made once for every fixture to copy.
var template struct {
	once sync.Once
	dir  string
	err  error
}

// fixtureTemplate makes the template the first time it is asked for, and
// returns its directory.
func fixtureTemplate(t *testing.T) string {
	t.Helper()
	template.once.Do(func() {
		if template.dir, template.err = os.MkdirTemp("", "specrun-fixture"); template.err != nil {
			return
		}
		origin, repo := filepath.Join(template.dir, "origin.git"), filepath.Join(template.dir, "repo")
		steps := [][]string{{"init", "-q", "--bare", "-b", "main", origin}, {"clone", "-q", origin, repo}}
		for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "base"}, {"push", "-q", "-u", "origin", "main"},
			{"remote", "set-head", "origin", "main"}} {
			steps = append(steps, append([]string{"-C", repo}, args...))
		}
		for i, args := range steps {
			if i == 2 {
				if template.err = os.WriteFile(filepath.Join(repo, "README.md"), []byte("fixture\n"), 0o644); template.err != nil {
					return
				}
			}
			if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
				template.err = fmt.Errorf("git %s: %v\n%s", strings.Join(args, " "), err, out)
				return
			}
		}
	})
	if template.err != nil {
		t.Fatal(template.err)
	}
	return template.dir
}

// fixtures maps a test to its fixture, so that runMain finds the host of the
// test's run.
var fixtures sync.Map

// specrunVars are the variables of the test process that no fixture's
// specrun sees: its settings, and the tokens of the real forge.
var specrunVars = []string{"SPECRUN_", "AF_SPEC_DIR=", "GITHUB_TOKEN=", "GH_TOKEN=", "GITLAB_TOKEN=", "GITHUB_API_URL=", "GITLAB_API_URL="}

// setenv sets a variable the fixture's specrun and its sessions see.
func (f *fixture) setenv(name, value string) { f.env[name] = value }

// host is the host the fixture's specrun runs on: the repository as its
// working directory, the test process's environment without specrun's own
// settings, with the fixture's variables over it, and the fixture's forge.
func (f *fixture) host() host {
	var env []string
	for _, kv := range os.Environ() {
		if !slices.ContainsFunc(specrunVars, func(p string) bool { return strings.HasPrefix(kv, p) }) {
			env = append(env, kv)
		}
	}
	for k, v := range f.env {
		env = append(env, k+"="+v)
	}
	return host{dir: f.root, env: env, newForge: f.newForge, now: f.now, builtSkill: f.builtSkill}
}

// newFixture is the fixture of a test that runs side by side with the
// others: it calls t.Parallel.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	if _, again := parallel.LoadOrStore(t, true); !again {
		t.Parallel()
	}
	return setUpFixture(t)
}

// parallel holds the tests that called t.Parallel, which a test may call once.
var parallel sync.Map

// newSerialFixture is the fixture of a test that changes something every run
// reads, the process's variables or its signals, and so runs alone.
func newSerialFixture(t *testing.T) *fixture {
	t.Helper()
	return setUpFixture(t)
}

func setUpFixture(t *testing.T) *fixture {
	t.Helper()
	base := t.TempDir()
	f := &fixture{root: filepath.Join(base, "repo"), origin: filepath.Join(base, "origin.git"),
		home: filepath.Join(base, "home"), state: filepath.Join(base, "state"), cache: filepath.Join(base, "cache"),
		scratch: filepath.Join(base, "scratch")}
	for _, d := range []string{f.home, f.state, f.cache, f.scratch} {
		mustMkdir(t, d)
	}
	f.record, f.script = filepath.Join(f.scratch, "record.jsonl"), filepath.Join(f.scratch, "script.json")
	f.now = time.Now
	f.forge = newMemForge()
	f.forge.starts = func() int { return len(f.starts(t)) }
	f.forge.actions = filepath.Join(f.scratch, "forge-actions.jsonl")
	f.newForge = func(string) (issuex.Client, issuex.Repo, error) {
		return f.forge, issuex.Repo{Owner: "acme", Name: "widgets"}, nil
	}
	f.env = map[string]string{
		"HOME": f.home, "XDG_STATE_HOME": f.state, "XDG_CACHE_HOME": f.cache,
		"SPECRUN_FAKE_RECORD": f.record, "SPECRUN_FAKE_SCRIPT": f.script, "SPECRUN_FAKE_FORGE": f.forge.actions,
		// A reconcile repairs nothing unless a test asks it to, with
		// --repair-max: the defects and untested items the other tests
		// leave are theirs.
		"SPECRUN_REPAIR_MAX": "0",
	}
	// A copy of the template, its clone pointed at its own origin, costs one
	// git process where making it costs seven.
	tmpl := fixtureTemplate(t)
	for _, d := range []string{"origin.git", "repo"} {
		if err := os.CopyFS(filepath.Join(base, d), os.DirFS(filepath.Join(tmpl, d))); err != nil {
			t.Fatal(err)
		}
	}
	git(t, f.root, "remote", "set-url", "origin", f.origin)
	fixtures.Store(t, f)
	t.Cleanup(func() { fixtures.Delete(t) })
	return f
}

// addPackage copies the v3 example into .specs/dirName as the package id,
// depending on deps, with every task in state, and commits and pushes it.
func (f *fixture) addPackage(t *testing.T, dirName, id, state string, deps ...string) string {
	t.Helper()
	dir := filepath.Join(f.root, ".specs", dirName)
	_, name, _ := strings.Cut(filepath.Base(dirName), "_")
	copyDir(t, example, dir)
	replaceIn(t, filepath.Join(dir, "prd.md"), `spec_id: "09"`, `spec_id: "`+id+`"`, `spec_name: "agent_mode"`, `spec_name: "`+name+`"`)
	for _, file := range []string{"requirements.json", "tasks.json", "progress.json"} {
		editJSON(t, filepath.Join(dir, file), func(m map[string]any) {
			m["spec_id"], m["spec_name"] = id, name
			switch file {
			case "tasks.json":
				list := []any{}
				for _, d := range deps {
					list = append(list, map[string]any{"spec": d, "reason": "needs " + d})
				}
				m["dependencies"] = list
			case "progress.json":
				for _, task := range m["tasks"].([]any) {
					task.(map[string]any)["state"] = state
				}
			}
		})
	}
	f.commit(t, "add "+dirName)
	git(t, f.root, "push", "-q")
	return dir
}

// commit commits everything in the repository and returns the commit.
func (f *fixture) commit(t *testing.T, message string) string {
	t.Helper()
	git(t, f.root, "add", "-A")
	git(t, f.root, "commit", "-q", "--allow-empty", "-m", message)
	return git(t, f.root, "rev-parse", "HEAD")
}

// plan writes the fake claude's plan, one entry per start.
func (f *fixture) plan(t *testing.T, runs ...map[string]any) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"runs": runs})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, f.script, string(b))
}

// starts is what the fake claude was started with, one entry per session.
func (f *fixture) starts(t *testing.T) []fakeStart {
	t.Helper()
	b, err := os.ReadFile(f.record)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []fakeStart
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var s fakeStart
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			t.Fatalf("record line %q: %v", line, err)
		}
		out = append(out, s)
	}
	return out
}

type fakeStart struct {
	Argv   []string `json:"argv"`
	Cwd    string   `json:"cwd"`
	WaitMS string   `json:"wait_ms"`
	Head   string   `json:"head"`
	RunID  string   `json:"run_id"`
}

// specDirs is the spec_dir each session was started for.
func specDirs(starts []fakeStart) []string {
	var out []string
	for _, s := range starts {
		for _, a := range s.Argv {
			if strings.HasPrefix(a, "/af-impl ") && !strings.HasPrefix(a, "/af-impl --") {
				out = append(out, strings.TrimSuffix(strings.TrimPrefix(a, "/af-impl "), " --unattended"))
			}
		}
	}
	return out
}

// runMain runs specrun on the test's fixture, in its repository, and decodes
// its envelope.
func runMain(t *testing.T, argv ...string) (envelope.Envelope[*Result], int, string) {
	t.Helper()
	f, ok := fixtures.Load(t)
	if !ok {
		t.Fatal("runMain needs the test's fixture: call newFixture with this t first")
	}
	var stdout, stderr bytes.Buffer
	code := runOn(f.(*fixture).host(), argv, &stdout, &stderr)
	env, err := envelope.Decode[*Result](stdout.Bytes())
	if err != nil {
		t.Fatalf("stdout is not one envelope (%v):\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if env.ExitCode != code {
		t.Fatalf("exit code %d, envelope says %d", code, env.ExitCode)
	}
	return env, code, stderr.String()
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "init.defaultBranch=main", "-c", "commit.gpgsign=false",
		"-c", "user.name=Specrun Test", "-c", "user.email=specrun@example.com", "-c", "core.hooksPath=" + os.DevNull}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, to)
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(to, e.Name()), string(b))
	}
}

func replaceIn(t *testing.T, path string, pairs ...string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, strings.NewReplacer(pairs...).Replace(string(b)))
}

func editJSON(t *testing.T, path string, edit func(m map[string]any)) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(out)+"\n")
}

// realPath is path with its symbolic links resolved, as a process sees its
// working directory.
func realPath(t *testing.T, path string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// logDir is the log directory specrun uses for the fixture when --log-dir is
// not given.
func (f *fixture) logDir() string {
	return filepath.Join(f.cache, "agent-fox", "specrun", filepath.Base(f.root))
}

// runDirs are the runs' directories in the default log directory, oldest
// first.
func (f *fixture) runDirs(t *testing.T) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(f.logDir(), "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	return dirs
}

// stepFile is the path of a step's file, name, in the directory of the run
// that wrote it; "" when no run holds one.
func (f *fixture) stepFile(t *testing.T, name string) string {
	t.Helper()
	for _, d := range f.runDirs(t) {
		if p := filepath.Join(d, name); exists(p) {
			return p
		}
	}
	return ""
}

// ledgerLines is the ledger in the default log directory, one map per line.
func (f *fixture) ledgerLines(t *testing.T) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.logDir(), "ledger.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("ledger line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// outcomes lists the outcome of each ledger line.
func outcomes(lines []map[string]any) []string {
	var out []string
	for _, l := range lines {
		out = append(out, fmt.Sprint(l["outcome"]))
	}
	return out
}

// pidsAlive reports which of the process ids the fake claude recorded in path
// are still running, after giving them a moment to go.
func pidsAlive(t *testing.T, path string) []int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var alive []int
	for _, field := range strings.Fields(string(b)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			t.Fatal(err)
		}
		for deadline := time.Now().Add(5 * time.Second); processAlive(pid) && time.Now().Before(deadline); {
			time.Sleep(20 * time.Millisecond)
		}
		if processAlive(pid) {
			alive = append(alive, pid)
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		}
	}
	return alive
}

// stubClock gives the fixture's specrun a clock that advances by step every
// time it is read.
func (f *fixture) stubClock(step time.Duration) {
	at := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	f.now = func() time.Time { at = at.Add(step); return at }
}

// statePath is the fixture repository's state file.
func (f *fixture) statePath() string {
	return filepath.Join(f.state, "agent-fox", "specrun", filepath.Base(f.root)+".json")
}

// earlierRun is the run a state file of writeState says started its step.
const earlierRun = "20261112T080000Z-0a0a"

// writeState writes the state file as specrun would have left it with the
// package in specDir running since the commit head, by the process pid.
func (f *fixture) writeState(t *testing.T, specDir, head string, pid int) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"done": []any{}, "running": map[string]any{
		"kind": "implement", "subject": filepath.Base(specDir), "spec_dir": specDir,
		"started_at": "2026-11-12T08:00:00Z", "head": head, "pid": pid, "run_id": earlierRun}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, f.statePath(), string(b))
}

// processAlive reports whether a process with the id runs.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}

// prompts is each session's prompt, a reconcile's shortened to
// "reconcile", a package's to its spec_dir.
func prompts(starts []fakeStart) []string {
	var out []string
	for _, s := range starts {
		for _, a := range s.Argv {
			switch {
			case strings.HasPrefix(a, "/af-impl --reconcile "):
				out = append(out, "reconcile")
			case strings.HasPrefix(a, "/af-impl --repair "):
				out = append(out, "repair "+strings.TrimPrefix(a, "/af-impl --repair "))
			case strings.HasPrefix(a, "/af-impl "):
				out = append(out, strings.TrimSuffix(strings.TrimPrefix(a, "/af-impl "), " --unattended"))
			}
		}
	}
	return out
}

// memForge is an in-memory forge: it holds labels, issues and comments,
// records every call in order, and fails a named call when told to.
type memForge struct {
	*issuex.NoOpClient
	mu       sync.Mutex
	labels   map[string]bool
	issues   []issuex.Issue
	comments map[int][]string
	calls    []string
	fail     map[string]int // a method's name, and how many of its calls fail
	// starts, when set, counts the sessions started so far; CreateLabel
	// records it, so a test can tell the labels came first.
	starts         func() int
	startsAtLabels []int
	// actions is the file a fake session writes its forge actions to; they
	// are applied before the next call, and are not the run's own calls.
	actions string
	applied int
}

// replay applies the actions a session wrote since the last call.
func (m *memForge) replay() {
	if m.actions == "" {
		return
	}
	b, err := os.ReadFile(m.actions)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for ; m.applied < len(lines); m.applied++ {
		var a struct {
			Op     string   `json:"op"`
			Issue  int      `json:"issue"`
			Labels []string `json:"labels"`
			Title  string   `json:"title"`
		}
		if json.Unmarshal([]byte(lines[m.applied]), &a) != nil {
			continue
		}
		if a.Op == "open" {
			n := len(m.issues) + 1
			m.issues = append(m.issues, issuex.Issue{Number: n, Title: a.Title, State: "open", Labels: a.Labels,
				HTMLURL:   fmt.Sprintf("https://forge.example/acme/widgets/issues/%d", n),
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)})
			continue
		}
		is := m.issue(a.Issue)
		switch a.Op {
		case "add_labels":
			for _, l := range a.Labels {
				if !slices.Contains(is.Labels, l) {
					is.Labels = append(is.Labels, l)
				}
			}
		case "close":
			is.State = "closed"
		}
	}
}

func (m *memForge) CloseIssue(_ context.Context, ref issuex.IssueRef, comment string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.replay()
	if err := m.call("CloseIssue", fmt.Sprint(ref.Number)); err != nil {
		return err
	}
	m.issue(ref.Number).State = "closed"
	m.comments[ref.Number] = append(m.comments[ref.Number], comment)
	return nil
}

func newMemForge() *memForge {
	return &memForge{NoOpClient: &issuex.NoOpClient{}, labels: map[string]bool{}, comments: map[int][]string{}, fail: map[string]int{}}
}

func (m *memForge) Authenticated() bool { return true }

func (m *memForge) call(name, detail string) error {
	m.replay()
	m.calls = append(m.calls, name+" "+detail)
	if m.fail[name] > 0 {
		m.fail[name]--
		return fmt.Errorf("%s: the forge said 502", name)
	}
	return nil
}

func (m *memForge) CreateLabel(_ context.Context, _ issuex.Repo, l issuex.Label) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.starts != nil {
		m.startsAtLabels = append(m.startsAtLabels, m.starts())
	}
	if err := m.call("CreateLabel", l.Name); err != nil {
		return err
	}
	if m.labels[l.Name] {
		return issuex.ErrConflict
	}
	m.labels[l.Name] = true
	return nil
}

func (m *memForge) ListIssues(_ context.Context, _ issuex.Repo, f issuex.IssueFilter) (issuex.IssueList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.call("ListIssues", strings.Join(f.Labels, ",")); err != nil {
		return issuex.IssueList{}, err
	}
	var out []issuex.Issue
	for _, is := range m.issues {
		if f.State != "" && f.State != "all" && is.State != f.State {
			continue
		}
		if !hasAll(is.Labels, f.Labels) {
			continue
		}
		out = append(out, is)
	}
	return issuex.IssueList{Issues: out}, nil
}

func (m *memForge) AddLabels(_ context.Context, ref issuex.IssueRef, labels []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.call("AddLabels", fmt.Sprint(ref.Number, labels)); err != nil {
		return err
	}
	is := m.issue(ref.Number)
	for _, l := range labels {
		if !slices.Contains(is.Labels, l) {
			is.Labels = append(is.Labels, l)
		}
	}
	return nil
}

func (m *memForge) RemoveLabel(_ context.Context, ref issuex.IssueRef, label string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.call("RemoveLabel", fmt.Sprint(ref.Number, " ", label)); err != nil {
		return err
	}
	is := m.issue(ref.Number)
	is.Labels = slices.DeleteFunc(is.Labels, func(l string) bool { return l == label })
	return nil
}

func (m *memForge) AddComment(_ context.Context, ref issuex.IssueRef, body string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.call("AddComment", fmt.Sprint(ref.Number)); err != nil {
		return "", err
	}
	m.comments[ref.Number] = append(m.comments[ref.Number], body)
	return fmt.Sprintf("https://forge.example/acme/widgets/issues/%d#c%d", ref.Number, len(m.comments[ref.Number])), nil
}

func (m *memForge) issue(n int) *issuex.Issue {
	for i := range m.issues {
		if m.issues[i].Number == n {
			return &m.issues[i]
		}
	}
	panic(fmt.Sprintf("no issue %d", n))
}

// add puts an issue on the forge, as a session would have opened it.
func (m *memForge) add(title, state string, labels ...string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.issues) + 1
	m.issues = append(m.issues, issuex.Issue{Number: n, Title: title, State: state, Labels: labels,
		HTMLURL:   fmt.Sprintf("https://forge.example/acme/widgets/issues/%d", n),
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)})
	return n
}

func (m *memForge) ReadIssue(_ context.Context, ref issuex.IssueRef) (issuex.IssueThread, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.call("ReadIssue", fmt.Sprint(ref.Number)); err != nil {
		return issuex.IssueThread{}, err
	}
	for _, is := range m.issues {
		if is.Number == ref.Number {
			return issuex.IssueThread{Issue: is}, nil
		}
	}
	return issuex.IssueThread{}, fmt.Errorf("issue %d: %w", ref.Number, issuex.ErrNotFound)
}

// writes lists the calls that change the forge.
func (m *memForge) writes() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, c := range m.calls {
		if !strings.HasPrefix(c, "ListIssues") && !strings.HasPrefix(c, "ReadIssue") {
			out = append(out, c)
		}
	}
	return out
}

// labelsOf is the labels of the issue, sorted.
func (m *memForge) labelsOf(n int) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.replay()
	out := slices.Clone(m.issue(n).Labels)
	slices.Sort(out)
	return out
}

func hasAll(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

// realForge makes specrun build its forge client as it does outside the
// tests, from the origin remote and the token variables, none of them set.
//
// The client reads its token from the test process's variables, so a test
// that calls it uses newSerialFixture.
func (f *fixture) realForge(t *testing.T) {
	t.Helper()
	f.newForge = defaultForge
	for _, v := range []string{"GITHUB_TOKEN", "GH_TOKEN", "GITLAB_TOKEN", "GITHUB_API_URL", "GITLAB_API_URL"} {
		t.Setenv(v, "")
		_ = os.Unsetenv(v)
	}
}
