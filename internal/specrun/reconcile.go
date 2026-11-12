package specrun

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agent-fox-dev/aflib/envelope"
	"github.com/agent-fox-dev/aflib/issuex"
	"github.com/agent-fox-dev/spec/afspec"
)

// A reconcile brings every open item in line with the base branch: a
// session for the judgement, rules for the rest.
const (
	kindReconcile         = "reconcile"
	defaultReconcileEvery = 5
	outcomeReconciled     = "reconciled"
	reasonHeadMoved       = "HEAD moved"

	warnReportMissing envelope.WarnCode = "reconcile_report_missing"
)

// reconcileDue reports whether the next step is a reconcile: after every
// --reconcile-every landed packages, and, when every package is done, once
// more unless one was the last step and no package landed since.
func (r *run) reconcileDue(allDone bool) bool {
	if r.opts.reconcileEvery > 0 && r.state.SinceReconcile >= r.opts.reconcileEvery {
		return true
	}
	return allDone && (r.state.SinceReconcile > 0 || !r.reconciledEver())
}

// reconciledEver reports whether the ledger holds a reconcile.
func (r *run) reconciledEver() bool {
	lines, _ := readLedger(r.logDir)
	for _, l := range lines {
		if l.Kind == kindReconcile {
			return true
		}
	}
	return false
}

// finalReconcile runs the reconcile due when every package is done, and
// ends the loop.
func (r *run) finalReconcile() int {
	if r.reconcileDue(true) {
		if stop := r.reconcile(); stop != nil {
			return r.stop(stop)
		}
	}
	return r.emit(envelope.ExitOK, fmt.Sprintf("%s: every package is done, landed %d", tool, len(r.result.Landed)), nil)
}

// adoptEntry is an issue a reconcile hands its session to adopt.
type adoptEntry struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Package string `json:"package"`
}

// adoptable selects the open issues a reconcile may hand over for adoption:
// those that carry no af: label and whose title starts with the directory
// name of a package under the spec root or its archive and a colon, as
// af-impl titled an open item before the label scheme.
func (r *run) adoptable() ([]adoptEntry, error) {
	var dirs []string
	for _, sub := range []string{"", "archive"} {
		names, _ := afspec.ListSpecDirs(filepath.Join(r.specsDir, sub))
		dirs = append(dirs, names...)
	}
	open, err := r.listOpen(nil)
	if err != nil {
		return nil, err
	}
	out := []adoptEntry{}
	for _, is := range open {
		if is.State != "open" || hasAfLabel(is.Labels) {
			continue
		}
		for _, d := range dirs {
			if strings.HasPrefix(is.Title, d+":") {
				out = append(out, adoptEntry{Number: is.Number, Title: is.Title, URL: is.HTMLURL, Package: d})
				break
			}
		}
	}
	return out, nil
}

func hasAfLabel(labels []string) bool {
	for _, l := range labels {
		if strings.HasPrefix(l, "af:") {
			return true
		}
	}
	return false
}

// reconcile runs a reconcile step: the issues it may adopt written to
// <step>-adopt.json, a session told where they are and where to report,
// and, when the session left the repository as it was, the rules. It
// returns the stop, or nil.
func (r *run) reconcile() *Stopped {
	before, f := r.syncBase()
	if f != nil {
		return &Stopped{Kind: kindReconcile, Subject: kindReconcile, Step: kindReconcile, Reason: f.Message}
	}
	earlier, _ := readLedger(r.logDir)
	step := stepName(len(earlier)+1, kindReconcile, kindReconcile)
	stop := func(reason string) *Stopped {
		return &Stopped{Step: step, Kind: kindReconcile, Subject: kindReconcile, Reason: reason}
	}
	adopt, err := r.adoptable()
	if err != nil {
		return stop(fmt.Sprintf("reconcile: listing the issues to adopt: %v", err))
	}
	adoptPath, reportPath := filepath.Join(r.logDir, step+"-adopt.json"), filepath.Join(r.logDir, step+"-report.md")
	b, _ := json.MarshalIndent(adopt, "", "  ")
	if err := os.WriteFile(adoptPath, append(b, '\n'), 0o644); err != nil {
		return stop(fmt.Sprintf("reconcile: the adopt file could not be written: %v", err))
	}
	started := now().UTC().Truncate(time.Second)
	r.state.Running = &runningStep{Kind: kindReconcile, Subject: kindReconcile, StartedAt: started.Format(time.RFC3339),
		Head: before, PID: os.Getpid()}
	r.saveState()

	reason := ""
	o, err := r.session(step).run(r.ctx, "/af-impl --reconcile "+adoptPath+" --report "+reportPath)
	switch {
	case err != nil:
		reason = reasonNoSession
	case o.interrupted:
		reason = reasonInterrupted
	case o.stalled:
		reason = reasonStalled
	default:
		reason = r.unmoved(before)
	}
	_ = os.WriteFile(filepath.Join(r.logDir, step+".log"), []byte(o.final), 0o644)
	line := ledgerLine{Kind: kindReconcile, Subject: kindReconcile, StartedAt: started.Format(time.RFC3339), Outcome: outcomeReconciled}
	if reason == "" {
		if _, err := os.Stat(reportPath); err != nil {
			msg := fmt.Sprintf("the reconcile session wrote no report at %s", reportPath)
			r.warnings = append(r.warnings, envelope.Warning{Code: warnReportMissing, Severity: envelope.SeverityLow, Stage: "reconcile", Message: msg})
			fmt.Fprintf(r.stderr, "%s: warning: %s\n", tool, msg)
		}
	}
	switch reason {
	case "":
	case reasonStalled:
		line.Outcome = outcomeStalled
	default:
		line.Outcome = outcomeStopped
	}
	ended := now().UTC().Truncate(time.Second)
	line.EndedAt, line.WallMinutes = ended.Format(time.RFC3339), ended.Sub(started).Minutes()
	if err := appendLedger(r.logDir, line); err != nil {
		fmt.Fprintf(r.stderr, "%s: the ledger could not be written: %v\n", tool, err)
	}
	if reason != "" {
		r.releaseStep()
		fmt.Fprintf(r.stderr, "%s: %s %s: %s\n", tool, step, line.Outcome, reason)
		return stop(reason)
	}
	fmt.Fprintf(r.stderr, "%s: %s reconciled in %.1f min\n", tool, step, line.WallMinutes)
	r.state.SinceReconcile = 0
	r.finishStep(kindReconcile, kindReconcile)
	return nil
}

// unmoved checks that a reconcile session, which changes no code, left the
// repository as it was: the base branch checked out, the tree clean, and
// HEAD at the commit before it. It returns the first check that fails, or "".
func (r *run) unmoved(before string) string {
	if branch, err := gitOutput(r.root, "branch", "--show-current"); err != nil || branch != r.base {
		return reasonNotOnBase
	}
	if status, err := gitOutput(r.root, "status", "--porcelain"); err != nil || strings.TrimSpace(status) != "" {
		return reasonDirty
	}
	if head, err := gitOutput(r.root, "rev-parse", "HEAD"); err != nil || head != before {
		return reasonHeadMoved
	}
	return ""
}

var _ = issuex.Issue{}
