---
spec_id: "17"
spec_name: "review_triage_convergence"
title: "Triage every review finding four ways and converge on two clean rounds"
status: "active"
created_at: "2026-11-06T12:57:04Z"
updated_at: "2026-11-06T14:36:37Z"
intent_hash: "d7b0f3b9d381d79052350b654463f92554e3477b0779d1dc2f183ef362e35109"
schema_version: 2
source: "docs/prd/04-review-the-plan-commit-the-render-and-version-the-steering.md"
---

# Triage every review finding four ways and converge on two clean rounds

## Intent

The adversarial review answers a finding two ways: accepted or rejected. A
rejection carries a reason, and that is all. So a finding the reviser cannot
refute but will not act on, and one that needs a trade-off nobody unattended
should settle alone, are both recorded as "rejected with a reason" — the same
word for refuting a reviewer, for deferring work and for a decision a person
should own. And a single clean review stops the rounds, so one sample decides
that a PRD has no serious flaw left.

This change gives the review the triage and convergence rules of EGM. Every
finding is answered `fixed`, `contested`, `escalated` or `declined`. A
contest must show counter-evidence a machine can find; an escalation takes a
provisional decision, writes it into the PRD and hands the question to the
caller. The rounds stop once two consecutive rounds are clean. The reviewer
also checks the PRD against the project's steering directives, and a
contradiction is a finding that cannot be declined. The record in the
envelope changes with the vocabulary, which is a breaking change an ADR
records.

## Goals

- The reviser answers every finding as `fixed`, `contested`, `escalated` or
  `declined`, and no answer that the run cannot check gets through: a contest
  shows counter-evidence that is found, an escalation carries its question,
  its options, its decision and the reason the reviser is unsure of it, and a
  decline gives a reason and is refused for a `critical` or a `steering`
  finding.
- An escalation's provisional decision is written into the PRD, so the
  package stays complete, and the question reaches the caller in
  `result.open_questions`.
- A later round's reviewer is handed every earlier finding with its answer. A
  contested finding may be raised again only through `reopens`, with evidence
  the contest did not answer; a declined or escalated one may not be raised
  again at all.
- The rounds stop on `converged` (two consecutive clean rounds),
  `rounds_exhausted` or `incomplete`. A clean round is one whose review
  raised nothing `critical` or `major`; its minor findings are still revised.
  The default of one round costs exactly what it costs today.
- With `--adversarial` and a steering that says something, a PRD that
  contradicts a directive gets a finding with the rubric `steering`, quoting
  the directive. That finding cannot be declined.
- `envelope.Review` records `clean` per round, `rubric` and the reviser's
  answer per finding, six derived totals and the three stop reasons. The old
  vocabulary is gone, and an ADR records the break.

## Non-goals

- The plan review. `--adversarial-plan`, the grounding facts, the
  `zero_decision` and `grounding` rubrics, the plan reviewer and reviser, and
  the `plan_review` and `plan_revision` phase names are the
  `adversarial_plan_review` package, which is written after this one and
  applies these same rules to the plan. This package makes the rules and the
  record ready for it, and reviews the PRD alone.
- A third rubric for tests that prove nothing. It is a later change, once the
  first rubrics show what a rubric finding costs.
- Checking a package against the steering by machine. The directives are
  prose, and design rule 3 of the format keeps the validator from judging
  prose. Alignment is the reviewer's job.
- A gate that refuses to generate or activate a package because of the
  steering. A contradiction is reported, as a finding, not refused.
- Raising the default round count. Converging takes at least two rounds, and
  raising the default would raise the cost of every `--adversarial` run. The
  documentation recommends `--adversarial-rounds 3`.
- Asking a person an escalated question during the run. `spec` runs
  unattended; the escalation is taken provisionally and handed on.
- A migration path for the old envelope vocabulary. There is no tagged
  release and no external consumer, so the fields are replaced, not carried
  twice.
- Changing the reviewer's severities, its 30-finding cap, its evidence rule,
  the failure handling of a round, or how `--total-budget` stops the rounds.
  Those carry over from spec 09 unchanged.
- Redefining spec 09's glossary entries for `disposition`, `finding` and
  `stop_reason`. The format keeps one definition per term across packages,
  so the new vocabulary is stated in this package's criteria and recorded in
  the ADR.
- The last scope of the input PRD, which is its own package, written by its
  own run:
  - **`adversarial_plan_review`** — `--adversarial-plan`, the in-memory plan
    validation, the grounding facts, the `zero_decision` and `grounding`
    rubrics, the plan reviewer and reviser, the plan review's failure
    handling, splits, and the two new phase names with their models.

## Background

### The review today

`--adversarial` reviews every PRD a run writes (spec 09, ADR 09,
`specgen/rounds.go`). `runRounds` drives the rounds: for each round it builds
a `reviewRequest`, calls `Review`, records the findings, and — when the
review raised anything — calls `Revise` with the findings and records the
dispositions. A round after the first is handed `earlier`, every finding of
the rounds before it.

The reviewer's contract is `specgen/review_tool.go`: `reviewSchema` takes a
list of at most `maxFindings` (30) findings, each with `severity` (one of
`critical`, `major`, `minor`), `section`, `flaw`, `evidence`, `remedy` and
`reopens`. `checkFinding` trims every field, reports an empty one, refuses a
severity outside the three, refuses a `reopens` in round 1 or one naming no
earlier finding, refuses a reopen whose evidence the rejection already
answered, and refuses a finding that repeats a rejected one by section and
flaw without naming it in `reopens`. The comparison is by
`sameEvidence`, which trims and collapses runs of whitespace. The run gives
each accepted finding the id `R<round>-F<n>`.

The reviser's contract is `specgen/revision_tool.go`: `revisionSchema` takes
the complete revised `body`, one `disposition` per finding (`accepted` or
`rejected`, with a `reason` for a rejection), and optional replacement
`open_questions`, `relevant_files` and `codebase_notes`.
`checkDispositions` refuses a finding answered twice or not at all, an
answer for an id that is not this round's, a disposition outside the two, and
a rejection with no reason. `checkPRDBody` and `samePRD` refuse a revision
that accepts a finding and leaves the PRD unchanged.

`runRounds` stops when a review raises nothing (`no_findings`), when a
revision finished and no finding was `critical` or `major`
(`no_serious_findings`), when the limit is reached (`rounds_exhausted`), or
when a phase did not finish or the total budget was spent
(`incomplete`). `buildReview` derives `findings_count`, `accepted_count`,
`rejected_count` and `unanswered_count` from the rounds, so the totals cannot
disagree with them.

The record lives in `envelope.Review` and its `ReviewRound` and
`ReviewFinding`, with the constants `SeverityCritical`/`Major`/`Minor`,
`DispositionAccepted`/`Rejected`/`Unanswered` and the four `ReviewStop`
values. `specgen/pipeline.go` aliases `Review`, `ReviewRound` and `Finding`
to them, `reviewClause` builds the summary clause from `RoundsRun`,
`FindingsCount` and `AcceptedCount`, and `SummaryView` in
`specgen/summary.go` keeps the review without its rounds under
`--detail summary`.

The prompts are four templates — `prd_review_system.md`,
`prd_review_user.md`, `prd_revision_system.md`, `prd_revision_user.md` — and
three blocks in `specgen/prompts.go`: `prdUnderReviewBlock`,
`findingsBlock` and `earlierRoundsBlock`, the last of which renders every
earlier finding through `writeFinding` and tells the reviewer and the reviser
different things about what has been settled.

### Where the open questions go

`envelope.OpenQuestion` is `{question, decision, why_unsure}`: the shape the
"self-resolve, flag low confidence" contract takes. The PRD phase fills them,
`revisionSchema` lets a revision replace them, and
`specgen/pipeline.go` copies `prd.OpenQuestions` onto the package it is
about to write. The run's summary counts them across every scope of a split.

### The steering in the review

`specgen/rounds.go` puts `project.Steering(e.specsDir)` into the review
request, and `steeringBlock` carries it into both the review and the revision
prompt. Nothing asks the reviewer to check the PRD against it: the directives
are there as context, and a PRD that contradicts one is reported by nothing.

### What lands beside this

Spec 16 (`steering_version`) changes `steeringBlock`'s signature, threads the
version through the same phase requests this package's prompts are built
from, and makes the steering *body* mean the text after the frontmatter. The
`steering` rubric's evidence is quoted against that body, so this package is
written on top of spec 16 rather than beside it.

## Requirements

### 1. The four dispositions

The reviser answers every finding with one of four dispositions:

- **`fixed`**: the PRD was changed to remedy it.
- **`contested`**: the finding does not hold. The answer carries
  `counter_evidence` that the reviewer could have read. That is either a
  quote from the PRD or the input that appears in it verbatim (whitespace
  aside), or a repository path that exists, with what it shows. A contest
  whose quote is not found and whose path does not exist is turned back to
  the reviser. A reason that only disagrees is turned back as well.
- **`escalated`**: the finding holds, but remedying it takes a decision the
  input, the steering and the code do not make, a trade-off a person should
  own.
- **`declined`**: the finding holds and is not remedied in this package. The
  answer gives a reason: the finding is outside the package's scope, or a
  later package carries it, named. A `critical` finding or a `steering`
  finding that is declined is turned back to the reviser.

The run sets **`unanswered`** on a finding whose revision did not finish. The
reviser cannot change a finding's severity or its rubric.

### 2. An escalation takes a provisional decision and hands the question on

- An escalated answer carries the `question`, two or three `options` each in
  one sentence, the `decision` taken, and the `reason` the reviser is unsure
  of it. An answer missing any of them is turned back.
- The decision is written into the package so that it stays complete: in the
  PRD it goes under `## Design Decisions`, opening with "Provisional:".
- Every escalation is also added to `result.open_questions`, as `question`,
  `decision` and `why_unsure`.

### 3. Repeats across rounds

- A later round's reviewer is handed every earlier finding with its
  disposition and answer.
- It may raise a contested finding again only through `reopens`, with
  evidence the contest did not answer.
- It may not raise a declined or an escalated finding again. A finding that
  repeats one, matched by section and flaw as today, is turned back to it.

### 4. When the rounds stop

- A round is **clean** when its review raises no `critical` or `major`
  finding.
- The rounds stop, with `stop_reason`:
  - `converged`: two consecutive rounds were clean;
  - `rounds_exhausted`: `--adversarial-rounds` rounds ran without two
    consecutive clean ones;
  - `incomplete`: a phase did not finish, or the budget was spent. This
    reason wins over the others.
- The minor findings of a clean round are still revised. A review with no
  findings is clean and has no revision.
- With the default of 1 round, the rounds always stop as `rounds_exhausted`
  or `incomplete`, at the cost of one round, as today. Converging takes at
  least 2 rounds.

### 5. The record

`envelope.Review`:

- each round carries `clean`;
- each finding carries `rubric`, and for the PRD review that is `prd` or
  `steering`;
- each finding carries the reviser's answer: `disposition`, plus `reason`,
  `counter_evidence`, `question`, `options` and `decision` where that
  disposition has them;
- the totals are `findings_count`, `fixed_count`, `contested_count`,
  `escalated_count`, `declined_count` and `unanswered_count`, and every one
  is derived from the rounds;
- `stop_reason` is one of `converged`, `rounds_exhausted` and `incomplete`.

`accepted`, `rejected`, `accepted_count`, `rejected_count`, `no_findings` and
`no_serious_findings` are removed. This is a breaking change to the envelope,
and an ADR records it. The summary clause becomes, for example,
`review: 2 rounds, 9 findings (1 steering), 7 fixed`.

### 6. Steering alignment in the PRD review

- The PRD reviewer checks what it reviews against every directive of the
  steering. A contradiction is a finding with the rubric `steering`.
- The evidence of a `steering` finding quotes the directive, which must
  appear verbatim (whitespace aside) in the steering body, and the text that
  contradicts it. A `steering` finding whose quote is not in the steering is
  turned back to the reviewer.
- A `steering` finding is `critical` or `major`. One submitted as `minor` is
  turned back.
- A `steering` finding is fixed, contested, or escalated. It is contested
  when the directive does not apply here, with counter-evidence. It is
  escalated when the input asks for what the directive forbids, so that a
  person either changes the input or amends the steering. It cannot be
  declined.
- With no steering, or without `--adversarial`, nothing checks alignment.

### 7. Documentation and build

- `docs/cli.md`: the dispositions and the stopping rule, the changed
  `result.review` fields, the changed summary clause, and the recommendation
  of `--adversarial-rounds 3`.
- `docs/api.md`: the changes to `envelope.Review`.
- `docs/architecture.md`: the triage and convergence rules in the round loop.
- An ADR recording the triage and convergence rules and the breaking change
  to the envelope, and that it replaces the vocabulary ADR 09 records.
- `make doc` for the changed exported identifiers.

## Design Decisions

1. **EGM's triage, with `escalated` taking a provisional decision.** `spec`
   runs unattended, so nobody is there to answer an escalation. Leaving the
   decision open would break the Zero-Decision Rule the review exists to
   enforce. The reviser takes one option, marks it provisional and hands the
   question to the caller in `open_questions`, the channel that already
   exists for decisions made under uncertainty.
2. **A contest must show counter-evidence the machine can find.** A quote
   that is not in the text, or a path that does not exist, is an assertion.
   Checking both by string match and file system call keeps a reviser from
   overruling a reviewer by insisting.
3. **Two consecutive clean rounds, and the default stays one round.** One
   clean review is a single sample. EGM's rule takes two. Raising the default
   would raise the cost of every `--adversarial` run, so the default stays 1,
   and the result says honestly that one round did not converge.
4. **The envelope breaks instead of carrying both vocabularies.** `accepted`
   maps onto `fixed`, but `rejected` splits into `contested` and `declined`,
   and nothing could fill either from the old value. The envelope has no
   tagged release and no external consumer, so this is the cheapest time to
   change it.
5. **A `steering` finding cannot be declined.** The steering holds what the
   project has decided for every package. A package that knowingly breaks a
   directive is the case this change exists to report. If a directive is
   wrong, the remedy is to amend the steering, and an escalation hands that
   decision to a person.
6. **An escalation's decision must appear verbatim in the revised body.**
   That is the same machine-checkable rule as a contest's counter-evidence,
   applied to the other direction: the reviser writes the decision, so it can
   copy it into `## Design Decisions`. Counting the entries of that section,
   or looking for the word "Provisional:", would pass a body that says
   something else.
7. **The run adds the escalations to the open questions after the rounds, not
   the reviser.** A reviser that forgot would drop the question, and a later
   round's revision may replace the open questions wholesale
   (`revisionSchema` allows it), which would drop an earlier round's. The run
   appends once, in round order, skipping a question already there.
8. **A clean round still has its revision, and a round with no findings has
   none.** A review with only minor findings has found something worth
   fixing; a review with nothing has nothing to answer, and paying for a
   revision phase to answer an empty list is waste. This is why a round with
   no findings counts as clean rather than as a stop of its own: two of them
   converge.
9. **`no_findings` and `no_serious_findings` are gone rather than kept as
   aliases.** Both said "the last review was clean", which under the new rule
   stops nothing on its own. Keeping them would make `stop_reason` mean two
   different things depending on which value it held.
10. **The reviser's schema carries no severity and no rubric field.** The
    cheapest way to stop a reviser changing either is to give it no place to
    put them; `checkFinding` already strips the fields the reviewer must not
    set.
11. **Spec 09's glossary entries for `disposition`, `finding` and
    `stop_reason` are not redefined here.** A term two packages define
    differently is a warning across specs, and the format keeps one
    definition per term. This package's criteria state the new vocabulary in
    full and the ADR records that it replaces ADR 09's, which is where a
    reader of spec 09 is sent.
12. **The documentation and the ADR are task steps, not criteria.** They are
    one-off changes to this repository rather than behaviour of the system
    under specification, and the steering forbids a test of a documentation
    file's content, so a criterion for them could be verified by no test this
    project allows.

## Dependencies

- **Spec 09, review the PRD adversarially** (active, all tasks done). This
  package replaces its dispositions, its stopping rule and its
  `envelope.Review`, and keeps its severities, its 30-finding cap, its
  evidence rules, its id scheme and its failure handling.
- **Spec 16, version the steering** (active, no task done). The `steering`
  rubric quotes the steering **body**, which spec 16 defines as the text
  after the frontmatter, and spec 16 changes the signature of
  `steeringBlock` and the steering plumbing of the same phase requests this
  package's prompts are built from.
- **ADR 02, choose the model per phase.** `prd_review` and `prd_revision`
  keep their models; this package adds no phase.
- **ADR 03, pipeline the scopes of a split.** Each scope's rounds run inside
  its own build, unchanged.
- **ADR 04, hand on what a phase learned.** The reviser may still replace
  `codebase_notes`, `open_questions` and `relevant_files`.
- **`docs/spec-kit-comparison.md`** §1.4, §4.9, §5 and §7.1 item 1: the
  recommendations this package carries out.

## Verified External API

- **`github.com/agent-fox-dev/agentkit-go`**, pinned in `go.mod`. The review
  and revision tools are built from `core.Tool`, `core.OKResult`,
  `core.ErrResult` and `core.ToolResult`, and their contracts from
  `schema.Object`, `schema.Prop`, `schema.Opt`, `schema.Array`,
  `schema.Enum`, `schema.String` and `(*schema.Schema).Describe` — every one
  of them already used in `specgen/review_tool.go` and
  `specgen/revision_tool.go` as this package will use it.
- **`github.com/agent-fox-dev/agentkit-go/workspace`**, same pin.
  `validateRelevantFiles` in `specgen/prd.go` resolves a path the model gave
  with `func (*workspace.Workspace) Resolve(string) (string, error)` and then
  calls `os.Stat` on the result, refusing a path that does not resolve inside
  the workspace. That is the pair of calls a contest's path is checked with,
  except that a contest accepts a directory as well as a regular file, so it
  takes the `os.Stat` error alone and not
  `validateRelevantFiles`' `IsRegular` test.
- No new dependency. The counter-evidence quote is a string match over text
  the run already holds, through the same whitespace normalisation
  `sameEvidence` in `specgen/review_tool.go` applies.
