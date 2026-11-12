package specrun

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/agent-fox-dev/aflib/issuex"
)

// host is what a run takes from the process around it: the working
// directory, the environment its variables are read from and its sessions
// inherit, how its forge client is made, its clock and the skill hash it was
// built with. Main gives a run the process's
// own; a test gives each run one of its own, so that runs can go side by
// side in one test binary.
type host struct {
	dir      string
	env      []string
	newForge func(root string) (issuex.Client, issuex.Repo, error)
	// now is the clock the steps are timed by, and builtSkill the hash of
	// the af-impl skill specrun was built with.
	now        func() time.Time
	builtSkill string
}

// processHost is the host of the process specrun runs in.
func processHost() host {
	dir, _ := os.Getwd()
	return host{dir: dir, env: os.Environ(), newForge: defaultForge, now: time.Now, builtSkill: builtSkillSHA256}
}

// lookup is the variable's value, the last of its entries winning, as a
// process started with env would see it.
func (h host) lookup(name string) (string, bool) {
	for i := len(h.env) - 1; i >= 0; i-- {
		if k, v, ok := strings.Cut(h.env[i], "="); ok && k == name {
			return v, true
		}
	}
	return "", false
}

// getenv is the variable's value, "" when it is not set.
func (h host) getenv(name string) string {
	v, _ := h.lookup(name)
	return v
}

// lookPath finds the program on the host's PATH, as a shell started with its
// environment would.
func (h host) lookPath(name string) (string, error) {
	if runtime.GOOS == "windows" {
		return exec.LookPath(name)
	}
	for _, dir := range filepath.SplitList(h.getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s: %w", name, exec.ErrNotFound)
}

// homeDir is the user's home directory: $HOME, else the process's.
func (h host) homeDir() (string, error) {
	if home := h.getenv("HOME"); home != "" {
		return home, nil
	}
	return os.UserHomeDir()
}
