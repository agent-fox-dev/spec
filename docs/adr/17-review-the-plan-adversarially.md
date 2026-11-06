# 17. Review the plan adversarially before the package is written

**Status:** accepted
**Date:** 2026-11-06
**Related:** [ADR 02](02-choose-the-model-per-phase.md), [ADR 03](03-pipeline-the-scopes-of-a-split.md), [ADR 09](09-review-the-prd-adversarially.md), [ADR 15](15-version-the-steering.md), [ADR 16](16-triage-every-review-finding-four-ways.md)

## Context

The adversarial review of ADR 09 reads the PRD; the requirements, tests and
tasks generated from it are checked only by the validator. That proves the
plan covers the PRD, not that a coder can carry it out without deciding the
design, nor that the files and symbols it names exist. In every package of
this repository already implemented, paths in `touches` existed nowhere — each
a decision the plan left to the coder, or one the coder overruled, with
nothing recording which was right. `docs/spec-kit-comparison.md` §4.4, §4.8,
§4.9 and §7.1 item 1 recommended a review of the plan.

## Decision

- **A second stage with its own flag.** `--adversarial-plan` reviews the plan
  of every package a run generates — `requirements.json`, `test_spec.json`
  and `tasks.json` — after the tasks phase and before the architecture phase
  and the write, so `architecture.md` is written from the revised plan. It is
  not part of `--adversarial`: it doubles what a review costs, and the two
  stages catch different flaws, a PRD that specifies the wrong thing and a
  plan a coder cannot follow. `--adversarial-rounds` is the limit of each.
- **Two written rubrics, plus the steering.** `zero_decision`: a coder who
  holds the package and the code has no design choice left to make — where the
  code goes; every name, signature, error, message, format and order a test
  asserts; no library, algorithm or data structure left open where it shows;
  no step that leaves the work open; no two readings a test tells apart.
  `grounding`: the plan names nothing that is not there — paths, symbols,
  claims about the code, test commands. `steering`, as in the PRD review
  (ADR 16), quotes the directive and cannot be declined. Every finding names
  its rubric and its place in the plan by id.
- **The grounding facts are computed, not judged.** Before the first round
  the run resolves every path in every task's `touches` inside the repository
  and looks at it: exists, as a file or a directory, or not. That is the one
  part of grounding a machine settles; the model judges what it cannot,
  whether a step creates the path and whether a symbol exists under the name
  the plan uses. A grounding finding about a missing path must name a path the
  facts list.
- **An invalid plan is not reviewed.** The plan is validated in memory with
  `afspec.ValidateGenerationStepComplete(StepTasks, …)` first; one that fails
  gets a `plan_review_skipped` warning and is written and validated as it would
  be without the flag, failing the run on the same errors. A review would pay
  to find what the validator already found.
- **The reviser may change the three artifacts and nothing else.** A decision
  the plan leaves open often belongs in a criterion's contract, so the
  requirements may change too. The PRD, the spec's identity, a task's state
  and the actors of the execution paths may not — the later scopes of a split
  were handed the actors when this package's requirements phase ended — and
  the revised plan must pass the schemas and C1 to C11, or it is turned back
  until it does.
- **The rounds are the PRD's.** One loop (`runStage`) drives both reviews: the
  same ids, dispositions and checks, repeat rules, two-clean-round
  convergence, failure handling and record shape (ADR 16). A plan phase that
  does not finish is a `plan_review_incomplete` warning and costs the review,
  never the package or the exit code.
- **Two phase names, at the requirements phase's model.** `plan_review` and
  `plan_revision` are phases `--phase-model` accepts. Every tier lists both
  explicitly at its requirements entry (`ADVANCED`: opus at `high`, where its
  base is `xhigh`), so the table is literal rather than a coincidence of where
  `base` points.

## Consequences

- A run with neither review flag runs exactly the phases it ran; with
  `--adversarial-plan`, each package costs up to two more phases per round,
  and the preflight prices them.
- The plan phases are never shown the input or `--context`: the plan answers
  to the PRD.
- A scope's plan rounds run inside its own package build, so the next scope's
  PRD phase does not wait for them (ADR 03); a resumed split reviews only the
  plans it writes.
- The generating tools already refuse a plan that breaks the format's rules at
  each step, so a run through the real phases always reaches the rounds; the
  gate is the backstop for a plan that reached the pipeline another way.
