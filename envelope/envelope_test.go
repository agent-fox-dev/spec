package envelope_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
)

// A failure that happened before there was a result carries none: the
// decoded result is nil, and the error is an error a caller can return.
func TestDecodeAFailureWithoutAResult(t *testing.T) {
	const out = `{
  "tool": "specr",
  "version": "v1.0.0",
  "ok": false,
  "status": "usage",
  "exit_code": 2,
  "summary": "no such task",
  "error": {
    "stage": "usage",
    "category": "usage",
    "message": "09_agent_mode has no task 9; its tasks are 1, 2, 3",
    "retryable": false,
    "resumable": false,
    "fix_hint": {"flag": "--task", "valid": ["1", "2", "3"]}
  },
  "duration_ms": 3,
  "started_at": "2026-10-15T08:00:00Z"
}
`
	env, err := envelope.DecodeSpecr([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if env.OK || env.Status != "usage" || env.ExitCode != envelope.ExitUsage || env.Result != nil {
		t.Errorf("envelope = %+v", env)
	}
	var failure error = env.Error
	var info *envelope.ErrorInfo
	if !errors.As(failure, &info) || info.Stage != "usage" || info.Category != envelope.CategoryUsage {
		t.Fatalf("error = %v", failure)
	}
	if failure.Error() != info.Message || info.FixHint == nil || strings.Join(info.FixHint.Valid, ",") != "1,2,3" {
		t.Errorf("error info = %+v", info)
	}
	if info.Unwrap() != nil || info.TotalBudgetUSD() != 0 {
		t.Error("a decoded error has no cause")
	}
}

// Fields a decoder does not know are ignored, so the commands can add to
// their output without breaking a program that reads it.
func TestDecodeIgnoresUnknownFields(t *testing.T) {
	const out = `{"tool":"spec","ok":true,"status":"done","exit_code":0,"a_field_from_the_future":{"x":1},
"result":{"spec_id":"07","spec_dir":".specs/07_x","another":[1,2]}}`
	env, err := envelope.DecodeSpec([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if !env.OK || env.Result == nil || env.Result.SpecID != "07" || env.Result.SpecDir != ".specs/07_x" {
		t.Errorf("envelope = %+v", env)
	}
}

// A command writes exactly one object; anything else is not its output.
func TestDecodeRefusesWhatIsNotOneEnvelope(t *testing.T) {
	for name, data := range map[string]string{
		"empty":       ``,
		"prose":       `specr: no input`,
		"an array":    `[]`,
		"two objects": `{"ok":true} {"ok":true}`,
		"wrong type":  `{"exit_code":"zero"}`,
	} {
		if _, err := envelope.DecodeSpec([]byte(data)); err == nil {
			t.Errorf("%s: decoded without an error", name)
		}
	}
}

// The result of a command this package has no type for reads into any, and
// into a type of the caller's own.
func TestDecodeAnyResult(t *testing.T) {
	const out = `{"ok":true,"result":{"n":3}}`
	env, err := envelope.Decode[map[string]any]([]byte(out))
	if err != nil || env.Result["n"] != float64(3) {
		t.Errorf("map result: %+v, %v", env, err)
	}
	own, err := envelope.Decode[struct{ N int }]([]byte(out))
	if err != nil || own.Result.N != 3 {
		t.Errorf("own result: %+v, %v", own, err)
	}
}

// Every kind of artifact reads back as it was written, and an invalid
// package keeps its valid:false, which is a fact, not an absence.
func TestArtifactRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		a    envelope.Artifact
		json string
	}{
		{envelope.Artifact{Kind: envelope.ArtifactSpecPackage, Path: ".specs/01_a", ID: "01", Valid: true},
			`{"kind":"spec_package","path":".specs/01_a","id":"01","valid":true}`},
		{envelope.Artifact{Kind: envelope.ArtifactSpecPackage, Path: ".specs/02_b", ID: "02", DryRun: true},
			`{"kind":"spec_package","path":".specs/02_b","id":"02","valid":false,"dry_run":true}`},
		{envelope.Artifact{Kind: envelope.ArtifactComment, URL: "https://example.com/1#c", Role: "prd"},
			`{"kind":"comment","url":"https://example.com/1#c","role":"prd"}`},
		{envelope.Artifact{Kind: envelope.ArtifactMarkdownFile, Path: "/tmp/task-3.md"},
			`{"kind":"markdown_file","path":"/tmp/task-3.md"}`},
	} {
		b, err := json.Marshal(tc.a)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != tc.json {
			t.Errorf("marshal: got %s\nwant %s", b, tc.json)
		}
		var back envelope.Artifact
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatal(err)
		}
		if back != tc.a {
			t.Errorf("round trip: got %+v, want %+v", back, tc.a)
		}
	}
}

// What a command builds is what a program reads: a full spec result, with
// every kind of field set, survives a trip through JSON unchanged.
func TestSpecEnvelopeRoundTrip(t *testing.T) {
	valid := true
	want := envelope.SpecEnvelope{
		Tool: "spec", Version: "v1.0.0", OK: true, Status: "done", Summary: "spec: wrote .specs/01_a",
		Warnings: []envelope.Warning{{Code: envelope.WarnToolErrors, Severity: envelope.SeverityLow, Stage: "phase", Message: "m"}},
		Result: &envelope.SpecResult{
			Package: envelope.Package{
				SpecDir: ".specs/01_a", SpecID: "01", SpecName: "a", Title: "A", Status: "active", Source: "text",
				Artifacts: []string{"prd.md", "requirements.json"}, Requirements: 2, Criteria: 5, Tests: 7, Tasks: 3,
				Validation:    envelope.ValidationReport{Valid: true},
				Traceability:  envelope.TraceReport{CriteriaCovered: 5, PathsUncovered: []string{"EP-1"}},
				OpenQuestions: []envelope.OpenQuestion{{Question: "q", Decision: "d", Why: "w"}},
				RelevantFiles: []envelope.RelevantFile{{Path: "a.go", Why: "entry"}},
				Detail:        "full",
			},
			FollowOnSpecs: []envelope.Package{{SpecDir: ".specs/02_b", SpecID: "02", Artifacts: []string{}}},
			Split:         []envelope.ScopeReport{{Name: "a", Scope: "s", Status: envelope.ScopeDone, Valid: &valid}},
			SplitPlan:     ".specs/a.split.json",
			Preflight:     []envelope.PreflightCheck{{Check: "schemas_valid", OK: true}},
			Estimate:      &envelope.Estimate{Phases: 4, MaxTurnsPerPhase: 60},
		},
		Artifacts:   []envelope.Artifact{{Kind: envelope.ArtifactSpecPackage, Path: ".specs/01_a", ID: "01", Valid: true}},
		SideEffects: []envelope.SideEffect{{Action: "comment", Target: "o/r#1", OK: true, URL: "u"}},
		Next:        []envelope.Next{{Tool: "impl", Input: ".specs/01_a", Flags: []string{}, Why: "w"}},
		Input:       &envelope.InputInfo{Kind: "text", Bytes: 12},
		Model:       &envelope.ModelInfo{Spec: "STANDARD", ID: "m", Phases: map[string]envelope.PhaseModelInfo{"prd": {ID: "m"}}},
		Usage: &envelope.UsageInfo{Turns: 4, CostUSD: 0.5, Phases: []envelope.PhaseInfo{
			{Name: "prd", Turns: 2, StopReason: "end_turn", ToolCalls: map[string]int{"list_files": 1}}}},
		DurationMS: 1200, StartedAt: "2026-10-15T08:00:00Z",
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := envelope.DecodeSpec(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip changed the envelope:\n got %+v\nwant %+v", got, want)
	}
}

// --detail summary writes a trimmed view of the result, which reads into the
// same type: the fields it leaves out are zero, and Detail says so.
func TestDecodeTheSummaryView(t *testing.T) {
	const out = `{"ok":true,"result":{"spec_dir":".specs/01_a","spec_id":"01","spec_name":"a","status":"active",
"artifacts":["prd.md"],"validation":{"valid":true,"error_count":0},
"traceability":{"criteria_covered":4,"paths_covered":1},"detail":"summary"}}`
	env, err := envelope.DecodeSpec([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	r := env.Result
	if r.Detail != "summary" || !r.Validation.Valid || r.Traceability.CriteriaCovered != 4 || r.Title != "" || r.Requirements != 0 {
		t.Errorf("result = %+v", r)
	}
}

// Sections hands out its own slice, so one caller cannot reorder another's.
func TestSectionsIsAFreshSlice(t *testing.T) {
	a := envelope.Sections()
	if strings.Join(a, ",") != "prd,architecture,requirements,test_spec,tasks" {
		t.Fatalf("sections = %v", a)
	}
	a[0] = "changed"
	if envelope.Sections()[0] != envelope.SectionPRD {
		t.Error("Sections shares its backing array")
	}
}

func TestStatusAndRetryable(t *testing.T) {
	for code, want := range map[int]string{envelope.ExitOK: "done", envelope.ExitFailed: "failed", envelope.ExitUsage: "usage", 7: "failed"} {
		if got := envelope.StatusFor(code); got != want {
			t.Errorf("StatusFor(%d) = %q, want %q", code, got, want)
		}
	}
	for cat, want := range map[string]bool{envelope.CategoryAPI: true, envelope.CategoryAborted: true, envelope.CategoryBudget: false, "": false} {
		if got := envelope.RetryableFor(cat); got != want {
			t.Errorf("RetryableFor(%q) = %v, want %v", cat, got, want)
		}
	}
}
