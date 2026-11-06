package main

import (
	"path/filepath"
	"testing"
)

func TestResolveInstall_ResolvesEachAgentsDirectory(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	elsewhere := t.TempDir()
	codexHome := t.TempDir()

	for name, tc := range map[string]struct {
		opts      options
		codexHome string
		want      string
	}{
		"claude, no project":  {options{agent: "claude"}, "", filepath.Join(home, ".claude/skills")},
		"codex, no project":   {options{agent: "codex"}, "", filepath.Join(home, ".codex/skills")},
		"codex, CODEX_HOME":   {options{agent: "codex"}, codexHome, filepath.Join(codexHome, "skills")},
		"cursor, no project":  {options{agent: "cursor"}, "", filepath.Join(home, ".cursor/skills")},
		"copilot, no project": {options{agent: "copilot"}, "", filepath.Join(home, ".copilot/skills")},

		"claude in a project":  {options{agent: "claude", project: project}, "", filepath.Join(project, ".claude/skills")},
		"codex in a project":   {options{agent: "codex", project: project}, "", filepath.Join(project, ".agents/skills")},
		"cursor in a project":  {options{agent: "cursor", project: project}, "", filepath.Join(project, ".cursor/skills")},
		"copilot in a project": {options{agent: "copilot", project: project}, "", filepath.Join(project, ".github/skills")},

		// CODEX_HOME does not reach a project install, which is the repository's
		// own directory and not the user's.
		"codex in a project with CODEX_HOME set": {options{agent: "codex", project: project}, codexHome, filepath.Join(project, ".agents/skills")},

		"an explicit directory":                 {options{agent: "claude", skillsDir: elsewhere}, "", elsewhere},
		"an explicit directory beats a project": {options{agent: "copilot", project: project, skillsDir: elsewhere}, "", elsewhere},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", tc.codexHome)
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
		"an agent outside the four": {
			options{agent: "gemini"},
			`unknown agent "gemini"; one of: claude, codex, cursor, copilot`,
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
