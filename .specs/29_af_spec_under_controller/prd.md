---
spec_id: "29"
spec_name: "af_spec_under_controller"
title: "Run af-spec under a controller"
status: "active"
created_at: "2026-11-28T09:55:10Z"
intent_hash: "2cc5ac16b05a76999a3960c73d9e2281b459bf99a5c9ce5382cab62d5336eba3"
contract_hash:
  requirements.json: "2719fa8c754dd149905149ea4bd485e854223dc85391d1327d506319c85fbaf3"
  tasks.json: "76405b9fbf3109fd5b46d3fb12ca4ea7223e51450595c9da815c6f755f183d7b"
schema_version: 3
profile: "lite"
source: "docs/prd/09-run-af-spec-and-af-impl-under-a-controller.md"
---

# Run af-spec under a controller

## Intent

`af-spec` assumes a person in the session: it stops to ask, picks its own
package number and activates what it wrote. A controller, such as
nightshift, starts the skill as a headless session on a work branch, has
nobody in the session to answer a question, gives each planner its package
number, and sends a plan back when a reviewer or a person objects. `af-spec`
gets the modes such a caller needs, and one way to tell it what happened: a
result file whose shape is published as a JSON Schema.

## Goals

- **`af-spec` runs with nobody to ask.** With `--unattended` it asks no
  question, decides every open point itself, records each decision, and
  ends with a package written or a refusal, never waiting for input.
- **The caller chooses the package number.** `af-spec --number <NN>` writes
  the package under that number, or refuses when the number is not free.
- **A planned package can stay a draft.** `af-spec --no-activate` leaves a
  package that validates in `draft`, so that the caller activates it once
  a person approved it.
- **A defect gets a package of its size.** `af-spec --defect` writes a
  `lite` package of one requirement for it.
- **Work too large for one package does not stop the run.** Unattended,
  `af-spec` writes the first scope and reports the others.
- **A draft can be revised from findings.** `af-spec <draft package>
  --findings <file>` revises the package and answers each finding.
- **Every unattended run leaves a result file.** With `--result <file>` the
  run writes one JSON document that says how it ended, what it produced and
  what it left open. A caller reads that file and nothing else of the
  session.
- **The result and the findings file have published schemas.**
  `specification/schemas/af-spec-result.v1.json` accepts every result an
  unattended run writes and refuses a document that breaks one of the rules
  below; `specification/schemas/af-spec-findings.v1.json` does the same for
  a findings file.

## Non-goals

- **`af-impl --no-merge`.** Implementing a package on the checked-out
  branch and stopping at the push is the package `30_af_impl_no_merge`.
- **`af-impl --rework`.** Fixing findings on finished work is the package
  `31_af_impl_rework`.
- **The controller.** What starts these sessions, where they run, what it
  does with a result and how the work lands is the controller's:
  `agent-fox-dev/nightshift`, `docs/prd/02-drive-work-items-through-the-hub.md`.
- **Changes to the package format, `afspec`, `speca`, `specr` or
  `specrun`.** The modes use the commands as they are.
- **A command that validates a result file.** The schemas are published; a
  caller validates with a JSON Schema validator of its own.
- **An unattended `af-prd`.** A controller hands `af-spec` the text it has;
  no separate PRD file is written for it.
- **Proving a defect while planning.** `af-spec --defect` writes no test
  and runs none. The failing run is recorded when the package is
  implemented, by the evidence rule of `28_close_task_on_evidence`.
- **Writing several packages in one `af-spec` run.** One run, one package.

## Background

**`af-spec`** (`skills/af-spec/SKILL.md`) writes a package in the session.
It stops for the person's answers when it reads its input (§1), when the
work needs a split (§4) and for an escalated finding of its review. It
takes the next free number itself (§3: the highest prefix in the spec root
and under `archive/`, plus one). It activates a package that validates
(§10), and leaves it a draft only when the person asks. Its input may be a
draft package to finish or repair. It commits as the repository's
instructions say. Its body is at the ceiling `make skills-check` enforces
(24,576 bytes, ADR 12), so what a mode adds goes into a reference file.

**What tells a caller how a run went.** Nothing: `af-spec` reports in the
session, in prose. The reconcile mode of `af-impl` writes a Markdown report
to a path given with `--report`. There is no machine-readable result.

**Activation freezes a package.** `speca <package> --status active` hashes
the `## Intent` section and, since `28_close_task_on_evidence`,
`requirements.json` and `tasks.json`; while one differs from its hash every
task write is refused. A package that a person may still send back for
revision therefore has to stay a draft until they approved it.

**Published schemas.** `specification/schemas/` holds the format's schemas
and those of `specrun`'s ledger and `run.json` (`specrun-ledger.v1.json`,
`specrun-run.v1.json`), JSON Schema draft 2020-12, each with an `$id` under
`https://agent-fox.dev/schemas/`, a description per member and
`additionalProperties: false`. `internal/specrun/ledger_test.go` validates
what `specrun` writes against them with
`github.com/santhosh-tekuri/jsonschema/v6`. Nothing under
`specification/schemas/` is a Go package today.

**Skill text is not tested.** `.specs/steering.md` forbids tests of a
document's content, and ADR 12 keeps the skills' checks in `make
skills-check`. A package that changes a skill therefore carries the skill's
text in its task steps, as `27_unattended_mode` does, and its criteria state
what a program can check.

**The controller this is written for.** nightshift starts a planner session
(`af-spec`) with `claude -p` in a clone of a task fork on a Parley hub. The
session's credential can fetch and push in the fork and nothing else. The
hub records every push of the branch as a version of a change. nightshift
gives each planner its package number, because several planners run at
once.

## Requirements

Constraint: Linux and macOS; Windows is best effort. Constraint: `af-spec`
run without any argument this package adds behaves exactly as it does
today. Constraint: in an unattended run, the mode's rules on branches,
merging and pushing take the place of the repository's own agent
instructions (`CLAUDE.md`, `AGENTS.md`) on those points; every other
instruction of the repository still holds.

### The result file

`--result <file>` is accepted by `af-spec` with `--unattended`. Given
without it, the skill says `--result needs --unattended, --no-merge or
--rework` and does nothing.

The file is one JSON document, written once, as the last thing the run
does, outside the repository's working tree when the path is outside it.
Every document has:

- `skill`: `af-spec`; `mode`: `unattended`;
- `outcome`: one of `written`, `invalid`, `push_failed`, `refused_input`,
  `refused_dirty`, `refused_already_specified`, `refused_spec_root`,
  `refused_number`, `refused_findings`;
- `reason`: one sentence, present when the outcome is a refusal or
  `push_failed`;
- `package`: `spec_dir`, `spec_id`, `spec_name`, `title`, `status`,
  `profile`, absent when the run refused before it knew the package;
- `branch` and `head`: the branch the run worked on and its last commit,
  and `pushed`: whether that commit is on `origin`;
- for a run that wrote a package (`written`, `invalid`, `push_failed`):
  `decisions`, `escalated` and `remaining_scopes`, each a list that may be
  empty;
- the members the sections below name.

A run that refuses before it changes anything still writes the file, with
the refusal as `outcome` and `reason`. A path that cannot be written is
said in the session's last message (`result file not written: <error>`);
the run's work is left as it is. A caller treats a session that left no
file, or one that does not parse, as failed.

Without `--result` the mode behaves the same and reports in the session
only.

### af-spec: unattended

`af-spec <input> --unattended` runs from its input to its report without
asking anything. The input is a path to a text file (the idea, in prose),
a path to a PRD, or a draft package.

- Where §1 would ask whether to write the PRD here or with `af-prd`, the
  run writes the package's `prd.md` itself, to the shared PRD shape. No
  file under `docs/prd/` is written.
- Every question the input leaves open is decided from the code and the
  repository's documents, and recorded in the package's
  `## Design Decisions`, numbered, with its reason. The result lists them
  as `decisions`, each with its `number`, the `decision` and its `reason`.
- A finding its review would escalate to the person is decided the same
  way, and listed in the result as `escalated`, each with the `finding`
  and the `decision`.
- The run works on the branch that is checked out. It creates no branch
  and switches to none. It commits the package as one commit and pushes
  that branch to `origin`. A push that is refused leaves the commit in
  place; the outcome is `push_failed`, with git's message as `reason`.

It refuses, changing nothing, with these outcomes:

- `refused_input`: the input path does not exist, is empty, or is an
  `active` or `sealed` package;
- `refused_dirty`: the working tree has uncommitted changes;
- `refused_already_specified`: the input's intent is that of an existing
  `active` or `sealed` package; `reason` names the package;
- `refused_spec_root`: `speca` cannot read the spec root, or it is not
  writable.

Otherwise the outcome is `written` (the package validates) or `invalid`
(it does not, after the repair loop the skill already runs). An `invalid`
package is committed and pushed as a draft all the same; the result
carries `failing_rules` as `speca --validate` reports them.

### af-spec: the number, the draft and the defect

`--number <NN>` makes the run use that number in place of the one §3 would
take. `<NN>` is a positive integer, written with or without leading zeros;
the directory uses it zero-padded to two digits, three past 99. The run
refuses, changing nothing, when the value is not a positive integer
(`refused_number`, `not a package number: "<value>"`), or when it is not
greater than every number in the spec root and under `archive/`
(`refused_number`, `<NN> is not above the highest package number <MM>`).
With a draft package as input, `--number` is refused when it differs from
the package's own (`refused_number`, `package is <MM>, not <NN>`).

`--no-activate` leaves a package that validates in `draft`. The report and
the result name the call that activates it,
`speca <NN> --status active`; in the result it is the member
`activate_with`.

`--defect` says that the input describes a fault in existing behaviour.
The run then writes a `lite` package with exactly one requirement: the
behaviour as it should be, with one criterion per observable part of it
(at most 8), each check naming the test that will assert it and the
existing test file it belongs in. Its tasks are at most 2. Its `prd.md`
says, under `## Background`, what the code does today and where
(`file:line`), as far as the run found it by reading; when it could not
find the fault in the code, it says so there and in the result
(`fault_located: false`), and writes the package from the description
alone. The run writes no test and changes no source file. `--defect` with
a draft package as input is refused (`refused_input`, `--defect takes a
description, not a package`).

All three combine with `--unattended` and with each other. `--number` and
`--no-activate` also work in a watched run, where a refusal is said in the
session.

### af-spec: work too large, and a revision

**Too large.** Where §4 would propose a split and ask which scope to
write, an unattended run writes the first scope, the foundational one, as
the package. The other scopes are named in the package's `## Non-goals`,
as today, and listed in the result as `remaining_scopes`, in the order
they are to be implemented, each with `name` (the `snake_case` name its
package would carry), `summary` (one sentence of what it covers) and
`text` (the part of the input it comes from, so that a later run can be
given it as its input). `--defect` never splits: a defect whose fix does
not fit one requirement is written as its first part, and the rest is
listed the same way.

**A revision.** `af-spec <draft package> --unattended --findings <file>`
revises a draft. The file is a JSON array of findings, each with `id` (a
string unique in the file), `source` (`review` or `person`) and `text`,
and optionally `criterion` or `task` (the id it concerns). For each
finding the run either changes the package or does not, and writes a
reply; a finding of source `person` is always followed, unless following
it would make the package invalid, which the reply then says. The result
lists `replies`, one per finding in the file's order, each with the
finding's `id`, `action` (`changed` or `unchanged`) and `text` (what was
changed, or why nothing was). The revised package is validated, committed
as one commit on the checked-out branch and pushed, with the outcomes of
*af-spec: unattended*.

The run refuses, changing nothing: a findings file that does not exist or
does not parse (`refused_findings`, with the parser's message), an empty
array (`refused_findings`, `no findings`), and a package that is not a
draft (`refused_input`, `<NN> is <status>; only a draft is revised`).

### End-to-end flows

**A plan written with nobody watching.** A controller checks out a new
branch in a clone, writes the idea to a file and starts
`af-spec idea.txt --unattended --number 62 --no-activate --result
plan.json`. The run reads the repository, writes `.specs/62_<name>/` with
its three artefacts, validates it with `speca`, commits it, pushes the
branch and writes `plan.json` with `outcome: written`, the package, the
decisions it made and no remaining scope. The package is a draft.

**A plan sent back.** A reviewer objects to two criteria. The controller
writes both to `findings.json` and starts `af-spec .specs/62_<name>
--unattended --findings findings.json --no-activate --result plan.json`.
The run changes one criterion, leaves the other with a reason, validates,
commits, pushes, and the result's `replies` say which was which.

## Design Decisions

1. **A planned package stays a draft until a person approved it; the
   caller activates it.** The owner decided it: activation freezes the
   contract, and a plan may be sent back.
2. **A defect is proven when it is implemented, not when it is planned.**
   The owner decided it: `af-spec` writes no code, and an active package
   already records each test failing first.
3. **Unattended, work too large for one package is written as its first
   scope, and the rest is reported.** The owner decided it.
4. **The result is a file, at a path the caller gives.** A session's last
   message is prose a caller would have to parse, and `--report <file>` of
   the reconcile mode is the precedent.
5. **A refusal writes the result too.** A caller should not have to tell a
   refusal from a crash by the absence of a file.
6. **`--number` must be above every existing number.** Packages are
   implemented in number order (ADR 29), and an archived number is never
   reused.
7. **The unattended `af-spec` works on the checked-out branch and pushes
   it.** The controller made the branch for this work; a skill that
   created another would leave the controller's branch empty.
8. **`--no-activate` and `--number` are arguments of their own, not part
   of `--unattended`.** A person in a watched run has the same two needs,
   and `spec --no-activate` already exists.
9. **The mode's rules on branches and pushing outrank the repository's
   agent instructions.** Those instructions commonly say "merge into main
   and push it", which is exactly what the caller asked the skill not to
   do.
10. **The result file and the findings file get published JSON Schemas,
    and the package's criteria are about those schemas.** The owner decided
    it in the session that wrote this package: skill text cannot be tested
    (steering, ADR 12), and an active package closes a task only on a test
    per criterion. The schemas are what a program can check, and what a
    controller validates a result against.
11. **One result schema per skill.** This package publishes
    `af-spec-result.v1.json`; `30_af_impl_no_merge` publishes
    `af-impl-result.v1.json`. The two skills share the common members and
    nothing else, and a schema of its own lets each package change its file
    without touching the other's tests.
12. **The skill's behaviour is carried in the task steps, and in
    `skills/af-spec/references/unattended.md`.** As in
    `27_unattended_mode`: the body of `SKILL.md` is at its ceiling, and a
    reference file loads only in the runs that use the mode.
13. **Profile: `lite`.** Three requirements, each about what a schema
    accepts and refuses; the end-to-end flows pass through a model session,
    which no smoke check can run.
14. **The schema tests live beside the schemas**, in a test-only Go package
    under `specification/schemas/`: nothing in Go writes a result file, so
    there is no production package whose tests they would join. Each test
    validates documents written in the test against the published file.
15. **Member names the PRD left open**: the activating call is
    `activate_with`; a decision is `{number, decision, reason}`; an
    escalated finding is `{finding, decision}`; `failing_rules` is the list
    of rule names `speca` reports (`C5`, `json_schema`).
16. **`reason` is required for every `refused_*` outcome and for
    `push_failed`, and `invalid` carries `failing_rules` in its place.**
    The PRD says "a refusal or a failure"; the failing rules are the
    reason an invalid package is invalid.
17. **A push that fails after an invalid package was committed is
    `push_failed`, and carries `failing_rules` too.** The caller needs
    both facts, and the outcome names the one that stops the hand-over.
18. **`written` and `invalid` mean the commit is on `origin`**
    (`pushed: true`); `push_failed` means it is not. A document that says
    otherwise is refused by the schema.
19. **A `written` result whose package is a draft names `activate_with`;
    one whose package is active must not.** A valid package is left a draft
    only by `--no-activate`.
20. **The schemas refuse a member they do not name**, as the `specrun`
    schemas do: a misspelled member is then an error, not a silent gap.
21. **A findings file is refused when it is not an array of findings of
    the published shape, or repeats an `id`** (`refused_findings`, saying
    what is wrong). "Does not parse" covers the shape; the schema cannot
    state that ids are unique, so the skill checks it.
22. **A finding names at most one of `criterion` and `task`.** The PRD says
    "or"; a finding about both is two findings.
23. **The reference documentation of the result file is
    `docs/controller.md`** (new): the arguments, the outcomes and the two
    schemas. `docs/architecture.md` lists the two schema files.
24. **The package's commit is `docs: specify <NN_name>`**, and a revision's
    is `docs: revise <NN_name>`: a package is a document, and the
    repository asks for conventional commits.
25. **The frontmatter's `source` is the input's path as given**, for an
    idea file too.
26. **The package a refusal names may hold any lifecycle status.** A run
    refused because its input is a sealed package still says which package
    that was; only a run that wrote a package is held to `draft` or
    `active`.

## Dependencies

- **`14_render_skills_from_one_source`** (archived): the skills' source
  under `skills/` and its render per agent, which every new argument goes
  through.
- **`28_close_task_on_evidence`** (`active`): the contract hash that makes
  a draft necessary before approval, and the evidence that proves a defect
  at implementation.
- **ADR 29**, `number-packages-in-implementation-order`: the rule
  `--number` keeps.
- **nightshift** (`agent-fox-dev/nightshift`,
  `docs/prd/02-drive-work-items-through-the-hub.md`): the first caller of
  these modes. It depends on this package; nothing here depends on it.

## Verified External API

- **`claude -p`**, read in `docs/specrun.md`: `claude -p "/af-impl
  <spec_dir> --unattended" --output-format stream-json --verbose`, the
  call `specrun` makes and a controller makes with `/af-spec` and the new
  arguments.
- **`speca`**, read in `docs/speca.md` and the skills:
  `speca <package> --status active`, `speca <package> --validate`,
  `speca --list --all`. `failing_rules` is present only when `valid` is
  `false`, and holds each broken rule once.
- **`github.com/santhosh-tekuri/jsonschema/v6`** v6.0.2, in `go.mod` and
  used in `internal/specrun/ledger_test.go`: `jsonschema.NewCompiler()`,
  `(*Compiler).Compile(path)`, `jsonschema.UnmarshalJSON(reader)`,
  `(*Schema).Validate(value)`.

## Glossary

- **controller**: a program that starts skill sessions, reads their result
  files and lands their work itself; nightshift is one.
- **work branch**: the branch a controller checked out for one piece of
  work, which a skill run commits to and pushes.
- **result file**: the JSON document a run writes at the path `--result`
  names, saying how it ended.
- **finding**: one objection to a package, from a reviewer or a person.
- **reply**: what a run did about one finding, and why.
- **package, draft, active, `lite`, evidence**: as the spec format and
  `docs/speca.md` define them.
