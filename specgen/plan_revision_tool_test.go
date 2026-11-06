package specgen

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/core"

	"github.com/agent-fox-dev/spec/afspec"
)

// planAsSubmitted is the plan's three artifacts as a reviser submits them,
// each one a JSON object a test may edit.
func planAsSubmitted(t *testing.T, plan *afspec.PartialSpec) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for key, artifact := range map[string]any{"requirements": plan.Requirements, "test_spec": plan.TestSpec, "tasks": plan.Tasks} {
		b, err := json.Marshal(artifact)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		out[key] = m
	}
	return out
}

// revisePlan submits a plan revision of plan answering findings.
func revisePlan(t *testing.T, plan *afspec.PartialSpec, findings []Finding, artifacts map[string]map[string]any,
	extra map[string]any, answers ...any) (core.ToolResult, *planRevisionSink) {
	t.Helper()
	var sink planRevisionSink
	payload := map[string]any{"dispositions": answers}
	for k, v := range artifacts {
		payload[k] = v
	}
	for k, v := range extra {
		payload[k] = v
	}
	tool := newPlanRevisionTool(&sink, PRD{Body: "## Intent\n\nThe loader loads.\n"}, plan, findings, nil)
	return tool.Execute(t.Context(), mustJSON(t, payload)), &sink
}

func planFindings(rubrics ...string) []Finding {
	var out []Finding
	for i, r := range rubrics {
		f := roundFindings("R1-F" + string(rune('1'+i)))[0]
		f.Rubric = r
		out = append(out, f)
	}
	return out
}

// A revision submits the three artifacts whole, and may change neither the
// PRD, nor the spec's identity, nor a task's state, nor the actors.
func TestPlanRevisionTool_ChangesThePlanAndNothingElse(t *testing.T) {
	plan := planUnderReview(t)
	findings := planFindings("zero_decision")
	edit := func(f func(a map[string]map[string]any)) map[string]map[string]any {
		a := planAsSubmitted(t, plan)
		f(a)
		return a
	}
	retitled := func(a map[string]map[string]any) {
		task := a["tasks"]["tasks"].([]any)[0].(map[string]any)
		task["title"] = task["title"].(string) + " in afspec/afspec.go"
	}
	firstPath := func(a map[string]map[string]any) map[string]any {
		return a["requirements"]["execution_paths"].([]any)[0].(map[string]any)
	}
	for _, tc := range []struct {
		name  string
		plan  map[string]map[string]any
		extra map[string]any
		says  []string // nil when accepted
	}{
		{"only the three artifacts", edit(retitled), nil, nil},
		{"the PRD body", edit(retitled), map[string]any{"body": "## Intent\n\nAnother intent.\n"}, []string{"PRD", "body"}},
		{"the spec id", edit(func(a map[string]map[string]any) { retitled(a); a["requirements"]["spec_id"] = "99" }), nil,
			[]string{"spec_id", "requirements"}},
		{"the spec name", edit(func(a map[string]map[string]any) { retitled(a); a["tasks"]["spec_name"] = "other" }), nil,
			[]string{"spec_name", "tasks"}},
		{"a task's state", edit(func(a map[string]map[string]any) {
			retitled(a)
			a["tasks"]["tasks"].([]any)[1].(map[string]any)["state"] = "done"
		}), nil, []string{"state", "task 2"}},
		{"an actor", edit(func(a map[string]map[string]any) {
			retitled(a)
			firstPath(a)["steps"].([]any)[0].(map[string]any)["actor"] = "a stranger"
		}), nil, []string{"actor", "a stranger"}},
		{"the paths and steps reordered, an actor's case changed", edit(func(a map[string]map[string]any) {
			retitled(a)
			step := firstPath(a)["steps"].([]any)[0].(map[string]any)
			step["actor"] = strings.ToUpper(step["actor"].(string))
		}), nil, nil},
		{"two forbidden things", edit(func(a map[string]map[string]any) {
			retitled(a)
			a["requirements"]["spec_id"] = "99"
			a["tasks"]["tasks"].([]any)[1].(map[string]any)["state"] = "done"
		}), nil, []string{"spec_id", "task 2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, sink := revisePlan(t, plan, findings, tc.plan, tc.extra, fixed("R1-F1"))
			got, _, recorded := sink.get()
			if tc.says == nil {
				if !r.OK || !recorded || !strings.Contains(got.Tasks.Tasks[0].Title, "in afspec/afspec.go") {
					t.Errorf("turned back or not recorded: %+v", r)
				}
				return
			}
			if r.OK || recorded {
				t.Fatalf("accepted: %+v", r)
			}
			for _, s := range tc.says {
				if !strings.Contains(r.Detail, s) {
					t.Errorf("the error does not say %q: %s", s, r.Detail)
				}
			}
		})
	}
}

// A revised plan that breaks the format's own rules is turned back with every
// rule it broke, and the one that passes is recorded.
func TestPlanRevisionTool_TurnsBackAPlanThatBreaksARule(t *testing.T) {
	plan := planUnderReview(t)
	findings := planFindings("grounding")
	for _, tc := range []struct {
		name string
		edit func(a map[string]map[string]any)
		says []string
	}{
		{"a schema rule", func(a map[string]map[string]any) {
			a["tasks"]["tasks"].([]any)[0].(map[string]any)["kind"] = "refactor"
		}, []string{"json_schema", "tasks.json"}},
		{"a cross-file rule", func(a map[string]map[string]any) {
			task := a["tasks"]["tasks"].([]any)[0].(map[string]any)
			task["tests"] = []any{"TS-01-99"}
		}, []string{"C6", "TS-01-99"}},
		{"three rules", func(a map[string]map[string]any) {
			task := a["tasks"]["tasks"].([]any)[0].(map[string]any)
			task["tests"] = []any{"TS-01-99"}
			task["criteria"] = []any{"01-REQ-99.1"}
			a["test_spec"]["tests"].([]any)[0].(map[string]any)["verifies"] = []any{"01-REQ-98.1"}
		}, []string{"TS-01-99", "01-REQ-99.1", "01-REQ-98.1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := planAsSubmitted(t, plan)
			tc.edit(a)
			r, sink := revisePlan(t, plan, findings, a, nil, fixed("R1-F1"))
			if _, _, recorded := sink.get(); r.OK || recorded {
				t.Fatalf("accepted: %+v", r)
			}
			for _, s := range tc.says {
				if !strings.Contains(r.Detail, s) {
					t.Errorf("the error does not say %q: %s", s, r.Detail)
				}
			}
		})
	}
	a := planAsSubmitted(t, plan)
	a["tasks"]["tasks"].([]any)[0].(map[string]any)["title"] = "A clearer title"
	if r, sink := revisePlan(t, plan, findings, a, nil, fixed("R1-F1")); !r.OK {
		t.Errorf("a valid revision was turned back: %s", r.Detail)
	} else if got, _, ok := sink.get(); !ok || got.Tasks.Tasks[0].Title != "A clearer title" {
		t.Errorf("the valid revision was not recorded")
	}
}

// A steering finding of a plan review cannot be declined; the plan's other
// rubrics can, with a reason.
func TestPlanRevisionTool_ASteeringFindingCannotBeDeclined(t *testing.T) {
	plan := planUnderReview(t)
	findings := planFindings("steering", "grounding", "zero_decision")
	findings[2].Severity = "minor"
	keep := func(id string) any { return contest(id, "it holds", "The loader loads.") }
	a := planAsSubmitted(t, plan)

	r, sink := revisePlan(t, plan, findings, a, nil, decline("R1-F1", "a later package"), keep("R1-F2"), keep("R1-F3"))
	if _, _, recorded := sink.get(); r.OK || recorded || !strings.Contains(r.Detail, "R1-F1") || !strings.Contains(r.Detail, "steering") ||
		!strings.Contains(r.Detail, "fixed, contested or escalated") {
		t.Errorf("a declined steering finding: %+v", r)
	}
	for _, answers := range [][]any{
		{keep("R1-F1"), decline("R1-F2", "spec 19 carries it"), keep("R1-F3")},
		{keep("R1-F1"), keep("R1-F2"), decline("R1-F3", "outside this package's scope")},
	} {
		if r, _ := revisePlan(t, plan, findings, a, nil, answers...); !r.OK {
			t.Errorf("a declined grounding or zero_decision finding was turned back: %s", r.Detail)
		}
	}
}
