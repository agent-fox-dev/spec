package afspec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// editRaw rewrites the JSON file at path through a generic map, which is
// another encoding than the library's: the keys come out sorted.
func editRaw(t *testing.T, path string, edit func(m map[string]any)) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	if b, err = json.MarshalIndent(m, "", "    "); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func contractHashOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	got, err := loadFixture(t, dir).ComputeContractHash()
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// firstCriterion is the first criterion of the first requirement of a
// requirements.json decoded into a map.
func firstCriterion(m map[string]any) map[string]any {
	return m["requirements"].([]any)[0].(map[string]any)["criteria"].([]any)[0].(map[string]any)
}

func firstTask(m map[string]any) map[string]any {
	return m["tasks"].([]any)[0].(map[string]any)
}

// The hash of a contract file is the hash of what it says: another encoding
// of it and its extension keys leave the hash as it was, and any other change
// changes the hash of that file and of no other.
func TestComputeContractHash_HashesWhatEachFileSays(t *testing.T) {
	req := func(dir string) string { return filepath.Join(dir, RequirementsFileName) }
	tasks := func(dir string) string { return filepath.Join(dir, TasksFileName) }
	withExtension := func(t *testing.T, dir string) {
		editRaw(t, req(dir), func(m map[string]any) { m["x-team"] = "search" })
	}

	for name, tc := range map[string]struct {
		before  func(t *testing.T, dir string) // makes the package the hash is first taken of
		edit    func(t *testing.T, dir string)
		changed string // the file whose hash changes, or "" for none
	}{
		"unchanged": {edit: func(*testing.T, string) {}},
		"saved again through Save": {edit: func(t *testing.T, dir string) {
			if err := loadFixture(t, dir).Save(dir); err != nil {
				t.Fatal(err)
			}
		}},
		"rewritten in another encoding": {edit: func(t *testing.T, dir string) {
			editRaw(t, req(dir), func(map[string]any) {})
			editRaw(t, tasks(dir), func(map[string]any) {})
		}},
		"an extension key added at the top level": {edit: withExtension},
		"an extension key added on a criterion": {edit: func(t *testing.T, dir string) {
			editRaw(t, req(dir), func(m map[string]any) { firstCriterion(m)["x-owner"] = "team" })
		}},
		"an extension key added on a task": {edit: func(t *testing.T, dir string) {
			editRaw(t, tasks(dir), func(m map[string]any) { firstTask(m)["x-estimate"] = 3 })
		}},
		"an extension key's value changed": {before: withExtension, edit: func(t *testing.T, dir string) {
			editRaw(t, req(dir), func(m map[string]any) { m["x-team"] = "index" })
		}},
		"an extension key removed": {before: withExtension, edit: func(t *testing.T, dir string) {
			editRaw(t, req(dir), func(m map[string]any) { delete(m, "x-team") })
		}},
		"a criterion's action edited": {changed: RequirementsFileName, edit: func(t *testing.T, dir string) {
			editRaw(t, req(dir), func(m map[string]any) { firstCriterion(m)["action"] = "do something else" })
		}},
		"a task's step edited": {changed: TasksFileName, edit: func(t *testing.T, dir string) {
			editRaw(t, tasks(dir), func(m map[string]any) { firstTask(m)["steps"] = []any{"another step"} })
		}},
		"the all_tests command edited": {changed: TasksFileName, edit: func(t *testing.T, dir string) {
			editRaw(t, tasks(dir), func(m map[string]any) {
				m["test_commands"].(map[string]any)["all_tests"] = "make test-all"
			})
		}},
		"a task's criteria reordered": {changed: TasksFileName, edit: func(t *testing.T, dir string) {
			editRaw(t, tasks(dir), func(m map[string]any) {
				task := firstTask(m)
				task["criteria"] = []any{"REQ-1.2", "REQ-1.1"}
			})
		}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := copyFixture(t, fixtureValidSpec)
			if tc.before != nil {
				tc.before(t, dir)
			}
			before := contractHashOf(t, dir)
			if len(before) != 2 || len(before[RequirementsFileName]) != 64 || len(before[TasksFileName]) != 64 {
				t.Fatalf("hash %v; want 64 hex digits for each of the two contract files", before)
			}
			tc.edit(t, dir)
			after := contractHashOf(t, dir)
			for _, file := range []string{RequirementsFileName, TasksFileName} {
				if same := before[file] == after[file]; same == (file == tc.changed) {
					t.Errorf("%s: hash %s, then %s; want it changed: %v", file, before[file], after[file], file == tc.changed)
				}
			}
		})
	}
}
