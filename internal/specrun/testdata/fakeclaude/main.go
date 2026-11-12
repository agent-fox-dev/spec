// Command fakeclaude stands in for claude in specrun's tests. The tests build
// it once and put it first on PATH, so specrun starts it as it starts the real
// one. It appends what it was started with to the file SPECRUN_FAKE_RECORD
// names, then follows the plan the file SPECRUN_FAKE_SCRIPT names for this
// start: the events to print, what to do to the repository, and how to end.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
)

// script is one plan per start; a start past the last uses the last.
type script struct {
	Runs []run `json:"runs"`
}

type run struct {
	Events     int      `json:"events"`       // assistant events to print
	EventGapMS int      `json:"event_gap_ms"` // the pause before each
	Actions    []string `json:"actions"`      // what to do to the repository, in order
	Result     *string  `json:"result"`       // the final message; none when absent
	Silence    int      `json:"silence_s"`    // then print nothing for this long
	Child      bool     `json:"child"`        // start a child that sleeps, recording both pids
	Exit       int      `json:"exit"`
}

// record is one line of the record file.
type record struct {
	Argv   []string `json:"argv"`
	Cwd    string   `json:"cwd"`
	WaitMS string   `json:"wait_ms"`
	Head   string   `json:"head"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("2.1.322 (Claude Code)")
		return
	}
	cwd, _ := os.Getwd()
	starts := countLines(os.Getenv("SPECRUN_FAKE_RECORD"))
	appendJSON(os.Getenv("SPECRUN_FAKE_RECORD"), record{Argv: os.Args[1:], Cwd: cwd,
		WaitMS: os.Getenv("CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS"), Head: gitOut("rev-parse", "HEAD")})

	r := plan(starts)
	for i := range r.Events {
		time.Sleep(time.Duration(r.EventGapMS) * time.Millisecond)
		fmt.Printf(`{"type":"assistant","n":%d}`+"\n", i+1)
	}
	specDir := ""
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "/af-impl ") {
			specDir = strings.TrimSpace(strings.TrimPrefix(a, "/af-impl "))
		}
	}
	for _, a := range r.Actions {
		act(a, specDir)
	}
	if r.Child || r.Silence > 0 {
		if r.Child {
			child := exec.Command("sleep", "60")
			if err := child.Start(); err == nil {
				appendLine(os.Getenv("SPECRUN_FAKE_PIDS"), fmt.Sprintf("%d %d", os.Getpid(), child.Process.Pid))
			}
		}
		time.Sleep(time.Duration(r.Silence) * time.Second)
	}
	if r.Result != nil {
		b, _ := json.Marshal(map[string]string{"type": "result", "result": *r.Result})
		fmt.Println(string(b))
	}
	os.Exit(r.Exit)
}

func plan(starts int) run {
	b, err := os.ReadFile(os.Getenv("SPECRUN_FAKE_SCRIPT"))
	if err != nil {
		return run{}
	}
	var s script
	if json.Unmarshal(b, &s) != nil || len(s.Runs) == 0 {
		return run{}
	}
	return s.Runs[min(starts, len(s.Runs)-1)]
}

// act does one thing to the repository, for the package in specDir.
func act(action, specDir string) {
	spec, _ := afspec.LoadSpec(specDir)
	branch := ""
	if spec != nil {
		branch = "feature/" + spec.SpecID + "-" + spec.SpecName
	}
	switch action {
	case "branch":
		git("switch", "-q", "-c", branch)
	case "done", "done_one", "leave_pending":
		var ids []int
		for i, t := range spec.Plan() {
			if action == "done_one" && i > 0 || action == "leave_pending" && i == 0 {
				continue
			}
			ids = append(ids, t.Id)
		}
		spec.Progress = spec.CompleteTaskStates(ids)
		if err := spec.SaveProgress(specDir); err != nil {
			fmt.Fprintln(os.Stderr, "fakeclaude:", err)
		}
		git("add", "-A")
		git("commit", "-q", "-m", "feat: "+action+" "+specDir)
	case "merge":
		git("switch", "-q", "main")
		git("merge", "-q", "--no-ff", "-m", "Merge "+branch, branch)
	case "land":
		act("branch", specDir)
		act("done", specDir)
		act("merge", specDir)
	case "dirty":
		_ = os.WriteFile("stray.txt", []byte("stray"), 0o644)
	case "copy_state":
		matches, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_STATE_HOME"), "agent-fox", "specrun", "*.json"))
		for _, m := range matches {
			if b, err := os.ReadFile(m); err == nil {
				_ = os.WriteFile(os.Getenv("SPECRUN_FAKE_STATE_COPY"), b, 0o644)
			}
		}
	}
}

func git(args ...string) {
	cmd := exec.Command("git", append([]string{"-c", "user.name=Fake Claude", "-c", "user.email=fake@example.com",
		"-c", "commit.gpgsign=false", "-c", "core.hooksPath=" + os.DevNull}, args...)...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	_ = cmd.Run()
}

func gitOut(args ...string) string {
	out, _ := exec.Command("git", args...).Output()
	return strings.TrimSpace(string(out))
}

func countLines(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(b), "\n")
}

func appendJSON(path string, v any) {
	b, _ := json.Marshal(v)
	appendLine(path, string(b))
}

func appendLine(path, line string) {
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}
