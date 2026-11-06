---
spec_id: "18"
spec_name: "adversarial_plan_review"
title: "Review the plan adversarially before the package is written"
status: "active"
created_at: "2026-11-06T13:31:22Z"
updated_at: "2026-11-06T15:07:28Z"
intent_hash: "b1db94b720174adaf9eeb59cc214d7c6fee1339dd3bb235c1f9f284c703ac691"
schema_version: 2
source: "docs/prd/04-review-the-plan-commit-the-render-and-version-the-steering.md"
---

# Review the plan adversarially before the package is written

## Intent

Nobody but its author reads the plan. The adversarial review reads the PRD,
and the requirements, tests and tasks generated from it are checked only by
the validator. That proves the plan covers the PRD, but not that a coder can
carry it out without deciding the design, and not that the files and symbols
it names exist. The packages in this repository show what the validator
cannot see: in each one already implemented, paths in `touches` exist nowhere,
each of them a decision the plan left to the coder or one the coder overruled,
with nothing recording which was right.

This change adds a second adversarial stage over the plan, after the tasks
phase and before the package is written. A fresh reviewer applies two written
rubrics — the Zero-Decision Rule, that a coder who holds the package and the
code should have no design choice left to make, and codebase grounding, that
the plan must not name what is not there — plus the project's steering
directives. Whether a path exists is computed, not judged. A fresh reviser
answers every finding and submits the revised plan, which must still pass the
format's own rules. The stage has its own flag, so a run that does not ask for
it pays nothing.

## Goals

- With `--adversarial-plan`, the plan of every package the run generates is
  reviewed and revised after the tasks phase, and before the architecture
  phase and the write. The plan is `requirements.json`, `test_spec.json` and
  `tasks.json`.
- The reviewer applies two written rubrics, `zero_decision` and `grounding`,
  plus steering alignment, and every finding names the rubric it applies and
  the place in the plan it is about, by id.
- Whether each path in a task's `touches` exists is computed from the file
  system before the first round and handed to the reviewer as the grounding
  facts. The model judges only what a machine cannot: whether a step creates
  the path, and whether a symbol exists under the name the plan uses.
- The reviser may change the three plan artifacts and nothing else, and the
  revised plan must pass the schemas and C1 to C11 before it is accepted.
- An invalid plan is not reviewed: the run says so and fails on the errors the
  validator already found.
- A plan review or revision that does not finish costs the run the rest of the
  review and never the package, and never the run's exit code.
- `--adversarial` and `--adversarial-plan` are independent, and a run that
  gives neither runs exactly the phases it runs today.

## Non-goals

- Reviewing `architecture.md`. It is optional, has no schema, and is written
  after the plan review from the revised plan.
- A plan review in the `af-spec` skill, or a command that reviews a package
  already on disk. The plan review is a stage of `spec`.
- A third rubric for tests that prove nothing. It is a later change, once the
  first two rubrics show what a rubric finding costs.
- Changing the triage, the repeat rules, the convergence rule or the record's
  shape. Those are the `review_triage_convergence` package, and the plan
  review applies them as they are.
- Changing the PRD review: its rubrics, its prompts and its checks stay as
  that package leaves them.
- Checking a plan against the steering by machine. The directives are prose,
  and design rule 3 of the format keeps the validator from judging prose.
  Alignment is the reviewer's job.
- A gate that refuses to generate a package because of the steering. A
  contradiction is reported, as a finding, not refused.
- Raising the default round count, or turning the plan review on by default.
  It doubles the cost of a review, so it is asked for.
- Measuring whether the reviews produce better code.
- `docs/spec-kit-comparison.md` itself, which is an analysis dated 2026-10-28
  and stays as written.

## Background

### What the plans in this repository show

The packages under `.specs/` show what the validator cannot see. In each of
the packages already implemented, 09 to 13, some paths in `touches` exist
nowhere in the repository:

| Package | Paths in `touches` that do not exist | For example |
|---|---|---|
| 09 | 8 | `specgen/rounds_failure_test.go`, `specgen/smoke_integration_test.go` |
| 10 | 2 | `cmd/specr/locate.go`, which the implementation put in `internal/specpkg/locate.go` |
| 11 | 7 | `cmd/speca/task_flags_test.go`, implemented as `cmd/speca/taskflags_test.go` |
| 12 | 1 | `cmd/speca/smoke_test.go` |
| 13 | 5 | `cmd/specr/smoke_test.go` |

Each of these was a decision the plan left to the coder, or one the coder
overruled. The plan decided where the code goes, the code went somewhere
else, and nothing recorded which was right. Some of those paths are also test
files the steering now forbids, and nothing reported the contradiction.

### Where the stage goes

`specgen/pipeline.go` `buildPackage` builds one package: `afspec.CreateSpec`,
then one `GenerateArtifact` call per `afspec.GenerationSteps` filling an
`afspec.PartialSpec`, then the optional architecture phase, then
`summarize`, then the write, then the verdict. The plan review goes between
the last generation step and the architecture phase, so `architecture.md` is
written from the revised plan.

`afspec.ValidateGenerationStepComplete(afspec.StepTasks, partial)` is the
format's own answer for a plan held in memory: the schemas and every
cross-file rule of §10.2 that is decidable once the tasks exist — which after
the tasks step is C1 to C11 — with every problem in one pass rather than one
class per regenerated artifact. It is what the artifact repair loop already
uses, and it is what gates and re-checks the plan here.

### The review machinery this builds on

`specgen/rounds.go` `runRounds` drives the PRD's rounds, and the
`reviewer` interface in `specgen/phases.go` is its model-driven half —
`Review(ctx, reviewRequest)` and `Revise(ctx, revisionRequest)`, implemented
by `agentAuthor` through `reviewPhase`, which gives a review phase the
read-only file tools, the code-search index, the run context block and
`maxTokens`. The reviewer's contract is `specgen/review_tool.go`
(`reviewSchema`, `checkFinding`, `maxFindings` of 30, `sameEvidence`'s
whitespace normalisation, the `R<round>-F<n>` ids) and the reviser's is
`specgen/revision_tool.go` (`revisionSchema`, `checkDispositions`).

`envelope.Review` is the record, and `specgen/pipeline.go` aliases `Review`,
`ReviewRound` and `Finding` to its types. The `review_triage_convergence`
package replaces the dispositions with `fixed`, `contested`, `escalated` and
`declined`, adds the `rubric` field and the per-round `clean` flag, derives
six totals, and reduces the stop reasons to `converged`,
`rounds_exhausted` and `incomplete`. This package applies all of that to the
plan unchanged, so the only vocabulary it adds is the two new rubrics.

### The phases and their models

`internal/agentrun/models.go` holds `Phases` and `tierTable`. Each tier has a
`base` entry and a `phases` map that overrides it. `prd_review` and
`prd_revision` are in no tier's map, so they fall to `base`. The requirements
phase falls to `base` in `SIMPLE` and `STANDARD` (`claude-sonnet-5-5` at
`medium` and `high`) and is overridden in `ADVANCED`
(`claude-opus-5-5` at `high`, where `base` is `xhigh`).

`cmd/spec/main.go` registers `--adversarial` and `--adversarial-rounds` and
refuses the rounds flag without `--adversarial` in `PreCheck`, before any
model call. `specgen/pipeline.go` `RunPreflight` counts the phases —
`phases += 2 * o.AdversarialRounds` when `Adversarial` — and `phaseModels`
lists a phase's model, skipping the two review phases when `Adversarial` is
off. `specgen/split_run.go` runs each scope's PRD review in the split
coordinator, before the package's number is reserved.

### How the plan is rendered for a reader

`afspec`'s per-artifact renderers — `(*RequirementsV2Json).Render`,
`(*TestSpecV2Json).Render` and `(*TasksV2Json).Render` — are what both
`RenderIndividual` and the package's combined render are built from. The plan
under review is in memory, not on disk, so it is rendered from those three
directly, in that order: the same text a person reads in a pull request,
which costs fewer tokens than the JSON and leaves nothing out.

## Requirements

### 1. The flag

- `--adversarial-plan` is a boolean flag, off by default. When it is on, the
  plan of every package the run generates is reviewed and revised after the
  tasks phase, and before the architecture phase and the write.
- `--adversarial-rounds N` sets the most rounds for each review the run does,
  the PRD's and the plan's, counted separately. Giving it without
  `--adversarial` or `--adversarial-plan` is a usage error, raised before any
  model call. Its range stays 1 to 5, and its default stays 1.
- `--adversarial` and `--adversarial-plan` are independent. Either, both or
  neither may be given.
- With `--dry-run`, the plan review runs like every other phase, and nothing
  is written.
- With `--preflight`, `estimate.phases` counts a plan review and a plan
  revision for every allowed round of every package still to be written, as
  an upper bound. `phase_models` lists `plan_review` and `plan_revision`.

### 2. Before the first round

- The plan is validated in memory against the package's own rules: the
  schemas and C1 to C11. A plan that does not validate is not reviewed. The
  run carries a `high` warning, `plan_review_skipped`, naming the scope and
  the number of errors. It then writes and validates the package as it does
  today, and fails on the same errors.
- The run lists, for every path in every task's `touches`, whether that path
  exists in the repository as a file or a directory. Paths are resolved
  against the repository root, and the check uses the file system, not a
  model. This list is the **grounding facts**.

### 3. What the reviewer and the reviser are given

The reviewer runs in a fresh conversation and is given:

- the PRD as it stands after its own review, if there was one;
- the plan rendered as Markdown: requirements, test specification and tasks,
  in the form the package's own render gives them;
- the grounding facts;
- the PRD phase's `codebase_notes`, marked as the author's claims to check;
- the steering with its version, the repository map, the read-only file tools
  and the code-search index that every phase has.

It is not given the original input. The PRD is what the plan answers to.

The reviser runs in another fresh conversation and is given the PRD, the plan,
the findings, the grounding facts, the `codebase_notes`, and the same project
context and tools as the reviewer.

### 4. The rubrics

Every finding of the plan review names the rubric it applies, in a field
`rubric`: `zero_decision`, `grounding` or `steering`. The prompt carries each
rubric in full, as these questions.

**`zero_decision`.** The plan fails if a coder who holds the package and the
code would have to make a design choice that the plan does not make. Of every
task and the tests it owns, the reviewer asks:

- Where does the code go? The file, package or type is named in the steps or
  in `touches`, or there is only one place it can go.
- Is every name, signature, error, message, format and order that a test
  asserts, or that another task relies on, stated?
- Is a library, algorithm or data structure left open where the choice shows
  in a test or to a caller?
- Does a step leave the work open, with words like "as appropriate", "if
  needed", "decide" or "for example"? The finding is the open choice, not the
  word.
- Could two coders who both follow the task write code that one of the
  package's tests passes for one and fails for the other?

**`grounding`.** The plan fails if it names what is not there. The reviewer
asks:

- Does each path in `touches` exist? If not, does a step of the same task, or
  of a task it depends on directly or transitively, create it? A path that
  does not exist and that nothing creates is a finding. Its evidence is the
  grounding fact.
- Does each symbol that a step or a test's pseudocode names (a function,
  type, method, flag or constant) exist in the code, or does a task of this
  package create it?
- Does each claim the plan makes about the existing code hold?
- Does a test command in `test_commands` run each test, given the file the
  test will live in?

**Severity** for a plan finding:

- `critical`: the plan builds the wrong thing, or its tests contradict its
  criteria.
- `major`: a choice is left to the coder that a test or another task depends
  on; a path or symbol does not exist and nothing creates it; or a directive
  is contradicted.
- `minor`: a choice no test or task sees, or the wording.

### 5. The finding

A plan finding has the fields of a PRD finding: severity, section, flaw,
evidence, remedy and `reopens`. It also has `rubric`. For the plan, `section`
names the place by id: a task (`task 3`), a test (`TS-15-4`), a criterion
(`15-REQ-2.1`) or an execution path. A review raises at most 30 findings. A
finding with an empty field is turned back to the reviewer, who either
supports it or withdraws it, as in the PRD review.

The triage, the repeat rules, the convergence rule and the record's shape are
the PRD review's, applied to the plan unchanged.

### 6. The revision

- The reviser answers every finding and submits the revised
  `requirements.json`, `test_spec.json` and `tasks.json`.
- It may change those three files. It may not change:
  - `prd.md`;
  - the spec id or name in any artifact;
  - a task's `state`;
  - the actors of the execution paths, which the later scopes of a split have
    already been given.

  A revision that changes any of these is turned back, with each change
  named.
- The revised plan must pass the schemas and C1 to C11. A revision that
  breaks a rule is turned back, with each broken rule named, until it passes,
  or until the phase runs out of turns or budget.

### 7. When a round cannot finish

- When a plan review or revision ends without a result (budget, turns,
  timeout, or a model or API error), the rounds stop. The package is written
  from the last plan that is complete: the plan as generated if no revision
  finished, otherwise the last finished revision's.
- The run carries a `high` warning, `plan_review_incomplete`. It names the
  scope, the round, the phase that failed (`plan_review` or `plan_revision`)
  and why it failed. The findings of a review whose revision did not finish
  are reported as `unanswered`.
- A failed plan review or revision never by itself fails the run or changes
  its exit code.
- `--total-budget` is checked before each plan review and each plan revision.
  When the run is over it, the rounds stop as above, and the warning names
  the budget.

### 8. Splits

- Each scope's plan goes through its rounds after its own tasks phase. The
  next scope's PRD and requirements phases do not wait for those rounds. The
  check of the next scope's package across specs still waits for this package
  to be written, as it does today.
- A resumed split reviews the plans of the packages it writes in this run, and
  no others.

### 9. Output and models

- Each package in the result carries a `plan_review` when
  `--adversarial-plan` was on. It has the shape of `review`. Without the
  flag, it is absent.
- The summary sentence adds a clause, for example
  `plan review: 2 rounds, 5 findings, 4 fixed`.
- Progress on stderr reports each phase, for example
  `scope 2 of 3 (issuex_github): reviewing the plan (round 1 of 2)`.
- `plan_review` and `plan_revision` are phase names that `--phase-model`
  accepts. Each tier runs both at the requirements phase's model and effort:

  | Tier | `plan_review`, `plan_revision` |
  |---|---|
  | `SIMPLE` | `claude-sonnet-5-5` `medium` |
  | `STANDARD` | `claude-sonnet-5-5` `high` |
  | `ADVANCED` | `claude-opus-5-5` `high` |

- `--budget`, `--max-turns`, `--phase-timeout` and `--max-tokens` apply to
  each plan review and plan revision as a phase of its own.

### 10. Steering alignment in the plan review

- The plan reviewer checks the plan against every directive of the steering. A
  contradiction is a finding with the rubric `steering`.
- The evidence of a `steering` finding quotes the directive, which must appear
  verbatim (whitespace aside) in the steering body, and the text that
  contradicts it. A `steering` finding whose quote is not in the steering is
  turned back to the reviewer.
- A `steering` finding is `critical` or `major`. One submitted as `minor` is
  turned back.
- A `steering` finding of a plan review cannot be declined.
- With no steering, or without `--adversarial-plan`, nothing checks alignment.

### 11. Documentation and build

- `docs/cli.md`: `--adversarial-plan`, the new meaning of
  `--adversarial-rounds`, the two rubrics, `plan_review` in the result, the
  new phase names and their tier entries, the warnings `plan_review_skipped`
  and `plan_review_incomplete`, and the new summary clause.
- `docs/configuration.md`: the two new phase names among those
  `--phase-model` accepts.
- `docs/architecture.md`: the plan review in the pipeline, between the tasks
  phase and the architecture phase.
- An ADR for the plan review with its two rubrics and the computed grounding
  facts.
- `make doc` for the changed exported identifiers.

## Design Decisions

1. **The plan review is a second stage with its own flag, not part of
   `--adversarial`.** It doubles the cost of a review. A user of
   `--adversarial` today keeps the phases and the cost they chose. The two
   stages catch different flaws: a PRD that specifies the wrong thing, and a
   plan a coder cannot follow.
2. **The plan is reviewed after the tasks phase, as a whole.** A
   zero-decision flaw is usually a gap between a test and the task that owns
   it, so neither file can be judged without the other. Reviewing before the
   architecture phase lets `architecture.md` be written from the revised
   plan.
3. **The plan reviewer reads the plan as Markdown, as a person would.** The
   reviewer reads the same text a pull request reviewer reads, which costs
   fewer tokens than the JSON, and the render leaves nothing out.
4. **The grounding facts are computed, not judged.** Whether a path exists is
   a file system call, and it is the one part of grounding a machine can
   settle. The model judges what a machine cannot: whether a step creates the
   path, and whether a symbol exists under the name the plan uses.
5. **The reviser may change `requirements.json` too.** A decision the plan
   leaves open often belongs in a criterion's contract, such as which error
   is returned. It may not change the actors, because the later scopes of a
   split were handed them when this package's requirements phase ended.
6. **An invalid plan is not reviewed.** The run fails on it anyway, and a
   review of a plan that breaks C1 to C11 pays to find what the validator
   already found.
7. **The plan is validated with
   `afspec.ValidateGenerationStepComplete(afspec.StepTasks, partial)`**, the
   call the artifact repair loop already uses, both for the gate before the
   first round and for re-checking each revision. It is the format's own
   answer over a plan held in memory, and it reports every problem in one
   pass rather than one class per regenerated artifact.
8. **`plan_review` and `plan_revision` get an explicit entry in every tier,
   not the tier's base entry.** `prd_review` and `prd_revision` fall to
   `base`, which in `ADVANCED` is `claude-opus-5-5` at `xhigh` — not the
   `high` the PRD's table asks for. Listing both phases in each tier's map
   with that tier's requirements entry makes the table literal rather than a
   coincidence of where `base` happens to point.
9. **A plan finding's `section` is checked against the plan's own ids.** The
   run holds the plan, so a section naming no task, test, criterion or path
   of it is a finding nobody can act on; refusing it costs one string
   comparison and saves a reviser guessing.
10. **The plan reviewer and reviser are two more methods on the existing
    `reviewer` interface, not a new one.** `agentAuthor` already implements
    it against the same `Runner`, and `reviewPhase` already gives a review
    phase its tools, its run context and its token ceiling. A second
    interface would make every test double implement both.
11. **The plan review adds no vocabulary beyond the two rubrics.** The
    dispositions, the repeat rules, the convergence rule and the record's
    shape come from the `review_triage_convergence` package and are applied
    unchanged, so a caller reads one record shape for both stages.
12. **The documentation and the ADR are task steps, not criteria.** They are
    one-off changes to this repository rather than behaviour of the system
    under specification, and the steering forbids a test of a documentation
    file's content, so a criterion for them could be verified by no test this
    project allows.

## Dependencies

- **Spec 17, triage every review finding four ways** (active, no task done).
  The plan review applies its dispositions, its counter-evidence and decision
  checks, its repeat rules, its convergence rule and its record shape
  unchanged, and the `steering` rubric it introduces.
- **Spec 09, review the PRD adversarially** (active, all tasks done). The
  `reviewer` interface, `runRounds`, `reviewPhase`, the 30-finding cap, the
  evidence rules, the `R<round>-F<n>` ids and the failure handling are its
  machinery, reused for the plan.
- **Spec 16, version the steering** (active, no task done). The plan
  reviewer is given the steering body with its version, as spec 16 defines
  them.
- **ADR 02, choose the model per phase.** Two new phase names, with their
  entry in each tier.
- **ADR 03, pipeline the scopes of a split.** A scope's plan review runs
  inside its own package's build, and the actors are frozen for the scopes
  after it.
- **ADR 04, hand on what a phase learned.** The plan reviewer and reviser
  read `codebase_notes` as claims.
- **`docs/spec-kit-comparison.md`** §4.4, §4.8, §4.9 and §7.1 item 1: the
  recommendations this package carries out.

## Verified External API

- **`github.com/agent-fox-dev/spec/afspec`**, this repository's own library:
  `func ValidateGenerationStepComplete(step GenerationStep, partial PartialSpec) ValidationResult`,
  read in `afspec/generation.go`. After `StepTasks` it runs the schemas and
  C1 to C11 over the plan held in memory, the cross-file rules running even
  when a schema fails, so one pass reports every problem.
- **`github.com/agent-fox-dev/agentkit-go`** v0.9.0, pinned in `go.mod`. The
  two new phases are built from `core.Tool`, `core.OKResult`,
  `core.ErrResult` and the `schema` helpers, exactly as
  `specgen/review_tool.go` and `specgen/revision_tool.go` build the PRD
  review's, and run through the same `agentrun.Phase` and `Runner.Run`.
- **`github.com/agent-fox-dev/agentkit-go/workspace`**, same pin:
  `func (w *Workspace) Resolve(p string) (string, error)`, which normalises
  and contains a model-supplied path. It is how a path in `touches` is
  resolved against the repository root before `os.Stat` decides whether it
  exists, the same pair `validateRelevantFiles` in `specgen/prd.go` uses.
- No new dependency.
