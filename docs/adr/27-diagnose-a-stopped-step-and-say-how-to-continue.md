# 27. Diagnose a stopped step and say how to continue

**Status:** accepted
**Date:** 2026-11-15
**Related:** [ADR 23](23-drive-the-implementation-loop-with-specrun.md), [ADR 25](25-group-specrun-logs-by-run.md)

## Context

A step that fails a landing check stops the loop with the words of the first
check that failed, such as `not on the base branch`. Those words say where
the session left the work, not why, and nothing else in the envelope did
either:

- In one run, a person replaced the local `main` with an unrelated commit
  while a session worked. The session finished the package and pushed its
  branch. Then it found it could not merge, and said so precisely in its
  final message. specrun reported `not on the base branch`, with
  `resumable: false`.
- The final message went only into the step's `.log` file, which the
  envelope did not point to. specrun shows a session's last words only when
  the session ended in an error.
- `resumable` was false on every stop. Yet a stopped step is kept in the
  state and taken up by the next start: running specrun again was the way
  on.
- The rewritten `main` went unnoticed for two and a half hours, until the
  session reached its merge.

## Decision

- **A stopped step is diagnosed from git.** This applies to every step the
  state keeps to take up again: an implement, repair or reconcile step that
  failed a landing check, stalled, was interrupted or ended in an error.
  `result.diagnosis` gives:
  - the branch checked out and whether the tree is clean;
  - how the base branch moved since the step started: `unchanged`,
    `advanced`, `rewritten` or `missing`;
  - how it stands to origin's: `same`, `ahead`, `behind`, `diverged` or
    `unrelated`;
  - its newest reflog entry, when it was rewritten or stands apart;
  - for an implement step, the package's branch `feature/<NN>-<spec_name>`:
    whether it exists, is pushed and is merged, and how many tasks are done
    on it, read from the branch's own `progress.json`;
  - `cause`, which words all this as one sentence and is appended to
    `error.message`.
  
  A broken base branch comes first, since no session can work around it.
- **The stop says what to do.** `result.recovery` lists the steps in order.
  Each has what it does, the command that does it, and `decide` when it
  discards or publishes something. The list ends with `specrun`, which takes
  the step up again. The same steps are printed to stderr. `error.resumable`
  is true for such a stop.
- **The session's last report goes with the stop.** `stopped.session_report`
  holds the first paragraph of the step's final message, and `stopped.log`
  the file that holds all of it, whatever the session ended with.
- **The base branch is watched while a session runs**, every 30 seconds.
  When it stops holding the commit the session started from, specrun warns
  at once: `base_rewritten`, on stderr and in the envelope. A person can then
  put the branch back before the session reaches its merge. specrun does not
  end the session: its committed work is good, and only the merge depends on
  the base branch.
- **The preflight uses the same diagnosis.** It covers a base branch that
  cannot be pulled with `--ff-only`, and a dry run shows a stopped step with
  its diagnosis, its recovery and `next_step.resumes`. No new flag is added:
  `--dry-run` is the status.
- **A finished package is landed by the next start, not by a new mode.**
  af-impl, run again on a package, continues its branch. With every task
  done there, it goes to conformance and the merge, which writes the
  `AF-Run:` trailer. The recovery says so. A landing-only mode of af-impl
  would save the second conformance pass, at the cost of a second way to
  land.

## Consequences

- Every stop of a kept step costs a handful of `git` processes, and a running
  session one `git rev-parse` every 30 seconds, plus a `merge-base` when the
  branch moved.
- The diagnosis reads origin's branch as the last fetch left it, and fetches
  nothing.
- Recovery commands are suggestions a person runs. specrun never moves the
  base branch itself: what a rewritten branch holds may be someone's work.
