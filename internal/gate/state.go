package gate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

// StateEnv is the variable that names the user's state directory.
const StateEnv = "XDG_STATE_HOME"

// StateDir is where speca keeps what a gate records, outside every
// repository: agent-fox/speca under $XDG_STATE_HOME when it is set to an
// absolute path, else under .local/state in the user's home directory, on
// Linux and macOS alike (ADR 22).
func StateDir() (string, error) {
	if x := os.Getenv(StateEnv); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "agent-fox", "speca"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("neither $" + StateEnv + " nor the home directory is set")
	}
	return filepath.Join(home, ".local", "state", "agent-fox", "speca"), nil
}

// RepoDir is the directory of one repository under the state directory: its
// base name, for a person reading the directory, and a hash of its absolute
// path, so that two repositories never share one.
func RepoDir(state, repo string) string {
	sum := sha256.Sum256([]byte(repo))
	return filepath.Join(state, filepath.Base(repo)+"-"+hex.EncodeToString(sum[:8]))
}
