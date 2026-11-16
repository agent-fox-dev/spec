//go:build unix

package specrun

import (
	"os"
	"path/filepath"
	"slices"
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
	f.setenv("SPECRUN_FAKE_PIDS", pids)
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
		b, err := os.ReadFile(f.stepFile(t, name))
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
	if f.stepFile(t, "0001-implement-01_a.stderr") == "" {
		t.Error("the session's stderr was not kept")
	}
}

// A stalled session is ended even when a process that left its group still
// holds its output open.
func TestSession_IsEndedWhenAProcessOutsideItsGroupHoldsItsOutput(t *testing.T) {
	f := newFixture(t)
	f.addPackage(t, "01_a", "01", "pending")
	pids := filepath.Join(f.scratch, "pids")
	f.setenv("SPECRUN_FAKE_PIDS", pids)
	f.plan(t, map[string]any{"events": 1, "detached_child": true, "silence_s": 60})
	start := time.Now()
	_, code, _ := runMain(t, "--stall", "300ms")
	if took := time.Since(start); code != 1 || took > 30*time.Second {
		t.Errorf("code %d after %v; want a stall within seconds", code, took)
	}
	pidsAlive(t, pids) // ends what the session left
}

// A second interrupt, or a termination, ends the session with what it
// started, records the step as stopped, and leaves it to be taken up again.
func TestSession_IsEndedWhenSpecrunIsInterruptedTwiceOrTerminated(t *testing.T) {
	for name, signals := range map[string][]syscall.Signal{
		"two interrupts": {syscall.SIGINT, syscall.SIGINT},
		"a termination":  {syscall.SIGTERM},
	} {
		t.Run(name, func(t *testing.T) {
			f := newSerialFixture(t) // the signal goes to the whole test process
			f.addPackage(t, "01_a", "01", "pending")
			pids := filepath.Join(f.scratch, "pids")
			f.setenv("SPECRUN_FAKE_PIDS", pids)
			f.plan(t, map[string]any{"events": 1, "child": true, "silence_s": 60})
			go signalOnce(pids, signals...)
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
		})
	}
}

// One interrupt lets the step in progress finish and starts no other: the
// package lands, the loop ends with nothing to take up again, and the next
// start goes on with the next package.
func TestSession_AnInterruptStopsTheLoopOnceTheStepEnds(t *testing.T) {
	f := newSerialFixture(t) // the interrupt goes to the whole test process
	f.addPackage(t, "01_a", "01", "pending")
	f.addPackage(t, "02_b", "02", "pending")
	first := land(0)
	first["silence_s"] = 2 // the interrupt comes while the session runs
	f.plan(t, first, land(0))
	go signalOnce(f.record, syscall.SIGINT)
	env, code, stderr := runMain(t)
	if code != 0 || !env.Result.StoppedAsAsked || env.Result.Stopped != nil || !slices.Equal(env.Result.Landed, []string{"01_a"}) ||
		!strings.Contains(env.Summary, "stopped as asked") {
		t.Fatalf("code %d, summary %q, result %+v, error %+v; want 01_a landed and a stop as asked", code, env.Summary, env.Result, env.Error)
	}
	if !strings.Contains(stderr, "stopping once the step in progress ends; interrupt again to stop now") {
		t.Errorf("stderr does not say the loop stops once the step ends:\n%s", stderr)
	}
	if got := specDirs(f.starts(t)); !slices.Equal(got, []string{".specs/01_a"}) {
		t.Errorf("sessions %v; want 01_a's alone", got)
	}
	if dry, _, _ := runMain(t, "--dry-run"); dry.Result.NextStep == nil || dry.Result.NextStep.Resumes {
		t.Errorf("next step %+v; want a new one, nothing taken up again", dry.Result.NextStep)
	}
	env, code, _ = runMain(t)
	if code != 0 || !slices.Equal(env.Result.Landed, []string{"02_b"}) {
		t.Errorf("the next start: code %d, landed %v; want 02_b", code, env.Result.Landed)
	}
}

// signalOnce sends the signals to the test process, one after another, once
// the file exists: a session has started.
func signalOnce(file string, signals ...syscall.Signal) {
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if exists(file) {
			for _, sig := range signals {
				_ = syscall.Kill(os.Getpid(), sig)
				time.Sleep(50 * time.Millisecond)
			}
			return
		}
	}
}
