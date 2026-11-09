package authoring

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/project"
)

func goProfile(t *testing.T) project.Profile {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return project.DetectProfile(dir)
}

func messages(entries []afspec.ValidationEntry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.Message)
	}
	return out
}

func TestLineReferenceRe_FindsALineOfAFile(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"edit afspec/afspec.go:88 to add it", []string{"afspec/afspec.go:88"}},
		{"see render.go:12-30", []string{"render.go:12-30"}},
		{"around line 40 of the file", []string{"line 40"}},
		{"Lines 3 to 9", []string{"Lines 3"}},
		{"add RenderedBytes to afspec/render.go", nil},
		{"listen on localhost:8080", nil},
		{"REQ-1.4 shares the check of REQ-1.2", nil},
		{"a timeout of 30s", nil},
	} {
		if got := lineReferenceRe.FindAllString(tc.text, -1); !slices.Equal(got, tc.want) {
			t.Errorf("%q: found %q, want %q", tc.text, got, tc.want)
		}
	}
}

func TestVagueWords_NamesEachWordOfACriterionInFieldOrder(t *testing.T) {
	cond := "the input is handled properly"
	got := VagueWords(&afspec.RequirementsV3Json{Requirements: []afspec.Requirement{{
		Id: "REQ-1", Criteria: []afspec.Criterion{{Id: "REQ-1.1", Condition: &cond, Action: "return a reasonable result, etc"}},
	}}})
	for _, h := range got {
		if h.Category != "warning" || h.Check != CheckVagueLanguage || h.Artifact != "requirements.json" || h.EntityID != "REQ-1.1" {
			t.Errorf("hint %+v", h)
		}
	}
	want := []string{
		`vague term "reasonable" in field action of criterion REQ-1.1`,
		`vague term "etc" in field action of criterion REQ-1.1`,
		`vague term "properly" in field condition of criterion REQ-1.1`,
	}
	if msgs := messages(got); !slices.Equal(msgs, want) {
		t.Errorf("hints = %q, want %q", msgs, want)
	}
}

func TestLineReferences_NamesStepsAndTouches(t *testing.T) {
	got := LineReferences(&afspec.TasksV3Json{Tasks: []afspec.Task{{
		Id:      2,
		Steps:   []string{"Add the field", "Fix the slice around store.go:88"},
		Touches: []string{"store.go", "render.go:12-30"},
	}}})
	want := []string{
		`step 2.2 names a line of a file, "store.go:88": say the behaviour or the decision, and name the file in touches`,
		`touches entry "render.go:12-30" of task 2 names a line of a file, "render.go:12-30": name the file alone`,
	}
	if msgs := messages(got); !slices.Equal(msgs, want) {
		t.Errorf("hints = %q, want %q", msgs, want)
	}
	for _, h := range got {
		if h.Check != CheckLineReference || h.Artifact != "tasks.json" || h.EntityID != "2" {
			t.Errorf("hint %+v", h)
		}
	}
}

func TestForeignTestCommands_NamesTheFieldAndTheProjectsOwnCommand(t *testing.T) {
	spec := "pytest tests/widget"
	got := ForeignTestCommands(&afspec.TasksV3Json{TestCommands: afspec.TestCommands{
		AllTests: "pytest -q", Linter: "go vet ./...", SpecTests: &spec,
	}}, goProfile(t))
	want := []string{
		`test_commands.all_tests is "pytest -q", which is a python command, but this project is go (detected from go.mod); the project's own is "go test ./... -count=1"`,
		`test_commands.spec_tests is "pytest tests/widget", which is a python command, but this project is go (detected from go.mod)`,
	}
	if msgs := messages(got); !slices.Equal(msgs, want) {
		t.Errorf("hints = %q, want %q", msgs, want)
	}
	if got[0].Check != CheckForeignTestCommand || got[0].EntityID != "test_commands.all_tests" {
		t.Errorf("hint %+v", got[0])
	}
}

// A clean package, a package missing its artifacts and a project of no known
// language all give an empty list, never nil, so a caller can write it as [].
func TestHints_IsEmptyWhenNothingIsFound(t *testing.T) {
	for name, spec := range map[string]*afspec.Spec{
		"nil":      nil,
		"no files": {},
		"clean": {
			Requirements: &afspec.RequirementsV3Json{Requirements: []afspec.Requirement{{Id: "REQ-1",
				Criteria: []afspec.Criterion{{Id: "REQ-1.1", Action: "return the cached widget"}}}}},
			Tasks: &afspec.TasksV3Json{TestCommands: afspec.TestCommands{AllTests: "pytest"},
				Tasks: []afspec.Task{{Id: 1, Steps: []string{"Add the cache"}, Touches: []string{"cache.go"}}}},
		},
	} {
		if got := Hints(spec, project.Profile{}); got == nil || len(got) != 0 {
			t.Errorf("%s: hints = %#v, want []", name, got)
		}
	}
}

func TestHints_OrdersRequirementsThenLinesThenCommands(t *testing.T) {
	got := Hints(&afspec.Spec{
		Requirements: &afspec.RequirementsV3Json{Requirements: []afspec.Requirement{{Id: "REQ-1",
			Criteria: []afspec.Criterion{{Id: "REQ-1.1", Action: "respond properly"}}}}},
		Tasks: &afspec.TasksV3Json{TestCommands: afspec.TestCommands{AllTests: "npm test"},
			Tasks: []afspec.Task{{Id: 1, Steps: []string{"see line 4"}}}},
	}, goProfile(t))
	var checks []string
	for _, h := range got {
		checks = append(checks, h.Check)
	}
	if want := []string{CheckVagueLanguage, CheckLineReference, CheckForeignTestCommand}; !slices.Equal(checks, want) {
		t.Errorf("checks = %q, want %q", checks, want)
	}
}
