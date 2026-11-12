package gate

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/agent-fox-dev/spec/envelope"
)

// Run is one gate in the run log: every gate that ended in pass or fail,
// reused ones included. A gate that could not run is not logged.
type Run struct {
	SpecID   string              `json:"spec_id"`
	At       string              `json:"at"`
	Commit   string              `json:"commit"`
	Level    string              `json:"level"`
	CleanEnv bool                `json:"clean_env"`
	Verdict  string              `json:"verdict"`
	Seconds  float64             `json:"seconds"`
	Reused   *envelope.GateReuse `json:"reused,omitempty"`
	Slowest  []envelope.GateUnit `json:"slowest,omitempty"`
}

// Duration is how long the gate took to check its tree: its own seconds, or,
// for a reused gate, those of the run it came from.
func (r Run) Duration() float64 {
	if r.Reused != nil {
		return r.Reused.Seconds
	}
	return r.Seconds
}

const runLogName = "runs.jsonl"

// AppendRun adds the run to the run log, one JSON line in one write.
func (s *Store) AppendRun(r Run) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, runLogName), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ReadRuns reads the run log of the repository at repo, in the order the runs
// were logged, creating nothing. A missing log is no run, and a line that
// cannot be parsed is skipped.
func ReadRuns(repo string) ([]Run, error) {
	state, err := StateDir()
	if err != nil {
		return nil, nil
	}
	f, err := os.Open(filepath.Join(RepoDir(state, repo), runLogName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var runs []Run
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var r Run
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.Level != "" {
			runs = append(runs, r)
		}
	}
	return runs, sc.Err()
}
