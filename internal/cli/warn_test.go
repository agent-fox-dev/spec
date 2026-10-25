package cli

import "testing"

// A warning is recorded against its code's one stage, which the caller never
// passes in.
func TestRun_WarnRecordsTheCodeAgainstItsStage(t *testing.T) {
	for code, stage := range map[WarnCode]string{
		WarnInputLooksLikePath:    "input",
		WarnEffortClamped:         "preflight",
		WarnToolErrors:            "phase",
		WarnOutputNotWritten:      "emit",
		WarnSpecInvalid:           "validate",
		WarnTaskNotReady:          "task",
		WarnTaskStateForced:       "task",
		WarnTasksOpen:             "status",
		WarnSupersedesNotRecorded: "status",
	} {
		t.Run(string(code), func(t *testing.T) {
			run := NewRun("speca", "test")
			run.Warn(code, "high", "  x  ")
			ws := run.Warnings()
			if len(ws) != 1 || ws[0].Code != code || ws[0].Stage != stage || ws[0].Severity != "high" || ws[0].Message != "x" {
				t.Errorf("Warn recorded %+v; want one warning against the stage %s", ws, stage)
			}
		})
	}
	if _, ok := WarnStage("no_such_code"); ok {
		t.Error("an undeclared code has a stage")
	}
}
