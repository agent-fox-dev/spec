# 26. Close a task on evidence

**Status:** accepted
**Date:** 2026-11-13
**Related:** [ADR 10](10-report-and-change-the-state-of-a-package-with-its-own-command.md), [ADR 22](22-run-the-gate-with-speca.md)

## Context

A task was recorded `done` when the gate passed, and the gate is the linter
and the whole test suite. "Done" said that nothing was broken. It did not say
that each criterion the task owns has a test, that the test ever failed
without the implementation, or that the criteria were still the ones the
package was activated with. `af-impl` asked the implementer to watch each new
test fail before implementing, to declare a test-first deviation when it
could not, and to map every criterion to its test in the commit. All three
were prose a model reported about itself, and nothing checked them. Activation
froze the PRD's `## Intent` with `intent_hash`, but an edit to a criterion, a
check or a test command in `requirements.json` or `tasks.json` went unnoticed,
so a spec could be edited to match the code.

## Decision

- **The contract is frozen at activation.** `speca <package> --status active`
  records `contract_hash` in the PRD frontmatter: the SHA-256 of
  `requirements.json` and of `tasks.json`, each over the canonical encoding
  `Save` writes, with every `x-` extension key left out, keyed by file name.
  One digest per file, so that a refusal can name the file that changed. A
  package *requires evidence* when it is active and has the hash. While either
  file differs from it, the task form, the evidence form and sealing are
  refused at stage `status`, with no `fix_hint`, `--force` or not, as for a
  drifted intent. No call re-accepts a changed contract: the files are
  restored, or a package that supersedes this one is written. A divergence
  found while implementing stays a deviation recorded in an ADR.
- **The hash covers both files whole.** `tasks.json` says which task owns
  which criterion and holds the command that produces the evidence; leaving
  it out would let either be changed to get a close. Extension keys are left
  out because the format gives them no meaning.
- **Two calls, not one.** `speca <package> --task N --evidence <file>`, made
  while the task is in progress and before the implementation exists, runs
  the tests the file maps to the task's criteria and records each one's
  failing run, with its time and the gate's tree id. `--state done` reads what
  was recorded, runs the tests again, and closes the task only when every
  criterion has an entry, every test has a failing run or is exempt, and every
  test passes; it records the passing runs with the state, in one write of
  `progress.json`. `speca <package> --verify-evidence` runs every recorded test
  of every done task on the tree as it is, for the merged tree, and writes
  nothing.
- **Any runner, through one project command.** `test_commands.evidence_tests`
  runs the tests it is given in place of `{tests}` and prints one JSON line per
  test with its status: `pass`, `fail`, `error`, `missing` or `skipped`. The
  format names the command, its placeholder and its lines; the project's tool
  does the running. A build or collection failure (`error`) is not a failing
  test.
- **Go is built in.** A Go module that names no `evidence_tests` runs
  `go test -json -count=1` once per package, with a `-run` pattern of exactly
  the tests named, so the repositories that exist need no adapter. No runner
  means no close without `--force`, and `--validate` warns of it beforehand.
- **An exemption is declared, stored and warned.** `no_red` with a reason, for
  a criterion whose behaviour existed before the task; a low warning at the
  close. The integration task maps paths to smoke tests and needs no failing
  run: the tasks before it made the paths live.
- **`--force` still closes,** with a high warning and the change marked
  forced; `af-impl` forbids it. Reopening a task removes its evidence.
- **Only packages activated from now on.** The presence of `contract_hash` is
  the switch: a package activated before this change closes its tasks as it
  always did. Every addition is optional in format version 3.

## Consequences

- The frontmatter gains `contract_hash`, `test_commands` gains
  `evidence_tests`, and a `progress.json` task entry gains `evidence`; the
  report gains `contract_hash`, `contract_drift`, `evidence_required`,
  `evidence_log`, and each task's `evidence`; `--verify-evidence` answers in
  `evidence_check`. `speca` gains `internal/evidence`.
- `speca` proves that a test exists, failed and passes; it does not judge
  whether the test asserts what its check says, nor inspect what changed
  between the failing and the passing run. Those stay reviews.
- A close runs the task's tests once more than before. The gate is unchanged:
  its passes are not tied to the evidence.
- The refusal of a changed contract is `speca`'s. The library's `Save` still
  takes an active package whose contract changed, so a program that writes a
  package through the library is not held to it.
- A test helper that edits an active package's contract files activates it
  without the hash, as a package activated before this change.
