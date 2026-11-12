# 22. Run the gate with speca

**Status:** accepted
**Date:** 2026-11-12
**Related:** [ADR 10](10-report-and-change-the-state-of-a-package-with-its-own-command.md)

## Context

The gate that decides whether a task or a branch may land is a package's own
`linter` and `all_tests` commands. `af-impl` had the model run them by hand:
at the baseline, after every task and after the integration task, in a clean
environment and after the conformance fixes, and on the merged tree, 14 to 16
runs for a ten-task package. The model kept the results in its head. It
checked trees that had already passed (the baseline of one package is the
merged tree of the one before, and a `--no-ff` merge onto a base that did not
move has the branch tip's tree), nothing measured what a run cost, and the
bookkeeping differed from run to run. Over 25 packages on one repository the
time in the gate grew from 3 to 44 minutes per package, and it was seen only in
wall time. The project template also had every task gate widened to the
repository's most thorough target (`make check`).

## Decision

- **The gate is a `speca` command, not instructions to the model.**
  `speca <package> --gate task|full` runs `linter`, then `all_tests`, and
  answers with one verdict, `pass`, `fail` or `not_run`, in the JSON envelope,
  exit code 0 for each. Recording, timing, parsing per-unit times and summing
  runs are mechanical, and as skill text they are fragile and cost model turns.
  `speca <package> --gate-report` sums the runs for the merge commit.
- **No shell.** A command string is split on white space with quotes and
  started directly. `test_commands` must need no shell features, and anything
  more would be a shell.
- **A pass is reused by tree id, never by commit.** Two commits with the same
  tree are the same code. The key is the tree id of the working tree (tracked
  and untracked files, ignored ones left out), the directory the commands run
  in as a path under the top level, the level, the two command strings and
  whether the environment was clean, so a clean-environment run is never
  answered by a normal one. The directory is beyond the four parts the spec
  named: without it, the same commands run in two directories of one
  repository would answer the second from the first's pass. **Only passes are recorded**: a failure is
  cheap to run again and must be seen.
- **The tree id is worked out without touching the repository**: a copy of
  the index in a temporary file, objects written to a temporary object
  directory, and `git write-tree --missing-ok`. The repository's object store
  is not given to git as an alternate: git refreshes the modification time of
  an object it finds in one, which would change files under `.git`.
- **The state lives under `$XDG_STATE_HOME`, else `~/.local/state`, in
  `agent-fox/speca`**, one directory per repository, on Linux and macOS alike:
  each command's log, the passes, and a run log of every gate that ended in
  pass or fail. Nothing is written into the repository. A gate that cannot be
  recorded (no tree id, a state directory that cannot be written) still runs
  and answers, with a `gate_not_recorded` warning: the verdict is still true.
- **A command times out after 20 minutes by default**, below the 30 minutes a
  driver waits for an event, and is ended with every process it started (a
  process group of its own on Unix). What a command leaves running when it
  exits is ended as well, and the command's own exit code stands.
- **The report selects runs by ancestry, not by time**: a run counts when the
  commit checked out at it is `--since` or a descendant of it that `HEAD`
  reaches, or `MERGE_HEAD` while a merge is uncommitted, since the gate on the
  merged tree runs before the merge is committed.
- **The thorough repository target runs once, before the merge commit.**
  `af-impl` runs the task gate after every implement task and the full gate at
  the baseline, after the integration task, after the conformance fix commit
  and on the merged tree, and the repository's `make check`, or its like, once
  on the merged tree. Formatting and tidiness cannot change a test's result,
  and the gate on the merged tree decides what lands. This order takes
  precedence over a project instruction to substitute the thorough target for
  every check.

## Consequences

- `AdminResult` gains `Gate` and `GateReport`; `speca` gains `internal/gate`,
  and the first forms that start a process and write outside the repository.
- Both levels run `all_tests` for now. The level is recorded and keyed, so a
  later `affected_tests` command for the task gate changes one command and no
  caller.
- Only Go's test output gives slowest units; for any other the list is empty.
- The state directory grows: logs and run-log lines are not pruned. Deleting
  it loses only reuse and the report's history.
- A reused pass is as good as the tree id: a test that reads something outside
  the working tree (a file under `$HOME`, the network, the clock) can pass on
  one run and fail on the next with the same tree, and the reuse hides the
  second run. The clean-environment run is keyed apart for that reason.
