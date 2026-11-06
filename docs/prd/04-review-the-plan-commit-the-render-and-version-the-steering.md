# Review the plan, commit the rendered package and version the steering

## Intent

Three gaps keep a package that validates from being a package anyone has
checked. Nobody but its author reads the plan: `--adversarial` reviews the
PRD, and the tests and tasks generated from it are checked only by the
validator. That proves the plan covers the PRD, but not that a coder can
carry it out without deciding the design, or that the files and symbols it
names exist. Nobody reads a package in a pull request either, because three
of its four files are JSON, and the Markdown that `specr` renders is never
committed. And nothing holds a package to the steering. `steering.md` is fed
to every phase, but it has no version, so no package records which directives
it was written under. When a plan contradicts a directive, no review or check
reports it.

This change adds a second adversarial stage over the plan, with two written
rubrics and the triage and convergence rules of EGM for both stages. Every
save writes the package's render beside its JSON as `spec.md`, and the
validator reports a render that no longer matches. The steering gets a
version and an amendment record. A package records the version it was
generated against, the verdict reports a steering that has moved on since,
and both reviews report a contradicted directive as a finding the run cannot
decline. These are items 1, 2 and 4 of `docs/spec-kit-comparison.md` §7.1.

## Goals

- With `--adversarial-plan`, the plan of every package the run generates is
  reviewed and revised before the package is written. The plan is
  `requirements.json`, `test_spec.json` and `tasks.json`. The reviewer
  applies two written rubrics, the Zero-Decision Rule and codebase grounding,
  plus steering alignment.
- Both reviews, the PRD's and the plan's, answer every finding as `fixed`,
  `contested`, `escalated` or `declined`, and stop once two consecutive rounds
  are clean, or when the round limit is reached.
- Every package that `afspec` writes carries a `spec.md`. Its bytes are a
  fixed header line followed by what `specr <package>` renders. A pull request
  that adds or changes a package can be reviewed by reading `spec.md` in the
  diff.
- `speca <package> --validate` reports a missing `spec.md` as a warning. A
  `spec.md` that differs from the render is an error for a draft or active
  package, and a warning for a sealed, superseded or archived one.
  `speca --render` brings `spec.md` up to date for one package or for every
  package, without changing any other file.
- `.specs/steering.md` carries a version and a dated record of its
  amendments. `spec` writes the version into each package's `prd.md`. For a
  draft or active package written under an earlier version, the verdict
  names the amendments made since.
- With either review on, a PRD or a plan that contradicts a steering
  directive gets a finding that quotes the directive. That finding cannot be
  declined.
- A run without `--adversarial-plan`, against an unversioned steering file,
  runs the same phases at the same cost as before. The one difference is
  that it also writes `spec.md`.

## Non-goals

- Reviewing `architecture.md`. It is optional, has no schema, and is
  written after the plan review from the revised plan.
- A plan review in the `af-spec` skill, or a command that reviews a package
  already on disk. The plan review is a stage of `spec`. The skill only
  renders `spec.md`.
- A third rubric for tests that prove nothing (§4.8 of the comparison). It
  is a later change, once the first two show what a rubric finding costs.
- Checking a package against the steering by machine. The directives are
  prose, and design rule 3 of the format keeps the validator from judging
  prose. Alignment is the reviewer's job. The machine checks only the
  version.
- A gate that refuses to generate or activate a package because of the
  steering (Spec Kit's Constitution Check as a gate). A contradiction is
  reported, as a finding or a warning, not refused.
- A Sync Impact Report: listing, after an amendment, every document and
  package to update. The verdict's warning lists the amendments a package
  missed. That is the whole of it.
- Changing the steering files of the other agent-fox repositories. This
  repository's copy is versioned. The others take the same text when they
  next copy it.
- Making `spec.md` canonical, or parsing it. The JSON and `prd.md` stay the
  package. `spec.md` is derived from them and is never read as data.
- `.gitattributes` entries that collapse or expand files in a diff.
- Re-rendering every `spec.md` automatically when the renderer changes.
  `speca --render` does it on request.
- Items 3, 5, 6 and 7 of §7.1. Item 3 is PRD 03 and spec 14.
- Measuring whether the reviews produce better code (§7.3 of the
  comparison).
- `docs/spec-kit-comparison.md` itself, which is an analysis dated
  2026-10-28 and stays as written.

## Background

### The review today

`--adversarial` reviews every PRD a run writes (PRD 01, ADR 09,
`specgen/rounds.go`). A fresh reviewer raises at most 30 findings, each with
a severity (`critical`, `major` or `minor`), a section, the flaw, its
evidence and a remedy. A fresh reviser answers each one as `accepted` or
`rejected`, and a rejection must give a reason. The rounds stop when a
review raises no `critical` or `major` finding (`no_serious_findings`) or
none at all (`no_findings`), or after `--adversarial-rounds` rounds (1 to 5,
default 1). A later round may raise a rejected finding again only through
`reopens`, with new evidence. The record lives in the result
(`envelope.Review`), never in the package.

After the PRD rounds, the requirements, test spec and tasks phases generate
the plan. The architecture phase, when asked, writes from that plan. The
package is then written, re-read from disk and validated
(`specgen/pipeline.go`, `buildPackage`). Nothing reviews the plan.

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
else, and nothing recorded which was right.

Some of those paths are also test files that the steering now forbids. On
2026-10-25 the steering gained *Tests and Spec Traceability*: "Do not create
per-spec test files, smoke, property, phase or group files". Packages 09 to
13 were written on 2026-10-21 and 2026-10-22, before that amendment, and
nothing in them says so. Package 14 was written on 2026-11-06, eleven days
after the amendment. Its integration task, task 11, still touches
`internal/skillgen/smoke_test.go`. Both `spec` and the `af-spec` skill read
the steering before they write a package, and nothing reported the
contradiction.

### Reading a package in a pull request

A package is `prd.md`, three JSON files and an optional `architecture.md`.
`specr <package>` renders all of them as one Markdown document
(`afspec.(*Spec).RenderCombined`). The render is deterministic, and leaves
out nothing but the `$schema` URIs and the artifacts' copies of the spec's
identity. Nobody commits the render, so a pull request that adds a package
shows a reviewer 60 to 126 KB of JSON. Measured on the packages in `.specs/`:

| Package | On disk | `specr` render |
|---|---|---|
| 09 | 151 KB | 127 KB |
| 10 | 128 KB | 107 KB |
| 13 | 77 KB | 65 KB |
| 14 | 101 KB | 88 KB |

A committed render therefore adds about 85 % to a package's size in the
repository.

Every write of a package goes through `afspec`. `(*Spec).Save` writes draft
and active packages, and refuses the others. `Transition` and `Supersede`
write in any state, through the same internal `saveToDisk`. That function
writes every artifact to a temporary file and then renames them all, so a
failed save changes none of them. `speca`'s task moves and status moves and
`spec`'s write all reach these functions. The `af-spec` skill writes the
files itself and validates them with `speca --validate`.

### The steering

`internal/project.Steering` reads `<spec root>/steering.md` whole. It treats
a symlink, an empty file, a file over 24 KB, the placeholder marker or a bare
`# Steering` heading as no steering. Every phase, the PRD review included,
gets the text in a `## Steering` block (`specgen/prompts.go`). The file has
no frontmatter and no version. Its history is in git alone: it was
standardised on 2026-10-15, gained *Generated Docs* the same day, and gained
*Tests and Spec Traceability* on 2026-10-25. The PRD frontmatter
(`prd-frontmatter.v2.json`) has no field that records the steering.

## Requirements

### 1. The plan review

#### The flag

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

#### Before the first round

- The plan is validated in memory against the package's own rules: the
  schemas and C1 to C11. A plan that does not validate is not reviewed. The
  run carries a `high` warning, `plan_review_skipped`, naming the scope and
  the number of errors. It then writes and validates the package as it does
  today, and fails on the same errors.
- The run lists, for every path in every task's `touches`, whether that path
  exists in the repository as a file or a directory. Paths are resolved
  against the repository root, and the check uses the file system, not a
  model. This list is the **grounding facts**.

#### What the reviewer is given

The reviewer runs in a fresh conversation and is given:

- the PRD as it stands after its own review, if there was one;
- the plan rendered as Markdown: requirements, test specification and tasks,
  in the form `spec.md` gives them;
- the grounding facts;
- the PRD phase's `codebase_notes`, marked as the author's claims to check;
- the steering with its version, the repository map, the read-only file
  tools and the code-search index that every phase has.

It is not given the original input. The PRD is what the plan answers to.

#### The rubrics

Every finding of the plan review names the rubric it applies, in a field
`rubric`: `zero_decision`, `grounding` or `steering` (see part 3). The
prompt carries each rubric in full, as these questions.

**`zero_decision`.** The plan fails if a coder who holds the package and the
code would have to make a design choice that the plan does not make. Of
every task and the tests it owns, the reviewer asks:

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

- Does each path in `touches` exist? If not, does a step of the same task,
  or of a task it depends on directly or transitively, create it? A path that
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
- `major`: a choice is left to the coder that a test or another task
  depends on; a path or symbol does not exist and nothing creates it; or a
  directive is contradicted.
- `minor`: a choice no test or task sees, or the wording.

#### The finding

A plan finding has the fields of a PRD finding: severity, section, flaw,
evidence, remedy and `reopens`. It also has `rubric`. For the plan, `section`
names the place by id: a task (`task 3`), a test (`TS-15-4`), a criterion
(`15-REQ-2.1`) or an execution path. A review raises at most 30 findings. A
finding with an empty field is turned back to the reviewer, who either
supports it or withdraws it, as in the PRD review.

#### The revision

- The reviser runs in another fresh conversation. It is given the PRD, the
  plan, the findings, the grounding facts, the `codebase_notes`, and the same
  project context and tools as the reviewer.
- It answers every finding as part 2 says, and submits the revised
  `requirements.json`, `test_spec.json` and `tasks.json`.
- It may change those three files. It may not change:
  - `prd.md`;
  - the spec id or name in any artifact;
  - a task's `state`;
  - the actors of the execution paths, which the later scopes of a split
    have already been given.

  A revision that changes any of these is turned back, with each change
  named.
- The revised plan must pass the schemas and C1 to C11. A revision that
  breaks a rule is turned back, with each broken rule named, until it
  passes, or until the phase runs out of turns or budget.

#### When a round cannot finish

- When a plan review or revision ends without a result (budget, turns,
  timeout, or a model or API error), the rounds stop. The package is written
  from the last plan that is complete: the plan as generated if no revision
  finished, otherwise the last finished revision's.
- The run carries a `high` warning, `plan_review_incomplete`. It names the
  scope, the round, the phase that failed (`plan_review` or
  `plan_revision`) and why it failed. The findings of a review whose
  revision did not finish are reported as `unanswered`.
- A failed plan review or revision never by itself fails the run or changes
  its exit code.
- `--total-budget` is checked before each plan review and each plan
  revision. When the run is over it, the rounds stop as above, and the
  warning names the budget.

#### Splits

- Each scope's plan goes through its rounds after its own tasks phase. The
  next scope's PRD and requirements phases do not wait for those rounds. The
  check of the next scope's package across specs still waits for this
  package to be written, as it does today.
- A resumed split reviews the plans of the packages it writes in this run,
  and no others.

#### Output and models

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

### 2. Triage and convergence, for both reviews

These rules replace the dispositions and the stopping rule of the PRD review,
and apply to the plan review too.

#### Dispositions

The reviser answers every finding with one of four dispositions:

- **`fixed`**: the PRD or the plan was changed to remedy it.
- **`contested`**: the finding does not hold. The answer carries
  `counter_evidence` that the reviewer could have read. That is either a
  quote from the PRD, the plan or the input that appears in it verbatim
  (whitespace aside), or a repository path that exists, with what it shows.
  A contest whose quote is not found and whose path does not exist is turned
  back to the reviser. A reason that only disagrees is turned back as well.
- **`escalated`**: the finding holds, but remedying it takes a decision that
  the input, the steering and the code do not make, a trade-off a person
  should own. The answer carries:
  - the `question`;
  - two or three `options`, each in one sentence;
  - the `decision` taken;
  - the `reason` the reviser is unsure of it.

  The decision is written into the package so that it stays complete. In the
  PRD it goes under `## Design Decisions`, opening with "Provisional:". In
  the plan it goes where the finding points. Every escalation is also added
  to `result.open_questions`, as `question`, `decision` and `why_unsure`.
- **`declined`**: the finding holds and is not remedied in this package. The
  answer gives a reason: the finding is outside the package's scope, or a
  later package carries it, named. A `critical` finding or a `steering`
  finding that is declined is turned back to the reviser.

The run sets **`unanswered`** on a finding whose revision did not finish.
The reviser cannot change a finding's severity or its rubric.

#### Repeats

- A later round's reviewer is handed every earlier finding with its
  disposition and answer.
- It may raise a contested finding again only through `reopens`, with
  evidence the contest did not answer.
- It may not raise a declined or an escalated finding again. A finding that
  repeats one, matched by section and flaw as today, is turned back to it.

#### When the rounds stop

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
- With the default of 1 round, the rounds always stop as
  `rounds_exhausted` or `incomplete`, at the cost of one round, as today.
  Converging takes at least 2 rounds.

#### The record

`envelope.Review`, for `review` and for `plan_review`:

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
`no_serious_findings` are removed. This is a breaking change to the
envelope, and an ADR records it. The summary clause of the PRD review becomes,
for example, `review: 2 rounds, 9 findings (1 steering), 7 fixed`.

### 3. The steering

#### Its version and its record

- `steering.md` may open with YAML frontmatter that holds two keys:

  ```yaml
  ---
  version: 2.1.0
  amendments:
    - version: 2.1.0
      date: 2026-11-06
      change: The file carries a version and this record.
    - version: 2.0.0
      date: 2026-10-25
      change: Tests are organised by behaviour; no per-spec, smoke, property, phase or group test files.
  ---
  ```

- A version is `MAJOR.MINOR.PATCH`: three non-negative integers with no
  leading zeros. Versions compare by number. Bump the MAJOR number when a
  directive is removed, or changed so that a package or code that followed
  it may now break it. Bump the MINOR number when a directive is added.
  Bump the PATCH number for any other change. The meaning is documented,
  and no machine checks it.
- Well-formed frontmatter:
  - has only the keys `version` and `amendments`;
  - has a `version` in that form;
  - has at least one amendment, newest first, each with a `version`, a
    `date` (`YYYY-MM-DD`) and a non-empty `change`;
  - has amendment versions that strictly decrease and dates that do not
    increase;
  - has a first amendment whose version is `version`.
- A steering file with no frontmatter is **unversioned**. It behaves as it
  does today, with no warning.
- A steering file whose frontmatter breaks a rule gets a `high` warning,
  `steering_malformed`, that names every broken rule. Both `spec` and
  `speca --validate` raise it. Its body is still used, and its version is
  treated as absent.
- The 24 KB bound, the placeholder and the bare-heading checks apply to the
  body after the frontmatter.
- Every phase is given the body without the frontmatter. When the file is
  versioned, the steering block names the version (`Steering version
  2.1.0`). The amendment record is not sent to the model.

#### The package's record

- The PRD frontmatter gains `steering_version`, an optional string in the
  version form. `spec` writes the steering's version into every package it
  writes when the steering is versioned, and leaves the field out when it is
  not. The field is added to `specification/schemas/prd-frontmatter.v2.json`
  and its copy in `afspec/schemas/`, to the Go types, and to
  `specification/spec-format-v2.md`.
- The verdict (`speca --validate`, and `spec`'s validation of what it wrote)
  gains a rule, `steering_version`. The rule applies to a draft or active
  package that has the field, when the spec root's steering is versioned:
  - Equal versions raise nothing.
  - A steering newer than the package raises a warning. It names both
    versions and every amendment after the package's version, newest first,
    each with its date and change. When one of them is a MAJOR bump, the
    warning says that a directive the package was written under has been
    removed or changed.
  - A package version newer than the steering, or one that is not in the
    amendment record, raises a warning that says so.
- The rule does not apply to a sealed, superseded or archived package, to a
  package without the field, or under an unversioned steering.
- A person who has checked a package against the amendments clears the
  warning by changing `steering_version` in `prd.md`. That is frontmatter,
  outside the hashed `## Intent`, so it can be changed after activation.

#### Steering alignment in both reviews

- The PRD reviewer and the plan reviewer each check what they review against
  every directive of the steering. A contradiction is a finding with the
  rubric `steering`.
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
- With no steering, or with neither review flag, nothing checks alignment.

#### This repository's steering

- `.specs/steering.md` gets frontmatter. Its record starts from the file's
  git history: an amendment for each of 2026-10-15 (standardised),
  2026-10-15 (*Generated Docs*) and 2026-10-25 (*Tests and Spec
  Traceability*), numbered by the rules above, then one for this change.
  The body does not change.
- The packages under `.specs/` do not get a `steering_version`. Nothing
  recorded which steering they were written under, and the field must not
  claim one.

### 4. The rendered package, `spec.md`

#### Writing it

- Every write of a package through `afspec` also writes `spec.md` in the
  package directory, in the same all-or-nothing write as the other files.
  That covers `Save`, `Transition` and `Supersede`, and through them every
  `spec` run, every `speca` task move and every `speca` status move.
- `spec.md` is one header line, a blank line, and then exactly what
  `specr <package>` returns in `result.markdown`: the whole package, with no
  scope and no budget. The header line is:

  ```
  <!-- Generated by afspec from prd.md, requirements.json, test_spec.json, tasks.json and architecture.md. Do not edit: change those files, or run speca <package> --render. -->
  ```

- The same package always renders to the same bytes. `spec.md` holds nothing
  that is not in the package. In particular, it holds no time of rendering.
- `afspec` exposes the file name and the expected bytes for a package, so
  that a program which writes the files itself can write `spec.md` too.
- `LoadSpec` does not read `spec.md` as data. The package is what it was.

#### Checking it

The verdict gains a rule, `rendered`. It applies to a package read from
disk, and not to a package held only in memory, such as one in a dry run.

| `spec.md` | Draft or active | Sealed, superseded or archived |
|---|---|---|
| absent | warning: `spec.md is missing; run speca <package> --render` | the same warning |
| differs from the render | error | warning |
| equal to the render | nothing | nothing |

The message for a difference names the first line that differs, and
`speca <package> --render`. An error counts like any other: a draft package
whose `spec.md` is stale cannot be activated until it is rendered again.

#### `speca --render`

- `speca <package> --render` writes `spec.md` for one package, whatever its
  status. It changes no other file, and does not touch `updated_at`.
- `speca --render` with no package renders every package under the spec
  root. With `--all`, it also renders those under `archive/`.
- `--dry-run` reports what would change and writes nothing.
- The result lists each package with `rendered`: `written`, `unchanged` or
  `failed`. A failed package also carries the reason, for example that it
  cannot be loaded. A package that fails does not stop the others from
  being rendered. The exit codes are `speca`'s own.
- `--render` is a form of its own. Combining it with `--validate`, `--list`,
  `--task` or `--status` is a usage error.

#### Its users

- The `af-spec` skill runs `speca <package> --render` before
  `speca <package> --validate`. The change is made in the skill source
  layout of spec 14.
- The packages under `.specs/` are rendered in this change with
  `speca --render`, and their `spec.md` files are committed.

### 5. Documentation and build

- `docs/cli.md`: `--adversarial-plan`, the new meaning of
  `--adversarial-rounds`, the dispositions and the stopping rule for both
  reviews, `plan_review`, the new phase names, the warnings
  `plan_review_skipped`, `plan_review_incomplete` and `steering_malformed`,
  and the changed summary clauses.
- `docs/speca.md`: the `--render` form, and the rules `rendered` and
  `steering_version` in the verdict.
- `docs/specr.md`: `spec.md` is `specr`'s render, committed.
- `docs/api.md`: the changes to `envelope.Review`, the new `plan_review`,
  and the `afspec` functions for `spec.md`.
- `docs/configuration.md`: the steering frontmatter, its rules and the
  meaning of each version number.
- `docs/architecture.md`: the plan review in the pipeline, and `spec.md` in
  the write.
- `specification/spec-format-v2.md`: `spec.md` as a generated file of the
  package that is not canonical, the rule `rendered`, the field
  `steering_version` and the rule `steering_version`.
- `README.md`: `spec.md` in the package table.
- ADRs: one for the plan review with the triage and convergence rules,
  including the breaking change to the envelope; one for committing
  `spec.md`; one for versioning the steering.
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
   reviewer reads the same text a pull request reviewer reads in `spec.md`,
   which costs fewer tokens than the JSON, and the render leaves nothing
   out.
4. **The grounding facts are computed, not judged.** Whether a path exists
   is a file system call, and it is the one part of grounding a machine can
   settle. The model judges what a machine cannot: whether a step creates
   the path, and whether a symbol exists under the name the plan uses.
5. **The reviser may change `requirements.json` too.** A decision the plan
   leaves open often belongs in a criterion's contract, such as which error
   is returned. It may not change the actors, because the later scopes of a
   split were handed them when this package's requirements phase ended.
6. **An invalid plan is not reviewed.** The run fails on it anyway, and a
   review of a plan that breaks C1 to C11 pays to find what the validator
   already found.
7. **EGM's triage, with `escalated` taking a provisional decision.** `spec`
   runs unattended, so nobody is there to answer an escalation. Leaving the
   decision open would break the Zero-Decision Rule the review exists to
   enforce. The reviser takes one option, marks it provisional (the
   convention of PRD 03) and hands the question to the caller in
   `open_questions`, the channel that already exists for decisions made
   under uncertainty.
8. **A contest must show counter-evidence the machine can find.** A quote
   that is not in the text, or a path that does not exist, is an assertion.
   Checking both by string match and file system call keeps a reviser from
   overruling a reviewer by insisting.
9. **Two consecutive clean rounds, and the default stays one round.** One
   clean review is a single sample. EGM's rule takes two. Raising the default
   would raise the cost of every `--adversarial` run, so the default stays 1,
   and the result says honestly that one round did not converge. The
   documentation recommends `--adversarial-rounds 3`.
10. **The envelope breaks instead of carrying both vocabularies.** `accepted`
    maps onto `fixed`, but `rejected` splits into `contested` and
    `declined`, and nothing could fill either from the old value. The
    envelope has no tagged release and no external consumer (§4.12 of the
    comparison), so this is the cheapest time to change it.
11. **`spec.md` is written by every save, in the same atomic write.** A
    render written by a separate step is a render someone forgets. It
    changes with every task move, which is right: the diff then shows the
    state a reviewer is approving.
12. **A stale `spec.md` is an error only while the package can be saved.**
    A draft or active package is fixed by one `speca --render`. A sealed
    package is history, and a renderer change must not make history
    invalid. Its stale render is still reported, as a warning.
13. **`speca` renders, not `specr`.** `specr` reads and never writes into a
    package. `speca` is the command that changes a package (ADR 10). The new
    form writes one derived file in any status, because `spec.md` is not
    part of the contract and touches no intent hash.
14. **`spec.md` holds the full render, the PRD included.** A reviewer reads
    one file. A render without the PRD would be cheaper, but it would send
    the reviewer back to `prd.md` for the reason behind every requirement.
15. **The steering version lives in frontmatter, the record as YAML.** The
    PRD's frontmatter is already parsed with `goccy/go-yaml`. A YAML list
    is checked without parsing Markdown headings, which is the brittleness
    the format chose JSON to avoid (§7.2 of the comparison).
16. **The amendment record is not sent to the model.** It grows with every
    amendment, and a phase needs the directives, not their history. The
    verdict's warning is where the history is read.
17. **A stale `steering_version` is a warning, and the person clears it.**
    Whether a package still holds under a new directive is a judgment. The
    warning says which amendments to check against, and changing the field
    records that someone checked.
18. **A `steering` finding cannot be declined.** The steering holds what the
    project has decided for every package. A package that knowingly breaks a
    directive is the case this change exists to report. If a directive is
    wrong, the remedy is to amend the steering, and an escalation hands that
    decision to a person.
19. **The packages in `.specs/` get no `steering_version`.** Recording a
    version they were not written under would make the warning lie about
    them.

## Dependencies

- **PRD 01 and ADR 09, the adversarial PRD review.** Its dispositions, its
  stopping rule and its `envelope.Review` change, as part 2 says. Its
  evidence rules, its 30-finding cap and its failure handling carry over to
  the plan review.
- **ADR 02, choose the model per phase.** Two new phase names, with their
  defaults in each tier.
- **ADR 03, pipeline the scopes of a split.** A scope's plan review runs
  inside its own package's build, and the actors are frozen for the scopes
  after it.
- **ADR 04, hand on what a phase learned.** The plan reviewer and reviser
  read `codebase_notes` as claims.
- **ADR 05 (`specr`) and ADR 10 (`speca`).** `spec.md` is `specr`'s
  render, and `speca` gains the `--render` form.
- **ADR 11, validate a package against the spec root.** The rule
  `steering_version` reads the spec root's steering, beside the rules across
  specs.
- **Spec 14, render the skills from one source** (active). The `af-spec`
  change is made in its layout once it lands.
- **`docs/spec-kit-comparison.md`** §4.4, §4.8, §4.9 and §7.1, items 1, 2
  and 4: the recommendations this PRD carries out.

## Verified External API

- **`github.com/goccy/go-yaml` v1.19.2**, already in `go.mod`, parses the
  PRD frontmatter in `afspec/prd.go`, and will parse the steering
  frontmatter: `func Unmarshal(data []byte, v interface{}) error`.
- No new dependency. Versions are compared as three integers, and
  `MAJOR.MINOR.PATCH` with no pre-release needs no semver library.
- **EGM** (<https://github.com/google/elephant-goldfish-model>): the triage
  vocabulary, the Zero-Decision Rule and the two-clean-rounds convergence are
  taken from the reading recorded in `docs/spec-kit-comparison.md` §1.4 and
  §5, of the repository cloned on 2026-10-28. They were not read again for
  this PRD.
