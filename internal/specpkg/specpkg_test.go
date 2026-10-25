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

// One TaskInfo per task in plan order, with depends_on copied and ready from
// the plan's own rule; a package without a plan lists none.
func TestTaskInfos_ListsThePlanWithReadiness(t *testing.T) {
	yes := true
	plan := &afspec.TasksV2Json{Tasks: []afspec.Task{
		{Id: 1, Title: "first", Kind: afspec.TaskKindImplement, State: afspec.TaskStateDone},
		{Id: 2, Title: "second", Kind: afspec.TaskKindImplement, State: afspec.TaskStatePending, DependsOn: []int{1}},
		{Id: 3, Title: "third", Kind: afspec.TaskKindImplement, State: afspec.TaskStatePending, DependsOn: []int{2}},
		{Id: 4, Title: "fourth", Kind: afspec.TaskKindIntegration, State: afspec.TaskStateInProgress, Optional: &yes, DependsOn: []int{1, 2}},
	}}
	infos := TaskInfos(&afspec.Spec{Tasks: plan})
	if len(infos) != 4 {
		t.Fatalf("TaskInfos = %+v", infos)
	}
	for i, info := range infos {
		task := plan.Tasks[i]
		if info.ID != task.Id || info.Title != task.Title || info.Kind != string(task.Kind) ||
			info.State != string(task.State) || info.Optional != task.IsOptional() {
			t.Errorf("entry %d = %+v; want task %+v", i, info, task)
		}
		if want := task.Id == 2; info.Ready != want {
			t.Errorf("task %d: ready = %v; want %v", task.Id, info.Ready, want)
		}
	}
	if !slices.Equal(infos[3].DependsOn, []int{1, 2}) {
		t.Errorf("depends_on = %v", infos[3].DependsOn)
	}
	infos[3].DependsOn[0] = 99
	if plan.Tasks[3].DependsOn[0] != 1 {
		t.Error("mutating a TaskInfo's depends_on changed the plan")
	}

	if got := TaskInfos(&afspec.Spec{}); got == nil || len(got) != 0 {
		t.Errorf("a spec without tasks gives %#v; want an empty, non-nil list", got)
	}
}

// A package whose format version is neither absent nor 2 is legacy: the v2
// types read none of its tests or tasks.
func TestLegacy_IsAFormatVersionOtherThanTwo(t *testing.T) {
	for version, want := range map[int]bool{0: false, afspec.SchemaVersion: false, 1: true, 3: true} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			if got := Legacy(&afspec.Spec{SchemaVersion: version}); got != want {
				t.Errorf("Legacy(version %d) = %v; want %v", version, got, want)
			}
		})
	}
}
