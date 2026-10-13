# 03. Pipeline the scopes of a split

**Status:** accepted
**Date:** 2026-10-13
**Related:** [ADR 02](02-choose-the-model-per-phase.md); agent-fox ADR 05
(write every scope of a split)

## Context

An input that is more than one spec's worth of work is written as one package
per scope. Each scope is a PRD phase followed by three generation phases (four
with `--architecture`), and the scopes ran strictly one after another: a
four-scope run was sixteen phases end to end, and the reason multi-spec runs
were slow.

What a later scope needs from an earlier one is its PRD. The follow-on PRD
phase is shown the earlier scopes' Intent, Goals and Non-goals; the
generation phases of a package see only their own PRD and artifacts, and a
package names another spec only by its id, in `## Dependencies` and the
tasks' `dependencies`. Inside one package the order is fixed —
requirements, then tests, then tasks, each seeing the artifacts before it —
because generating them in parallel is how the previous format left tests
owned by no task.

## Decision

- **The PRD phases stay in order; the packages overlap.** A scope's package is
  built as soon as its PRD exists, beside the next scope's PRD phase, with at
  most `--max-parallel` (default 3) packages in flight. A later PRD sees an
  earlier scope still being generated through the PRD this run wrote for it,
  held in memory.
- **The order of the plan is kept where it is observable.** Package numbers
  are reserved by the coordinator in scope order, and the result lists the
  packages in plan order, whichever finishes first.
- **A failure stops new work, not work in flight.** No PRD phase or package
  starts after a failure; packages already being generated finish and are
  recorded in the plan, because they are paid for and the next run would
  otherwise generate them again. The next run on the same input writes the
  scopes still without a package.
- **`--max-parallel 1` is the old order**, each scope's PRD waiting for the
  package before it.

## Consequences

- A split of N scopes takes about N PRD phases plus one package's generation,
  instead of N of each, when `--max-parallel` covers the scopes in flight.
- Up to `--max-parallel` + 1 agents call the model at once; an account with a
  low rate limit lowers it.
- `--total-budget` counts finished phases, so packages in flight can take a
  run past it by up to their per-phase ceilings.
- A run that fails may leave a later scope written and an earlier one not.
  The resumed run writes the earlier one against the packages on disk, later
  ones included, as the split table shows.
