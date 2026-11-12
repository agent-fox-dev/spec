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
	"time"
)

// now is specrun's clock; a test replaces it.
var now = time.Now

// The outcomes of a step.
const (
	outcomeLanded  = "landed"
	outcomeStopped = "stopped"
	outcomeStalled = "stalled"
)

const ledgerName = "ledger.jsonl"

// ledgerLine is one line of ledger.jsonl: one step.
type ledgerLine struct {
	Kind        string  `json:"kind"`
	Subject     string  `json:"subject"`
	StartedAt   string  `json:"started_at"`
	EndedAt     string  `json:"ended_at"`
	WallMinutes float64 `json:"wall_minutes"`
	Outcome     string  `json:"outcome"`
	// Gate and OpenItems are the AF-Run trailer's objects of a landed
	// package's merge commit, as the trailer gave them; absent without one.
	Gate      json.RawMessage `json:"gate,omitempty"`
	OpenItems json.RawMessage `json:"open_items,omitempty"`
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

// printLedger writes the ledger in dir as a table.
func printLedger(w io.Writer, dir string) error {
	lines, err := readLedger(dir)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		fmt.Fprintf(w, "no step is recorded in %s\n", filepath.Join(dir, ledgerName))
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STEP\tKIND\tSUBJECT\tSTARTED\tMINUTES\tOUTCOME")
	for i, l := range lines {
		fmt.Fprintf(tw, "%04d\t%s\t%s\t%s\t%.1f\t%s\n", i+1, l.Kind, l.Subject, l.StartedAt, l.WallMinutes, l.Outcome)
	}
	return tw.Flush()
}
