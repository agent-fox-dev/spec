package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestResolveInstall_ResolvesEachAgentsDirectory(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	elsewhere := t.TempDir()
	codexHome := t.TempDir()
	piDir := t.TempDir()

	for name, tc := range map[string]struct {
		opts      options
		codexHome string
		piDir     string
		want      string
	}{
		"claude, no project":      {options{agent: "claude"}, "", "", filepath.Join(home, ".claude/skills")},
		"codex, no project":       {options{agent: "codex"}, "", "", filepath.Join(home, ".codex/skills")},
		"codex, CODEX_HOME":       {options{agent: "codex"}, codexHome, "", filepath.Join(codexHome, "skills")},
		"cursor, no project":      {options{agent: "cursor"}, "", "", filepath.Join(home, ".cursor/skills")},
		"copilot, no project":     {options{agent: "copilot"}, "", "", filepath.Join(home, ".copilot/skills")},
		"pi, no project":          {options{agent: "pi"}, "", "", filepath.Join(home, ".pi/agent/skills")},
		"pi, PI_CODING_AGENT_DIR": {options{agent: "pi"}, "", piDir, filepath.Join(piDir, "skills")},
		"opencode, no project":    {options{agent: "opencode"}, "", "", filepath.Join(home, ".config/opencode/skills")},
		"claude in a project":     {options{agent: "claude", project: project}, "", "", filepath.Join(project, ".claude/skills")},
		"codex in a project":      {options{agent: "codex", project: project}, "", "", filepath.Join(project, ".agents/skills")},
		"cursor in a project":     {options{agent: "cursor", project: project}, "", "", filepath.Join(project, ".cursor/skills")},
		"copilot in a project":    {options{agent: "copilot", project: project}, "", "", filepath.Join(project, ".github/skills")},
		"pi in a project":         {options{agent: "pi", project: project}, "", "", filepath.Join(project, ".pi/skills")},
		"opencode in a project":   {options{agent: "opencode", project: project}, "", "", filepath.Join(project, ".opencode/skills")},

		// Neither CODEX_HOME nor PI_CODING_AGENT_DIR reaches a project install,
		// which is the repository's own directory and not the user's.
		"codex in a project with CODEX_HOME set":       {options{agent: "codex", project: project}, codexHome, "", filepath.Join(project, ".agents/skills")},
		"pi in a project with PI_CODING_AGENT_DIR set": {options{agent: "pi", project: project}, "", piDir, filepath.Join(project, ".pi/skills")},

		"an explicit directory":                 {options{agent: "claude", skillsDir: elsewhere}, "", "", elsewhere},
		"an explicit directory beats a project": {options{agent: "copilot", project: project, skillsDir: elsewhere}, "", "", elsewhere},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", tc.codexHome)
			t.Setenv("PI_CODING_AGENT_DIR", tc.piDir)
			_, got, err := resolveInstall(tc.opts)
			if err != nil {
				t.Fatalf("resolveInstall: %v", err)
			}
			if got != tc.want {
				t.Errorf("directory = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveInstall_RefusesAnUnknownAgentOrAProjectThatIsNotADirectory(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, "a-regular-file")
	writeTree(t, home, map[string]string{"a-regular-file": "not a directory\n"})

	for name, tc := range map[string]struct {
		opts options
		want string
	}{
		"an agent outside the table": {
			options{agent: "gemini"},
			`unknown agent "gemini"; one of: claude, codex, cursor, copilot, pi, opencode`,
		},
		"a PROJECT that does not exist": {
			options{agent: "claude", project: filepath.Join(home, "nowhere")},
			"PROJECT " + filepath.Join(home, "nowhere") + " is not a directory",
		},
		"a PROJECT that is a regular file": {
			options{agent: "claude", project: file},
			"PROJECT " + file + " is not a directory",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", home)
			_, dir, err := resolveInstall(tc.opts)
			if err == nil {
				t.Fatalf("resolveInstall gave no error, directory %q", dir)
			}
			if err.Error() != tc.want {
				t.Errorf("error =\n  %s\nwant\n  %s", err, tc.want)
			}
		})
	}
}

func TestMain_InstallsEachSkillAndReplacesItsDirectoryWhole(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	source := t.TempDir()
	writeTree(t, source, threeSkills())

	// The directory already holds a stale reference file of af-spec and a skill
	// this render knows nothing about.
	skills := filepath.Join(home, ".claude", "skills")
	writeTree(t, skills, map[string]string{
		"af-spec/SKILL.md":            "an older af-spec\n",
		"af-spec/references/stale.md": "dropped from the source since\n",
		"other-skill/SKILL.md":        "somebody else's skill\n",
	})

	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--source", source, "--install"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
	for _, name := range []string{"af-prd", "af-spec", "af-impl"} {
		if _, err := os.Stat(filepath.Join(skills, name, skillDoc)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(skills, name, refsSubdir)); err != nil {
			t.Errorf("%s/references: %v", name, err)
		}
		want := "installed: " + name + " -> " + filepath.Join(skills, name) + "\n"
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout does not carry %q:\n%s", want, stdout.String())
		}
	}
	if _, err := os.Stat(filepath.Join(skills, "af-spec", refsSubdir, "stale.md")); !os.IsNotExist(err) {
		t.Error("the stale reference file survived the install")
	}
	if _, err := os.Stat(filepath.Join(skills, "other-skill", skillDoc)); err != nil {
		t.Errorf("the unrelated skill was touched: %v", err)
	}

	// Installing again after a reference file is dropped from the source leaves
	// nothing of it behind.
	if err := os.Remove(filepath.Join(source, "af-spec", refsSubdir, "verdict.md")); err != nil {
		t.Fatal(err)
	}
	writeTree(t, source, map[string]string{"af-spec/references/tasks.md": "# the plan, with no link onwards\n"})
	stdout.Reset()
	if code := Main([]string{"--source", source, "--install"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("the second install gave code %d, stderr %q", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(skills, "af-spec", refsSubdir, "verdict.md")); !os.IsNotExist(err) {
		t.Error("the dropped reference file is still installed")
	}
	if _, err := os.Stat(filepath.Join(skills, "other-skill", skillDoc)); err != nil {
		t.Errorf("the unrelated skill was touched by the second install: %v", err)
	}
}

func TestMain_InstallsNothingWhenTheSourceHoldsAProblem(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	source := t.TempDir()
	writeTree(t, source, map[string]string{
		"af-prd/SKILL.md":  skillFile("af-prd", "Read [references/gone.md](references/gone.md).\n"),
		"af-impl/SKILL.md": docOf(paddedFrontmatter, bodyOfBytes("# af-impl\n", bodyLimit+1)),
	})
	skills := t.TempDir()
	writeTree(t, skills, map[string]string{"unrelated.txt": "kept\n"})
	before := listing(t, skills)

	var stdout, stderr bytes.Buffer
	code := Main([]string{"--source", source, "--install", "--skills-dir", skills}, &stdout, &stderr)
	if code == exitOK {
		t.Fatalf("code %d, want non-zero", code)
	}
	for _, want := range []string{"links references/gone.md", "over the 24,576-byte limit"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr does not name %q:\n%s", want, stderr.String())
		}
	}
	if strings.Contains(stdout.String(), "installed:") {
		t.Errorf("stdout claims an install: %q", stdout.String())
	}
	if got := listing(t, skills); !slices.Equal(got, before) {
		t.Errorf("the directory changed: %v, want %v", got, before)
	}
}

func TestMain_WarnsWhenTheAgentAlsoLoadsADirectoryHoldingTheSameSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	source := t.TempDir()
	writeTree(t, source, threeSkills())
	// An earlier Claude Code install, which copilot also reads.
	claude := filepath.Join(home, ".claude", "skills")
	writeTree(t, claude, map[string]string{"af-spec/SKILL.md": "the one Claude Code installed\n"})

	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--source", source, "--install", "--agent", "copilot"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
	want := "warning: copilot also loads " + filepath.Join(claude, "af-spec") + "; it will see af-spec twice"
	if !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr does not warn %q:\n%s", want, stderr.String())
	}
	// It installs all the same, and leaves the other directory alone.
	if _, err := os.Stat(filepath.Join(home, ".copilot", "skills", "af-spec", skillDoc)); err != nil {
		t.Errorf("af-spec was not installed for copilot: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(claude, "af-spec", skillDoc))
	if err != nil || string(data) != "the one Claude Code installed\n" {
		t.Errorf("the claude directory was changed: %q, %v", data, err)
	}
}

func TestMain_UninstallsTheSourcesSkillsAndNothingElse(t *testing.T) {
	source := t.TempDir()
	writeTree(t, source, threeSkills())
	skills := t.TempDir()
	writeTree(t, skills, map[string]string{
		"af-prd/SKILL.md":      "installed earlier\n",
		"af-spec/SKILL.md":     "installed earlier\n",
		"other-skill/SKILL.md": "somebody else's skill\n",
		"notes.md":             "a loose file\n",
	})

	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--source", source, "--uninstall", "--skills-dir", skills}, &stdout, &stderr); code != exitOK {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "removed: af-prd\n") || !strings.Contains(got, "removed: af-spec\n") {
		t.Errorf("stdout = %q", got)
	}
	// af-impl was not installed, so it is skipped without a line.
	if strings.Contains(stdout.String(), "af-impl") {
		t.Errorf("stdout names af-impl, which was not installed: %q", stdout.String())
	}
	want := []string{"notes.md", "other-skill", filepath.Join("other-skill", "SKILL.md")}
	if got := listing(t, skills); !slices.Equal(got, want) {
		t.Errorf("the directory holds %v, want %v", got, want)
	}
}

func TestMain_RefusesAnUnknownAgentOrABadProjectWithoutWriting(t *testing.T) {
	source := t.TempDir()
	writeTree(t, source, threeSkills())
	skills := t.TempDir()
	writeTree(t, skills, map[string]string{"unrelated.txt": "kept\n"})
	before := listing(t, skills)
	file := filepath.Join(t.TempDir(), "a-regular-file")
	writeTree(t, filepath.Dir(file), map[string]string{"a-regular-file": "not a directory\n"})

	for name, tc := range map[string]struct {
		argv []string
		want string
	}{
		"an unknown agent": {
			[]string{"--agent", "gemini"},
			`unknown agent "gemini"; one of: claude, codex, cursor, copilot, pi, opencode`,
		},
		"a project that does not exist": {
			[]string{"--project", filepath.Join(source, "nowhere")},
			"PROJECT " + filepath.Join(source, "nowhere") + " is not a directory",
		},
		"a project that is a regular file": {
			[]string{"--project", file},
			"PROJECT " + file + " is not a directory",
		},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			argv := append([]string{"--source", source, "--install", "--skills-dir", skills}, tc.argv...)
			if code := Main(argv, &stdout, &stderr); code == exitOK {
				t.Fatalf("code %d, want non-zero", code)
			}
			if got := strings.TrimRight(stderr.String(), "\n"); got != tc.want {
				t.Errorf("stderr =\n  %s\nwant\n  %s", got, tc.want)
			}
			if got := listing(t, skills); !slices.Equal(got, before) {
				t.Errorf("the directory changed: %v", got)
			}
		})
	}
}

// --sha256 prints the SHA-256 of a skill's SKILL.md as it is installed for the
// agent, and writes nothing.
func TestMain_PrintsTheHashOfASkillAsItIsInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	source := t.TempDir()
	writeTree(t, source, threeSkills())
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--source", source, "--sha256", "af-impl"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("--sha256 wrote %v", entries)
	}
	var install bytes.Buffer
	if code := Main([]string{"--source", source, "--install"}, &install, &stderr); code != exitOK {
		t.Fatalf("install: code %d, stderr %q", code, stderr.String())
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "af-impl", skillDoc))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if got, want := strings.TrimSpace(stdout.String()), hex.EncodeToString(sum[:]); got != want {
		t.Errorf("--sha256 printed %q; the installed file hashes to %q", got, want)
	}
	stdout.Reset()
	if code := Main([]string{"--source", source, "--sha256", "no-such-skill"}, &stdout, &stderr); code != exitUsage {
		t.Errorf("an unknown skill: code %d", code)
	}
}
