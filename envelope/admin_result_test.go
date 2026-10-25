package envelope_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
)

// adminForms are one AdminResult for each form of speca.
func adminForms() (report, verdict, list envelope.AdminResult) {
	identity := envelope.AdminResult{
		SpecDir: ".specs/09_agent_mode", SpecID: "09", SpecName: "agent_mode", Title: "Agent mode",
		Status: "active", CreatedAt: "2026-10-22T07:23:01Z", UpdatedAt: "2026-10-22T07:23:01Z",
	}
	report = identity
	report.Valid = ptr(false)
	report.FailingRules = []string{"C3"}
	report.Tasks = []envelope.TaskInfo{{ID: 1, Title: "t", Kind: "implement", State: "pending", Ready: true}}
	report.TaskCounts = &envelope.TaskCounts{Pending: 1, Total: 1}
	report.NextTask = &report.Tasks[0]
	report.AllowedStatus = []string{"sealed"}
	report.DependsOn = []string{"08"}
	report.Dependents = []string{}

	verdict = identity
	verdict.Valid = ptr(true)
	verdict.Validation = &envelope.ValidationReport{Valid: true}
	verdict.Traceability = &envelope.TraceReport{CriteriaCovered: 3}

	list = envelope.AdminResult{Packages: []envelope.PackageInfo{{SpecDir: ".specs/01_a", SpecID: "01", Valid: ptr(true), TaskCounts: &envelope.TaskCounts{}}}}
	return report, verdict, list
}

// Each form of speca writes the fields it produces and no others; valid:false
// is written, and the slices a form shows as [] are written empty but left
// out when nil.
func TestAdminResult_EachFormWritesOnlyItsOwnFields(t *testing.T) {
	report, verdict, list := adminForms()

	t.Run("zero", func(t *testing.T) {
		if got := keysOf(t, envelope.AdminResult{}); len(got) != 0 {
			t.Errorf("the zero AdminResult writes %v; want nothing", sortedKeys(got))
		}
	})

	t.Run("report", func(t *testing.T) {
		got := keysOf(t, report)
		for _, k := range []string{"tasks", "allowed_status", "depends_on", "dependents", "task_counts", "next_task", "failing_rules", "valid"} {
			if _, ok := got[k]; !ok {
				t.Errorf("report lacks %s", k)
			}
		}
		for _, k := range []string{"validation", "traceability", "packages"} {
			if _, ok := got[k]; ok {
				t.Errorf("report has %s", k)
			}
		}
		if string(got["valid"]) != "false" {
			t.Errorf("a package that does not validate writes valid = %s; want false, present", got["valid"])
		}
	})

	t.Run("report with empty lists", func(t *testing.T) {
		empty := report
		empty.Tasks, empty.AllowedStatus, empty.DependsOn, empty.TaskCounts = []envelope.TaskInfo{}, []string{}, []string{}, &envelope.TaskCounts{}
		empty.NextTask, empty.FailingRules = nil, nil
		got := keysOf(t, empty)
		for _, k := range []string{"tasks", "allowed_status", "depends_on", "dependents"} {
			if string(got[k]) != "[]" {
				t.Errorf("an empty %s writes %s; want []", k, got[k])
			}
		}
	})

	t.Run("report with nil lists", func(t *testing.T) {
		nilSlices := report
		nilSlices.Tasks, nilSlices.AllowedStatus, nilSlices.DependsOn, nilSlices.Dependents = nil, nil, nil, nil
		got := keysOf(t, nilSlices)
		for _, k := range []string{"tasks", "allowed_status", "depends_on", "dependents"} {
			if _, ok := got[k]; ok {
				t.Errorf("a nil %s is written", k)
			}
		}
	})

	t.Run("verdict", func(t *testing.T) {
		got := keysOf(t, verdict)
		for _, k := range []string{"tasks", "task_counts", "next_task", "allowed_status", "depends_on", "dependents", "failing_rules", "packages"} {
			if _, ok := got[k]; ok {
				t.Errorf("the verdict has %s", k)
			}
		}
		for _, k := range []string{"spec_dir", "valid", "validation", "traceability"} {
			if _, ok := got[k]; !ok {
				t.Errorf("the verdict lacks %s", k)
			}
		}
	})

	t.Run("list", func(t *testing.T) {
		if got := sortedKeys(keysOf(t, list)); !slices.Equal(got, []string{"packages"}) {
			t.Errorf("the list writes %v; want only packages", got)
		}
	})

	t.Run("empty list", func(t *testing.T) {
		if got := keysOf(t, envelope.AdminResult{Packages: []envelope.PackageInfo{}}); len(got) != 1 || string(got["packages"]) != "[]" {
			t.Errorf("an empty list writes %v; want only packages: []", got)
		}
	})
}

// DecodeSpeca reads the envelope of every form and of a failure, and refuses
// what is not one envelope.
func TestDecodeSpeca(t *testing.T) {
	report, verdict, list := adminForms()
	for name, res := range map[string]envelope.AdminResult{"report": report, "verdict": verdict, "list": list} {
		t.Run(name, func(t *testing.T) {
			in := envelope.AdminEnvelope{Tool: "speca", Version: "v", OK: true, Status: "done", Summary: "s", Result: &res}
			b, err := json.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			out, err := envelope.DecodeSpeca(b)
			if err != nil {
				t.Fatal(err)
			}
			if out.Tool != "speca" || !out.OK || !reflect.DeepEqual(out.Result, in.Result) {
				t.Errorf("decoded %+v; want %+v", out.Result, in.Result)
			}
		})
	}

	t.Run("failure", func(t *testing.T) {
		failed := envelope.AdminEnvelope{Tool: "speca", ExitCode: envelope.ExitUsage, Status: "usage",
			Error: &envelope.ErrorInfo{Stage: "usage", Category: envelope.CategoryUsage, Message: "m"}}
		b, err := json.Marshal(failed)
		if err != nil {
			t.Fatal(err)
		}
		out, err := envelope.DecodeSpeca(b)
		if err != nil || out.Result != nil || out.Error == nil || out.Error.Stage != "usage" || out.ExitCode != envelope.ExitUsage {
			t.Errorf("an error envelope decoded to %+v, %v", out, err)
		}
	})

	for name, bad := range map[string]string{
		"not json":        "not json",
		"two objects":     `{"tool":"speca"} {"tool":"speca"}`,
		"valid as string": `{"result":{"valid":"yes"}}`,
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			if _, err := envelope.DecodeSpeca([]byte(bad)); err == nil {
				t.Errorf("DecodeSpeca(%q) = nil error", bad)
			}
		})
	}
}

// ready is written only for a ready task, a TaskCounts always writes its five
// counts, and a PackageInfo writes what its package's load produced.
func TestAdminResult_TaskAndPackageShapes(t *testing.T) {
	t.Run("ready task", func(t *testing.T) {
		if got := keysOf(t, envelope.TaskInfo{ID: 1, Title: "t", Kind: "implement", State: "pending", Ready: true}); string(got["ready"]) != "true" {
			t.Errorf("a ready task writes ready = %s; want true", got["ready"])
		}
		if got := keysOf(t, envelope.TaskInfo{ID: 1, Title: "t", Kind: "implement", State: "pending"}); got["ready"] != nil {
			t.Errorf("a task that is not ready writes ready = %s; want it absent", got["ready"])
		}
	})

	t.Run("task counts", func(t *testing.T) {
		want := []string{"done", "dropped", "in_progress", "pending", "total"}
		if got := sortedKeys(keysOf(t, envelope.TaskCounts{})); !slices.Equal(got, want) {
			t.Errorf("TaskCounts writes %v; want %v", got, want)
		}
	})

	t.Run("package that did not load", func(t *testing.T) {
		failed := envelope.PackageInfo{SpecDir: ".specs/03_x", LoadError: "cannot read prd.md"}
		if got := sortedKeys(keysOf(t, failed)); !slices.Equal(got, []string{"load_error", "spec_dir"}) {
			t.Errorf("a package that did not load writes %v; want spec_dir and load_error", got)
		}
	})

	t.Run("archived", func(t *testing.T) {
		if got := keysOf(t, envelope.PackageInfo{SpecDir: "x", Archived: true}); string(got["archived"]) != "true" {
			t.Errorf("an archived package writes archived = %s", got["archived"])
		}
		if got := keysOf(t, envelope.PackageInfo{SpecDir: "x"}); got["archived"] != nil {
			t.Error("a live package writes archived")
		}
	})

	t.Run("loaded package", func(t *testing.T) {
		loaded := envelope.PackageInfo{SpecDir: ".specs/01_a", SpecID: "01", SpecName: "a", Title: "A", Status: "draft",
			Valid: ptr(false), TaskCounts: &envelope.TaskCounts{Done: 1, Total: 2},
			NextTask: &envelope.TaskInfo{ID: 2, Title: "t", Kind: "implement", State: "pending", Ready: true}}
		b, err := json.Marshal(loaded)
		if err != nil {
			t.Fatal(err)
		}
		var back envelope.PackageInfo
		if err := json.Unmarshal(b, &back); err != nil || !reflect.DeepEqual(back, loaded) {
			t.Errorf("round trip = %+v, %v; want %+v", back, err, loaded)
		}
		if got := keysOf(t, loaded); string(got["valid"]) != "false" {
			t.Errorf("an invalid package writes valid = %s; want false, present", got["valid"])
		}
	})
}

// changes and dry_run round-trip through DecodeSpeca and are left out when
// empty.
func TestAdminResult_ChangesAndDryRun(t *testing.T) {
	changes := []envelope.Change{
		{Kind: "task", ID: 2, From: "pending", To: "in_progress"},
		{Kind: "task", ID: 3, From: "dropped", To: "pending", Forced: true},
	}
	with := envelope.AdminResult{SpecDir: ".specs/09_x", Changes: changes, DryRun: true}
	b, err := json.Marshal(envelope.AdminEnvelope{Tool: "speca", Result: &with})
	if err != nil {
		t.Fatal(err)
	}
	got, err := envelope.DecodeSpeca(b)
	if err != nil || got.Result == nil || !reflect.DeepEqual(got.Result.Changes, changes) || !got.Result.DryRun {
		t.Fatalf("decoded %+v, %v; want changes %+v and dry_run", got.Result, err, changes)
	}

	without := keysOf(t, envelope.AdminResult{SpecDir: ".specs/09_x", Changes: []envelope.Change{}})
	for _, k := range []string{"changes", "dry_run"} {
		if _, ok := without[k]; ok {
			t.Errorf("a result with no changes and no dry run writes %s", k)
		}
	}
}

// A change writes only the fields of its own kind.
func TestChange_WritesOnlyTheFieldsOfItsKind(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change envelope.Change
		want   map[string]string
	}{
		{"task with only an id", envelope.Change{Kind: "task", ID: 4},
			map[string]string{"kind": `"task"`, "id": "4"}},
		{"task", envelope.Change{Kind: "task", ID: 2, From: "pending", To: "in_progress"},
			map[string]string{"kind": `"task"`, "id": "2", "from": `"pending"`, "to": `"in_progress"`}},
		{"forced task", envelope.Change{Kind: "task", ID: 3, From: "dropped", To: "pending", Forced: true},
			map[string]string{"kind": `"task"`, "id": "3", "from": `"dropped"`, "to": `"pending"`, "forced": "true"}},
		{"status", envelope.Change{Kind: "status", From: "active", To: "sealed"},
			map[string]string{"kind": `"status"`, "from": `"active"`, "to": `"sealed"`}},
		{"moved", envelope.Change{Kind: "moved", From: ".specs/03_x", To: ".specs/archive/03_x"},
			map[string]string{"kind": `"moved"`, "from": `".specs/03_x"`, "to": `".specs/archive/03_x"`}},
		{"supersedes", envelope.Change{Kind: "supersedes", Spec: "12", Added: "03"},
			map[string]string{"kind": `"supersedes"`, "spec": `"12"`, "added": `"03"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := keysOf(t, tc.change)
			if len(got) != len(tc.want) {
				t.Errorf("writes %v; want %d members", sortedKeys(got), len(tc.want))
			}
			for k, v := range tc.want {
				if string(got[k]) != v {
					t.Errorf("%s = %s, want %s", k, got[k], v)
				}
			}
		})
	}
}
