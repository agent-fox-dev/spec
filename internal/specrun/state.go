package specrun

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/agent-fox-dev/aflib/envelope"
	"github.com/agent-fox-dev/spec/afspec"
)

// state is what specrun keeps between runs, in one JSON file outside the
// repository (ADR 23): the steps done, and the step running. The repository
// records the rest of the truth.
type state struct {
	Done    []doneStep   `json:"done"`
	Running *runningStep `json:"running,omitempty"`
	// Owner is the process id of the specrun whose loop works on the
	// repository, from its first step to its end; absent otherwise.
	Owner int `json:"owner,omitempty"`
	// LabelCheck is the landed package whose label rules a forge call cut
	// short: the next start applies them again before it chooses a step.
	LabelCheck string `json:"label_check,omitempty"`
	// PendingComments are the comments of label changes made but not yet
	// posted: the next start posts them before anything else, since the
	// issue they speak of no longer shows what was missing.
	PendingComments []pendingComment `json:"pending_comments,omitempty"`
	// LastLanded is the package that landed last: every start checks it for
	// serious defects before it chooses a step, so a restart is not the way
	// past a stop.
	LastLanded string `json:"last_landed,omitempty"`
	// SinceReconcile counts the packages landed since the last reconcile.
	SinceReconcile int `json:"since_reconcile"`
}

// pendingComment is a comment a label rule owes an issue.
type pendingComment struct {
	Issue int    `json:"issue"`
	Body  string `json:"body"`
}

// doneStep is a step that landed.
type doneStep struct {
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	EndedAt string `json:"ended_at"`
}

// runningStep is the step under way, or one that stopped and is to be run
// again: its kind and package, when it started, the commit HEAD was at, and
// the process id of the specrun running it, 0 once that specrun has ended.
type runningStep struct {
	Kind      string `json:"kind"`
	Subject   string `json:"subject"`
	SpecDir   string `json:"spec_dir"`
	StartedAt string `json:"started_at"`
	Head      string `json:"head"`
	PID       int    `json:"pid"`
}

// statePath is the state file of the repository at root: <its name>.json
// under agent-fox/specrun in the user's state directory ($XDG_STATE_HOME when
// it is an absolute path, else ~/.local/state).
func statePath(root string) (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" || !filepath.IsAbs(dir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "agent-fox", "specrun", filepath.Base(root)+".json"), nil
}

// loadState reads the state file. A missing one is an empty state; one that
// cannot be parsed, or whose running step belongs to a live process, refuses
// the start.
func (r *run) loadState() *envelope.ErrorInfo {
	path, err := statePath(r.root)
	if err != nil {
		return failure("state", "environment", "the state file could not be found: %v", err)
	}
	r.statePath = path
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return failure("state", "environment", "the state file %s could not be read: %v", path, err)
	}
	if err := json.Unmarshal(b, &r.state); err != nil {
		return failure("state", "input", "the state file %s cannot be parsed (%v): specrun --reset-state discards it", path, err)
	}
	if run := r.state.Running; run != nil && run.PID > 0 && isSpecrun(run.PID) {
		return failure("state", "input", "the state file %s records %s %s as running in process %d, a specrun that is alive: "+
			"another specrun works on this repository", path, run.Kind, run.Subject, run.PID)
	}
	if o := r.state.Owner; o > 0 && isSpecrun(o) {
		return failure("state", "input", "the state file %s records process %d, a specrun that is alive, as working on this repository", path, o)
	}
	return nil
}

// saveState writes the state through a temporary file that is renamed, so
// that the file parses whenever it exists.
func (r *run) saveState() {
	if r.statePath == "" {
		return
	}
	if err := writeAtomic(r.statePath, r.state); err != nil {
		fmt.Fprintf(r.stderr, "%s: the state file %s could not be written: %v\n", tool, r.statePath, err)
	}
}

func writeAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), path)
}

// resetState is --reset-state: the state file removed, and nothing else.
func (r *run) resetState() int {
	path, err := statePath(r.root)
	if err != nil {
		return r.emit(envelope.ExitFailed, "", failure("state", "environment", "the state file could not be found: %v", err))
	}
	switch err := os.Remove(path); {
	case errors.Is(err, fs.ErrNotExist):
		return r.emit(envelope.ExitOK, fmt.Sprintf("%s: there is no state file %s", tool, path), nil)
	case err != nil:
		return r.emit(envelope.ExitFailed, "", failure("state", "environment", "the state file %s could not be removed: %v", path, err))
	}
	return r.emit(envelope.ExitOK, fmt.Sprintf("%s: removed the state file %s", tool, path), nil)
}

// resume checks the step the state records as running against the
// repository, once the base branch is up to date. A package with no open task
// whose base branch moved past the recorded commit landed: its ledger line is
// written. A package that still exists is run again (af-impl
// continues its branch when there is one). A package that is gone is dropped.
// It returns the package to run again, or nil to choose anew.
func (r *run) resume() *afspec.NextPackage {
	run := r.state.Running
	if run == nil {
		return nil
	}
	dir := filepath.Join(r.root, filepath.FromSlash(run.SpecDir))
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		r.state.Running = nil
		r.saveState()
		return nil
	}
	if len(spec.OpenTasks()) == 0 && r.movedPast(run.Head) {
		ended := now().UTC().Truncate(time.Second)
		started, perr := time.Parse(time.RFC3339, run.StartedAt)
		minutes := 0.0
		if perr == nil {
			minutes = ended.Sub(started).Minutes()
		}
		line := ledgerLine{Kind: run.Kind, Subject: run.Subject, StartedAt: run.StartedAt,
			EndedAt: ended.Format(time.RFC3339), WallMinutes: minutes, Outcome: outcomeLanded}
		if t, ok := r.readTrailer(); ok {
			line.Gate, line.OpenItems = t.Gate, t.OpenItems
		}
		if err := appendLedger(r.logDir, line); err != nil {
			fmt.Fprintf(r.stderr, "%s: the ledger could not be written: %v\n", tool, err)
		}
		// It landed in the run that was interrupted, not in this one: it is in
		// the ledger, and neither in landed nor counted against --max.
		fmt.Fprintf(r.stderr, "%s: %s %s landed before this run, as the repository shows\n", tool, run.Kind, run.Subject)
		r.state.LabelCheck = run.SpecDir // its issues are checked like any landed package's
		r.state.LastLanded = run.SpecDir
		r.finishStep(run.Kind, run.Subject)
		return nil
	}
	return &afspec.NextPackage{Dir: run.SpecDir, Spec: spec}
}

// movedPast reports whether HEAD is a descendant of commit and not commit
// itself.
func (r *run) movedPast(commit string) bool {
	head, err := gitOutput(r.root, "rev-parse", "HEAD")
	if err != nil || commit == "" || head == commit {
		return false
	}
	_, err = gitOutput(r.root, "merge-base", "--is-ancestor", commit, head)
	return err == nil
}

// startStep records the step as running.
func (r *run) startStep(kind string, next *afspec.NextPackage, head string, started time.Time) {
	r.state.Running = &runningStep{Kind: kind, Subject: filepath.Base(next.Dir), SpecDir: next.Dir,
		StartedAt: started.Format(time.RFC3339), Head: head, PID: os.Getpid()}
	r.saveState()
}

// finishStep moves the running step to the steps done, and records its
// package as the last that landed.
func (r *run) finishStep(kind, subject string) {
	r.state.Done = append(r.state.Done, doneStep{Kind: kind, Subject: subject, EndedAt: now().UTC().Format(time.RFC3339)})
	if r.state.Running != nil && kind == kindImplement {
		// A package that landed counts towards the next reconcile, whether
		// or not a rule after it stops the loop.
		r.state.LastLanded = r.state.Running.SpecDir
		r.state.SinceReconcile++
	}
	r.state.Running = nil
	r.saveState()
}

// releaseStep keeps the stopped step as running, to be run again, with no
// process holding it.
func (r *run) releaseStep() {
	if r.state.Running != nil {
		r.state.Running.PID = 0
		r.saveState()
	}
}

// hold records this specrun as the one working on the repository, so that a
// second one is refused between steps too.
func (r *run) hold() {
	if r.statePath != "" && !r.holding {
		r.state.Owner, r.holding = os.Getpid(), true
		r.saveState()
	}
}

// release gives the repository up when the loop ends, however it ends, if
// this run held it.
func (r *run) release() {
	if r.holding {
		r.state.Owner, r.holding = 0, false
		r.saveState()
	}
}
