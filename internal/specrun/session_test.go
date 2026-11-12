//go:build unix

package specrun

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A session that writes no event for --stall is ended with every process it
// started, recorded as stalled, and the loop stops.
func TestSession_IsEndedWhenItStalls(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	pids := filepath.Join(f.scratch, "pids")
	t.Setenv("SPECRUN_FAKE_PIDS", pids)
	f.plan(t, map[string]any{"events": 1, "child": true, "silence_s": 60})
	env, code, _ := runMain(t, "--stall", "300ms")
	if code != 1 || env.Result == nil || env.Result.Stopped == nil || env.Result.Stopped.Reason != "stalled" {
		t.Fatalf("code %d, result %+v, error %+v; want a stalled stop", code, env.Result, env.Error)
	}
	if got := outcomes(f.ledgerLines(t)); strings.Join(got, ",") != "stalled" {
		t.Errorf("ledger outcomes %v", got)
	}
	if alive := pidsAlive(t, pids); len(alive) != 0 {
		t.Errorf("processes %v of the session are still running", alive)
	}
}

// A session that keeps writing events is not ended, however long it runs.
func TestSession_IsNotEndedWhileItWritesEvents(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.plan(t, map[string]any{"events": 10, "event_gap_ms": 100, "actions": []string{"land"}})
	env, code, _ := runMain(t, "--stall", "300ms")
	if code != 0 || env.Result == nil || len(env.Result.Landed) != 1 {
		t.Errorf("code %d, result %+v, error %+v; want the package landed", code, env.Result, env.Error)
	}
}

// Every line a session writes is saved as <step>.jsonl, and the text of its
// last result event as <step>.log, empty when there is none.
func TestSession_SavesItsStreamAndItsFinalMessage(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending")
	f.plan(t, map[string]any{"events": 3, "actions": []string{"land"}, "result": "done"},
		map[string]any{"events": 1, "actions": []string{"land"}})
	if _, code, stderr := runMain(t); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(f.logDir(), name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	stream := strings.Split(strings.TrimSpace(read("0001-implement-01_a.jsonl")), "\n")
	if len(stream) != 4 || !strings.Contains(stream[3], `"type":"result"`) {
		t.Errorf("the first stream holds %d lines: %q", len(stream), stream)
	}
	if got := read("0001-implement-01_a.log"); got != "done" {
		t.Errorf("the first final message is %q; want done", got)
	}
	if got := read("0002-implement-02_b.log"); got != "" {
		t.Errorf("the second final message is %q; want it empty", got)
	}
	if !exists(filepath.Join(f.logDir(), "0001-implement-01_a.stderr")) {
		t.Error("the session's stderr was not kept")
	}
}

// A stalled session is ended even when a process that left its group still
// holds its output open.
func TestSession_IsEndedWhenAProcessOutsideItsGroupHoldsItsOutput(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	pids := filepath.Join(f.scratch, "pids")
	t.Setenv("SPECRUN_FAKE_PIDS", pids)
	f.plan(t, map[string]any{"events": 1, "detached_child": true, "silence_s": 60})
	start := time.Now()
	_, code, _ := runMain(t, "--stall", "300ms")
	if took := time.Since(start); code != 1 || took > 30*time.Second {
		t.Errorf("code %d after %v; want a stall within seconds", code, took)
	}
	pidsAlive(t, pids) // ends what the session left
}

// An interrupt ends the session with what it started, records the step as
// stopped, and leaves it to be taken up again.
func TestSession_IsEndedWhenSpecrunIsInterrupted(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	pids := filepath.Join(f.scratch, "pids")
	t.Setenv("SPECRUN_FAKE_PIDS", pids)
	f.plan(t, map[string]any{"events": 1, "child": true, "silence_s": 60})
	go func() {
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if exists(pids) {
				_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
				return
			}
		}
	}()
	env, code, _ := runMain(t)
	if code != 1 || env.Result == nil || env.Result.Stopped == nil || env.Result.Stopped.Reason != "interrupted" {
		t.Fatalf("code %d, result %+v, error %+v; want an interrupted stop", code, env.Result, env.Error)
	}
	if alive := pidsAlive(t, pids); len(alive) != 0 {
		t.Errorf("processes %v of the session are alive", alive)
	}
	if got := outcomes(f.ledgerLines(t)); strings.Join(got, ",") != "stopped" {
		t.Errorf("ledger %v", got)
	}
}
