package agentrun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The project context holds the context files that exist, in order, and
// leaves out one that is a symlink, empty or over the bound.
func TestProjectContextBlock(t *testing.T) {
	write := func(t *testing.T, dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("both, in order", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "CLAUDE.md", "claude rules")
		write(t, dir, "AGENTS.md", "agent rules")
		got := projectContextBlock(dir)
		a, c := strings.Index(got, "--- BEGIN AGENTS.md ---\nagent rules\n"), strings.Index(got, "--- BEGIN CLAUDE.md ---\nclaude rules\n")
		if !strings.Contains(got, "## Project context") || a < 0 || c < a {
			t.Errorf("block = %q", got)
		}
	})

	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, dir string)
	}{
		{"none", func(*testing.T, string) {}},
		{"empty", func(t *testing.T, dir string) { write(t, dir, "AGENTS.md", "") }},
		{"blank", func(t *testing.T, dir string) { write(t, dir, "AGENTS.md", " \n\t\n") }},
		{"over the bound", func(t *testing.T, dir string) { write(t, dir, "AGENTS.md", strings.Repeat("x", maxContextBytes+1)) }},
		{"a symlink", func(t *testing.T, dir string) {
			target := filepath.Join(t.TempDir(), "elsewhere.md")
			if err := os.WriteFile(target, []byte("outside rules"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(dir, "AGENTS.md")); err != nil {
				t.Skip(err)
			}
		}},
	} {
		t.Run("leaves out "+tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			if got := projectContextBlock(dir); got != "" {
				t.Errorf("block = %q, want none", got)
			}
		})
	}
}
