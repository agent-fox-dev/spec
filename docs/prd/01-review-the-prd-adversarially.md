# Review the PRD adversarially before generating the package

## Intent

A PRD that `spec` writes in one unattended pass has been checked only by the
agent that wrote it, so that agent's blind spots are carried into every
artifact generated from it. With `--adversarial`, a separate agent that did
not write the PRD looks for its flaws. Another fresh agent then revises the
PRD to answer them, and only after that are the requirements, tests and
tasks generated from it, so a package is built on a PRD that has stood up to
a critic.

## Goals

- With `--adversarial`, every PRD a run writes is reviewed and revised before
  its requirements phase starts. That covers the PRD of an undivided input and
  the PRD of each scope of a split written in this run.
- `--adversarial-rounds N` (1 to 5, default 1) sets the most review and
  revision rounds a PRD gets. The rounds stop early once a review raises no
  `critical` or `major` finding.
- Without `--adversarial`, a run is unchanged: the same phases, prompts, cost
  and output as before this feature.
- The result reports, per package and per round, every finding the reviewer
  raised and what became of it: `accepted` (the PRD was changed) or
  `rejected` (with the reviser's reason).
- A review or revision that cannot finish never costs the user the package.
  The run generates from the last complete PRD and says so in a
  high-severity warning.
- `--preflight` counts the review in its estimate and lists its models in
  `phase_models`, and `--phase-model` can run the review and the revision on
  models of their own.

## Non-goals

- Reviewing `requirements.json`, `test_spec.json`, `tasks.json` or
  `architecture.md` adversarially. Only the PRD is reviewed.
- Asking a person anything. The run stays unattended.
- More than one reviewer per round, or reviewers debating each other.
- A command or mode that reviews a PRD without generating a package.
- Reviewing the PRD of a package already on disk. This includes the scopes a
  resumed split wrote in an earlier run.
- Writing the review into the package on disk. The package keeps exactly the
  files the format defines.
- Changing the split decision. A reviewer may say the PRD is more than one
  spec's worth of work, but the revision does not re-plan the split.

## Background

A run today is one PRD phase followed by three generation phases, for
`requirements.json`, `test_spec.json` and `tasks.json` (`specgen/prd.go`,
`specgen/pipeline.go`), plus an optional architecture phase. Nobody is
waiting to answer questions, so the PRD phase settles every open question
itself. It records each answer in `## Design Decisions` and reports the ones
it is least sure of as `result.open_questions`. It also hands on
`codebase_notes`, its reading of the code, to the later phases
(ADR 04).

Validation then checks the structure of the generated artifacts: every
criterion is tested, every test is owned by a task, the test commands match
the project's language. Nothing checks the substance of the PRD itself. A PRD
can miss an error path, misread the code, propose a component that already
exists, or drop something the input asked for, and it still yields a package
that validates but specifies the wrong thing. Each generation phase builds on
the PRD in front of it without questioning it, so a flaw in the PRD costs
three phases of work built on it.

The model per phase is chosen by tier and by `--phase-model` (ADR 02), and
the scopes of a split are pipelined: each scope's PRD phase sees the PRDs of
the scopes before it (ADR 03).

## Requirements

### The flags

- `--adversarial` is a boolean flag, off by default. When it is on, every PRD
  the run writes is reviewed and revised before its package is generated.
- `--adversarial-rounds N` sets the most rounds a PRD gets. It takes 1 to 5
  and defaults to 1. Each of these is a usage error, raised before any model
  call:
  - a value outside 1 to 5
  - `--adversarial-rounds` given without `--adversarial`
- With `--dry-run`, the review and revision run like every other phase, and
  nothing is written.
- With `--preflight`, the review is counted and nothing runs. `estimate.phases`
  counts a review and a revision for every round of every package still to be
  written. The rounds can stop early, so this figure is an upper bound.
  `phase_models` lists the model and effort of `prd_review` and
  `prd_revision`.

### One round

A round is one review followed by one revision.

- **The review** runs in a fresh conversation that has seen nothing of the
  PRD phase's work except what it is handed:
  - the PRD as it stands
  - the original input and any `--context`
  - the PRD phase's `codebase_notes`, marked as the author's claims to be
    checked, not facts
  - the steering, the repository map, and the read-only file tools every
    phase has

  It is told to find flaws, not to approve. It looks for:
  - behaviours without an error path
  - goals that cannot be verified
  - claims about the code (in the PRD or in the notes) that the code
    contradicts
  - a proposed component that already exists under another name
  - scope the input did not ask for, and asks of the input that the PRD
    dropped
  - wording that two readers would turn into different requirements
  - sections that contradict each other
  - Design Decisions whose reason does not hold
  - internal design (package layout, types, signatures) that leaked into the
    PRD
- **Each finding** carries:
  - a severity: `critical` (the package would specify the wrong thing),
    `major` (a requirement would be missing or untestable) or `minor`
    (wording, clarity)
  - the PRD section it concerns
  - the flaw, in one or two sentences
  - the evidence: a quote from the PRD or the input, or a file path and what
    it shows
  - a suggested remedy

  A review raises at most 30 findings. A finding with no evidence is turned
  back to the reviewer to support or withdraw.
- **A review with no findings** ends the rounds. The PRD goes on unchanged.
- **The revision** runs in another fresh conversation. It is handed:
  - the PRD
  - the findings
  - the original input and `--context`
  - the `codebase_notes`
  - the same project context and read-only tools as the review

  It answers every finding, either:
  - `accepted`: the PRD is changed to remedy it, or
  - `rejected`: with a reason, which must say why the finding does not hold,
    not merely that the author disagrees

  A revision that leaves a finding unanswered, or rejects one without a
  reason, is turned back to it naming the finding.
- **The revised PRD** has the same shape the PRD phase produces. Its spec
  name, title and split are those of the PRD it revises. A revision that
  changes them is turned back. It may change:
  - any section of the body, including `## Intent`, which is not hashed until
    the package is activated
  - the open questions
  - the relevant files
  - the `codebase_notes`, when a finding showed a note to be wrong (bounded
    to the same 12000 bytes)
- **A revised PRD with no `## Intent` section** is turned back, as it is for
  the PRD phase.

### More than one round

- After a round, another round runs if both of these hold:
  - the round's review raised at least one `critical` or `major` finding
  - fewer than `--adversarial-rounds` rounds have run

  A review whose findings are all `minor` is still revised, and is the last
  round.
- A later round's reviewer is handed, beside the current PRD, every earlier
  round's findings with their dispositions and reasons. It may raise a
  finding that was rejected before only with evidence the rejection did not
  answer. A repeat without new evidence is turned back to it.
- A later round's reviser is handed the earlier rounds the same way.

### When a round cannot finish

- If a review or a revision ends without a result (budget, turns, timeout,
  model or API error), the rounds stop. Every later phase is generated from
  the last PRD that is complete: the PRD phase's own when no revision
  finished, or the last finished revision's.
- The run then carries a `high` warning, `prd_review_incomplete`, naming:
  - the scope
  - the round
  - the phase that failed (`prd_review` or `prd_revision`)
  - why it failed
- A review that finished but whose revision did not reports its findings
  with the disposition `unanswered`.
- A failed review or revision never by itself fails the run or changes its
  exit code.
- `--total-budget` is checked before each review and each revision. When the
  run is over it, the rounds stop as above, with the warning naming the
  budget as the reason.

### Splits

- In a split, each scope's PRD goes through its rounds before its package
  starts. The PRD phase of the next scope is shown the revised PRD, not the
  original.
- A resumed split reviews the PRDs of the scopes it writes in this run, and
  no others.

### The output

- Each package in the result, the first one and every follow-on package of a
  split, carries a `review` when `--adversarial` was on. It holds:
  - the rounds run and the rounds allowed
  - why the rounds stopped: `no_findings`, `no_serious_findings`,
    `rounds_exhausted` or `incomplete`
  - per round, the findings, each with its severity, section, flaw,
    evidence, remedy, disposition and, when rejected, the reason
- The summary sentence adds one clause, for example
  `review: 2 rounds, 9 findings, 7 accepted`.
- Without `--adversarial`, `review` is absent and the summary is unchanged.
- `result.open_questions` are those of the final PRD.
- Progress on stderr reports each review and revision as a phase, with its
  label, cost and turns, like every other phase. For example:
  `scope 2 of 3 (issuex_github): reviewing the PRD (round 1 of 2)`.
- The `prd.md` written is the final PRD. `--comment` posts the final PRD.
- Every phase's cost appears in the run's per-phase usage under its own phase
  name.

### Models and ceilings

- `prd_review` and `prd_revision` are phase names that `--phase-model`
  accepts, like `prd` and `requirements`.
- Each tier runs both at the PRD phase's model and effort:

  | Tier | `prd_review`, `prd_revision` |
  |---|---|
  | `SIMPLE` | `claude-sonnet-5-5` `medium` |
  | `STANDARD` | `claude-sonnet-5-5` `high` |
  | `ADVANCED` | `claude-opus-5-5` `xhigh` |

  A model id given to `--model`, and `--effort`, apply to them as to every
  phase.
- `--budget`, `--max-turns`, `--phase-timeout` and `--max-tokens` apply to
  each review and each revision as a phase of its own.

## Design Decisions

1. **A fresh reviser, not the PRD's author.** The author would be inclined
   to defend its own choices. A third agent weighs each finding on its
   evidence. The cost is a third read of the code, softened by the
   `codebase_notes`.
2. **The reviewer sees the author's `codebase_notes`, marked as claims.**
   Without them it would read the same code again, which is the cost ADR 04
   removed. Marking them as claims keeps the reviewer checking them against
   the code instead of trusting them.
3. **The round count is a separate flag, `--adversarial-rounds`, and the
   flag that turns the review on stays a plain switch.** `--adversarial` on
   its own is then the common case: one round. A round count with nothing to
   count is refused instead of silently ignored.
4. **At most 5 rounds, stopping early once nothing serious is found.** Each
   round is two full phases. A review that finds only `minor` points does not
   justify paying for another.
5. **Later rounds see the earlier findings and dispositions.** Otherwise a
   reviewer with a clean context raises the same rejected point every round,
   and the rounds go in circles.
6. **A failed review or revision is a warning, not a failed run.** The review
   only improves a PRD that is already complete. Losing the package because
   the critic ran out of budget would be worse than generating from the
   unreviewed PRD. A failed `--architecture` phase is already handled the
   same way.
7. **The revision cannot change the spec name, the title or the split.** The
   split plan is recorded and package numbers are reserved from the PRD
   phase's decision. Re-planning in a revision would make that unstable. A
   reviewer that thinks the work should be split can say so as a finding,
   and the reviser answers it in the PRD's own text.
8. **The review is recorded in the result, not in the package.** The package
   format defines its files, and the review is about how the PRD was made,
   not part of what it specifies. What a remedy settled appears in the PRD's
   own Design Decisions.
9. **The review phases run at the PRD phase's tier.** Judging a PRD takes
   the same judgment as writing one. A cheaper critic would miss the flaws
   the feature exists to catch.
10. **At most 30 findings per review, each with evidence.** An unsupported
    finding costs a revision without telling it anything it can check.
    A cap keeps a review from listing every stylistic preference.

## Dependencies

- **ADR 02, choose the model per phase.** Two new phase names, and their
  defaults in each tier.
- **ADR 03, pipeline the scopes of a split.** A scope's PRD now ends after
  its rounds, which delays both its package and the next scope's PRD phase.
- **ADR 04, hand on what a phase learned.** The reviewer and the reviser read
  `codebase_notes`, and the reviser may correct them.
