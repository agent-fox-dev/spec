package spec

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// TS-10-23: make build installs spec, specr and speca the same way, with the
// same -ldflags, and its help line names all three.
func TestBuildInstallsEveryCommandTheSameWay_TS10_23(t *testing.T) {
	makeCmd, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make is not installed")
	}
	cmd := exec.Command(makeCmd, "-n", "build")
	cmd.Env = append(os.Environ(), "AGENTKIT_UPDATE=0")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("make -n build: %v", err)
	}

	var flags, paths []string
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "go install") {
			continue
		}
		i := strings.LastIndex(line, " ./cmd/")
		if i < 0 {
			t.Errorf("an install with no command: %q", line)
			continue
		}
		flags = append(flags, line[:i])
		paths = append(paths, line[i+1:])
	}
	slices.Sort(paths)
	if want := []string{"./cmd/spec", "./cmd/speca", "./cmd/specr"}; !slices.Equal(paths, want) {
		t.Fatalf("make build installs %v; want %v", paths, want)
	}
	for _, f := range flags {
		if !strings.Contains(f, "-ldflags") || f != flags[0] {
			t.Errorf("install flags differ: %q and %q", flags[0], f)
		}
	}
	if n := strings.Count(string(out), "-ldflags"); n != 3 {
		t.Errorf("-ldflags appears %d times; want 3", n)
	}

	makefile, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	var help string
	for _, line := range strings.Split(string(makefile), "\n") {
		if strings.HasPrefix(line, "build:") {
			_, help, _ = strings.Cut(line, "##")
		}
	}
	words := strings.FieldsFunc(help, func(r rune) bool { return r == ' ' || r == ',' || r == '.' })
	for _, command := range []string{"spec", "specr", "speca"} {
		if !slices.Contains(words, command) {
			t.Errorf("the build target's help line %q does not name %s", strings.TrimSpace(help), command)
		}
	}
}
