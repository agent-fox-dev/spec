//go:build unix

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
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
