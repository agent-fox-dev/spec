package specrun

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"text/tabwriter"
)

// The outcomes of a step.
const (
	outcomeLanded  = "landed"
	outcomeStopped = "stopped"
	outcomeStalled = "stalled"
)

const ledgerName = "ledger.jsonl"

// ledgerLine is one line of ledger.jsonl: one step.
type ledgerLine struct {
	// RunID is the run that wrote the line; its directory under runs/
	// holds the step's files. Lines written before runs had IDs have none.
	RunID       string  `json:"run_id,omitempty"`
	Kind        string  `json:"kind"`
	Subject     string  `json:"subject"`
	StartedAt   string  `json:"started_at"`
	EndedAt     string  `json:"ended_at"`
	WallMinutes float64 `json:"wall_minutes"`
	Outcome     string  `json:"outcome"`
	// ResumedFrom is, on a line that records what a session of an earlier
	// run did, as a landing found on restart, the run that started the step.
	ResumedFrom string `json:"resumed_from,omitempty"`
	// ClaudeSessionID is the session_id of the step's claude stream, which
	// claude --resume takes; absent when the stream gave none.
	ClaudeSessionID string `json:"claude_session_id,omitempty"`
	// Gate and OpenItems are the AF-Run trailer's objects of a landed
	// package's merge commit, as the trailer gave them; absent without one.
	Gate      json.RawMessage `json:"gate,omitempty"`
	OpenItems json.RawMessage `json:"open_items,omitempty"`
	// Issues are the serious defects a landed package left open, which
	// stopped the loop.
	Issues []StopIssue `json:"issues,omitempty"`
	// Adopted, Closed and Settled count, for a reconcile, the issues of the
	// adopt file that carry a kind, the issues open before it and closed
	// after it, and the deviations accepted or rejected.
	Adopted *int `json:"adopted,omitempty"`
	Closed  *int `json:"closed,omitempty"`
	Settled *int `json:"settled,omitempty"`
}

// readLedger reads the ledger in dir; a missing ledger is no line, and a line
// that cannot be parsed is skipped.
func readLedger(dir string) ([]ledgerLine, error) {
	f, err := os.Open(filepath.Join(dir, ledgerName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []ledgerLine
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var l ledgerLine
		if json.Unmarshal(sc.Bytes(), &l) == nil && l.Kind != "" {
			out = append(out, l)
		}
	}
	return out, sc.Err()
}

// appendLedger adds one line to the ledger in dir, in one write.
func appendLedger(dir string, l ledgerLine) error {
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, ledgerName), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// record adds the step's line to the ledger, marked with this run's ID.
func (r *run) record(l ledgerLine) {
	l.RunID = r.runID
	if err := appendLedger(r.logDir, l); err != nil {
		fmt.Fprintf(r.stderr, "%s: the ledger could not be written: %v\n", tool, err)
	}
}

// slowerThanUsual reports whether a package that landed in minutes took more
// than twice the median of the packages that landed before it, and the
// median; with none before, it never did. Stopped and stalled steps are not
// part of the median.
func slowerThanUsual(earlier []ledgerLine, minutes float64) (float64, bool) {
	var landed []float64
	for _, l := range earlier {
		if l.Kind == kindImplement && l.Outcome == outcomeLanded {
			landed = append(landed, l.WallMinutes)
		}
	}
	if len(landed) == 0 {
		return 0, false
	}
	slices.Sort(landed)
	median := landed[len(landed)/2]
	if len(landed)%2 == 0 {
		median = (landed[len(landed)/2-1] + landed[len(landed)/2]) / 2
	}
	return median, minutes > 2*median
}

// printLedger writes the ledger in dir as a table, only the steps of the run
// runID when it is not empty. A step keeps its number in the whole ledger,
// which its files are named by.
func printLedger(w io.Writer, dir, runID string) error {
	lines, err := readLedger(dir)
	if err != nil {
		return err
	}
	var rows []int
	for i, l := range lines {
		if runID == "" || l.RunID == runID {
			rows = append(rows, i)
		}
	}
	switch {
	case len(rows) == 0 && runID != "":
		fmt.Fprintf(w, "no step of run %s is recorded in %s\n", runID, filepath.Join(dir, ledgerName))
		return nil
	case len(rows) == 0:
		fmt.Fprintf(w, "no step is recorded in %s\n", filepath.Join(dir, ledgerName))
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STEP\tRUN\tKIND\tSUBJECT\tSTARTED\tMINUTES\tOUTCOME")
	for _, i := range rows {
		l, run := lines[i], lines[i].RunID
		if run == "" {
			run = "-"
		}
		fmt.Fprintf(tw, "%04d\t%s\t%s\t%s\t%s\t%.1f\t%s\n", i+1, run, l.Kind, l.Subject, l.StartedAt, l.WallMinutes, l.Outcome)
	}
	return tw.Flush()
}
