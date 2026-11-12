package gate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/agent-fox-dev/spec/envelope"
)

// Key is what makes two gates the same gate: the same tree, the same
// directory of it the commands run in, the same level, commands and
// environment. A pass is reused by tree, never by commit (ADR 22).
type Key struct {
	Tree     string `json:"tree"`
	Dir      string `json:"dir"`
	Level    string `json:"level"`
	Linter   string `json:"linter"`
	AllTests string `json:"all_tests"`
	CleanEnv bool   `json:"clean_env"`
	// AffectedTests is the affected-tests command as it ran, its base in
	// place, so that two bases are two keys; empty, and left out of the
	// record's name, for a gate that ran all_tests.
	AffectedTests string `json:"affected_tests,omitempty"`
}

// name is the file name of the key's record.
func (k Key) name() string {
	b, _ := json.Marshal(k)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]) + ".json"
}

// Pass is a recorded gate that passed.
type Pass struct {
	Key     Key                 `json:"key"`
	Commit  string              `json:"commit"`
	At      string              `json:"at"`
	Seconds float64             `json:"seconds"`
	Slowest []envelope.GateUnit `json:"slowest,omitempty"`
}

// Store is what speca keeps for one repository under the state directory:
// the logs of the commands, the passes by key, and the run log.
type Store struct {
	Dir string
}

// OpenStore opens the store of the repository at repo, under the state
// directory, creating it where it does not exist. The error names the path
// that could not be made.
func OpenStore(repo string) (*Store, error) {
	state, err := StateDir()
	if err != nil {
		return nil, err
	}
	s := &Store{Dir: RepoDir(state, repo)}
	for _, d := range []string{s.LogDir(), s.passDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// LogDir is where the commands' logs go.
func (s *Store) LogDir() string { return filepath.Join(s.Dir, "logs") }

func (s *Store) passDir() string { return filepath.Join(s.Dir, "passes") }

// Lookup returns the pass recorded under k. A record that cannot be read or
// parsed, or that is of another key, counts as none.
func (s *Store) Lookup(k Key) (Pass, bool) {
	b, err := os.ReadFile(filepath.Join(s.passDir(), k.name()))
	if err != nil {
		return Pass{}, false
	}
	var p Pass
	if json.Unmarshal(b, &p) != nil || p.Key != k {
		return Pass{}, false
	}
	return p, true
}

// Record writes p under its key, through a temporary file it renames, so that
// a reader never sees half a record.
func (s *Store) Record(p Pass) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.passDir(), p.Key.name()), b)
}

func writeAtomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	return nil
}
