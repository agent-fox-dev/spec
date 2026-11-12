package gate

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// cleanVars are the variables a clean environment sets, every one of them
// replacing what the caller had.
var cleanVars = []string{"HOME", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM", "GIT_TERMINAL_PROMPT"}

// CleanEnv is base with HOME set to home, git's global and system
// configuration pointed at the null device and switched off, and
// GIT_TERMINAL_PROMPT=0, so that a test that passes only on this machine's
// configuration fails; every other variable is kept.
func CleanEnv(base []string, home string) []string {
	out := make([]string, 0, len(base)+len(cleanVars))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(cleanVars, name) {
			out = append(out, kv)
		}
	}
	return append(out, "HOME="+home, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
}

// NewHome creates an empty directory, outside every repository, for a clean
// gate's HOME, and returns it with the function that removes it.
func NewHome() (string, func(), error) {
	dir, err := os.MkdirTemp("", "speca-gate-home-")
	if err != nil {
		return "", nil, err
	}
	return dir, func() { removeAll(dir) }, nil
}

// removeAll removes dir with everything in it, making its directories
// writable first where a command left them read-only (Go's module cache does).
func removeAll(dir string) {
	if os.RemoveAll(dir) == nil {
		return
	}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
	_ = os.RemoveAll(dir)
}
