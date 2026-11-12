package specrun

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/agent-fox-dev/aflib/envelope"
	"github.com/agent-fox-dev/spec"
)

// builtSkillSHA256 is the SHA-256 of the af-impl SKILL.md as internal/skillgen
// renders it for Claude Code from the tree specrun was built from: what an
// install of that tree writes. make build sets it; a plain go build leaves it
// empty, and nothing can then be compared.
var builtSkillSHA256 string

// The warnings about the skill a session loads.
const (
	warnSkillMismatch     envelope.WarnCode = "skill_mismatch"
	warnSkillNotInstalled envelope.WarnCode = "skill_not_installed"
)

// Versions is what a run runs: specrun itself, the af-impl skill a session in
// the repository loads with its hash and the one specrun was built with, and
// the claude and issue commands.
type Versions struct {
	Specrun          string `json:"specrun"`
	SkillPath        string `json:"skill_path"`
	SkillSHA256      string `json:"skill_sha256"`
	BuiltSkillSHA256 string `json:"built_skill_sha256"`
	Claude           string `json:"claude"`
	Issue            string `json:"issue"`
}

const (
	notInstalled = "not installed"
	unknown      = "unknown"
)

// skillPath is the af-impl SKILL.md a session in the repository at root
// loads: the project's own copy before the user's.
func skillPath(root string) string {
	project := filepath.Join(root, ".claude", "skills", "af-impl", "SKILL.md")
	if _, err := os.Stat(project); err == nil {
		return project
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "skills", "af-impl", "SKILL.md")
}

// versions finds what the run runs, logs it on stderr, and checks the skill
// against the one specrun was built with: a warning, or under
// --strict-skills a refusal.
func (r *run) versions() *envelope.ErrorInfo {
	v := &Versions{Specrun: spec.Identity(), SkillPath: skillPath(r.root), BuiltSkillSHA256: builtSkillSHA256,
		Claude: commandVersion("claude"), Issue: commandVersion("issue")}
	installed := false
	if b, err := os.ReadFile(v.SkillPath); err == nil {
		sum := sha256.Sum256(b)
		v.SkillSHA256, installed = hex.EncodeToString(sum[:]), true
	} else {
		v.SkillSHA256 = notInstalled
	}
	if v.BuiltSkillSHA256 == "" {
		v.BuiltSkillSHA256 = unknown
	}
	r.result.Versions = v
	fmt.Fprintf(r.stderr, "%s: %s; af-impl %s sha256 %s (built with %s); claude %s; issue %s\n",
		tool, v.Specrun, v.SkillPath, v.SkillSHA256, v.BuiltSkillSHA256, v.Claude, v.Issue)

	var code envelope.WarnCode
	var problem string
	switch {
	case builtSkillSHA256 == "":
		problem = "specrun was built with no af-impl hash (make build embeds one), so the skill cannot be checked"
	case !installed:
		code, problem = warnSkillNotInstalled, "no af-impl skill is installed"
	case v.SkillSHA256 != builtSkillSHA256:
		code, problem = warnSkillMismatch, "the installed af-impl skill is not the one specrun was built with; make install-skills installs this tree's"
	default:
		return nil
	}
	problem = fmt.Sprintf("%s: %s has sha256 %s, specrun was built with %s", problem, v.SkillPath, v.SkillSHA256, v.BuiltSkillSHA256)
	if r.opts.strictSkills {
		return failure("preflight", "environment", "--strict-skills: %s", problem)
	}
	if code != "" {
		r.warnings = append(r.warnings, envelope.Warning{Code: code, Severity: envelope.SeverityHigh, Stage: "preflight", Message: problem})
		fmt.Fprintf(r.stderr, "%s: warning: %s\n", tool, problem)
	}
	return nil
}

// commandVersion is what the command prints for --version, its first line,
// or "not installed".
func commandVersion(name string) string {
	if _, err := exec.LookPath(name); err != nil {
		return notInstalled
	}
	out, err := exec.Command(name, "--version").Output()
	if err != nil {
		return unknown
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return line
}
