package specgen

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
)

// tasksTouching is a plan whose tasks touch the given paths, one list a task.
func tasksTouching(touches ...[]string) *afspec.TasksV3Json {
	plan := &afspec.TasksV3Json{}
	for i, t := range touches {
		plan.Tasks = append(plan.Tasks, afspec.Task{Id: i + 1, Touches: t})
	}
	return plan
}

// The grounding facts say, from the file system, whether each path the tasks
// name exists, once each, in task order and then touches order.
func TestGroundingFacts_SayWhetherEachPathExists(t *testing.T) {
	ws := newWorkspace(t)
	writeFiles(t, ws.Root(), map[string]string{"pkg/a.go": "package pkg\n"})
	if err := os.Mkdir(filepath.Join(ws.Root(), "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		plan *afspec.TasksV3Json
		want []groundingFact
	}{
		{"files, directories and what is not there", tasksTouching([]string{"pkg/a.go", "missing.go"}, []string{"docs", "../outside.go"}),
			[]groundingFact{{"pkg/a.go", true}, {"missing.go", false}, {"docs", true}, {"../outside.go", false}}},
		{"a path two tasks name", tasksTouching([]string{"pkg/a.go"}, []string{"docs", "pkg/a.go"}),
			[]groundingFact{{"pkg/a.go", true}, {"docs", true}}},
		{"no path at all", tasksTouching(nil, nil), []groundingFact{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := groundingFacts(ws, tc.plan); !slices.Equal(got, tc.want) {
				t.Errorf("facts = %+v; want %+v", got, tc.want)
			}
		})
	}
}
