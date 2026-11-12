package specrun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agent-fox-dev/aflib/envelope"
)

// example is the worked example of format v3 the fixture's packages copy, by
// an absolute path, since a fixture changes the working directory.
var example, _ = filepath.Abs(filepath.Join("..", "..", "afspec", "testdata", "v3_example"))

// fakeClaude is the fake claude, built once per run of the package's tests.
var fakeClaude struct {
	once sync.Once
	dir  string
	err  error
}

// fakeBin builds the fake claude into a directory of its own and returns it.
func fakeBin(t *testing.T) string {
	t.Helper()
	fakeClaude.once.Do(func() {
		if fakeClaude.dir, fakeClaude.err = os.MkdirTemp("", "specrun-fake-claude"); fakeClaude.err != nil {
			return
		}
		out, err := exec.Command("go", "build", "-o", filepath.Join(fakeClaude.dir, "claude"),
			"github.com/agent-fox-dev/spec/internal/specrun/testdata/fakeclaude").CombinedOutput()
		if err != nil {
			fakeClaude.err = fmt.Errorf("building the fake claude: %v\n%s", err, out)
		}
	})
	if fakeClaude.err != nil {
		t.Fatal(fakeClaude.err)
	}
	return fakeClaude.dir
}

func TestMain(m *testing.M) {
	code := m.Run()
	if fakeClaude.dir != "" {
		_ = os.RemoveAll(fakeClaude.dir)
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
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	bin := fakeBin(t) // before HOME moves: the build reads the user's module cache
	base := t.TempDir()
	f := &fixture{root: filepath.Join(base, "repo"), origin: filepath.Join(base, "origin.git"),
		home: filepath.Join(base, "home"), state: filepath.Join(base, "state"), cache: filepath.Join(base, "cache"),
		scratch: filepath.Join(base, "scratch")}
	for _, d := range []string{f.home, f.state, f.cache, f.scratch} {
		mustMkdir(t, d)
	}
	f.record, f.script = filepath.Join(f.scratch, "record.jsonl"), filepath.Join(f.scratch, "script.json")
	t.Setenv("HOME", f.home)
	t.Setenv("XDG_STATE_HOME", f.state)
	t.Setenv("XDG_CACHE_HOME", f.cache)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SPECRUN_FAKE_RECORD", f.record)
	t.Setenv("SPECRUN_FAKE_SCRIPT", f.script)
	for _, v := range []string{"SPECRUN_MAX", "SPECRUN_STALL", "SPECRUN_LOG_DIR", "SPECRUN_STRICT_SKILLS", "AF_SPEC_DIR"} {
		t.Setenv(v, "")
		_ = os.Unsetenv(v)
	}
	git(t, base, "init", "-q", "--bare", "-b", "main", f.origin)
	git(t, base, "clone", "-q", f.origin, f.root)
	writeFile(t, filepath.Join(f.root, "README.md"), "fixture\n")
	f.commit(t, "base")
	git(t, f.root, "push", "-q", "-u", "origin", "main")
	git(t, f.root, "remote", "set-head", "origin", "main")
	t.Chdir(f.root)
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
}

// specDirs is the spec_dir each session was started for.
func specDirs(starts []fakeStart) []string {
	var out []string
	for _, s := range starts {
		for _, a := range s.Argv {
			if strings.HasPrefix(a, "/af-impl ") {
				out = append(out, strings.TrimPrefix(a, "/af-impl "))
			}
		}
	}
	return out
}

// runMain runs specrun in the working directory and decodes its envelope.
func runMain(t *testing.T, argv ...string) (envelope.Envelope[*Result], int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Main(argv, &stdout, &stderr)
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

// stubClock replaces specrun's clock for the rest of the test with one that
// advances by step every time it is read.
func stubClock(t *testing.T, step time.Duration) {
	t.Helper()
	was := now
	at := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	now = func() time.Time { at = at.Add(step); return at }
	t.Cleanup(func() { now = was })
}

// statePath is the fixture repository's state file.
func (f *fixture) statePath() string {
	return filepath.Join(f.state, "agent-fox", "specrun", filepath.Base(f.root)+".json")
}

// writeState writes the state file as specrun would have left it with the
// package in specDir running since the commit head, by the process pid.
func (f *fixture) writeState(t *testing.T, specDir, head string, pid int) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"done": []any{}, "running": map[string]any{
		"kind": "implement", "subject": filepath.Base(specDir), "spec_dir": specDir,
		"started_at": "2026-11-12T08:00:00Z", "head": head, "pid": pid}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, f.statePath(), string(b))
}
