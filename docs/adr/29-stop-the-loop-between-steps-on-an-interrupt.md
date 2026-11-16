# 29. Stop the loop between steps on an interrupt

**Status:** accepted
**Date:** 2026-11-16
**Related:** [ADR 23](23-drive-the-implementation-loop-with-specrun.md), [ADR 27](27-diagnose-a-stopped-step-and-say-how-to-continue.md)

## Context

An interrupt of `specrun` ended the session at once. Its cost depended on
when it came, and the person interrupting could not see that:

- **Between steps:** nothing was lost.
- **In a task:** the task's uncommitted work was lost, up to about 15
  minutes. The working tree was also left dirty on the package's branch.
  The next start then failed with the bare words of `git switch`, or carried
  the changes onto the base branch and stopped the session there.
- **In the landing:** after the merge commit and before the push, the next
  start saw the package as landed, from the local base branch. The push and
  the issue updates were left undone.

`--max` stops the loop cleanly, but it is set at start. A loop already
running had no way to finish its package and stop.

## Decision

- **The first interrupt asks the loop to stop between steps.** specrun says
  so on stderr. The session in progress runs to its end and is checked as
  usual. No further step starts: implement, reconcile and repair are each
  refused before their state is written. A refused repair stays in the
  queue. The loop ends with exit code 0, `result.stopped_as_asked`, and a
  summary naming the step it did not start. The state holds nothing to take
  up again.
- **A second interrupt, or a termination, stops at once,** as an interrupt
  did before. `SIGTERM` keeps that meaning because whatever sends it, a
  service manager or a shutdown, will not wait for a step that can take
  hours.
- **The terminal's interrupt reaches `specrun` alone.** The session already
  ran in a process group of its own. The `git` commands specrun runs now do
  too, so a first interrupt cannot kill a pull, or the base branch's watch,
  halfway.
- **The preflight refuses a working tree that is not clean,** before it
  checks out the base branch. The refusal is diagnosed as ADR 27 diagnoses a
  stop, for the step the state keeps to take up again. So is a base branch
  that cannot be checked out. Changes on the stopped package's branch are
  the unfinished task, which af-impl starts again from the branch's last
  commit. The first recovery step sets them aside with
  `git stash push --include-untracked`, a step the person decides.

## Consequences

- Stopping between steps can take as long as the step in progress: up to
  hours for an implement step. The message says how to stop at once.
- A loop that has been asked to stop still runs the forge's label rules and
  the defect check between steps, since they start no session. What it does
  not start, a due reconcile or queued repairs, the next start does first.
- A start no longer carries a person's uncommitted changes onto the base
  branch. Someone who edits files in the repository while specrun is stopped
  has to commit or stash them first.
