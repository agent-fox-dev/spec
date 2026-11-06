package specpkg

import (
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
)

// Display shows a path relative to the root when it is inside it, and
// absolute otherwise.
func TestDisplay(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	outside := filepath.Join(root, "elsewhere", "01_a")
	for _, tc := range []struct {
		name, dir, want string
	}{
		{"inside", filepath.Join(repo, ".specs", "01_a"), filepath.Join(".specs", "01_a")},
		{"outside", outside, outside},
		{"the root itself", repo, "."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Display(tc.dir, repo); got != tc.want {
				t.Errorf("Display = %q, want %q", got, tc.want)
			}
		})
	}
}

// The rule of an entry is its Check, or its Category when the Check is empty,
// and each is named once, in the order it first appears.
func TestRules_NamesEachRuleOnceInOrder(t *testing.T) {
	entries := []afspec.ValidationEntry{
		{Check: "C3"}, {Check: "C7"}, {Check: "C3"},
		{Category: "schema"},
		{Category: "cross_spec_actor", Check: "cross_spec_actor"},
	}
	if got := Rules(entries); !slices.Equal(got, []string{"C3", "C7", "schema", "cross_spec_actor"}) {
		t.Errorf("Rules = %v", got)
	}
	if got := Rules(nil); len(got) != 0 {
		t.Errorf("Rules(nil) = %v; want none", got)
	}
}

// One TaskInfo per task in plan order, the derived integration task last,
// with depends_on copied, the state from the progress and ready from the
// plan's own rule; a package without a plan lists none.
func TestTaskInfos_ListsThePlanWithReadiness(t *testing.T) {
	spec := afspec.CreateSpec("05", "plan")
	spec.Tasks.Tasks = []afspec.Task{
		{Id: 1, Title: "first", Criteria: []string{"REQ-1"}, Steps: []string{"s"}},
		{Id: 2, Title: "second", Criteria: []string{"REQ-1"}, Steps: []string{"s"}, DependsOn: []int{1}},
		{Id: 3, Title: "third", Criteria: []string{"REQ-1"}, Steps: []string{"s"}, DependsOn: []int{1, 2}},
	}
	spec.Progress = &afspec.ProgressV3Json{Tasks: []afspec.TaskProgress{
		{Id: 1, State: afspec.TaskStateDone}, {Id: 2, State: afspec.TaskStatePending},
		{Id: 3, State: afspec.TaskStatePending}, {Id: 4, State: afspec.TaskStateInProgress},
	}}
	plan := spec.Plan()
	infos := TaskInfos(spec)
	if len(infos) != 4 || len(plan) != 4 {
		t.Fatalf("TaskInfos = %+v", infos)
	}
	for i, info := range infos {
		task := plan[i]
		if info.ID != task.Id || info.Title != task.Title || info.Kind != string(task.Kind) ||
			info.State != string(task.State) || !slices.Equal(info.DependsOn, task.DependsOn) {
			t.Errorf("entry %d = %+v; want task %+v", i, info, task)
		}
		if want := task.Id == 2; info.Ready != want {
			t.Errorf("task %d: ready = %v; want %v", task.Id, info.Ready, want)
		}
	}
	last := infos[3]
	if last.ID != 4 || last.Kind != string(afspec.TaskKindIntegration) || last.Title != afspec.IntegrationTaskTitle ||
		last.State != string(afspec.TaskStateInProgress) || !slices.Equal(last.DependsOn, []int{1, 2, 3}) {
		t.Errorf("the integration task = %+v", last)
	}
	infos[2].DependsOn[0] = 99
	if spec.Tasks.Tasks[2].DependsOn[0] != 1 {
		t.Error("mutating a TaskInfo's depends_on changed the plan")
	}

	if got := TaskInfos(&afspec.Spec{}); got == nil || len(got) != 0 {
		t.Errorf("a spec without tasks gives %#v; want an empty, non-nil list", got)
	}
}

// A package whose format version is neither absent nor 3 is legacy: the v3
// types read none of its progress and little of its plan.
func TestLegacy_IsAFormatVersionOtherThanThree(t *testing.T) {
	for version, want := range map[int]bool{0: false, afspec.SchemaVersion: false, 1: true, 2: true, 4: true} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			if got := Legacy(&afspec.Spec{SchemaVersion: version}); got != want {
				t.Errorf("Legacy(version %d) = %v; want %v", version, got, want)
			}
		})
	}
}
