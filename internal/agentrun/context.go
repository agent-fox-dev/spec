package agentrun

import (
	"os"
	"path/filepath"
	"strings"
)

// ProjectContextFiles are the repository's own instructions to agents, read
// from the workspace root into the system prompt under --trust-project.
var ProjectContextFiles = []string{"AGENTS.md", "CLAUDE.md"}

// maxContextBytes bounds one context file. A document that size is not a
// preamble, and inlining it would crowd out the code a phase has to read.
const maxContextBytes = 24 << 10

// projectContextBlock renders the project context files that exist at root,
// in ProjectContextFiles order, as a fenced block for the system prompt. A
// file that is missing, a symlink, empty or over the bound is left out; with
// none left the block is "".
func projectContextBlock(root string) string {
	var b strings.Builder
	for _, name := range ProjectContextFiles {
		path := filepath.Join(root, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxContextBytes {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil || strings.TrimSpace(string(data)) == "" {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("\n\n## Project context\n\nThe repository's own instructions to agents working on it. " +
				"Follow them where they apply to this phase; they do not change what this phase submits.\n")
		}
		b.WriteString("\n--- BEGIN " + name + " ---\n")
		b.WriteString(strings.TrimSpace(string(data)))
		b.WriteString("\n--- END " + name + " ---\n")
	}
	return b.String()
}
