package specrun

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agent-fox-dev/aflib/envelope"
	"github.com/agent-fox-dev/spec/afspec"
)

// Diagnosis is where the repository stands when the loop stopped, read from
// git: what the words of a failed landing check do not say. A check names
// where the work was left, not why, and the why is what a person needs before
// they can continue (ADR 27).
type Diagnosis struct {
	// Cause is the finding in one sentence, empty when git shows nothing the
	// stop's reason does not already say.
	Cause string `json:"cause,omitempty"`
	// Branch is the branch checked out, "" when HEAD is detached; Clean says
	// whether the working tree is clean, and Uncommitted names the first
	// paths that are not.
	Branch      string   `json:"branch,omitempty"`
	Clean       bool     `json:"clean"`
	Uncommitted []string `json:"uncommitted,omitempty"`
	// Base is the base branch, BaseHead its commit now, and BaseAtStart the
	// commit it was at when the step started.
	Base        string `json:"base"`
	BaseHead    string `json:"base_head,omitempty"`
	BaseAtStart string `json:"base_at_start,omitempty"`
	// BaseSinceStart is how the base branch moved since the step started:
	// unchanged, advanced (it still holds the commit the step started from),
	// rewritten (it does not) or missing.
	BaseSinceStart string `json:"base_since_start,omitempty"`
	// BaseVsOrigin is how the base branch stands to origin's: same, ahead,
	// behind, diverged or unrelated (no shared history); absent when origin
	// has no such branch.
	BaseVsOrigin string `json:"base_vs_origin,omitempty"`
	// BaseLastMove is the newest reflog entry of the base branch, when it was
	// rewritten or stands apart from origin's.
	BaseLastMove string `json:"base_last_move,omitempty"`
	// WorkBranch is the branch the package's work goes on, for a stopped
	// implement step.
	WorkBranch *WorkBranch `json:"work_branch,omitempty"`
}

// WorkBranch is the feature branch of a package, as af-impl names it.
type WorkBranch struct {
	Name string `json:"name"`
	// Exists says whether the branch exists here; the rest is absent when not.
	Exists bool   `json:"exists"`
	Head   string `json:"head,omitempty"`
	// Pushed says whether origin's branch is at the same commit, Merged
	// whether the base branch holds it.
	Pushed bool `json:"pushed"`
	Merged bool `json:"merged"`
	// TasksDone counts the tasks done or dropped on the branch, of
	// TasksTotal.
	TasksDone  int `json:"tasks_done"`
	TasksTotal int `json:"tasks_total"`
}

// RecoveryStep is one thing to do, in order, before the loop can continue.
type RecoveryStep struct {
	// Do says what the step does and why.
	Do string `json:"do"`
	// Run is the command that does it, run in the repository; absent when it
	// takes a person's judgement of what to run.
	Run string `json:"run,omitempty"`
	// Decide is true when the step discards or publishes something: a person
	// decides whether it is right.
	Decide bool `json:"decide,omitempty"`
}

// How the base branch moved since a step started.
const (
	baseUnchanged = "unchanged"
	baseAdvanced  = "advanced"
	baseRewritten = "rewritten"
	baseMissing   = "missing"
)

// How the base branch stands to origin's.
const (
	originSame      = "same"
	originAhead     = "ahead"
	originBehind    = "behind"
	originDiverged  = "diverged"
	originUnrelated = "unrelated"
)

// warnBaseRewritten is raised when the base branch stops holding the commit
// a running session started from: the session cannot land on it.
const warnBaseRewritten envelope.WarnCode = "base_rewritten"

// diagnose reads where the repository stands. step is the step that stopped,
// kept as running in the state; nil when no step ran, as in the preflight.
func (r *run) diagnose(step *runningStep) *Diagnosis {
	d := &Diagnosis{Base: r.base}
	if w, err := worktreeOf(r.root); err == nil {
		d.Branch, d.Clean = w.branch, w.clean
	}
	if !d.Clean {
		d.Uncommitted = r.uncommitted(5)
	}
	d.BaseHead = commitOf(r.root, "refs/heads/"+r.base)
	if step != nil && step.Head != "" {
		d.BaseAtStart = step.Head
		switch {
		case d.BaseHead == "":
			d.BaseSinceStart = baseMissing
		case d.BaseHead == step.Head:
			d.BaseSinceStart = baseUnchanged
		case isAncestor(r.root, step.Head, d.BaseHead):
			d.BaseSinceStart = baseAdvanced
		default:
			d.BaseSinceStart = baseRewritten
		}
	}
	if origin := commitOf(r.root, "refs/remotes/origin/"+r.base); origin != "" && d.BaseHead != "" {
		d.BaseVsOrigin = relation(r.root, d.BaseHead, origin)
	}
	if d.BaseSinceStart == baseRewritten || d.BaseVsOrigin == originDiverged || d.BaseVsOrigin == originUnrelated {
		d.BaseLastMove = lastMove(r.root, r.base)
	}
	if step != nil && step.Kind == kindImplement && step.SpecDir != "" {
		d.WorkBranch = r.workBranch(step.SpecDir, d.BaseHead)
	}
	d.Cause = d.cause(step)
	return d
}

// cause words the diagnosis as one sentence, the base branch first, since a
// broken base branch is what no session can work around.
func (d *Diagnosis) cause(step *runningStep) string {
	var parts []string
	switch d.BaseSinceStart {
	case baseRewritten:
		s := fmt.Sprintf("the base branch %s was rewritten during the step: it no longer holds %s, the commit the step started from (now %s",
			d.Base, short(d.BaseAtStart), short(d.BaseHead))
		if d.BaseVsOrigin == originUnrelated {
			s += fmt.Sprintf(", with no history shared with origin/%s", d.Base)
		}
		parts = append(parts, s+")")
	case baseMissing:
		parts = append(parts, fmt.Sprintf("the base branch %s no longer exists", d.Base))
	default:
		switch d.BaseVsOrigin {
		case originUnrelated:
			parts = append(parts, fmt.Sprintf("the base branch %s shares no history with origin/%s", d.Base, d.Base))
		case originDiverged:
			parts = append(parts, fmt.Sprintf("the base branch %s and origin/%s have diverged", d.Base, d.Base))
		}
	}
	if d.BaseLastMove != "" && len(parts) > 0 {
		parts[len(parts)-1] += "; it last moved " + d.BaseLastMove
	}
	if w := d.WorkBranch; w != nil && step != nil {
		switch {
		case !w.Exists:
			parts = append(parts, fmt.Sprintf("there is no branch %s", w.Name))
		case w.Merged && w.TasksDone < w.TasksTotal:
			parts = append(parts, fmt.Sprintf("%s is merged into %s with %d of %d tasks done", w.Name, d.Base, w.TasksDone, w.TasksTotal))
		case w.Merged:
		case w.TasksDone == w.TasksTotal:
			pushed := "not pushed"
			if w.Pushed {
				pushed = "pushed"
			}
			parts = append(parts, fmt.Sprintf("%s is finished on %s (%d of %d tasks done, %s) but not merged into %s",
				step.Subject, w.Name, w.TasksDone, w.TasksTotal, pushed, d.Base))
		default:
			parts = append(parts, fmt.Sprintf("%s has %d of %d tasks done on %s", step.Subject, w.TasksDone, w.TasksTotal, w.Name))
		}
	}
	if !d.Clean {
		on := d.Branch
		if on == "" {
			on = "a detached HEAD"
		}
		parts = append(parts, fmt.Sprintf("the working tree on %s has uncommitted changes (%s)", on, strings.Join(d.Uncommitted, ", ")))
	}
	return strings.Join(parts, "; ")
}

// recovery is what to do, in order, before running specrun again continues
// the work: the working tree cleaned, the base branch put back on origin's,
// then specrun itself, which takes the stopped step up again.
func (r *run) recovery(d *Diagnosis, step *runningStep) []RecoveryStep {
	var out []RecoveryStep
	if !d.Clean {
		out = append(out, RecoveryStep{Decide: true, Run: "git status",
			Do: "commit, stash or remove the uncommitted changes: specrun starts only on a clean working tree"})
	}
	broken := d.BaseSinceStart == baseRewritten || d.BaseVsOrigin == originDiverged || d.BaseVsOrigin == originUnrelated
	switch {
	case d.BaseSinceStart == baseMissing:
		out = append(out, RecoveryStep{Do: fmt.Sprintf("make %s again from origin/%s", d.Base, d.Base),
			Run: fmt.Sprintf("git branch %s origin/%s", d.Base, d.Base)})
	case broken && d.BaseVsOrigin != "":
		run := fmt.Sprintf("git branch -f %s origin/%s", d.Base, d.Base)
		if d.Branch == d.Base {
			run = "git reset --hard origin/" + d.Base
		}
		out = append(out, RecoveryStep{Decide: true, Run: run,
			Do: fmt.Sprintf("put %s back on origin/%s, which discards the commits only the local %s holds (git log origin/%s..%s lists them, and the reflog keeps them): specrun pulls %s with --ff-only and lands on top of origin's",
				d.Base, d.Base, d.Base, d.Base, d.Base, d.Base)})
	}
	out = append(out, RecoveryStep{Run: tool, Do: continueWith(d, step)})
	return out
}

// continueWith says what running specrun again does with the stopped step.
func continueWith(d *Diagnosis, step *runningStep) string {
	if step == nil {
		return "start the loop again"
	}
	switch step.Kind {
	case kindRepair:
		return "take up the repair of #" + step.Subject + " again"
	case kindReconcile:
		return "run the reconcile again"
	}
	w := d.WorkBranch
	switch {
	case w == nil || !w.Exists:
		return fmt.Sprintf("run %s again from its first task", step.Subject)
	case !w.Merged && w.TasksDone == w.TasksTotal:
		return fmt.Sprintf("take up %s again: af-impl continues %s, finds every task done, and goes on to conformance and the merge", step.Subject, w.Name)
	default:
		return fmt.Sprintf("take up %s again: af-impl continues %s from its first open task", step.Subject, w.Name)
	}
}

// workBranch reads the package's feature branch: whether it exists, is
// pushed and merged, and how many of its tasks are done on it, from the
// progress file the branch holds.
func (r *run) workBranch(specDir, baseHead string) *WorkBranch {
	spec, err := afspec.LoadSpec(filepath.Join(r.root, filepath.FromSlash(specDir)))
	if err != nil {
		return nil
	}
	w := &WorkBranch{Name: "feature/" + spec.SpecID + "-" + spec.SpecName}
	if w.Head = commitOf(r.root, "refs/heads/"+w.Name); w.Head == "" {
		return w
	}
	w.Exists = true
	w.Pushed = commitOf(r.root, "refs/remotes/origin/"+w.Name) == w.Head
	w.Merged = baseHead != "" && isAncestor(r.root, w.Head, baseHead)
	spec.Progress = nil
	if b, err := gitOutput(r.root, "show", w.Name+":"+specDir+"/"+afspec.ProgressFileName); err == nil {
		var p afspec.ProgressV3Json
		if json.Unmarshal([]byte(b), &p) == nil {
			spec.Progress = &p
		}
	}
	for _, t := range spec.Plan() {
		w.TasksTotal++
		if t.State == afspec.TaskStateDone || t.State == afspec.TaskStateDropped {
			w.TasksDone++
		}
	}
	return w
}

// uncommitted is the first n paths git status shows as not committed.
func (r *run) uncommitted(n int) []string {
	out, err := gitOutput(r.root, "status", "--porcelain")
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 3 && len(paths) < n {
			paths = append(paths, line[3:])
		}
	}
	return paths
}

// commitOf is the commit ref names, "" when it names none.
func commitOf(root, ref string) string {
	id, err := gitOutput(root, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return ""
	}
	return id
}

// isAncestor reports whether commit a is b or one of b's ancestors.
func isAncestor(root, a, b string) bool {
	_, err := gitOutput(root, "merge-base", "--is-ancestor", a, b)
	return err == nil
}

// relation is how the commit local stands to origin's commit.
func relation(root, local, origin string) string {
	if local == origin {
		return originSame
	}
	base, err := gitOutput(root, "merge-base", local, origin)
	switch {
	case err != nil || base == "":
		return originUnrelated
	case base == origin:
		return originAhead
	case base == local:
		return originBehind
	}
	return originDiverged
}

// lastMove words the newest reflog entry of branch: the commit, when, and
// git's own account of the move. A move with no account was a direct write
// of the ref (git update-ref), which no commit, merge or reset makes.
func lastMove(root, branch string) string {
	out, err := gitOutput(root, "log", "-g", "-1", "--date=iso", "--format=%h%x09%gd%x09%gs", "refs/heads/"+branch)
	if err != nil || out == "" {
		return ""
	}
	fields := strings.SplitN(out, "\t", 3)
	for len(fields) < 3 {
		fields = append(fields, "")
	}
	when := fields[1]
	if i, j := strings.Index(when, "@{"), strings.LastIndex(when, "}"); i >= 0 && j > i {
		when = when[i+2 : j]
	}
	how := fmt.Sprintf("%q", fields[2])
	if fields[2] == "" {
		how = "a direct write of the ref, with no reflog message"
	}
	return fmt.Sprintf("to %s at %s, by %s", fields[0], when, how)
}

// short is a commit's abbreviated id.
func short(id string) string {
	if len(id) > 7 {
		return id[:7]
	}
	return id
}

// sessionReport is the start of what a step's session said last: its first
// paragraph, on one line, cut to n characters.
func sessionReport(final string, n int) string {
	for _, para := range strings.Split(strings.ReplaceAll(final, "\r\n", "\n"), "\n\n") {
		if para = strings.Join(strings.Fields(para), " "); para != "" {
			return shorten(para, n)
		}
	}
	return ""
}

// watchBase watches the base branch while a session runs, and warns as soon
// as it stops holding the commit the session started from: a session cannot
// land on a base branch that was rewritten under it, and finds out only at
// its merge, hours later. A person warned at once can put the branch back
// before then. The returned function ends the watch and keeps its warnings.
func (r *run) watchBase(ctx context.Context, subject string) func() {
	every := r.host.baseWatch
	start := ""
	if r.base != "" {
		start = commitOf(r.root, "refs/heads/"+r.base)
	}
	if every <= 0 || start == "" {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var found []envelope.Warning
	go func() {
		defer close(done)
		tick := time.NewTicker(every)
		defer tick.Stop()
		warned := start
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			now := commitOf(r.root, "refs/heads/"+r.base)
			if now == warned || now != "" && isAncestor(r.root, start, now) {
				continue
			}
			warned = now
			what := fmt.Sprintf("was rewritten: it no longer holds %s, the commit the step started from (now %s", short(start), short(now))
			if move := lastMove(r.root, r.base); move != "" {
				what += "; it last moved " + move
			}
			what += ")"
			if now == "" {
				what = "was deleted"
			}
			msg := fmt.Sprintf("the base branch %s %s while %s runs; the session cannot land on it: put %s back (git branch -f %s origin/%s) before the session reaches its merge",
				r.base, what, subject, r.base, r.base, r.base)
			found = append(found, envelope.Warning{Code: warnBaseRewritten, Severity: envelope.SeverityHigh, Stage: "land", Message: msg})
			fmt.Fprintf(r.stderr, "%s: warning: %s\n", tool, msg)
		}
	}()
	return func() {
		cancel()
		<-done
		r.warnings = append(r.warnings, found...)
	}
}

// explainStop fills in the stop of a step the state keeps as running: the
// diagnosis, what to do, the start of the session's last report and where
// the rest of it is. It returns whether running specrun again continues the
// step.
func (r *run) explainStop(s *Stopped) bool {
	step := r.state.Running
	if step == nil || step.PID != 0 {
		return false
	}
	if s.Step != "" && r.runDir != "" {
		log := filepath.Join(r.runDir, s.Step+".log")
		if b, err := os.ReadFile(log); err == nil {
			s.Log = log
			s.SessionReport = sessionReport(string(b), 600)
		}
	}
	r.result.Diagnosis = r.diagnose(step)
	r.result.Recovery = r.recovery(r.result.Diagnosis, step)
	return true
}

// printStop writes the stop's explanation to stderr, for a person.
func (r *run) printStop(s *Stopped) {
	if d := r.result.Diagnosis; d != nil && d.Cause != "" {
		fmt.Fprintf(r.stderr, "%s:   cause: %s\n", tool, d.Cause)
	}
	if s != nil && s.SessionReport != "" {
		fmt.Fprintf(r.stderr, "%s:   the session said: %s\n", tool, s.SessionReport)
	}
	if s != nil && s.Log != "" {
		fmt.Fprintf(r.stderr, "%s:   its last report: %s\n", tool, s.Log)
	}
	printRecovery(r.stderr, r.result.Recovery)
}

// printRecovery writes the recovery steps, numbered, each command under its
// step.
func printRecovery(w io.Writer, steps []RecoveryStep) {
	if len(steps) == 0 {
		return
	}
	fmt.Fprintf(w, "%s: to continue:\n", tool)
	for i, st := range steps {
		decide := ""
		if st.Decide {
			decide = "[your call] "
		}
		fmt.Fprintf(w, "%s:   %d. %s%s\n", tool, i+1, decide, st.Do)
		if st.Run != "" {
			fmt.Fprintf(w, "%s:        $ %s\n", tool, st.Run)
		}
	}
}
