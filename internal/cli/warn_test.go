package cli

import (
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
)

// TS-11-50: the warning codes of the task form have their values and the stage
// task, and the shell's names are the envelope's constants.
func TestTaskWarningCodes_TS11_50(t *testing.T) {
	for _, tc := range []struct {
		code WarnCode
		want string
	}{
		{WarnTaskNotReady, "task_not_ready"},
		{WarnTaskStateForced, "task_state_forced"},
	} {
		if string(tc.code) != tc.want {
			t.Errorf("code %q; want %q", tc.code, tc.want)
		}
		if stage, ok := WarnStage(tc.code); !ok || stage != "task" {
			t.Errorf("WarnStage(%q) = %q, %v; want task", tc.code, stage, ok)
		}
	}
	if WarnTaskNotReady != envelope.WarnTaskNotReady || WarnTaskStateForced != envelope.WarnTaskStateForced {
		t.Error("the shell's aliases differ from the envelope's constants")
	}
	if WarnSpecInvalid != envelope.WarnSpecInvalid {
		t.Error("an existing alias changed")
	}
}

// TS-12-56: the warning codes of the status form have their values, are the
// envelope's constants, and are raised against the stage status.
func TestStatusWarningCodes_TS12_56(t *testing.T) {
	for _, tc := range []struct {
		code WarnCode
		want string
	}{
		{WarnTasksOpen, "tasks_open"},
		{WarnSupersedesNotRecorded, "supersedes_not_recorded"},
	} {
		if string(tc.code) != tc.want {
			t.Errorf("code %q; want %q", tc.code, tc.want)
		}
		run := NewRun("speca", "test")
		run.Warn(tc.code, "high", "x")
		ws := run.Warnings()
		if len(ws) != 1 || ws[0].Code != tc.code || ws[0].Stage != "status" {
			t.Errorf("Warn(%q) recorded %+v; want one warning against the stage status", tc.code, ws)
		}
	}
	if WarnTasksOpen != envelope.WarnTasksOpen || WarnSupersedesNotRecorded != envelope.WarnSupersedesNotRecorded {
		t.Error("the shell's aliases differ from the envelope's constants")
	}
	if WarnTaskNotReady != envelope.WarnTaskNotReady || WarnSpecInvalid != envelope.WarnSpecInvalid {
		t.Error("an existing alias changed")
	}
}
