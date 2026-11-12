package specrun

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/agent-fox-dev/aflib/envelope"
)

// warnTrailerMissing is raised for a landed package whose merge commit has no
// AF-Run trailer holding one JSON object.
const warnTrailerMissing envelope.WarnCode = "trailer_missing"

// runTrailer is what specrun reads of the AF-Run trailer: its gate and
// open_items objects, kept as they were written; any other key is ignored.
type runTrailer struct {
	Gate      json.RawMessage `json:"gate"`
	OpenItems json.RawMessage `json:"open_items"`
}

// readTrailer reads the AF-Run trailer of the commit at HEAD, the merge
// commit at the tip of the base branch. It warns trailer_missing, naming the
// commit, when there is none or its value is not one JSON object.
func (r *run) readTrailer() (runTrailer, bool) {
	// The commit and its trailer in one git process.
	out, err := gitOutput(r.root, "log", "-1", "--format=%H%n%(trailers:key=AF-Run,valueonly,unfold)", "HEAD")
	commit, value, _ := strings.Cut(out, "\n")
	var t runTrailer
	value = strings.TrimSpace(value)
	if line, _, _ := strings.Cut(value, "\n"); err == nil {
		value = line
	}
	if err != nil || !strings.HasPrefix(value, "{") || json.Unmarshal([]byte(value), &t) != nil {
		msg := fmt.Sprintf("the merge commit %s has no AF-Run trailer holding one JSON object; its ledger line has no gate or open-item counts", commit)
		r.warnings = append(r.warnings, envelope.Warning{Code: warnTrailerMissing, Severity: envelope.SeverityLow, Stage: "land", Message: msg})
		fmt.Fprintf(r.stderr, "%s: warning: %s\n", tool, msg)
		return runTrailer{}, false
	}
	return t, true
}

// trailerSummary is the short form of the trailer's counts: the full gate's
// runs and seconds, the reused gates and the items opened.
func trailerSummary(t runTrailer) string {
	var g struct {
		Full struct {
			Runs    int     `json:"runs"`
			Seconds float64 `json:"seconds"`
			Reused  int     `json:"reused"`
		} `json:"full"`
		Task struct {
			Reused int `json:"reused"`
		} `json:"task"`
	}
	var items struct {
		Opened map[string]int `json:"opened"`
	}
	_ = json.Unmarshal(t.Gate, &g)
	_ = json.Unmarshal(t.OpenItems, &items)
	opened := 0
	for _, n := range items.Opened {
		opened += n
	}
	return fmt.Sprintf("; gate %d full in %gs, %d reused; %d opened", g.Full.Runs, g.Full.Seconds, g.Full.Reused+g.Task.Reused, opened)
}
