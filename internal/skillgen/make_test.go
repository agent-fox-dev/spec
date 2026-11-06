package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// repoRoot is the checkout, relative to this package, where the Makefile is.
const repoRoot = "../.."

// makeOutput runs make in the checkout and returns its combined output. The
// arguments are always a plan (--dry-run) or help, never a target that builds:
// running "make check" from a test would re-enter go test.
//
// It pins SKILLS_SRC, because make passes a command-line variable down to a
// sub-make: without it, a "make check SKILLS_SRC=..." around these tests would
// decide which tree they read.
func makeOutput(t *testing.T, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	argv := append([]string{"-C", repoRoot, "SKILLS_SRC=" + defaultSource}, args...)
	out, err := exec.Command("make", argv...).CombinedOutput()
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

// The three behaviours below run the real make targets end to end, against the
// repository's own skills tree, with every component real: make, the render and
// the file system. They are the paths a maintainer walks.

func TestMake_SkillsRendersTheCommittedTreeForEveryAgent(t *testing.T) {
	out := filepath.Join(t.TempDir(), "skills")
	makeOutput(t, "skills", "SKILLS_OUT="+out)

	// Every agent gets every skill.
	src, problems := readSource(os.DirFS(repoSkills), repoSkills)
	if len(problems) != 0 {
		t.Fatalf("problems: %s", strings.Join(lines(problems), " | "))
	}
	for _, a := range agents {
		for _, sk := range src.skills {
			if _, err := os.Stat(filepath.Join(out, a.name, sk.name, skillDoc)); err != nil {
				t.Errorf("%s: %v", a.name, err)
			}
		}
	}

	// A maintainer reads the codex af-spec and finds that agent's frontmatter
	// and that agent's invocation syntax.
	data, err := os.ReadFile(filepath.Join(out, "codex", "af-spec", skillDoc))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	if keys := frontmatterKeys(doc); !slices.Equal(keys, []string{"name", "description"}) {
		t.Errorf("codex af-spec frontmatter keys = %v, want [name description]", keys)
	}
	if !strings.Contains(doc, "$af-impl") {
		t.Error("codex af-spec does not invoke af-impl in codex's syntax")
	}
	if strings.Contains(doc, "{{invoke") || strings.Contains(doc, "$ARGUMENTS") {
		t.Error("codex af-spec still carries a token or an argument placeholder")
	}
	// Its references/ holds the files it links.
	entries, err := os.ReadDir(filepath.Join(out, "codex", "af-spec", refsSubdir))
	if err != nil || len(entries) == 0 {
		t.Errorf("codex af-spec has no reference files: %v", err)
	}
}

func TestMake_InstallsIntoARepositoryForCodexAndRemovesThemAgain(t *testing.T) {
	project := t.TempDir()

	installed := makeOutput(t, "install-skills", "AGENT=codex", "PROJECT="+project)
	skills := filepath.Join(project, ".agents", "skills")
	for _, name := range []string{"af-prd", "af-spec", "af-impl"} {
		if !strings.Contains(installed, "installed: "+name+" -> "+filepath.Join(skills, name)) {
			t.Errorf("the install did not name %s:\n%s", name, installed)
		}
		if _, err := os.Stat(filepath.Join(skills, name, skillDoc)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(skills, name, refsSubdir)); err != nil {
			t.Errorf("%s/references: %v", name, err)
		}
	}

	removed := makeOutput(t, "uninstall-skills", "AGENT=codex", "PROJECT="+project)
	for _, name := range []string{"af-prd", "af-spec", "af-impl"} {
		if !strings.Contains(removed, "removed: "+name) {
			t.Errorf("the uninstall did not name %s:\n%s", name, removed)
		}
		if _, err := os.Stat(filepath.Join(skills, name)); !os.IsNotExist(err) {
			t.Errorf("%s is still installed", name)
		}
	}
}

func TestMake_SkillsCheckNamesEveryRuleABrokenSkillBreaksAndWritesNothing(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	// A copy of the committed tree, broken twice: one skill links a reference
	// file that does not exist, another's body is padded past the ceiling.
	source := filepath.Join(t.TempDir(), "skills")
	copyTree(t, repoSkills, source)
	appendTo(t, filepath.Join(source, "af-prd", skillDoc),
		"\nRead [references/nowhere.md](references/nowhere.md).\n")
	appendTo(t, filepath.Join(source, "af-spec", skillDoc), strings.Repeat("x", bodyLimit))
	out := filepath.Join(t.TempDir(), "out")

	cmd := exec.Command("make", "-C", repoRoot, "SKILLS_SRC="+source, "skills-check")
	report, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("make skills-check succeeded over a broken tree:\n%s", report)
	}
	for _, want := range []string{
		"links references/nowhere.md, which is in neither",
		"over the 24,576-byte limit",
	} {
		if !strings.Contains(string(report), want) {
			t.Errorf("the report does not name %q:\n%s", want, report)
		}
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("a broken run wrote an output tree")
	}
}
