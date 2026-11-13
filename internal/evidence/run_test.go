package evidence

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/internal/gate"
)

// script writes an executable shell script named name into a new directory
// and returns its path.
func script(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// The command is split as the gate splits one and started with no shell, in
// the directory given, with one argument per test in place of {tests}.
func TestRun_StartsTheProgramWithTheTestsInPlaceOfThePlaceholder(t *testing.T) {
	t.Parallel()
	out := filepath.Join(t.TempDir(), "argv")
	stub := script(t, "stub", `pwd > '`+out+`'; for a in "$@"; do echo "$a"; done >> '`+out+`'`)
	dir := t.TempDir()

	if _, err := (Runner{Command: stub + ` --flag "two words" {tests}`, Dir: dir}).Run([]string{"a:T1", "b:T2/sub"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	realDir, _ := filepath.EvalSymlinks(dir)
	if got, _ := filepath.EvalSymlinks(lines[0]); got != realDir {
		t.Errorf("the program ran in %s; want %s", lines[0], dir)
	}
	if want := []string{"--flag", "two words", "a:T1", "b:T2/sub"}; !slices.Equal(lines[1:], want) {
		t.Errorf("the program received %q; want %q", lines[1:], want)
	}
}

// Each test asked for takes the status of its one result line; every other
// line, and a line for a test not asked for, is ignored; no line is missing,
// and two lines of different status are an error.
func TestParseResults_GivesEachTestTheStatusOfItsLine(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		stdout string
		want   map[string]Status
	}{
		"a line each": {`{"test":"A","status":"pass"}` + "\n" + `{"test":"B","status":"fail"}`,
			map[string]Status{"A": Pass, "B": Fail}},
		"the other lines ignored": {"=== RUN A\n" + `{"test":"A"}` + "\n" + `{"test":"A","status":"skipped"}` + "\n" + `[1,2]` + "\n" + `{"test":"B","status":"error"}`,
			map[string]Status{"A": Skipped, "B": Error}},
		"a line for a test not asked for": {`{"test":"C","status":"fail"}` + "\n" + `{"test":"A","status":"pass"}`,
			map[string]Status{"A": Pass, "B": Missing}},
		"no line":                           {"", map[string]Status{"A": Missing, "B": Missing}},
		"two lines of different status":     {`{"test":"A","status":"pass"}` + "\n" + `{"test":"A","status":"fail"}`, map[string]Status{"A": Error, "B": Missing}},
		"the same status twice":             {`{"test":"A","status":"pass"}` + "\n" + `{"test":"A","status":"pass"}`, map[string]Status{"A": Pass, "B": Missing}},
		"a status outside the five":         {`{"test":"A","status":"flaky"}`, map[string]Status{"A": Missing, "B": Missing}},
		"a missing test reported missing":   {`{"test":"B","status":"missing"}`, map[string]Status{"A": Missing, "B": Missing}},
		"a line with no newline at the end": {`{"test":"A","status":"fail"}`, map[string]Status{"A": Fail, "B": Missing}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := parseResults([]byte(tc.stdout), []string{"A", "B"}); !maps.Equal(got, tc.want) {
				t.Errorf("statuses %v; want %v", got, tc.want)
			}
		})
	}

	// The command's exit code decides nothing.
	stub := script(t, "stub", `echo '{"test":"A","status":"pass"}'; exit 3`)
	got, err := (Runner{Command: stub + " {tests}", Dir: t.TempDir()}).Run([]string{"A"})
	if err != nil || got.Status["A"] != Pass {
		t.Errorf("a command that exits 3: %v, %v; want A pass", got.Status, err)
	}
}

// The command's stdout and stderr go to a log under the gate's log directory,
// and the result names it.
func TestRun_KeepsTheOutputInALog(t *testing.T) {
	t.Setenv(gate.StateEnv, t.TempDir())
	store, err := gate.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stub := script(t, "stub", `echo '{"test":"A","status":"fail"}'; echo 'on stderr' >&2`)

	got, err := (Runner{Command: stub + " {tests}", Dir: t.TempDir(), LogDir: store.LogDir()}).Run([]string{"A"})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(got.Log) != store.LogDir() {
		t.Fatalf("log %q; want a file under %s", got.Log, store.LogDir())
	}
	b, err := os.ReadFile(got.Log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `{"test":"A","status":"fail"}`) || !strings.Contains(string(b), "on stderr") {
		t.Errorf("the log holds %q; want the command's stdout and stderr", b)
	}
}
