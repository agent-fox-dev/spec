# 09. Review the PRD adversarially before generating the package

**Status:** accepted
**Date:** 2026-10-21
**Related:** [ADR 02](02-choose-the-model-per-phase.md),
[ADR 03](03-pipeline-the-scopes-of-a-split.md),
[ADR 04](04-hand-on-what-a-phase-learned.md)

## Context

A run writes its PRD in one unattended pass, and only the agent that wrote
it ever checks it. Validation then checks the structure of what is generated
from it: every criterion tested, every test owned. Nothing checks the PRD's
substance. A PRD can miss an error path, misread the code, propose a
component that already exists, or drop something the input asked for. Its
package still validates, and three generation phases are built on the flaw.

## Decision

`spec --adversarial` puts review rounds between the PRD phase and the
package (`specgen/rounds.go`). A round has two phases:

- **`prd_review`** is a fresh agent that looks for the PRD's flaws. It sees
  what the author saw, with the author's `codebase_notes` labelled as claims
  to check. It submits findings, each with a severity and evidence.
- **`prd_revision`** is another fresh agent that answers every finding,
  `accepted` with the PRD changed or `rejected` with a reason, and submits
  the revised PRD.

`--adversarial-rounds` (1 to 5, default 1) bounds the rounds. They stop early
once a review raises nothing `critical` or `major`.

- **The reviser is neither the author nor the reviewer.** The author would
  defend its own choices. A third agent weighs each finding on its evidence.
- **The revision cannot change the spec name, the title or the split.** Its
  tool has no field for them. The split plan and the package numbers depend
  on the PRD phase's decision, and leaving the fields out costs no repair turn
  and no union parameter of a strict tool.
- **Repeats are caught in Go where they can be.** Findings get run-assigned
  ids `R<round>-F<n>`, and a later finding raises a rejected one again only by
  naming it in `reopens`. A reopen with the evidence the rejection answered is
  turned back, and so is a repeat of a rejected section and flaw that names no
  `reopens`. The prompt holds the reviewer to the rest of the rule.
- **The review only improves a PRD that is already complete.** A review or
  revision that does not finish, or a `--total-budget` already spent, stops
  the rounds with a `high` `prd_review_incomplete` warning, and the package is
  built from the last complete PRD. An interrupt is not such a failure: it
  fails the run as aborted at stage `prd_review` or `prd_revision`.
- **Where the rounds run.**
  - For an undivided input, they run after `--name` and before the number is
    reserved.
  - In a split, they run in the coordinator, before the scope's number is
    reserved and its package started. The next scope's PRD phase is then
    shown the revised PRD and notes, and the packages already in flight keep
    being generated beside it.
- **The review phases run on the tier's base entry**, the PRD phase's model
  and effort. Judging a PRD takes the judgment writing one does. They are
  `--phase-model` kinds of their own, and a choice for `prd` does not move
  them.
- **The review lives in the result only**, as `Package.Review`. The package
  keeps the files the format defines, and the `--comment` text is the final
  PRD.

## Consequences

- A reviewed PRD costs two to ten more phases. The preflight estimate counts
  every allowed round, an upper bound for those phases.
- `agentrun.Phases` has two more kinds. `model.phases`, the `--phase-model`
  help and `effort_clamped` list them even without `--adversarial`, as they
  list `architecture` without `--architecture`. Without the flag, nothing that
  runs, is written or is reported changes.
- A reviewer can still be wrong, and a reviser can accept a wrong finding.
  The record of every finding and its answer is in the result for a person to
  read.
