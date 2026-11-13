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
	"strconv"
	"strings"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
)

// trailer is the paragraph the merge commit of this start ends with.
var trailer string

// script is one plan per start; a start past the last uses the last. A
// reconcile session follows the reconcile plans, counted apart.
type script struct {
	Runs      []run       `json:"runs"`
	Reconcile []reconcile `json:"reconcile"`
	Repair    []repair    `json:"repair"`
}

// repair is the plan of a repair session: merge a fix branch into main, with
// an AF-Run trailer naming an issue and an outcome or none; close the issue
// or mark it failed on the forge; or misbehave.
type repair struct {
	Merge   bool   `json:"merge"`
	Outcome string `json:"outcome"` // the trailer's repair.outcome; none when empty
	For     int    `json:"for"`     // the issue the trailer names; the repaired one when 0
	Close   bool   `json:"close"`
	Fail    bool   `json:"fail"`
	Stay    bool   `json:"stay_on_branch"`
	Dirty   bool   `json:"dirty"`
	Silence int    `json:"silence_s"`
	// Opens are issues the session opens, each its labels; the first is
	// its title.
	Opens [][]string `json:"opens"`
}

// reconcile is the plan of a reconcile session: what it does on the forge,
// whether it writes its report, and how it misbehaves.
type reconcile struct {
	Forge    []json.RawMessage `json:"forge"`     // actions, appended to SPECRUN_FAKE_FORGE
	NoReport bool              `json:"no_report"` // write no report
	Actions  []string          `json:"actions"`   // commit, dirty, branch: misbehave on the repository
	Silence  int               `json:"silence_s"`
}

type run struct {
	SessionID  string   `json:"session_id"`     // when set, an init event giving it comes first
	Events     int      `json:"events"`         // assistant events to print
	EventGapMS int      `json:"event_gap_ms"`   // the pause before each
	Actions    []string `json:"actions"`        // what to do to the repository, in order
	Result     *string  `json:"result"`         // the final message; none when absent
	IsError    bool     `json:"is_error"`       // the result event says the session ended in an error
	APIStatus  int      `json:"api_status"`     // the result event's api_error_status; none when 0
	Silence    int      `json:"silence_s"`      // then print nothing for this long
	Child      bool     `json:"child"`          // start a child that sleeps, recording both pids
	Detached   bool     `json:"detached_child"` // the same, the child in a process group of its own
	Exit       int      `json:"exit"`
	// Trailer is a paragraph the merge commit's message ends with.
	Trailer string `json:"trailer"`
}

// record is one line of the record file.
type record struct {
	Argv   []string `json:"argv"`
	Cwd    string   `json:"cwd"`
	WaitMS string   `json:"wait_ms"`
	Head   string   `json:"head"`
	RunID  string   `json:"run_id"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("2.1.322 (Claude Code)")
		return
	}
	cwd, _ := os.Getwd()
	if len(os.Args) > 2 && strings.HasPrefix(os.Args[2], "/af-impl --repair ") {
		appendJSON(os.Getenv("SPECRUN_FAKE_RECORD"), record{Argv: os.Args[1:], Cwd: cwd,
			WaitMS: os.Getenv("CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS"), Head: gitOut("rev-parse", "HEAD"), RunID: os.Getenv("SPECRUN_RUN_ID")})
		repairSession(os.Args[2])
		return
	}
	if len(os.Args) > 2 && strings.HasPrefix(os.Args[2], "/af-impl --reconcile ") {
		appendJSON(os.Getenv("SPECRUN_FAKE_RECORD"), record{Argv: os.Args[1:], Cwd: cwd,
			WaitMS: os.Getenv("CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS"), Head: gitOut("rev-parse", "HEAD"), RunID: os.Getenv("SPECRUN_RUN_ID")})
		reconcileSession(os.Args[2])
		return
	}
	starts := countLines(os.Getenv("SPECRUN_FAKE_RECORD")) - countPrompts(os.Getenv("SPECRUN_FAKE_RECORD"), "/af-impl --reconcile ") -
		countPrompts(os.Getenv("SPECRUN_FAKE_RECORD"), "/af-impl --repair ")
	appendJSON(os.Getenv("SPECRUN_FAKE_RECORD"), record{Argv: os.Args[1:], Cwd: cwd,
		WaitMS: os.Getenv("CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS"), Head: gitOut("rev-parse", "HEAD"), RunID: os.Getenv("SPECRUN_RUN_ID")})

	r := plan(starts)
	if r.SessionID != "" {
		fmt.Printf(`{"type":"system","subtype":"init","session_id":%q}`+"\n", r.SessionID)
	}
	for i := range r.Events {
		time.Sleep(time.Duration(r.EventGapMS) * time.Millisecond)
		fmt.Printf(`{"type":"assistant","n":%d}`+"\n", i+1)
	}
	specDir := ""
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "/af-impl ") {
			specDir = strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(a, "/af-impl ")), " --unattended")
		}
	}
	trailer = r.Trailer
	for _, a := range r.Actions {
		act(a, specDir)
	}
	if r.Child || r.Detached || r.Silence > 0 {
		if r.Child || r.Detached {
			child := exec.Command("sleep", "60")
			child.Stdout = os.Stdout
			detach(child, r.Detached)
			if err := child.Start(); err == nil {
				appendLine(os.Getenv("SPECRUN_FAKE_PIDS"), fmt.Sprintf("%d %d", os.Getpid(), child.Process.Pid))
			}
		}
		time.Sleep(time.Duration(r.Silence) * time.Second)
	}
	if r.Result != nil {
		ev := map[string]any{"type": "result", "result": *r.Result, "is_error": r.IsError}
		if r.APIStatus != 0 {
			ev["api_error_status"] = r.APIStatus
		}
		b, _ := json.Marshal(ev)
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
	case "checkout":
		git("switch", "-q", branch)
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
		args := []string{"merge", "-q", "--no-ff", "-m", "Merge " + branch}
		if trailer != "" {
			args = append(args, "-m", trailer)
		}
		git(append(args, branch)...)
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

// reconcileSession follows the reconcile plan for this start: the forge
// actions, the report at the path the prompt names, and any misbehaviour.
func reconcileSession(prompt string) {
	fields := strings.Fields(prompt)
	report := ""
	for i, f := range fields {
		if f == "--report" && i+1 < len(fields) {
			report = fields[i+1]
		}
	}
	var p reconcile
	if b, err := os.ReadFile(os.Getenv("SPECRUN_FAKE_SCRIPT")); err == nil {
		var s script
		if json.Unmarshal(b, &s) == nil && len(s.Reconcile) > 0 {
			p = s.Reconcile[min(countPrompts(os.Getenv("SPECRUN_FAKE_RECORD"), "/af-impl --reconcile ")-1, len(s.Reconcile)-1)]
		}
	}
	fmt.Println(`{"type":"assistant","n":1}`)
	for _, a := range p.Forge {
		appendLine(os.Getenv("SPECRUN_FAKE_FORGE"), string(a))
	}
	for _, a := range p.Actions {
		switch a {
		case "commit":
			_ = os.WriteFile("reconciled.txt", []byte("x"), 0o644)
			git("add", "-A")
			git("commit", "-q", "-m", "reconcile changed code")
		case "dirty":
			_ = os.WriteFile("stray.txt", []byte("x"), 0o644)
		case "branch":
			git("switch", "-q", "-c", "elsewhere")
		}
	}
	if !p.NoReport && report != "" {
		_ = os.WriteFile(report, []byte("# Reconcile\n\nNothing to report.\n"), 0o644)
	}
	time.Sleep(time.Duration(p.Silence) * time.Second)
	fmt.Println(`{"type":"result","result":"reconciled"}`)
}

// countPrompts counts the sessions in the record file whose prompt starts
// with prefix.
func countPrompts(path, prefix string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(b), prefix)
}

// repairSession follows the repair plan for this start.
func repairSession(prompt string) {
	issue, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(prompt, "/af-impl --repair ")))
	var p repair
	if b, err := os.ReadFile(os.Getenv("SPECRUN_FAKE_SCRIPT")); err == nil {
		var s script
		if json.Unmarshal(b, &s) == nil && len(s.Repair) > 0 {
			p = s.Repair[min(countPrompts(os.Getenv("SPECRUN_FAKE_RECORD"), "/af-impl --repair ")-1, len(s.Repair)-1)]
		}
	}
	fmt.Println(`{"type":"assistant","n":1}`)
	branch := fmt.Sprintf("fix/%d-x", issue)
	if p.Merge || p.Stay {
		git("switch", "-q", "-c", branch)
		_ = os.WriteFile(fmt.Sprintf("fix-%d.txt", issue), []byte("fixed"), 0o644)
		git("add", "-A")
		git("commit", "-q", "-m", fmt.Sprintf("fix: #%d", issue))
	}
	if p.Merge {
		git("switch", "-q", "main")
		args := []string{"merge", "-q", "--no-ff", "-m", "Merge " + branch}
		if p.Outcome != "" {
			n := p.For
			if n == 0 {
				n = issue
			}
			args = append(args, "-m", fmt.Sprintf(`AF-Run: {"repair":{"issue":%d,"outcome":%q}}`, n, p.Outcome))
		}
		git(append(args, branch)...)
	}
	if p.Close {
		appendLine(os.Getenv("SPECRUN_FAKE_FORGE"), fmt.Sprintf(`{"op":"close","issue":%d}`, issue))
	}
	if p.Fail {
		appendLine(os.Getenv("SPECRUN_FAKE_FORGE"), fmt.Sprintf(`{"op":"add_labels","issue":%d,"labels":["af:repair-failed"]}`, issue))
	}
	for _, o := range p.Opens {
		b, _ := json.Marshal(map[string]any{"op": "open", "title": o[0], "labels": o[1:]})
		appendLine(os.Getenv("SPECRUN_FAKE_FORGE"), string(b))
	}
	if p.Dirty {
		_ = os.WriteFile("stray.txt", []byte("x"), 0o644)
	}
	time.Sleep(time.Duration(p.Silence) * time.Second)
	fmt.Println(`{"type":"result","result":"repaired"}`)
}
