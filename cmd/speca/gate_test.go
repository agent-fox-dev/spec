//go:build unix

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/agent-fox-dev/spec/internal/cli"
)

// Both levels run linter, then all_tests, each once, and pass when both exit 0.
// A full gate carries a list of slowest units, a task gate none.
func TestGate_RunsLinterThenAllTestsAndPasses(t *testing.T) {
	for _, level := range []string{"full", "task"} {
		t.Run(level, func(t *testing.T) {
			g := newGateRepo(t)
			env, members := g.gate(t, level)
			r := env.Result.Gate
			if r.Level != level || r.Verdict != "pass" || r.CleanEnv || r.Reason != "" || r.Reused != nil {
				t.Errorf("gate %+v; want a %s pass in the inherited environment", r, level)
			}
			if string(members["clean_env"]) != "false" {
				t.Errorf("clean_env = %s; want false, written", members["clean_env"])
			}
			if got := commandNames(r.Commands); !slices.Equal(got, []string{"linter", "all_tests"}) {
				t.Fatalf("commands %v; want linter, then all_tests", got)
			}
			sum := 0.0
			for _, c := range r.Commands {
				if c.ExitCode == nil || *c.ExitCode != 0 || c.Seconds < 0 || c.TimedOut || c.Tail != nil {
					t.Errorf("command %+v; want exit code 0, no tail", c)
				}
				sum += c.Seconds
			}
			if diff := r.Seconds - sum; diff > 0.0015 || diff < -0.0015 {
				t.Errorf("seconds %v; want the sum of the commands', %v", r.Seconds, sum)
			}
			if got := g.runs(t); !slices.Equal(got, []string{"linter", "all_tests"}) {
				t.Errorf("the scripts ran %v; want linter, then all_tests, once each", got)
			}
			_, hasSlowest := members["slowest"]
			if hasSlowest != (level == "full") {
				t.Errorf("slowest = %s; want it present for full only", members["slowest"])
			}
		})
	}
}

// The gate stops at the first command that does not exit 0, and starts none
// after it.
func TestGate_StopsAtTheFirstCommandThatFails(t *testing.T) {
	for name, tc := range map[string]struct {
		linter, allTests string
		want             []string
		code             int
	}{
		"the linter fails":    {"exit 3", "", []string{"linter"}, 3},
		"all_tests fails":     {"", "exit 1", []string{"linter", "all_tests"}, 1},
		"all_tests is killed": {"", "kill -9 $$", []string{"linter", "all_tests"}, -1},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGateRepo(t)
			g.setCommands(t, g.script(t, "linter", tc.linter), g.script(t, "all_tests", tc.allTests))
			env, _ := g.gate(t, "full")
			r := env.Result.Gate
			if r.Verdict != "fail" || !slices.Equal(commandNames(r.Commands), tc.want) {
				t.Fatalf("verdict %q, commands %v; want fail, %v", r.Verdict, commandNames(r.Commands), tc.want)
			}
			last := r.Commands[len(r.Commands)-1]
			if last.ExitCode == nil || *last.ExitCode != tc.code || last.TimedOut {
				t.Errorf("the failing command %+v; want exit code %d", last, tc.code)
			}
			if got := g.runs(t); !slices.Equal(got, tc.want) {
				t.Errorf("the scripts ran %v; want %v", got, tc.want)
			}
		})
	}
}

// A command that cannot start ends the gate as not_run, with the command and
// why in the reason, and no command after it starts.
func TestGate_IsNotRunWhenACommandCannotStart(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-program")
	for name, tc := range map[string]struct {
		set     func(g *gateRepo)
		command string
		ran     []string
	}{
		"the linter's program does not exist": {func(g *gateRepo) {
			g.setCommands(t, missing, g.script(t, "all_tests", ""))
		}, "linter", nil},
		"the all_tests program does not exist": {func(g *gateRepo) {
			g.setCommands(t, g.script(t, "linter", ""), missing+" ./...")
		}, "all_tests", []string{"linter"}},
		"a program on no path": {func(g *gateRepo) {
			g.setCommands(t, "speca-gate-no-such-program lint", g.script(t, "all_tests", ""))
		}, "linter", nil},
		"the linter's quote is left open": {func(g *gateRepo) {
			g.setCommands(t, "make 'lint", g.script(t, "all_tests", ""))
		}, "linter", nil},
		"test_commands names no command": {func(g *gateRepo) {
			editJSON(t, filepath.Join(g.dir, "tasks.json"), func(m map[string]any) { m["test_commands"] = map[string]any{} })
		}, "linter", nil},
		"test_commands names no all_tests": {func(g *gateRepo) {
			editJSON(t, filepath.Join(g.dir, "tasks.json"), func(m map[string]any) {
				m["test_commands"] = map[string]any{"linter": g.script(t, "linter", "")}
			})
		}, "all_tests", []string{"linter"}},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGateRepo(t)
			tc.set(g)
			env, _ := g.gate(t, "full")
			r := env.Result.Gate
			if r.Verdict != "not_run" || !strings.Contains(r.Reason, tc.command) {
				t.Fatalf("verdict %q, reason %q; want not_run naming %s", r.Verdict, r.Reason, tc.command)
			}
			for _, c := range r.Commands {
				if c.Name == tc.command && c.ExitCode != nil {
					t.Errorf("the command that could not start has exit code %d", *c.ExitCode)
				}
			}
			if got := g.runs(t); !slices.Equal(got, tc.ran) {
				t.Errorf("the scripts ran %v; want %v", got, tc.ran)
			}
		})
	}
}

// A command's program is started directly: what a shell would read reaches it
// as literal arguments, and a quoted stretch as one.
func TestGate_StartsTheProgramWithItsArgumentsAndNoShell(t *testing.T) {
	g := newGateRepo(t)
	args := filepath.Join(g.bin, "args")
	echo := g.script(t, "all_tests", `for a in "$@"; do echo "$a"; done > '`+args+`'`)
	g.setCommands(t, g.script(t, "linter", ""), echo+` a|b '$HOME c' * && x > y`)
	env, _ := g.gate(t, "full")
	if v := env.Result.Gate.Verdict; v != "pass" {
		t.Fatalf("verdict %q", v)
	}
	b, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n"), []string{"a|b", "$HOME c", "*", "&&", "x", ">", "y"}; !slices.Equal(got, want) {
		t.Errorf("the program received %q; want %q", got, want)
	}
	if exists(filepath.Join(g.root, "y")) {
		t.Error("a redirection was carried out")
	}
}

// A command still running at --timeout is ended with every process it started,
// and the gate fails; nothing of it is kept for reuse.
func TestGate_EndsACommandAndItsProcessesAtTheTimeout(t *testing.T) {
	g := newGateRepo(t)
	pids := filepath.Join(g.bin, "pids")
	g.setCommands(t, g.script(t, "linter", ""),
		g.script(t, "all_tests", `sleep 60 & echo $$ $! > '`+pids+`'; wait`))

	start := time.Now()
	env, members := g.gate(t, "full", "--timeout", "300ms")
	if took := time.Since(start); took > 20*time.Second {
		t.Errorf("the gate took %v", took)
	}
	r := env.Result.Gate
	if r.Verdict != "fail" || len(r.Commands) != 2 {
		t.Fatalf("verdict %q, commands %+v; want fail after two commands", r.Verdict, r.Commands)
	}
	if c := r.Commands[1]; !c.TimedOut || c.ExitCode != nil {
		t.Errorf("all_tests %+v; want timed_out and no exit code", c)
	}
	var cmds []map[string]any
	if err := json.Unmarshal(members["commands"], &cmds); err != nil {
		t.Fatal(err)
	}
	if _, ok := cmds[1]["exit_code"]; ok {
		t.Errorf("all_tests carries an exit_code: %v", cmds[1])
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

	again, _ := g.gate(t, "full", "--timeout", "300ms")
	if again.Result.Gate.Reused != nil {
		t.Error("a gate that timed out was reused")
	}
}

// Each command's output, stdout and stderr together, goes to a log of its own
// under the state directory, outside the repository, named in its entry.
func TestGate_KeepsEachCommandsOutputInALogOfItsOwn(t *testing.T) {
	g := newGateRepo(t)
	g.setCommands(t, g.script(t, "linter", "echo out-1; echo err-1 >&2"), g.script(t, "all_tests", numbered(3000)))
	env, _ := g.gate(t, "full")
	r := env.Result.Gate
	if r.Verdict != "pass" || len(r.Commands) != 2 {
		t.Fatalf("verdict %q, commands %+v", r.Verdict, r.Commands)
	}
	for _, c := range r.Commands {
		if !filepath.IsAbs(c.Log) || strings.HasPrefix(c.Log, g.root+string(filepath.Separator)) ||
			!strings.HasPrefix(c.Log, g.state+string(filepath.Separator)) {
			t.Errorf("%s's log %q is not an absolute path under the state directory %s and outside %s", c.Name, c.Log, g.state, g.root)
		}
	}
	if r.Commands[0].Log == r.Commands[1].Log {
		t.Fatalf("the two commands share the log %s", r.Commands[0].Log)
	}
	lint, err := os.ReadFile(r.Commands[0].Log)
	if err != nil || !strings.Contains(string(lint), "out-1") || !strings.Contains(string(lint), "err-1") {
		t.Errorf("the linter's log holds %q (%v); want out-1 and err-1", lint, err)
	}
	tests, err := os.ReadFile(r.Commands[1].Log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(tests), "\n"), "\n")
	if len(lines) != 3000 || lines[0] != "line 1" || lines[2999] != "line 3000" {
		t.Errorf("all_tests' log holds %d lines, from %q; want the 3000 in order", len(lines), lines[0])
	}
	// A second gate writes logs of its own.
	again, _ := g.gate(t, "task")
	for i, c := range again.Result.Gate.Commands {
		if c.Log == r.Commands[i].Log {
			t.Errorf("two gates share the log %s", c.Log)
		}
	}
}

// A command that fails or times out carries the last 40 lines of its output,
// all of them when there are fewer; a command that exits 0 carries none.
func TestGate_PutsTheLastFortyLinesOfAFailureInTheVerdict(t *testing.T) {
	last := func(from, to int) []string {
		var out []string
		for i := from; i <= to; i++ {
			out = append(out, "line "+strconv.Itoa(i))
		}
		return out
	}
	for name, tc := range map[string]struct {
		body    string
		timeout []string
		want    []string
	}{
		"no line":                       {"exit 1", nil, nil},
		"one line":                      {numbered(1) + "; exit 1", nil, last(1, 1)},
		"40 lines":                      {numbered(40) + "; exit 1", nil, last(1, 40)},
		"41 lines":                      {numbered(41) + "; exit 1", nil, last(2, 41)},
		"500 lines":                     {numbered(500) + "; exit 1", nil, last(461, 500)},
		"41 lines, the last unfinished": {numbered(40) + `; printf "line 41"; exit 1`, nil, last(2, 41)},
		"3 lines, then the timeout":     {numbered(3) + "; sleep 60", []string{"--timeout", "500ms"}, last(1, 3)},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGateRepo(t)
			g.setCommands(t, g.script(t, "linter", "echo fine"), g.script(t, "all_tests", tc.body))
			env, members := g.gate(t, append([]string{"full"}, tc.timeout...)...)
			r := env.Result.Gate
			if r.Verdict != "fail" || len(r.Commands) != 2 {
				t.Fatalf("verdict %q, commands %+v", r.Verdict, r.Commands)
			}
			if r.Commands[0].Tail != nil {
				t.Errorf("the passing linter carries a tail %q", *r.Commands[0].Tail)
			}
			c := r.Commands[1]
			if c.Tail == nil {
				t.Fatalf("the failing command has no tail; commands as written: %s", members["commands"])
			}
			var got []string
			if *c.Tail != "" {
				got = strings.Split(*c.Tail, "\n")
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("tail has %d lines %q; want %d lines %q", len(got), got, len(tc.want), tc.want)
			}
		})
	}
}

// Whatever the commands print, and however much, speca's stdout is one JSON
// object and holds none of it.
func TestGate_KeepsTheCommandsOutputOffStdout(t *testing.T) {
	g := newGateRepo(t)
	g.setCommands(t, g.script(t, "linter", `echo '{"ok":false}'`),
		g.script(t, "all_tests", `echo FIVE-MEGABYTES-FOLLOW; head -c 5000000 /dev/zero | tr '\0' 'x'; echo`))
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--dir", g.root, "05", "--gate", "full"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var first map[string]any
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("stdout does not begin with a JSON object: %v", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		t.Errorf("stdout holds more than one JSON object (%v)", err)
	}
	if out := stdout.String(); strings.Contains(out, `{"ok":false}`) || strings.Contains(out, "FIVE-MEGABYTES-FOLLOW") || stdout.Len() > 100_000 {
		t.Errorf("stdout (%d bytes) holds the commands' output", stdout.Len())
	}
	if strings.Contains(stderr.String(), "FIVE-MEGABYTES-FOLLOW") {
		t.Error("stderr holds the commands' output")
	}
}

// probeScript is an all_tests script that writes what it sees of its
// environment to the probe file, one key=value line each.
func probeScript(g *gateRepo, t *testing.T, probe string) string {
	return g.script(t, "all_tests", `{
echo "home=$HOME"
echo "listing=$(ls -A "$HOME")"
echo "prompt=$GIT_TERMINAL_PROMPT"
echo "probe=$GATE_PROBE"
echo "global=$(git config --global --list 2>/dev/null)"
echo "system=$(git config --system --list 2>/dev/null)"
} > '`+probe+`'`)
}

// readProbe reads the probe file as key=value lines.
func readProbe(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	key := ""
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && slices.Contains([]string{"home", "listing", "prompt", "probe", "global", "system"}, k) {
			key, out[k] = k, v
		} else if key != "" {
			out[key] += "\n" + line
		}
	}
	return out
}

// With --clean-env the commands see an empty HOME made for the gate, no global
// or system git configuration and GIT_TERMINAL_PROMPT=0, and everything else
// speca had; without it they see speca's environment as it is.
func TestGate_RunsTheCommandsInACleanEnvironmentOnRequest(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	for _, clean := range []bool{true, false} {
		t.Run("clean "+strconv.FormatBool(clean), func(t *testing.T) {
			g := newGateRepo(t)
			home := t.TempDir()
			writeFile(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = Gate Probe\n")
			t.Setenv("HOME", home)
			homeGitConfig(t)
			t.Setenv("GATE_PROBE", "1")
			t.Setenv("GIT_TERMINAL_PROMPT", "")
			probe := filepath.Join(g.bin, "probe")
			g.setCommands(t, g.script(t, "linter", ""), probeScript(g, t, probe))
			args := []string{"full"}
			if clean {
				args = append(args, "--clean-env")
			}
			env, _ := g.gate(t, args...)
			if r := env.Result.Gate; r.Verdict != "pass" || r.CleanEnv != clean {
				t.Fatalf("verdict %q, clean_env %v", r.Verdict, r.CleanEnv)
			}
			p := readProbe(t, probe)
			if p["probe"] != "1" {
				t.Errorf("GATE_PROBE = %q; want it inherited", p["probe"])
			}
			if !clean {
				if p["home"] != home || !strings.Contains(p["global"], "user.name=Gate Probe") {
					t.Errorf("HOME %q, global configuration %q; want the test's", p["home"], p["global"])
				}
				return
			}
			if p["home"] == home || p["home"] == "" || p["listing"] != "" || p["prompt"] != "0" || p["global"] != "" || p["system"] != "" {
				t.Errorf("the command saw %v; want an empty HOME of its own, GIT_TERMINAL_PROMPT 0 and no git configuration", p)
			}
			if strings.HasPrefix(p["home"], g.root) {
				t.Errorf("the clean HOME %s is inside the repository", p["home"])
			}
		})
	}
}

// The HOME a clean gate made is removed when the gate ends, whatever its
// verdict, with what the commands wrote into it.
func TestGate_RemovesTheCleanHomeWhateverTheVerdict(t *testing.T) {
	for name, tc := range map[string]struct {
		end     string
		timeout []string
		verdict string
	}{
		"a pass":    {"exit 0", nil, "pass"},
		"a failure": {"exit 1", nil, "fail"},
		"a timeout": {"sleep 60", []string{"--timeout", "500ms"}, "fail"},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGateRepo(t)
			seen := filepath.Join(g.bin, "home")
			g.setCommands(t, g.script(t, "linter", ""), g.script(t, "all_tests",
				`echo "$HOME" > '`+seen+`'; touch "$HOME/file"; mkdir "$HOME/ro"; touch "$HOME/ro/f"; chmod 555 "$HOME/ro"; `+tc.end))
			env, _ := g.gate(t, append([]string{"full", "--clean-env"}, tc.timeout...)...)
			if v := env.Result.Gate.Verdict; v != tc.verdict {
				t.Fatalf("verdict %q; want %s", v, tc.verdict)
			}
			b, err := os.ReadFile(seen)
			if err != nil {
				t.Fatal(err)
			}
			if home := strings.TrimSpace(string(b)); home == "" || exists(home) {
				t.Errorf("the clean HOME %q is still there", home)
			}
		})
	}
}

// A gate that passed is recorded outside the repository, and the same gate
// on the same tree is answered from the record, running nothing.
func TestGate_ReusesARecordedPassForTheSameTree(t *testing.T) {
	g := newGateRepo(t)
	before := snapshot(t, g.root)
	first, firstMembers := g.gate(t, "full")
	f := first.Result.Gate
	if f.Verdict != "pass" || f.Reused != nil || g.allTestsRuns(t) != 1 {
		t.Fatalf("first gate %+v, all_tests ran %d times", f, g.allTestsRuns(t))
	}
	records, _ := filepath.Glob(filepath.Join(g.state, "agent-fox", "speca", "*", "*", "*.json"))
	if len(records) == 0 {
		t.Error("no record under the state directory")
	}
	if !maps.Equal(before, snapshot(t, g.root)) {
		t.Error("the gate changed the repository")
	}

	second, members := g.gate(t, "full")
	r := second.Result.Gate
	if r.Verdict != "pass" || r.Reused == nil {
		t.Fatalf("second gate %+v; want a reused pass", r)
	}
	if r.Reused.Commit != git(t, g.root, "rev-parse", "HEAD") || r.Reused.Seconds != f.Seconds {
		t.Errorf("reused %+v; want the commit %s and the seconds %v", r.Reused, git(t, g.root, "rev-parse", "HEAD"), f.Seconds)
	}
	if _, err := time.Parse(time.RFC3339, r.Reused.At); err != nil {
		t.Errorf("reused.at %q is not RFC 3339: %v", r.Reused.At, err)
	}
	if string(members["commands"]) != "[]" || r.Seconds != 0 || string(members["slowest"]) != string(firstMembers["slowest"]) {
		t.Errorf("commands %s, seconds %v, slowest %s; want [], 0 and the first gate's %s",
			members["commands"], r.Seconds, members["slowest"], firstMembers["slowest"])
	}
	if g.allTestsRuns(t) != 1 {
		t.Errorf("all_tests ran %d times; want once", g.allTestsRuns(t))
	}
}

// A gate runs again when one part of its key differs from every recorded
// pass, and is reused when only what is not in the key changed: an ignored
// file, or the commit holding the same tree.
func TestGate_RunsAgainWhenThePartsOfItsKeyChange(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(t *testing.T, g *gateRepo) (flags []string, commit string)
		reused bool
	}{
		"a tracked file edited": {func(t *testing.T, g *gateRepo) ([]string, string) {
			editFile(t, filepath.Join(g.dir, "prd.md"), "## Intent", "## Intent\n\nEdited.")
			return nil, ""
		}, false},
		"an untracked file added": {func(t *testing.T, g *gateRepo) ([]string, string) {
			writeFile(t, filepath.Join(g.root, "notes.txt"), "notes")
			return nil, ""
		}, false},
		"the task level": {func(t *testing.T, g *gateRepo) ([]string, string) {
			return []string{"task"}, ""
		}, false},
		"the all_tests string changed by one argument": {func(t *testing.T, g *gateRepo) ([]string, string) {
			g.setCommands(t, filepath.Join(g.bin, "linter"), filepath.Join(g.bin, "all_tests")+" -v")
			return nil, ""
		}, false},
		"a clean environment": {func(t *testing.T, g *gateRepo) ([]string, string) {
			return []string{"full", "--clean-env"}, ""
		}, false},
		"an ignored file added": {func(t *testing.T, g *gateRepo) ([]string, string) {
			writeFile(t, filepath.Join(g.root, "x.log"), "ignored")
			return nil, git(t, g.root, "rev-parse", "HEAD")
		}, true},
		"the change the pass covered committed": {func(t *testing.T, g *gateRepo) ([]string, string) {
			// The recorded pass is of an uncommitted change; committing it
			// keeps the tree.
			writeFile(t, filepath.Join(g.root, "change.txt"), "change")
			before := git(t, g.root, "rev-parse", "HEAD")
			g.gate(t, "full")
			git(t, g.root, "add", "-A")
			git(t, g.root, "commit", "-q", "-m", "the change")
			return nil, before
		}, true},
		"a --no-ff merge of a tip that passed, the base not moved": {func(t *testing.T, g *gateRepo) ([]string, string) {
			git(t, g.root, "switch", "-q", "-c", "feature")
			writeFile(t, filepath.Join(g.root, "feature.txt"), "feature")
			git(t, g.root, "add", "-A")
			git(t, g.root, "commit", "-q", "-m", "feature")
			g.gate(t, "full")
			tip := git(t, g.root, "rev-parse", "HEAD")
			git(t, g.root, "switch", "-q", "main")
			git(t, g.root, "merge", "-q", "--no-ff", "-m", "merge feature", "feature")
			return nil, tip
		}, true},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGateRepo(t)
			writeFile(t, filepath.Join(g.root, ".gitignore"), "*.log\n")
			git(t, g.root, "add", "-A")
			git(t, g.root, "commit", "-q", "-m", "ignore logs")
			g.gate(t, "full")
			flags, commit := tc.change(t, g)
			runs := g.allTestsRuns(t)
			if len(flags) == 0 {
				flags = []string{"full"}
			}
			env, _ := g.gate(t, flags...)
			r := env.Result.Gate
			if (r.Reused != nil) != tc.reused {
				t.Fatalf("reused %+v; want reused %v", r.Reused, tc.reused)
			}
			ran := g.allTestsRuns(t) > runs
			if ran == tc.reused {
				t.Errorf("all_tests ran: %v; want %v", ran, !tc.reused)
			}
			if tc.reused && r.Reused.Commit != commit {
				t.Errorf("reused.commit %s; want %s", r.Reused.Commit, commit)
			}
		})
	}
}

// A failure and a gate that could not run are never reused.
func TestGate_NeverReusesAFailureOrAGateThatDidNotRun(t *testing.T) {
	for name, tc := range map[string]struct {
		set     func(t *testing.T, g *gateRepo)
		verdict string
		runs    int
	}{
		"all_tests exits 1": {func(t *testing.T, g *gateRepo) {
			g.setCommands(t, g.script(t, "linter", ""), g.script(t, "all_tests", "exit 1"))
		}, "fail", 2},
		"the linter's program is missing": {func(t *testing.T, g *gateRepo) {
			g.setCommands(t, filepath.Join(g.bin, "no-such-linter"), g.script(t, "all_tests", ""))
		}, "not_run", 0},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGateRepo(t)
			tc.set(t, g)
			a, _ := g.gate(t, "full")
			b, _ := g.gate(t, "full")
			if a.Result.Gate.Verdict != tc.verdict || b.Result.Gate.Verdict != tc.verdict || b.Result.Gate.Reused != nil {
				t.Errorf("verdicts %q and %q, second reused %+v; want %s twice, never reused",
					a.Result.Gate.Verdict, b.Result.Gate.Verdict, b.Result.Gate.Reused, tc.verdict)
			}
			if n := g.allTestsRuns(t); n != tc.runs {
				t.Errorf("all_tests ran %d times; want %d", n, tc.runs)
			}
		})
	}
}

// Working out the tree changes nothing of the repository: not the index, not
// the staged changes, not the object store.
func TestGate_LeavesTheIndexAndTheObjectStoreAsTheyWere(t *testing.T) {
	g := newGateRepo(t)
	editFile(t, filepath.Join(g.dir, "prd.md"), "## Intent", "## Intent\n\nStaged.")
	git(t, g.root, "add", filepath.Join(".specs", "05_gate", "prd.md"))
	editFile(t, filepath.Join(g.dir, "tasks.json"), `"spec_id"`, `"spec_id" `)
	writeFile(t, filepath.Join(g.root, "untracked.txt"), "untracked")
	status := git(t, g.root, "status", "--porcelain")
	before := snapshot(t, g.root)
	contents := treeBytes(t, filepath.Join(g.root, ".git"))

	env, _ := g.gate(t, "full")
	if env.Result.Gate.Verdict != "pass" {
		t.Fatalf("verdict %q", env.Result.Gate.Verdict)
	}
	if !maps.Equal(before, snapshot(t, g.root)) || !maps.Equal(contents, treeBytes(t, filepath.Join(g.root, ".git"))) {
		t.Error("the gate changed a file under the repository")
	}
	if got := git(t, g.root, "status", "--porcelain"); got != status {
		t.Errorf("status %q; want %q", got, status)
	}
	if got := git(t, g.root, "diff", "--cached", "--name-only"); got != ".specs/05_gate/prd.md" {
		t.Errorf("staged %q; want the PRD alone", got)
	}
}

// Without a tree id (no work tree, or no git) the gate runs and answers, and
// a warning says it was not recorded.
func TestGate_RunsButRecordsNothingWithoutATreeID(t *testing.T) {
	for name, set := range map[string]func(t *testing.T) *gateRepo{
		"no .git": newGateDir,
		"no git on the path": func(t *testing.T) *gateRepo {
			g := newGateRepo(t)
			t.Setenv("PATH", t.TempDir())
			return g
		},
	} {
		t.Run(name, func(t *testing.T) {
			g := set(t)
			for i := range 2 {
				env, _ := g.gate(t, "full")
				if r := env.Result.Gate; r.Verdict != "pass" || r.Reused != nil {
					t.Errorf("gate %d: verdict %q, reused %+v; want a pass that ran", i+1, r.Verdict, r.Reused)
				}
				if codes := warningCodes(env); !slices.Equal(codes, []string{"gate_not_recorded"}) {
					t.Errorf("gate %d: warnings %v; want gate_not_recorded", i+1, codes)
				} else if w := env.Warnings[0]; !strings.Contains(w.Message, "tree id") {
					t.Errorf("warning %q does not say there is no tree id", w.Message)
				}
			}
			if n := g.allTestsRuns(t); n != 2 {
				t.Errorf("all_tests ran %d times; want twice", n)
			}
		})
	}
}

// A state directory that cannot be written does not stop the gate; a record
// that cannot be read counts as none, and the next pass replaces it.
func TestGate_RunsWhenTheStateDirectoryFailsIt(t *testing.T) {
	t.Run("the state directory is a file", func(t *testing.T) {
		g := newGateRepo(t)
		file := filepath.Join(g.state, "file")
		writeFile(t, file, "")
		t.Setenv("XDG_STATE_HOME", file)
		env, _ := g.gate(t, "full")
		r := env.Result.Gate
		if r.Verdict != "pass" || !slices.Equal(warningCodes(env), []string{"gate_not_recorded"}) {
			t.Fatalf("verdict %q, warnings %v; want a pass and gate_not_recorded", r.Verdict, warningCodes(env))
		}
		if !strings.Contains(env.Warnings[0].Message, file) {
			t.Errorf("warning %q does not name %s", env.Warnings[0].Message, file)
		}
		for _, c := range r.Commands {
			if c.Log != "" {
				t.Errorf("%s has the log %s", c.Name, c.Log)
			}
		}
	})
	t.Run("a record that cannot be parsed", func(t *testing.T) {
		g := newGateRepo(t)
		g.gate(t, "full")
		records, _ := filepath.Glob(filepath.Join(g.state, "agent-fox", "speca", "*", "passes", "*.json"))
		if len(records) != 1 {
			t.Fatalf("records %v; want one", records)
		}
		writeFile(t, records[0], "not json")
		b, _ := g.gate(t, "full")
		if b.Result.Gate.Reused != nil || g.allTestsRuns(t) != 2 || len(b.Warnings) != 0 {
			t.Errorf("reused %+v, all_tests ran %d times, warnings %v; want a run", b.Result.Gate.Reused, g.allTestsRuns(t), warningCodes(b))
		}
		if c, _ := g.gate(t, "full"); c.Result.Gate.Reused == nil {
			t.Error("the pass did not replace the record that could not be parsed")
		}
	})
}

// Both forms run for a package in every lifecycle status, and leave the
// repository as it was.
func TestGate_RunsInEveryStatusAndWritesNothingUnderTheRepository(t *testing.T) {
	g := newGateDir(t)
	refs := map[string]string{}
	for i, status := range []string{"draft", "active", "sealed", "superseded", "archived"} {
		id := strconv.Itoa(11 + i)
		dirName := id + "_" + status
		if status == "superseded" || status == "archived" {
			dirName = "archive/" + dirName
		}
		dir := packageWith(t, g.root, dirName, id, status, status)
		// A command string of its own, so that no package's gate is
		// answered from another's pass.
		setCommandsOf(t, dir, filepath.Join(g.bin, "linter"), filepath.Join(g.bin, "all_tests")+" "+status)
		refs[status] = filepath.Join(".specs", dirName)
	}
	gitInit(t, g.root)
	before := snapshot(t, g.root)
	for status, ref := range refs {
		t.Run(status, func(t *testing.T) {
			a := g.gateOf(t, ref, "full")
			b := g.gateOf(t, ref, "task")
			c, _ := g.report(t, ref, "--since", "HEAD")
			if a.Result.Gate.Verdict != "pass" || b.Result.Gate.Verdict != "pass" || c.Result.GateReport.Full.Runs != 1 {
				t.Errorf("full %q, task %q, report %+v; want two passes and one full run", a.Result.Gate.Verdict,
					b.Result.Gate.Verdict, c.Result.GateReport)
			}
		})
	}
	if !maps.Equal(before, snapshot(t, g.root)) {
		t.Error("a form changed the repository")
	}
	if n := g.allTestsRuns(t); n != 10 {
		t.Errorf("all_tests ran %d times; want a full and a task gate for each of the five", n)
	}
}

// A command that exits 0 passes even when a process it left behind holds its
// output open, and that process does not outlive the gate.
func TestGate_PassesACommandThatLeavesAProcessBehind(t *testing.T) {
	g := newGateRepo(t)
	pid := filepath.Join(g.bin, "pid")
	g.setCommands(t, g.script(t, "linter", ""), g.script(t, "all_tests", `sleep 60 & echo $! > '`+pid+`'; exit 0`))
	env, _ := g.gate(t, "full")
	r := env.Result.Gate
	if r.Verdict != "pass" || len(r.Commands) != 2 || r.Commands[1].ExitCode == nil || *r.Commands[1].ExitCode != 0 {
		t.Fatalf("gate %+v; want a pass with all_tests exiting 0", r)
	}
	b, err := os.ReadFile(pid)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	alive := func() bool { return !errors.Is(syscall.Kill(n, 0), syscall.ESRCH) }
	for deadline := time.Now().Add(5 * time.Second); alive() && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
	}
	if alive() {
		_ = syscall.Kill(n, syscall.SIGKILL)
		t.Error("the process the command left behind is still running")
	}
}

// Two directories of one repository run two gates: a pass in one is not
// reused for the other, though tree and commands are the same.
func TestGate_KeepsTheGatesOfTwoDirectoriesOfARepositoryApart(t *testing.T) {
	g := newGateDir(t)
	linter, allTests := filepath.Join(g.bin, "linter"), filepath.Join(g.bin, "all_tests")
	for _, sub := range []string{"a", "b"} {
		dir := addPackage(t, filepath.Join(g.root, "svc", sub), "05_gate", "05", "gate")
		setCommandsOf(t, dir, linter, allTests)
	}
	gitInit(t, g.root)
	for i, sub := range []string{"a", "b", "a"} {
		env, code, _ := run(t, "--dir", filepath.Join(g.root, "svc", sub), "05", "--gate", "full")
		if code != 0 || env.Result == nil || env.Result.Gate == nil {
			t.Fatalf("svc/%s: code %d, error %+v", sub, code, env.Error)
		}
		if reused := env.Result.Gate.Reused != nil; reused != (i == 2) {
			t.Errorf("gate %d, svc/%s: reused %v; want %v", i+1, sub, reused, i == 2)
		}
	}
	if n := g.allTestsRuns(t); n != 2 {
		t.Errorf("all_tests ran %d times; want once in each directory", n)
	}
}

// With affected_tests, the task gate runs the linter, then that command with
// the full id of the base commit in place of {base}, and not all_tests.
func TestGate_RunsTheAffectedTestsFromABaseCommit(t *testing.T) {
	for name, tc := range map[string]struct {
		base func(g *gateRepo, t *testing.T) []string
		rev  string
	}{
		"no --base":     {func(*gateRepo, *testing.T) []string { return nil }, "HEAD"},
		"HEAD~1":        {func(*gateRepo, *testing.T) []string { return []string{"--base", "HEAD~1"} }, "HEAD~1"},
		"a branch name": {func(*gateRepo, *testing.T) []string { return []string{"--base", "older"} }, "older"},
		"an abbreviated id": {func(g *gateRepo, t *testing.T) []string {
			return []string{"--base", git(t, g.root, "rev-parse", "--short", "HEAD~1")}
		}, "HEAD~1"},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGateRepo(t)
			g.setAffected(t, "BASE={base}")
			g.commit(t, "affected tests")
			git(t, g.root, "branch", "older", "HEAD~1")
			env, _ := g.gate(t, append([]string{"task"}, tc.base(g, t)...)...)
			want := git(t, g.root, "rev-parse", tc.rev)
			r := env.Result.Gate
			if got := commandNames(r.Commands); !slices.Equal(got, []string{"linter", "affected_tests"}) {
				t.Fatalf("commands %v; want linter, then affected_tests", got)
			}
			if r.Base != want || g.affectedArgs(t) != "BASE="+want {
				t.Errorf("base %q, the script got %q; want %s", r.Base, g.affectedArgs(t), want)
			}
			if g.allTestsRuns(t) != 0 {
				t.Errorf("all_tests ran")
			}
		})
	}
}

// Without affected_tests the task gate runs all_tests as before, and has no
// base, whether or not --base was given.
func TestGate_RunsAllTestsWithoutAnAffectedTestsCommand(t *testing.T) {
	g := newGateRepo(t)
	env, members := g.gate(t, "task", "--base", "HEAD")
	if got := commandNames(env.Result.Gate.Commands); !slices.Equal(got, []string{"linter", "all_tests"}) {
		t.Errorf("commands %v; want linter, then all_tests", got)
	}
	if _, ok := members["base"]; ok {
		t.Errorf("base = %s; want it absent", members["base"])
	}
}

// Two task gates of one tree with different bases are two keys; the same
// base is reused.
func TestGate_TakesEachBaseAsAKeyOfItsOwn(t *testing.T) {
	g := newGateRepo(t)
	g.setAffected(t, "BASE={base}")
	a := g.commit(t, "A")
	writeFile(t, filepath.Join(g.root, "b.txt"), "b")
	b := g.commit(t, "B")
	writeFile(t, filepath.Join(g.root, "c.txt"), "c")
	g.commit(t, "C")
	for i, step := range []struct {
		base   []string
		reused bool
	}{{[]string{"--base", a}, false}, {[]string{"--base", a}, true}, {[]string{"--base", b}, false}, {nil, false}} {
		before := len(g.runs(t))
		env, _ := g.gate(t, append([]string{"task"}, step.base...)...)
		ran := len(g.runs(t)) > before
		if (env.Result.Gate.Reused != nil) != step.reused || ran == step.reused {
			t.Errorf("gate %d %v: reused %v, ran %v; want reused %v", i+1, step.base, env.Result.Gate.Reused != nil, ran, step.reused)
		}
	}
}

// A --base that cannot be used is refused before anything runs.
func TestGate_RefusesABaseItCannotUse(t *testing.T) {
	for name, tc := range map[string]struct {
		git  bool
		args []string
		says string
	}{
		"a base that names no commit": {true, []string{"--gate", "task", "--base", "nosuch"}, "nosuch"},
		"--base with the full level":  {true, []string{"--gate", "full", "--base", "HEAD"}, "--gate task"},
		"--base without --gate":       {true, []string{"--base", "HEAD"}, "--gate task"},
		"no git work tree":            {false, []string{"--gate", "task"}, "not in a git work tree"},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGateDir(t)
			if tc.git {
				gitInit(t, g.root)
			}
			g.setAffected(t, "BASE={base}")
			env, code, _ := run(t, append([]string{"--dir", g.root, "05"}, tc.args...)...)
			if code != cli.ExitUsage || env.Error == nil || env.Error.Category != "usage" || !strings.Contains(env.Error.Message, tc.says) {
				t.Errorf("code %d, error %+v; want a usage error saying %q", code, env.Error, tc.says)
			}
			if len(g.runs(t)) != 0 {
				t.Errorf("the scripts ran %v; want none", g.runs(t))
			}
		})
	}
}

// stepClock makes every reading of the gate's clock step seconds after the
// one before, so that each command takes step seconds.
func stepClock(t *testing.T, step float64) {
	t.Helper()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := gateClock
	gateClock = func() time.Time {
		at = at.Add(time.Duration(step * float64(time.Second)))
		return at
	}
	t.Cleanup(func() { gateClock = old })
}

// A full gate, run or reused, is measured against the steering's budget,
// 120 seconds when it sets none; a task gate is not.
func TestGate_MeasuresAFullGateAgainstTheBudget(t *testing.T) {
	g := newGateRepo(t)
	g.writeSteering(t, "gate_budget_seconds: 1\n")
	g.commit(t, "steering")
	stepClock(t, 0.5) // two commands of 0.5 s each: 1 s, the budget
	env, _ := g.gate(t, "full")
	if b := env.Result.Gate.Budget; b == nil || b.Seconds != 1 || b.Exceeded {
		t.Errorf("budget %+v; want 1 s, not exceeded at exactly 1 s", b)
	}
	writeFile(t, filepath.Join(g.root, "x.txt"), "x")
	stepClock(t, 1)
	env, _ = g.gate(t, "full")
	b := env.Result.Gate.Budget
	if b == nil || b.Seconds != 1 || !b.Exceeded || b.OverBySeconds == nil || *b.OverBySeconds != 1 {
		t.Fatalf("budget %+v; want 1 s, exceeded by 1", b)
	}
	if again, _ := g.gate(t, "full"); again.Result.Gate.Reused == nil || !reflect.DeepEqual(again.Result.Gate.Budget, b) {
		t.Errorf("the reused gate's budget %+v; want %+v", again.Result.Gate.Budget, b)
	}
	if task, members := g.gate(t, "task"); task.Result.Gate.Budget != nil || members["budget"] != nil {
		t.Errorf("a task gate has a budget: %s", members["budget"])
	}

	os.Remove(filepath.Join(g.root, ".specs", "steering.md"))
	stepClock(t, 0.01)
	if env, _ := g.gate(t, "full"); env.Result.Gate.Budget == nil || env.Result.Gate.Budget.Seconds != 120 {
		t.Errorf("budget %+v; want 120 s without a steering", env.Result.Gate.Budget)
	}
}

// A budget that is no whole number above zero is the default, with a
// steering_malformed warning naming it.
func TestGate_TakesTheDefaultForABudgetItCannotRead(t *testing.T) {
	g := newGateRepo(t)
	g.writeSteering(t, "gate_budget_seconds: fast\n")
	env, _ := g.gate(t, "full")
	if b := env.Result.Gate.Budget; b == nil || b.Seconds != 120 {
		t.Errorf("budget %+v; want 120 s", b)
	}
	var codes []string
	for _, w := range env.Warnings {
		codes = append(codes, string(w.Code))
		if w.Code == cli.WarnSteeringMalformed && !strings.Contains(w.Message, "gate_budget_seconds") {
			t.Errorf("warning %q does not name gate_budget_seconds", w.Message)
		}
	}
	if !slices.Equal(codes, []string{string(cli.WarnSteeringMalformed)}) {
		t.Errorf("warnings %v; want one steering_malformed", codes)
	}
}

// A full gate's growth is measured against the passing full gate recorded
// for the tree of the merge base of HEAD and the base branch.
func TestGate_MeasuresTheGrowthOverTheBaseBranch(t *testing.T) {
	g := newGateRepo(t)
	stepClock(t, 5) // 10 s on main
	g.gate(t, "full")
	git(t, g.root, "switch", "-q", "-c", "feature")
	writeFile(t, filepath.Join(g.root, "f.txt"), "f")
	g.commit(t, "F")
	stepClock(t, 8) // 16 s on the branch
	env, _ := g.gate(t, "full")
	if gr := env.Result.Gate.Growth; gr == nil || gr.BaseSeconds != 10 || gr.GrewBy != 0.6 {
		t.Errorf("growth %+v; want base 10 s, grew by 0.6", gr)
	}

	// While a merge is uncommitted on main, main's tree before it is the base.
	git(t, g.root, "switch", "-q", "main")
	git(t, g.root, "merge", "-q", "--no-ff", "--no-commit", "feature")
	if env, _ := g.gate(t, "full"); env.Result.Gate.Growth == nil || env.Result.Gate.Growth.BaseSeconds != 10 {
		t.Errorf("growth during the merge %+v; want base 10 s", env.Result.Gate.Growth)
	}

	other := newGateRepo(t)
	git(t, other.root, "switch", "-q", "-c", "feature")
	writeFile(t, filepath.Join(other.root, "f.txt"), "f")
	other.commit(t, "F")
	if env, members := other.gate(t, "full"); env.Result.Gate.Growth != nil || members["growth"] != nil {
		t.Errorf("growth %s; want it absent when main's tree was never gated", members["growth"])
	}
}
