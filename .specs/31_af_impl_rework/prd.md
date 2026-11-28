---
spec_id: "31"
spec_name: "af_impl_rework"
title: "Send finished work back to af-impl"
status: "active"
created_at: "2026-11-28T10:40:00Z"
intent_hash: "0faa8cf9d056e98fed2e04a34dd297669770aa748ef55cdac541e95ed9de3fba"
contract_hash:
  requirements.json: "14253a58181cde24eaa03698c55c93975bdc1165bb87bcb9b49e6d9931ece5c4"
  tasks.json: "aefeac2f343d3621c8ba2e5eaf5aa917902177874f4201d2160339c9bd4d843f"
schema_version: 3
profile: "lite"
source: "docs/prd/09-run-af-spec-and-af-impl-under-a-controller.md"
---

# Send finished work back to af-impl

## Intent

A controller that lands work itself sends it back when its gate fails, a
reviewer objects, a person rejects it, or the landing conflicts. `af-impl`
cannot take that: a package whose tasks are all done is refused, because
there is nothing to implement. With `--rework <findings file>`, `af-impl`
changes finished work in answer to findings, each behaviour fix proven by a
test that failed first, moves no task state, answers every finding, and
says how it ended in the result file. The findings file and the result have
published JSON Schemas.

## Goals

- **Finished work can be sent back.** `af-impl <package> --rework
  <findings file>` fixes findings on a package whose tasks are all done,
  without moving a task state, and answers each finding.
- **A conflict at landing is a finding like any other.** The run rebases
  the work branch onto the base and replaces the branch on `origin`.
- **A person's finding cannot be declined.** Neither can one marked
  binding.
- **Every rework leaves a result file.** With `--result <file>` the run
  writes one JSON document with its outcome and one reply per finding.
- **The findings file and the result have published schemas.**
  `specification/schemas/af-impl-findings.v1.json` accepts a findings file
  and refuses a malformed one; `af-impl-result.v1.json` accepts every
  result a rework writes and refuses a document that breaks one of the
  rules below.

## Non-goals

- **Rework of a package with open tasks.** `--rework` is for work whose
  tasks are all done; an unfinished package is continued by running
  `af-impl <package> --no-merge` on it again.
- **`af-impl --no-merge` itself** (`30_af_impl_no_merge`) **and the
  `af-spec` modes** (`29_af_spec_under_controller`).
- **The controller.** What starts these sessions, what it does with a
  result and how the work lands is the controller's:
  `agent-fox-dev/nightshift`, `docs/prd/02-drive-work-items-through-the-hub.md`.
- **Changes to `specrun`, the package format, `afspec`, `speca` or
  `specr`.** `af-impl --repair <issue>` is unchanged too: it fixes one forge
  issue and merges.
- **Reading or writing forge issues.** What the rework leaves open goes to
  the result file.
- **A command that validates a findings or result file.** The schemas are
  published; a caller validates with a JSON Schema validator of its own.

## Background

**`af-impl`** (`skills/af-impl/SKILL.md`) refuses in §2 a package whose
tasks are all `done` or `dropped`: there is nothing to implement. Its
`--repair <issue>` (`references/repair.md`) fixes one forge issue and
merges. It never force-pushes and never rewrites a landed commit
(*Never*). Its conformance pass fixes a defect of its own branch with a
regression test written and seen to fail first
(`references/conformance.md`).

**`--no-merge`** (`30_af_impl_no_merge`,
`skills/af-impl/references/no-merge.md`) makes the checked-out branch the
work branch, names the base with `--base`, calls no forge command, ends at
the push of the work branch and writes its result to the path `--result`
names, in the shape `specification/schemas/af-impl-result.v1.json` gives
for the mode `no_merge`. Its open items are the entries of `open_items`.

**Published schemas and skill text.** `29_af_spec_under_controller` sets
the conventions: tests beside the schemas in the test-only Go package under
`specification/schemas/`, and the skill's behaviour carried in task steps
because skill text is not tested (steering, ADR 12).

**The controller this is written for.** nightshift's hub records every
push of the branch as a version of a change, refuses to land a merge commit
(`merge_commits_unsupported`), and lands the change itself, by replaying its
commits onto the target, after nightshift's gate and a reviewer passed it.
When the gate fails, the reviewer objects, the person rejects, or the
landing conflicts, nightshift starts the implementer again with what was
found.

## Requirements

Constraint: Linux and macOS; Windows is best effort. Constraint: `af-impl`
run without `--rework` behaves exactly as it does without this package.
Constraint: in this mode, the mode's rules on branches, merging and pushing
take the place of the repository's own agent instructions (`CLAUDE.md`,
`AGENTS.md`) on those points; every other instruction of the repository
still holds.

### af-impl: rework

`af-impl <package> --rework <findings file>` changes finished work in
answer to findings. It implies everything `--no-merge` says about the
branch, the base, the forge and the end, and takes `--base`, `--result`
and `--unattended` (with which each finding's fix runs in a subagent, one
at a time).

The findings file is a JSON array, each finding with `id` (unique in the
file), `source` (`gate`, `review`, `person`, `health` or `conflict`),
`text`, and optionally `file`, `line`, `criterion`, `output` (a command's
output) and `binding` (`true`: it must be fixed, not answered).

The run refuses, changing nothing: `refused_findings` (the file does not
exist, does not parse, or is empty), `refused_open_tasks` (a task of the
package is `pending` or `in_progress`; `reason` names it and says to run
`af-impl <package> --no-merge`), and the refusals of `--no-merge`.

For each finding, in the file's order:

- **A finding about behaviour** (the code does something else than the
  package asks, or than the finding's text shows it should) gets a test
  first: the behaviour, at the level where it lives, in the test file of
  the area it exercises, run on the unchanged code. When it fails, the run
  fixes the code until it passes: the finding is `fixed`. When it passes
  on the unchanged code, nothing is fixed, the test is kept, and the
  finding is `not_reproduced`.
- **A finding that is not about behaviour** (a document, a name, a
  structure, a test that asserts too little) is fixed directly, or
  declined.
- **A finding the run declines** is answered with the reason and is
  `declined`. A finding of source `person`, and one with `binding`, cannot
  be declined: when the run cannot fix it, the finding is `failed`.
- **A finding of source `conflict`** makes the run, before anything else,
  fetch the base branch and rebase the work branch onto it, resolving the
  conflicts with the finding's report in view. It never merges the base
  branch into the work branch. The finding is `fixed` when the rebase
  completed and the gate passed.

Each fix is one `fix: <subject>` commit whose body names the finding's
`id` and the test, ending in the trailer `Spec: <NN_name>, rework`. No
task state moves, and `prd.md`, `requirements.json` and `tasks.json` are
not changed. Reference documentation that a fix makes wrong is corrected
in the same commit.

Then the run takes `speca <package> --gate full` on the tip. On a landable
verdict it pushes the work branch; after a rebase the push replaces the
branch on `origin`, and only when `origin`'s branch is still where the run
fetched it. This is the one case in which `af-impl` rewrites pushed
commits, and it rewrites none of the base branch.

The outcomes:

- `reworked`: at least one finding is `fixed`, the gate is landable and
  the branch is pushed;
- `nothing_changed`: no finding was fixed (all `declined` or
  `not_reproduced`); nothing is pushed;
- `rework_failed`: a finding is `failed`, or the gate is not landable
  after the fixes were tried twice. The commits stay local, nothing is
  pushed, and `gate` holds what each command returned;
- `push_failed`, and the refusals above.

The result has `skill`: `af-impl`, `mode`: `rework`, and the common
members of every result (`outcome`, `reason` for a refusal or
`push_failed`, `package`, `branch`, `head`, `pushed`). It lists `replies`,
one per finding in the file's order: the finding's `id`, `action` (`fixed`,
`declined`, `not_reproduced` or `failed`), `text` (what was done, or why
not), and `commit` and `test` where there are any. It also holds
`open_items`, in the shape of `--no-merge`, for what the rework itself
leaves open.

### End-to-end flows

**Work sent back.** The controller's reviewer objects to an unchecked
error and to a missing test. The controller starts `af-impl
.specs/62_<name> --unattended --rework findings.json --result
rework.json`. The run writes a failing test for the first, fixes it,
writes the missing test for the second, passes the full gate, pushes two
`fix:` commits, and the result's `replies` are two `fixed`.

**A conflict at landing.** The hub reports that the change no longer
applies. The controller writes the report as one finding of source
`conflict` and starts the rework. The run rebases the branch onto the
base, resolves the conflict, passes the gate and replaces the branch on
`origin`.

## Design Decisions

1. **A rework mode of `af-impl` exists.** The owner decided it: without it
   a controller cannot send work back.
2. **A failed rework is not pushed.** It would replace a version that
   passed the gate with one that does not.
3. **Rework rebases for a conflict and never merges the base in.** The hub
   this is written for refuses a change that holds a merge commit. It is
   the one exception to "never force-push", limited to the work branch and
   to a push that replaces only what the run fetched.
4. **A finding from a person, or marked binding, cannot be declined.** A
   person's answer outranks the implementer's judgement; an implementer
   that cannot follow it says so and fails.
5. **The findings file and the result have published JSON Schemas, and the
   package's criteria are about them.** The owner decided it for the three
   packages of this PRD: skill text cannot be tested, and an active package
   closes a task only on a test per criterion.
6. **The result of a rework is the mode `rework` of
   `af-impl-result.v1.json`**, the file `30_af_impl_no_merge` publishes:
   one schema per skill, with the outcomes held to their mode.
7. **The skill's behaviour is carried in the task steps, and in
   `skills/af-impl/references/rework.md`.** The body of `SKILL.md` is at
   its ceiling.
8. **Profile: `lite`.** Three requirements, each about what a schema
   accepts and refuses; the flows pass through a model session, which no
   smoke check can run.
9. **The test a `not_reproduced` finding leaves is its own commit,
   `test: <subject>`, with the same body and trailer as a fix.** "The test
   is kept" needs a commit. It is pushed with the branch when the outcome
   is `reworked`; in a `nothing_changed` run it stays local, because the
   PRD says nothing is pushed there, and a push would make the hub record
   a new version of a change in which nothing was fixed. The reply names
   the commit and the test either way.
10. **`nothing_changed` means no finding was fixed and none failed.** The
    PRD's "no finding led to a commit" is read as "to a fix": by decision
    9 a `not_reproduced` finding leaves a test commit.
11. **A reply's `commit` is required when its action is `fixed`, and its
    `test` and `commit` when it is `not_reproduced`.** A `fixed` reply to a
    finding that is not about behaviour has no test; the `commit` of a
    fixed `conflict` finding is the tip of the rebased branch.
12. **`reworked` holds a `fixed` reply and no `failed` one; `rework_failed`
    holds a `failed` reply or a `gate` whose verdict is not `pass`.** The
    schema refuses a document whose outcome and replies disagree.
13. **`reworked` means the tip is on `origin`** (`pushed: true`);
    `push_failed` means it is not. For `nothing_changed` and
    `rework_failed`, `pushed` says what is true of the tip: `false` once a
    local commit exists.
14. **A rework's result carries `package`, `replies` and `open_items`**,
    and neither `tasks`, `deviations`, `summary` nor `run`: no task moved,
    and the record of a fix is its commit and its reply.
15. **A findings file is refused when it is not an array of findings of
    the published shape, or repeats an `id`** (`refused_findings`, saying
    what is wrong). The schema cannot state that ids are unique, so the
    skill checks it.
16. **`line` needs `file`, and is a positive integer; `criterion` is an id
    of the form `REQ-N.C`.** A line with no file points nowhere.
17. **"Tried twice" is the attempt rule of §5 applied to the whole
    rework**: when the full gate is not landable after the fixes, the run
    fixes what the gate names once more and takes the gate again; a second
    verdict that is not landable is `rework_failed`.
18. **In a package that requires evidence, the run takes `speca <package>
    --verify-evidence` after the full gate**, as `--no-merge` does: a fix
    must not break a test a task was closed on. A `fail` is a verdict that
    is not landable.
19. **The replacing push is `git push
    --force-with-lease=<branch>:<commit fetched>`**, which is how git
    replaces a branch only when it is still where it was fetched. A refusal
    of it is `push_failed`.
20. **With `--unattended`, the orchestrating session commits each fix**,
    as it commits each task in `27_unattended_mode`: the subagent returns
    the reply, the commit's subject and body and the test it saw fail.
21. **The reference documentation is `docs/controller.md`**, which
    `29_af_spec_under_controller` creates: this package adds `--rework`,
    the findings file, the outcomes and the schema to it.

## Dependencies

- **`30_af_impl_no_merge`** (`draft`): everything `--rework` implies about
  the branch, the base, the forge and the end, the result schema this
  package extends and the shape of `open_items`.
- **`29_af_spec_under_controller`** (`draft`): the result file's common
  members and the conventions of its schema and tests.
- **`27_unattended_mode`** (`active`): `af-impl --unattended`, whose
  subagent per unit of work `--rework` reuses for a finding.
- **`19_speca_gate`** (`active`): the full gate and its answer.
- **nightshift** (`agent-fox-dev/nightshift`,
  `docs/prd/02-drive-work-items-through-the-hub.md`): the first caller of
  this mode. It depends on this package; nothing here depends on it.

## Verified External API

- **`speca`**, read in `docs/speca.md` and the skills:
  `speca <package>`, `speca <package> --gate full`,
  `speca <package> --verify-evidence`.
- **Parley's hub**, read in `docs/api.md` of `agent-fox-dev/parley` at
  commit `7eabfb9`: a push to a branch that is the source of a change
  records its next version; a change that holds a merge commit is refused
  (`merge_commits_unsupported`); a land replays the change's commits onto
  the target. These are why the rework rebases.
- **`github.com/santhosh-tekuri/jsonschema/v6`** v6.0.2, in `go.mod`:
  `jsonschema.NewCompiler()`, `(*Compiler).Compile(path)`,
  `jsonschema.UnmarshalJSON(reader)`, `(*Schema).Validate(value)`.

## Glossary

- **controller**: a program that starts skill sessions, reads their result
  files and lands their work itself; nightshift is one.
- **work branch**: the branch a controller checked out for one piece of
  work, which a skill run commits to and pushes.
- **finding**: one objection to finished work, from a gate, a reviewer, a
  person, a health command or a landing conflict.
- **reply**: what a run did about one finding, and why.
- **rework**: a run of `af-impl` that fixes findings on a package whose
  tasks are all done.
- **open item, kind, severity**: as `af-impl`'s conformance pass defines
  them (`skills/af-impl/references/conformance.md`).
