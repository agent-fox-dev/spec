package specrun

import (
	"encoding/json"
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
	sortQueue(out)
	return out[:min(len(out), o.repairMax)]
}

// sortQueue orders repairs most severe first, then oldest first.
func sortQueue(q []queued) {
	slices.SortStableFunc(q, func(a, b queued) int {
		if d := slices.Index(repairKinds, a.Severity) - slices.Index(repairKinds, b.Severity); d != 0 {
			return d
		}
		if c := a.Created.Compare(b.Created); c != 0 {
			return c
		}
		return a.Number - b.Number
	})
}

// repairSerious is --repair where the stop rule stops: the serious defects
// stop names, as stop does, are repaired instead, most severe first then
// oldest first, with no cap, and the check is made again after them. A
// security defect without --repair-security, or one whose repair already
// failed, is left to a person: the loop stops as the stop rule does, before
// any repair. It returns the stop, or nil.
func (r *run) repairSerious(specDir string, stop *Stopped) *Stopped {
	spec := r.findPackage(specDir)
	if spec == nil {
		return stop
	}
	issues, err := r.listOpen([]string{"af:defect", "af:from-" + spec.SpecID})
	if err != nil {
		stop.Reason = fmt.Sprintf("serious defects: listing the defects of %s: %v", specDir, err)
		stop.Issues = nil
		return stop
	}
	var queue []queued
	for _, is := range issues {
		i := slices.IndexFunc(stop.Issues, func(s StopIssue) bool { return s.Number == is.Number })
		if i < 0 {
			continue
		}
		sev := stop.Issues[i].Severity
		if (sev == "security" && !r.opts.repairSecurity) || slices.Contains(is.Labels, "af:repair-failed") {
			return stop
		}
		queue = append(queue, queued{Number: is.Number, Title: is.Title, URL: is.HTMLURL, Severity: sev, Created: is.CreatedAt})
	}
	sortQueue(queue)
	rest := slices.DeleteFunc(r.state.RepairQueue, func(q queued) bool {
		return slices.ContainsFunc(queue, func(s queued) bool { return s.Number == q.Number })
	})
	r.state.RepairQueue = append(queue, rest...)
	r.saveState()
	if s := r.drainQueue(); s != nil {
		return s
	}
	serious, err := r.seriousDefects(specDir)
	switch {
	case err != nil:
		stop.Reason, stop.Issues = err.Error(), nil
	case len(serious) > 0:
		stop.Issues = serious
	default:
		return nil
	}
	return stop
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
		// The queue may come from an earlier run, under other flags.
		is, err := r.findIssue(q.Number)
		if err != nil {
			r.saveState()
			return &Stopped{Step: "repair-" + strconv.Itoa(q.Number), Kind: kindRepair, Subject: strconv.Itoa(q.Number), Reason: err.Error()}
		}
		if why := r.leftToAPerson(is); why != "" {
			r.saveState()
			fmt.Fprintf(r.stderr, "%s: #%d left out of the repairs: %s\n", tool, q.Number, why)
			continue
		}
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
	started := r.host.now().UTC().Truncate(time.Second)
	r.state.Running = &runningStep{Kind: kindRepair, Subject: subject, StartedAt: started.Format(time.RFC3339), Head: head, PID: os.Getpid()}
	r.saveState()

	reason, outcome := "", outcomeFixed
	var failedIssue *StopIssue
	o, err := r.session(step).run(r.ctx, fmt.Sprintf("/af-impl --repair %d", issue))
	switch {
	case err != nil:
		reason = reasonNoSession
	case o.interrupted:
		reason = reasonInterrupted
	case o.stalled:
		reason = reasonStalled
	default:
		outcome, failedIssue, reason = r.repairLeft(issue)
	}
	_ = os.WriteFile(filepath.Join(r.logDir, step+".log"), []byte(o.final), 0o644)
	ended := r.host.now().UTC().Truncate(time.Second)
	line := ledgerLine{Kind: kindRepair, Subject: subject, StartedAt: started.Format(time.RFC3339), EndedAt: ended.Format(time.RFC3339),
		WallMinutes: ended.Sub(started).Minutes(), Outcome: outcome}
	switch reason {
	case "", reasonRepairFailed:
	case reasonStalled:
		line.Outcome = outcomeStalled
	default:
		line.Outcome = outcomeStopped
	}
	if err := appendLedger(r.logDir, line); err != nil {
		fmt.Fprintf(r.stderr, "%s: the ledger could not be written: %v\n", tool, err)
	}
	fmt.Fprintf(r.stderr, "%s: %s %s in %.1f min\n", tool, step, line.Outcome, line.WallMinutes)
	if reason != "" && reason != reasonRepairFailed {
		r.releaseStep()
		return stop(reason)
	}
	r.finishStep(kindRepair, subject)
	if reason == reasonRepairFailed {
		s := stop(reason)
		s.Issues = []StopIssue{*failedIssue}
		return s
	}
	return nil
}

// The outcomes of a repair, and the reasons a repair step stops the loop.
const (
	outcomeFixed        = "fixed"
	outcomeAlreadyFixed = "already_fixed"
	outcomeFailed       = "failed"
	reasonRepairFailed  = "repair failed"
	reasonIssueOpen     = "issue neither closed nor marked failed"
)

// repairLeft checks what a repair session left, from the repository and the
// forge, not from the session's account: the base branch checked out and the
// tree clean, then the issue closed (fixed, or already_fixed when the merge
// commit's AF-Run trailer says so for this issue) or open and marked
// af:repair-failed (failed, which stops the loop when its severity is in
// --stop-on). Anything else is the reason the loop stops.
func (r *run) repairLeft(issue int) (outcome string, failed *StopIssue, reason string) {
	if branch, err := gitOutput(r.root, "branch", "--show-current"); err != nil || branch != r.base {
		return outcomeStopped, nil, reasonNotOnBase
	}
	if status, err := gitOutput(r.root, "status", "--porcelain"); err != nil || strings.TrimSpace(status) != "" {
		return outcomeStopped, nil, reasonDirty
	}
	is, err := r.findIssue(issue)
	if err != nil {
		return outcomeStopped, nil, err.Error()
	}
	switch {
	case is.State != "open":
		return r.repairOutcome(issue), nil, ""
	case slices.Contains(is.Labels, "af:repair-failed"):
		if sev := severityOf(is); sev != "" && slices.Contains(r.opts.stopOn, sev) {
			return outcomeFailed, &StopIssue{Number: is.Number, URL: is.HTMLURL, Title: is.Title, Severity: sev}, reasonRepairFailed
		}
		return outcomeFailed, nil, ""
	}
	return outcomeStopped, nil, reasonIssueOpen
}

// findIssue reads one issue, open or closed, from the forge.
func (r *run) findIssue(n int) (issuex.Issue, error) {
	t, err := r.forge.ReadIssue(r.ctx, issuex.IssueRef{Repo: r.repo, Number: n})
	if err != nil {
		return issuex.Issue{}, fmt.Errorf("repair: reading issue #%d: %w", n, err)
	}
	return t.Issue, nil
}

// leftToAPerson says why an issue the state queued is not repaired now, as
// the labels and the flags of this run stand: closed since, its repair
// failed, or a security defect this run may not repair. Empty when it is
// repaired.
func (r *run) leftToAPerson(is issuex.Issue) string {
	switch {
	case is.State != "open":
		return "it is closed"
	case slices.Contains(is.Labels, "af:repair-failed"):
		return "its repair failed"
	case slices.Contains(is.Labels, "af:severity-security") && !r.opts.repairSecurity:
		return "it is a security defect and --repair-security is not given"
	}
	return ""
}

// repairOutcome is already_fixed when the AF-Run trailer of the commit at the
// tip of the base branch says so for this issue, else fixed.
func (r *run) repairOutcome(issue int) string {
	value, err := gitOutput(r.root, "log", "-1", "--format=%(trailers:key=AF-Run,valueonly,unfold)", "HEAD")
	if err != nil {
		return outcomeFixed
	}
	line, _, _ := strings.Cut(strings.TrimSpace(value), "\n")
	var t struct {
		Repair struct {
			Issue   int    `json:"issue"`
			Outcome string `json:"outcome"`
		} `json:"repair"`
	}
	if json.Unmarshal([]byte(line), &t) == nil && t.Repair.Issue == issue && t.Repair.Outcome == outcomeAlreadyFixed {
		return outcomeAlreadyFixed
	}
	return outcomeFixed
}

// rerunRepair is a repair recorded as running at start: done when its issue
// is closed, with a ledger line of outcome fixed; failed, as a repair that
// failed, when the session marked it af:repair-failed before it was
// recorded; dropped when this run may not repair it; and run again
// otherwise.
func (r *run) rerunRepair(issue int) *Stopped {
	is, err := r.findIssue(issue)
	if err != nil {
		return r.repair(issue)
	}
	subject := strconv.Itoa(issue)
	earlier, _ := readLedger(r.logDir)
	step := stepName(len(earlier)+1, kindRepair, subject)
	record := func(outcome string) {
		ended := r.host.now().UTC().Truncate(time.Second)
		if err := appendLedger(r.logDir, ledgerLine{Kind: kindRepair, Subject: subject, StartedAt: r.state.Running.StartedAt,
			EndedAt: ended.Format(time.RFC3339), Outcome: outcome}); err != nil {
			fmt.Fprintf(r.stderr, "%s: the ledger could not be written: %v\n", tool, err)
		}
		fmt.Fprintf(r.stderr, "%s: %s %s before this run, as the forge shows\n", tool, step, outcome)
		r.finishStep(kindRepair, subject)
	}
	switch {
	case is.State != "open":
		record(outcomeFixed)
		return nil
	case slices.Contains(is.Labels, "af:repair-failed"):
		record(outcomeFailed)
		if sev := severityOf(is); sev != "" && slices.Contains(r.opts.stopOn, sev) {
			return &Stopped{Step: step, Kind: kindRepair, Subject: subject, Reason: reasonRepairFailed,
				Issues: []StopIssue{{Number: is.Number, URL: is.HTMLURL, Title: is.Title, Severity: sev}}}
		}
		return nil
	case r.leftToAPerson(is) != "":
		fmt.Fprintf(r.stderr, "%s: #%d left out of the repairs: %s\n", tool, issue, r.leftToAPerson(is))
		r.state.Running = nil
		r.saveState()
		return nil
	}
	return r.repair(issue)
}

// severityOf is the issue's af:severity-* label, without its prefix; empty
// when it has none.
func severityOf(is issuex.Issue) string {
	for _, l := range is.Labels {
		if s, ok := strings.CutPrefix(l, "af:severity-"); ok {
			return s
		}
	}
	return ""
}
