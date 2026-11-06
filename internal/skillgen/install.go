package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// options are an install's inputs, as the make targets pass AGENT, PROJECT and
// SKILLS_DIR through.
type options struct {
	agent     string
	project   string
	skillsDir string
}

// resolveInstall picks the agent and the directory to install into. Both
// refusals are checked before the source tree is read, so neither renders
// anything.
func resolveInstall(o options) (agent, string, error) {
	a, found := lookupAgent(o.agent)
	if !found {
		return agent{}, "", fmt.Errorf("unknown agent %q; one of: %s",
			o.agent, strings.Join(agentNames(), ", "))
	}
	if o.project != "" {
		info, err := os.Stat(o.project)
		if err != nil || !info.IsDir() {
			return agent{}, "", fmt.Errorf("PROJECT %s is not a directory", o.project)
		}
	}
	dir, err := a.skillsDir(o)
	if err != nil {
		return agent{}, "", err
	}
	return a, dir, nil
}

// skillsDir is where this agent loads skills from, given the options: an
// explicit directory wins, then the agent's directory inside the project, then
// its user directory.
func (a agent) skillsDir(o options) (string, error) {
	switch {
	case o.skillsDir != "":
		return o.skillsDir, nil
	case o.project != "":
		return filepath.Join(o.project, a.projectDir), nil
	}
	return a.userSkillsDir()
}

// userSkillsDir is the agent's directory under the user's home. codex reads
// CODEX_HOME, and its directory is that value plus skills/, not a dotted
// directory inside it.
func (a agent) userSkillsDir() (string, error) {
	if a.homeEnv != "" {
		if base := os.Getenv(a.homeEnv); base != "" {
			return filepath.Join(base, "skills"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, a.userDir), nil
}
