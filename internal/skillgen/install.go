package main

import (
	"fmt"
	"io"
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

// defaultAgent is the agent an install serves when none is named, so that
// "make install-skills" with no AGENT does what it did before the render
// served four agents.
const defaultAgent = "claude"

// install renders every skill for one agent and replaces each one's directory
// under dir. Only <dir>/<name> is replaced, so a reference file dropped from
// the source is not left installed and no other entry under dir is touched.
func (s *source) install(a agent, dir string, o options, stdout, stderr io.Writer) error {
	for _, other := range a.alsoLoads(o) {
		for _, sk := range s.skills {
			if _, err := os.Stat(filepath.Join(other, sk.name)); err != nil {
				continue
			}
			fmt.Fprintf(stderr, "warning: %s also loads %s; it will see %s twice\n",
				a.name, filepath.Join(other, sk.name), sk.name)
		}
	}
	for _, sk := range s.skills {
		target := filepath.Join(dir, sk.name)
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		if err := writeSkill(target, renderSkill(sk, a)); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "installed: %s -> %s\n", sk.name, target)
	}
	return nil
}

// alsoLoads is the other directories this agent reads besides the one being
// installed into, so that a skill it would see twice can be named. A project
// install compares the project's directories and a user install the user's.
func (a agent) alsoLoads(o options) []string {
	if o.project != "" {
		return joinAll(o.project, a.alsoProject)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return joinAll(home, a.alsoUser)
}

// joinAll joins each relative directory onto base.
func joinAll(base string, dirs []string) []string {
	joined := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		joined = append(joined, filepath.Join(base, dir))
	}
	return joined
}

// uninstall removes each named skill from dir and names the ones it removed. A
// name the directory does not hold is skipped without a line, and no entry
// under dir other than those names is touched.
func uninstall(names []string, dir string, stdout io.Writer) error {
	for _, name := range names {
		target := filepath.Join(dir, name)
		if _, err := os.Stat(target); err != nil {
			continue
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "removed: %s\n", name)
	}
	return nil
}
