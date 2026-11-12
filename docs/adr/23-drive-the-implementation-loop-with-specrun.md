# 23. Drive the implementation loop with specrun

**Status:** accepted
**Date:** 2026-11-12
**Related:** [ADR 10](10-report-and-change-the-state-of-a-package-with-its-own-command.md), [ADR 22](22-run-the-gate-with-speca.md)

## Context

The implementation loop ran `/af-impl` on one package after another, each in a
fresh headless Claude Code session, through `scripts/impl-specs.sh`: about 100
lines of bash around `speca --next`, `claude -p` and four checks that the
package landed. It saved only each session's final message, hardcoded the
branch `main`, could not resume, could not tell a session that had hung from
one that was working, and had no test: its decisions could only be tried by a
real run of several hours. What the loop must do next (stop and repair
decisions, a reconcile, label rules, a ledger) is a state machine a script
cannot hold.

## Decision

- **The driver is a Go command in this repository, `specrun`**, on `afspec` and
  `aflib/envelope`. Its rules are tested in seconds against a fake `claude`
  first on `PATH` and a fixture repository; the facts it decides on come from
  `afspec` in process.
- **The choice of the next package lives in `afspec` (`ChooseNext`)**, which
  `speca --next` and `specrun` both call: `specrun` may not import
  `cmd/speca` or `internal/specpkg`, which link `agentkit-go`, and a second
  copy of the rule would drift.
- **Claude Code stays the only model runtime, through `claude -p`.** `specrun`
  calls no model API and links nothing of `agentkit-go`.
- **Sessions are checked, not trusted.** After every session, whatever its exit
  code, four checks read the repository: the base branch is checked out, the
  tree is clean, `HEAD` moved, and the package has no open task. A session that
  ends early, stalls or claims too much is caught the same way every time.
- **State is one JSON file, outside the repository**, under the user's state
  directory: the steps done and the step running, with the commit and the
  process id. The repository holds the rest of the truth, so a resumed run
  checks the running step against it. A second `specrun` is refused while the
  state records a live `specrun` (by its process id, and by the name `ps`
  gives it) as running a step or as holding the repository, from its first
  step to its end.
- **An interrupt or a termination of `specrun` ends the session's process
  group**, since the session runs in a group of its own that the terminal's
  signals do not reach; the step is left to be taken up again.
- **A session that writes nothing for `--stall` (30 minutes) is ended** with
  its process group, and `CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS` is set to the
  same time, so that a session never waits on its subagents longer than
  `specrun` waits on it.
- **Every session's stream is kept**, with its final message and a ledger line
  of what the step took, in a log directory outside the working tree, since
  af-impl refuses a dirty tree.
- **The base branch is `origin/HEAD`'s, else `main`**, with no flag.
- **`specrun` is built with the hash of the af-impl skill it was built from**,
  the render for Claude Code, and warns (or, with `--strict-skills`, refuses)
  when the installed one differs.

## Consequences

- `scripts/impl-specs.sh` is gone; `specrun` is installed by `make build` with
  the other three commands.
- `aflib` is a dependency of the module, at a pseudo-version: it has no tag.
- A `specrun` built with `go build` has no embedded hash and cannot tell
  whether the installed skill is the right one.
- The ledger's gate and open-item fields, and every forge write, come with the
  packages after this one.
