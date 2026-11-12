package specrun

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/agent-fox-dev/aflib/issuex"
)

// A repair is one session that closes one defect or one untested item on a
// branch and a merge of its own.
const (
	kindRepair       = "repair"
	defaultRepairOn  = "security,data-loss,correctness,minor,untested"
	defaultRepairMax = 5
	untested         = "untested"
)

// repairKinds are what --repair-on may hold, in the order the queue takes
// them: the severities, most severe first, then untested items.
var repairKinds = append(slices.Clone(severities), untested)

// parseRepairOn reads --repair-on; the empty string is none.
func parseRepairOn(v, from string) ([]string, error) {
	out := []string{}
	if v == "" {
		return out, nil
	}
	for _, k := range strings.Split(v, ",") {
		if !slices.Contains(repairKinds, k) {
			return nil, fmt.Errorf("%s %q: %q is not something to repair; the kinds are %s, comma-separated, and '' repairs nothing",
				from, v, k, strings.Join(repairKinds, ", "))
		}
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out, nil
}

// queued is an issue of the repair queue.
type queued struct {
	Number   int       `json:"number"`
	Title    string    `json:"title"`
	URL      string    `json:"url"`
	Severity string    `json:"severity"`
	Created  time.Time `json:"created_at"`
}

// repairQueue is what the labels give to repair: every open defect whose one
// severity is in --repair-on, and every open untested item when it holds
// untested; never an issue marked af:repair-failed, nor a security defect
// unless --repair-security allows it. Most severe first, then oldest first,
// at most --repair-max.
func repairQueue(issues []issuex.Issue, o *options) []queued {
	out := []queued{}
	for _, is := range issues {
		if is.State != "open" || slices.Contains(is.Labels, "af:repair-failed") {
			continue
		}
		kind := ""
		switch {
		case slices.Contains(is.Labels, "af:defect"):
			var sev []string
			for _, l := range is.Labels {
				if s, ok := strings.CutPrefix(l, "af:severity-"); ok {
					sev = append(sev, s)
				}
			}
			if len(sev) != 1 || (sev[0] == "security" && !o.repairSecurity) {
				continue
			}
			kind = sev[0]
		case slices.Contains(is.Labels, "af:untested"):
			kind = untested
		default:
			continue
		}
		if !slices.Contains(o.repairOn, kind) || slices.ContainsFunc(out, func(q queued) bool { return q.Number == is.Number }) {
			continue
		}
		out = append(out, queued{Number: is.Number, Title: is.Title, URL: is.HTMLURL, Severity: kind, Created: is.CreatedAt})
	}
	slices.SortStableFunc(out, func(a, b queued) int {
		if d := slices.Index(repairKinds, a.Severity) - slices.Index(repairKinds, b.Severity); d != 0 {
			return d
		}
		if c := a.Created.Compare(b.Created); c != 0 {
			return c
		}
		return a.Number - b.Number
	})
	return out[:min(len(out), o.repairMax)]
}

// buildQueue builds the repair queue from the forge's labels as they stand.
func (r *run) buildQueue() ([]queued, error) {
	defects, err := r.listOpen([]string{"af:defect"})
	if err != nil {
		return nil, fmt.Errorf("repair: listing the defects: %w", err)
	}
	items, err := r.listOpen([]string{"af:untested"})
	if err != nil {
		return nil, fmt.Errorf("repair: listing the untested items: %w", err)
	}
	return repairQueue(append(defects, items...), r.opts), nil
}

// drainQueue runs the repairs the state holds, in order, each issue leaving
// the queue as its step starts. It returns the stop, or nil.
func (r *run) drainQueue() *Stopped {
	for len(r.state.RepairQueue) > 0 {
		q := r.state.RepairQueue[0]
		r.state.RepairQueue = r.state.RepairQueue[1:]
		if stop := r.repair(q.Number); stop != nil {
			return stop
		}
	}
	return nil
}

// repair runs the repair step of one issue: one session told its number,
// then the step's ledger line. It returns the stop, or nil.
func (r *run) repair(issue int) *Stopped {
	earlier, _ := readLedger(r.logDir)
	subject := strconv.Itoa(issue)
	step := stepName(len(earlier)+1, kindRepair, subject)
	stop := func(reason string) *Stopped {
		if r.ctx.Err() != nil {
			reason = reasonInterrupted
		}
		return &Stopped{Step: step, Kind: kindRepair, Subject: subject, Reason: reason}
	}
	head, _ := gitOutput(r.root, "rev-parse", "HEAD")
	started := now().UTC().Truncate(time.Second)
	r.state.Running = &runningStep{Kind: kindRepair, Subject: subject, StartedAt: started.Format(time.RFC3339), Head: head, PID: os.Getpid()}
	r.saveState()

	reason := ""
	o, err := r.session(step).run(r.ctx, fmt.Sprintf("/af-impl --repair %d", issue))
	switch {
	case err != nil:
		reason = reasonNoSession
	case o.interrupted:
		reason = reasonInterrupted
	case o.stalled:
		reason = reasonStalled
	}
	_ = os.WriteFile(filepath.Join(r.logDir, step+".log"), []byte(o.final), 0o644)
	ended := now().UTC().Truncate(time.Second)
	line := ledgerLine{Kind: kindRepair, Subject: subject, StartedAt: started.Format(time.RFC3339), EndedAt: ended.Format(time.RFC3339),
		WallMinutes: ended.Sub(started).Minutes(), Outcome: outcomeFixed}
	switch reason {
	case "":
	case reasonStalled:
		line.Outcome = outcomeStalled
	default:
		line.Outcome = outcomeStopped
	}
	if err := appendLedger(r.logDir, line); err != nil {
		fmt.Fprintf(r.stderr, "%s: the ledger could not be written: %v\n", tool, err)
	}
	fmt.Fprintf(r.stderr, "%s: %s %s in %.1f min\n", tool, step, line.Outcome, line.WallMinutes)
	if reason != "" {
		r.releaseStep()
		return stop(reason)
	}
	r.finishStep(kindRepair, subject)
	return nil
}

// The outcomes of a repair.
const outcomeFixed = "fixed"
