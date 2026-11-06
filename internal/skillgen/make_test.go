package main

import (
	"os/exec"
	"strings"
	"testing"
)

// repoRoot is the checkout, relative to this package, where the Makefile is.
const repoRoot = "../.."

// makeOutput runs make in the checkout and returns its combined output. The
// arguments are always a plan (--dry-run) or help, never a target that builds:
// running "make check" from a test would re-enter go test.
func makeOutput(t *testing.T, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	out, err := exec.Command("make", append([]string{"-C", repoRoot}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("make %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestMake_ListsTheRendersTargets(t *testing.T) {
	help := makeOutput(t, "help")
	for _, target := range []string{"skills", "skills-check", "install-skills", "uninstall-skills"} {
		if !strings.Contains(help, target) {
			t.Errorf("make help does not list %q:\n%s", target, help)
		}
	}
}

func TestMake_RunsTheRendersChecksInsideCheckAfterTest(t *testing.T) {
	plan := makeOutput(t, "-n", "check")
	test := strings.Index(plan, "go test ./...")
	render := strings.Index(plan, "internal/skillgen")
	switch {
	case test < 0:
		t.Fatalf("the plan of make check does not run the tests:\n%s", plan)
	case render < 0:
		t.Fatalf("the plan of make check does not run the render's checks:\n%s", plan)
	case render < test:
		t.Error("the plan of make check runs the render's checks before the tests")
	}
	if !strings.Contains(plan, "--check") {
		t.Errorf("the plan of make check does not pass --check:\n%s", plan)
	}
}

func TestMake_RunsTheRendersChecksWithNoNetworkAndNoCredential(t *testing.T) {
	plan := makeOutput(t, "-n", "skills-check")
	// The render imports only the standard library and go-yaml, so its own
	// target does not move the agentkit-go pin, which is what would need the
	// network, and it names no credential.
	for _, unwanted := range []string{"update-agentkit", "API_KEY", "TOKEN", "curl", "http"} {
		if strings.Contains(plan, unwanted) {
			t.Errorf("the plan of make skills-check mentions %q:\n%s", unwanted, plan)
		}
	}
	if !strings.Contains(plan, "go run ./internal/skillgen") {
		t.Errorf("the plan of make skills-check does not run the render:\n%s", plan)
	}
}

func TestMake_CleanRemovesTheRendersOutput(t *testing.T) {
	plan := makeOutput(t, "-n", "clean")
	// The render writes under build/, which clean removes whole.
	if !strings.Contains(plan, "rm -rf build") {
		t.Errorf("the plan of make clean does not remove build/:\n%s", plan)
	}
}
