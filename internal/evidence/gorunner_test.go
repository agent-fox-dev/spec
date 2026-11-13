package evidence

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// goModule lays out a Go module in a new directory, with the files given by
// their path under it, and returns the directory.
func goModule(t *testing.T, files map[string]string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("-short: the built-in runner builds and runs go test")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the go tool is not on the path")
	}
	dir := t.TempDir()
	files["go.mod"] = "module example.com/m\n\ngo 1.27\n"
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// statusesOf runs the tests with the built-in runner in dir.
func statusesOf(t *testing.T, dir string, tests ...string) []Status {
	t.Helper()
	got, err := (Runner{Dir: dir}).Run(tests)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]Status, len(tests))
	for i, test := range tests {
		out[i] = got.Status[test]
	}
	return out
}

// Without an evidence command, in a Go module, each <package>:<Test> and
// <package>:<Test>/<subtest> takes the status go test reports for it, run
// afresh every time.
func TestRun_RunsTheNamedGoTestsAndReadsTheirResults(t *testing.T) {
	dir := goModule(t, map[string]string{"p/p_test.go": `package p

import "testing"

func TestPass(t *testing.T) {}
func TestFail(t *testing.T) { t.Fatal("no") }
func TestSkip(t *testing.T) { t.Skip("not here") }
func TestParent(t *testing.T) {
	t.Run("ok", func(t *testing.T) {})
	t.Run("bad", func(t *testing.T) { t.Fatal("no") })
}
`})
	tests := []string{"./p:TestPass", "./p:TestFail", "./p:TestSkip", "./p:TestParent/bad", "./p:TestParent/ok"}
	want := []Status{Pass, Fail, Skipped, Fail, Pass}
	for range 2 {
		if got := statusesOf(t, dir, tests...); !slices.Equal(got, want) {
			t.Errorf("statuses %v; want %v", got, want)
		}
	}
}

// No test of a package runs but the ones named, and the parents of the
// subtests named.
func TestRun_RunsNoGoTestThatWasNotNamed(t *testing.T) {
	marker := func(name string) string { return filepath.Join(t.TempDir(), name) }
	other, twin := marker("other"), marker("twin")
	dir := goModule(t, map[string]string{"q/q_test.go": `package q

import (
	"os"
	"testing"
)

func TestNamed(t *testing.T) {}
func TestNamedTwin(t *testing.T) { os.WriteFile(` + "`" + twin + "`" + `, nil, 0o644) }
func TestOther(t *testing.T) { os.WriteFile(` + "`" + other + "`" + `, nil, 0o644) }
`})
	if got := statusesOf(t, dir, "./q:TestNamed"); !slices.Equal(got, []Status{Pass}) {
		t.Fatalf("statuses %v; want pass", got)
	}
	for _, m := range []string{other, twin} {
		if _, err := os.Stat(m); err == nil {
			t.Errorf("%s ran though it was not named", filepath.Base(m))
		}
	}
}

// Every test asked of a package that does not build is an error; a test that
// does not exist, and an identifier with no package, are missing; the others
// keep their own statuses.
func TestRun_GivesErrorToEveryTestOfAPackageThatDoesNotBuild(t *testing.T) {
	dir := goModule(t, map[string]string{
		"ok/ok_test.go":         "package ok\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) {}\n",
		"broken/broken_test.go": "package broken\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { undefined() }\n",
	})
	got := statusesOf(t, dir, "./broken:TestX", "./broken:TestY", "./ok:TestOK", "./ok:TestNoSuch", "no-separator", "./ok:")
	if want := []Status{Error, Error, Pass, Missing, Missing, Missing}; !slices.Equal(got, want) {
		t.Errorf("statuses %v; want %v", got, want)
	}
}
