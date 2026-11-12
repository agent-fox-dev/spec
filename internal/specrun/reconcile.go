package specrun

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
		if l.Kind == kindReconcile && l.Outcome == outcomeReconciled {
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
		if r.ctx.Err() != nil {
			reason = reasonInterrupted
		}
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
	openNow, err := r.listOpen(nil)
	if err != nil {
		return stop(fmt.Sprintf("reconcile: listing the open issues: %v", err))
	}
	openBefore := map[int]bool{}
	for _, is := range openNow {
		openBefore[is.Number] = true
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
		counts, err := r.reconcileRules(adopt, openBefore, before)
		if err != nil {
			reason = err.Error()
			if r.ctx.Err() != nil {
				reason = reasonInterrupted
			}
		} else {
			line.Adopted, line.Closed, line.Settled = &counts.adopted, &counts.closed, &counts.settled
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
	fmt.Fprintf(r.stderr, "%s: %s reconciled in %.1f min: %d adopted, %d closed, %d settled\n", tool, step, line.WallMinutes,
		*line.Adopted, *line.Closed, *line.Settled)
	for _, d := range r.result.OpenDeviations {
		fmt.Fprintf(r.stderr, "%s: open deviation #%d %s (%s) %s\n", tool, d.Number, d.Title, d.CreatedAt, d.URL)
	}
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

// The rules a reconcile applies after its session, and its warning for a
// deviation a person both accepted and rejected.
const (
	ruleAdoptionUnsettled = "adoption-unsettled"
	ruleDeviationAccepted = "deviation-accepted"
	ruleDeviationRejected = "deviation-rejected"

	warnDeviationConflict envelope.WarnCode = "deviation_conflict"
)

// adrPath finds the first ADR an issue's body cites.
var adrPath = regexp.MustCompile(`docs/adr/[A-Za-z0-9._-]+\.md`)

// hasKind reports whether the labels hold one of the kinds.
func hasKind(labels []string) bool {
	return slices.ContainsFunc(labels, func(l string) bool { return slices.Contains(kindLabels, l) })
}

// adoptionUnsettled is the rule adoption-unsettled: an issue of the adopt
// file still open with no kind becomes a deviation of the package its title
// names, for a person to decide. ids maps a package directory to its spec id.
func adoptionUnsettled(adopt []adoptEntry, issues map[int]issuex.Issue, ids map[string]string, base string) []labelChange {
	var out []labelChange
	for _, a := range adopt {
		is, ok := issues[a.Number]
		if !ok || is.State != "open" || hasKind(is.Labels) {
			continue
		}
		c := labelChange{Issue: a.Number, Rule: ruleAdoptionUnsettled,
			Comment: fmt.Sprintf("specrun, rule adoption-unsettled, on %s: the reconcile left this issue of %s with no kind. "+
				"It is now af:deviation, for a person to decide.", base, a.Package)}
		for _, l := range []string{"af:deviation", "af:from-" + ids[a.Package]} {
			if !slices.Contains(is.Labels, l) {
				c.Add = append(c.Add, l)
			}
		}
		out = append(out, c)
	}
	return out
}

// deviationRules are the rules deviation-accepted and deviation-rejected over
// the open issues: a deviation a person accepted is closed, naming the ADR it
// cites; one a person rejected becomes a correctness defect. One both
// accepted and rejected is nobody's decision, and is returned apart.
func deviationRules(issues []issuex.Issue, base string) (changes []labelChange, conflicts []issuex.Issue) {
	for _, is := range issues {
		if is.State != "open" || !slices.Contains(is.Labels, "af:deviation") {
			continue
		}
		accepted, rejected := slices.Contains(is.Labels, "af:accepted"), slices.Contains(is.Labels, "af:rejected")
		switch {
		case accepted && rejected:
			conflicts = append(conflicts, is)
		case accepted:
			adr := "it cites no ADR"
			if p := adrPath.FindString(is.Body); p != "" {
				adr = "its decision is recorded in " + p
			}
			changes = append(changes, labelChange{Issue: is.Number, Rule: ruleDeviationAccepted, Close: true,
				Comment: fmt.Sprintf("specrun, rule deviation-accepted, on %s: a person accepted this deviation; %s.", base, adr)})
		case rejected:
			c := labelChange{Issue: is.Number, Rule: ruleDeviationRejected, Remove: []string{"af:deviation", "af:rejected"},
				Comment: fmt.Sprintf("specrun, rule deviation-rejected, on %s: a person rejected this deviation, "+
					"so the code must do what the spec asks. It is now a correctness defect.", base)}
			for _, l := range []string{"af:defect", "af:severity-correctness"} {
				if !slices.Contains(is.Labels, l) {
					c.Add = append(c.Add, l)
				}
			}
			changes = append(changes, c)
		}
	}
	return changes, conflicts
}

// packageIDs maps the directory name of every package under the spec root
// and its archive to its spec id; one that does not load keeps its number.
func (r *run) packageIDs() map[string]string {
	out := map[string]string{}
	for _, sub := range []string{"", "archive"} {
		dir := filepath.Join(r.specsDir, sub)
		names, _ := afspec.ListSpecDirs(dir)
		for _, n := range names {
			if spec, err := afspec.LoadSpec(filepath.Join(dir, n)); err == nil {
				out[n] = spec.SpecID
			} else {
				out[n], _, _ = strings.Cut(n, "_")
			}
		}
	}
	return out
}

// reconcileCounts are what a reconcile's ledger line counts.
type reconcileCounts struct {
	adopted, closed, settled int
}

// reconcileRules settles by rule what needs no judgement after a reconcile
// session: the adoptions it left unsettled, the deviations a person
// decided, then incomplete-labels for every package and awaits-finished. It
// counts what the reconcile adopted, closed and settled, and lists the open
// deviations nobody has settled, oldest first.
func (r *run) reconcileRules(adopt []adoptEntry, openBefore map[int]bool, base string) (reconcileCounts, error) {
	var n reconcileCounts
	r.result.OpenDeviations = nil // the list as this reconcile leaves it
	all, err := r.forge.ListIssues(r.ctx, r.repo, issuex.IssueFilter{State: "all", Limit: 25600})
	if err != nil {
		return n, fmt.Errorf("reconcile: listing the issues: %w", err)
	}
	byNumber := map[int]issuex.Issue{}
	for _, is := range all.Issues {
		byNumber[is.Number] = is
	}
	for _, a := range adopt {
		if is, ok := byNumber[a.Number]; ok && hasKind(is.Labels) {
			n.adopted++
		}
	}
	ids := r.packageIDs()
	if err := r.apply(adoptionUnsettled(adopt, byNumber, ids, base)); err != nil {
		return n, err
	}
	open, err := r.listOpen(nil)
	if err != nil {
		return n, fmt.Errorf("reconcile: listing the open issues: %w", err)
	}
	devs, conflicts := deviationRules(open, base)
	for _, is := range conflicts {
		msg := fmt.Sprintf("issue #%d %q carries both af:accepted and af:rejected, which is nobody's decision; it is left as it is", is.Number, is.Title)
		r.warnings = append(r.warnings, envelope.Warning{Code: warnDeviationConflict, Severity: envelope.SeverityLow, Stage: "reconcile", Message: msg})
		fmt.Fprintf(r.stderr, "%s: warning: %s\n", tool, msg)
	}
	if err := r.apply(devs); err != nil {
		return n, err
	}
	n.settled = len(devs)
	var labels []labelChange
	for dir, id := range ids {
		issues, err := r.packageIssues(id, dir)
		if err != nil {
			return n, fmt.Errorf("%s: listing the issues of %s: %w", ruleIncompleteLabels, dir, err)
		}
		labels = append(labels, incompleteLabels(issues, id, base)...)
	}
	awaiting, err := r.listOpen([]string{"af:awaits"})
	if err != nil {
		return n, fmt.Errorf("%s: listing the awaits issues: %w", ruleAwaitsFinished, err)
	}
	awaiting = slices.DeleteFunc(awaiting, func(is issuex.Issue) bool {
		return slices.ContainsFunc(labels, func(c labelChange) bool { return c.Issue == is.Number })
	})
	if err := r.apply(append(labels, awaitsFinished(awaiting, r.knownPackages(), base)...)); err != nil {
		return n, err
	}
	after, err := r.listOpen(nil)
	if err != nil {
		return n, fmt.Errorf("reconcile: listing the open issues: %w", err)
	}
	stillOpen := map[int]bool{}
	for _, is := range after {
		stillOpen[is.Number] = true
		if slices.Contains(is.Labels, "af:deviation") && !slices.Contains(is.Labels, "af:accepted") && !slices.Contains(is.Labels, "af:rejected") {
			r.result.OpenDeviations = append(r.result.OpenDeviations, OpenDeviation{Number: is.Number, Title: is.Title, URL: is.HTMLURL,
				CreatedAt: is.CreatedAt.UTC().Format(time.RFC3339)})
		}
	}
	for num := range openBefore {
		if !stillOpen[num] {
			n.closed++
		}
	}
	slices.SortStableFunc(r.result.OpenDeviations, func(a, b OpenDeviation) int { return strings.Compare(a.CreatedAt, b.CreatedAt) })
	return n, nil
}

// wouldReconcile is what a dry run reports of a due reconcile: the step, the
// issues it would hand over, and the deviation changes it would make.
func (r *run) wouldReconcile() *envelope.ErrorInfo {
	r.result.NextStep = &NextStep{Kind: kindReconcile}
	adopt, err := r.adoptable()
	if err != nil {
		return failure("forge", "forge", "reconcile: listing the issues to adopt: %v", err)
	}
	r.result.WouldAdopt = adopt
	open, err := r.listOpen(nil)
	if err != nil {
		return failure("forge", "forge", "reconcile: listing the open issues: %v", err)
	}
	base, _ := gitOutput(r.root, "rev-parse", "HEAD")
	devs, _ := deviationRules(open, base)
	r.result.WouldChange = append(r.result.WouldChange, devs...)
	return nil
}
