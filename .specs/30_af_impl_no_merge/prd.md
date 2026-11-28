---
spec_id: "30"
spec_name: "af_impl_no_merge"
title: "Stop af-impl at the pushed branch"
status: "active"
created_at: "2026-11-28T10:20:00Z"
intent_hash: "e0838d6bdaa39addf6974d677620029a270c91e3e5a028b6f7e0c0335cae4c74"
contract_hash:
  requirements.json: "bf04815cc9dccd2d958f897c35dd61ff930ce4a90351514fd621df7b53bf46b5"
  tasks.json: "4ab10b64d0489d51c47af7396885c9e5a6769b1c76f7756639d4f7f0242f47b4"
schema_version: 3
profile: "lite"
source: "docs/prd/09-run-af-spec-and-af-impl-under-a-controller.md"
---

# Stop af-impl at the pushed branch

## Intent

`af-impl` ends by merging into the base branch and by opening forge issues,
which suits a person in the session and `specrun`. A controller, such as
nightshift, lands the work itself through a hub: its implementer session
cannot write the base branch and has no forge token. With `--no-merge`,
`af-impl` implements the package on the branch it was started on, pushes
that branch and stops there, and it says what it did and what it left open
in a result file whose shape is published as a JSON Schema.

## Goals

- **`af-impl` can stop at the pushed branch.** With `--no-merge` it
  implements the package on the branch it was started on, pushes that
  branch, and neither merges, nor touches the base branch, nor calls the
  forge.
- **The open items go to the caller, not to the forge.** What a landing
  would file as issues is listed in the result, each with its kind.
- **Every such run leaves a result file.** With `--result <file>` the run
  writes one JSON document that says how it ended, what it produced and
  what it left open. A caller reads that file and nothing else of the
  session.
- **The result has a published schema.**
  `specification/schemas/af-impl-result.v1.json` accepts every result a
  `--no-merge` run writes and refuses a document that breaks one of the
  rules below.

## Non-goals

- **`af-impl --rework`.** Fixing findings on finished work is the package
  `31_af_impl_rework`.
- **The `af-spec` modes.** They are the package
  `29_af_spec_under_controller`.
- **The controller.** What starts these sessions, where they run, what it
  does with a result and how the work lands is the controller's:
  `agent-fox-dev/nightshift`, `docs/prd/02-drive-work-items-through-the-hub.md`.
- **Changes to `specrun`.** It keeps calling `af-impl` as it does, and
  `af-impl` without the new arguments behaves as it does today.
- **Changes to the package format, `afspec`, `speca` or `specr`.** The mode
  uses the commands as they are.
- **Reading or writing forge issues in this mode.** The open items go to
  the result file; whoever lands the work files them.
- **A command that validates a result file.** The schema is published; a
  caller validates with a JSON Schema validator of its own.

## Background

**`af-impl`** (`skills/af-impl/SKILL.md`) implements a package and runs to
the merge. It refuses to start on the work branch (§2), creates
`feature/<NN>-<slug>` from the base branch (§4), reads open items from the
forge with `issue` in its survey (`references/survey.md`), opens an issue
for every open item, merges into the base branch on a full gate of the
merged tree, pushes the base branch and closes and relabels issues
(`references/landing.md`). It never force-pushes. Its `--unattended`
(`references/unattended.md`) runs each implement task in a subagent and
changes nothing else. A package whose tasks are all `done` or `dropped` is
refused in §2: there is nothing to implement. The open items a run leaves
are the `unmet` list of `references/conformance.md`, each with one kind
(`awaits`, `defect` with a severity, `untested`, `deviation`). Its body is
within 200 bytes of the ceiling `make skills-check` enforces (24,576 bytes,
ADR 12), so what a mode adds goes into a reference file.

**What tells a caller how a run went.** `specrun` reads git: the merge
commit on the base branch and its `AF-Run:` trailer, whose `gate` object is
copied from `speca <package> --gate-report` and whose `open_items` object
counts what the run opened, closed, passed on and reclassified. There is no
machine-readable result of a run that did not merge.

**The gate's answer.** `speca <package> --gate full` answers in
`result.gate`: `level`, `verdict` (`pass`, `fail` or `not_run`),
`commands`, each with `name`, `exit_code`, `seconds`, `log` and, when it
failed, `tail`, and more (`docs/speca.md`).

**Published schemas and skill text.** `29_af_spec_under_controller`
publishes `af-spec-result.v1.json` and sets the conventions this package
follows: the common members of a result, a schema per skill, tests beside
the schemas in the test-only Go package under `specification/schemas/`,
and the skill's behaviour carried in task steps because skill text is not
tested (steering, ADR 12).

**The controller this is written for.** nightshift starts implementer
sessions (`af-impl`) with `claude -p` in a clone of a task fork on a Parley
hub. The session's credential can fetch and push in the fork and nothing
else: it cannot write the base branch and has no forge token. The hub
records every push of the branch as a version of a change, refuses to land
a merge commit, and lands the change itself after nightshift's gate and a
reviewer passed it.

## Requirements

Constraint: Linux and macOS; Windows is best effort. Constraint: `af-impl`
run without any argument this package adds behaves exactly as it does
today. Constraint: in this mode, the mode's rules on branches, merging and
pushing take the place of the repository's own agent instructions
(`CLAUDE.md`, `AGENTS.md`) on those points; every other instruction of the
repository still holds.

### The result file

`--result <file>` is accepted by `af-impl` with `--no-merge`. Given without
it, the skill says `--result needs --unattended, --no-merge or --rework`
and does nothing.

The file is one JSON document, written once, as the last thing the run
does, outside the repository's working tree when the path is outside it.
Every document has:

- `skill`: `af-impl`; `mode`: `no_merge`;
- `outcome`: one of the values named below;
- `reason`: one sentence, present when the outcome is a refusal or
  `push_failed`;
- `package`: `spec_dir`, `spec_id`, `spec_name`, `title`, `status`,
  `profile`, absent when the run refused before it knew the package;
- `branch` and `head`: the branch the run worked on and its last commit,
  and `pushed`: whether that commit is on `origin`;
- the members named below.

A run that refuses before it changes anything still writes the file, with
the refusal as `outcome` and `reason`. A path that cannot be written is
said in the session's last message (`result file not written: <error>`);
the run's work is left as it is. A caller treats a session that left no
file, or one that does not parse, as failed.

Without `--result` the mode behaves the same and reports in the session
only.

### af-impl: stop at the pushed branch

`af-impl <package> --no-merge` implements the package on the branch that
is checked out, and ends when that branch is pushed. It combines with
`--unattended`. `--base <branch>` names the base branch; without it, the
base is the default branch of `origin`.

What differs from a run without it:

- **Where it starts.** The checked-out branch is the work branch. It must
  not be the base branch (`refused_branch`, `checked out on the base
  branch <name>`), and the package must be on it. The branch may already
  hold commits ahead of the base: the package's own, and earlier work of
  the same package, which the run continues as §4 describes. The run
  creates no branch.
- **The forge.** The run calls no forge command. The survey reads no open
  items, and says so in its brief. No issue is opened, closed, commented
  on or relabelled.
- **The dependencies.** A dependency of the package that is neither sealed
  nor has its last task `done`, judged on the work branch, refuses the
  run (`refused_dependency`, naming the package).
- **The end.** After the conformance pass the run takes
  `speca <package> --gate full` on the tip of the work branch. On a
  landable verdict it pushes the work branch to `origin` and stops there,
  on the work branch. It does not check out, merge into, or push the base
  branch.
- **The record.** What the merge commit's message would have held (the
  open items, the deviations, one entry per task, the verification, the
  conformance findings) is written into the result as `summary`, as
  Markdown, and the numbers of the `AF-Run:` trailer as `run`.

The outcomes:

- `complete`: every task is `done` or `dropped`, the full gate is
  landable, the branch is pushed, and no open item remains;
- `complete_with_open_items`: the same, with a non-empty `open_items`;
- `parked`: a task failed twice. Its `wip:` commit stays at the tip of the
  work branch, which is pushed; `parked` holds the task's number and the
  check's output;
- `gate_failed`: every task is done and the full gate on the tip is not
  landable. The branch is pushed; `gate` holds what each command returned;
- `push_failed`: the push was refused; `reason` is git's message, and the
  commits stay local;
- a refusal: `refused_branch`, `refused_dependency`, `refused_dirty` (the
  working tree is not clean), `refused_package` (a required file is
  missing, the package does not validate, its status is not `draft` or
  `active`, its intent or contract drifted, or `speca` refuses the first
  task move; `reason` says which), each changing nothing.

The result also holds `tasks` (per task: `number`, `outcome`, `commit`,
the gate `verdict`), `deviations` (each with its key, its reason and the
ADR that records it) and `open_items`: the `unmet` list, one entry per
open item, with `kind` (`awaits`, `defect`, `untested` or `deviation`),
`severity` for a defect, `awaits` (the package id) for an `awaits` item,
`title` (at most 60 characters, as the issue's title would be), `missing`,
`why`, `ids`, `evidence`, `adr` where there is one, and `closes` (what
would close it). An outbound handoff its receiver does not take yet is an
`awaits` entry, as in a landing.

Running the same command again on a branch that was parked, or whose push
failed, continues it, as a run without `--no-merge` continues its branch.

### End-to-end flow

**An implementation handed over.** After the person approved the plan, the
controller activates the package on the branch and starts `af-impl
.specs/62_<name> --unattended --no-merge --result impl.json`. The run
surveys without the forge, implements each task in a subagent with its
evidence recorded, writes the documentation, runs the conformance pass and
the full gate, pushes the branch and writes `impl.json` with `outcome:
complete_with_open_items` and one `untested` item. The base branch is as
it was, and no issue exists.

## Design Decisions

1. **The result is a file, at a path the caller gives, and a refusal
   writes it too.** As in `29_af_spec_under_controller`.
2. **`--no-merge` runs the full gate on the branch tip, not on a merged
   tree.** There is no merged tree; the caller that lands the work gates
   what it lands.
3. **A parked or gate-failed branch is pushed.** Unfinished first work is
   lost with the clone unless it is pushed.
4. **The mode's rules on branches and merging outrank the repository's
   agent instructions.** Those instructions commonly say "merge into main
   and push it", which is exactly what the caller asked the skill not to
   do.
5. **The result has a published JSON Schema, `af-impl-result.v1.json`, and
   the package's criteria are about it.** The owner decided it for the
   three packages of this PRD: skill text cannot be tested, and an active
   package closes a task only on a test per criterion.
6. **The skill's behaviour is carried in the task steps, and in
   `skills/af-impl/references/no-merge.md`.** The body of `SKILL.md` is at
   its ceiling, and a reference file loads only in the runs that use the
   mode.
7. **Profile: `lite`.** Three requirements, each about what the schema
   accepts and refuses; the end-to-end flow passes through a model session,
   which no smoke check can run.
8. **`reason` is required for every `refused_*` outcome and for
   `push_failed`.** `parked` and `gate_failed` carry the check's output and
   the gate in its place.
9. **`complete`, `complete_with_open_items`, `parked` and `gate_failed`
   mean the tip is on `origin`** (`pushed: true`); `push_failed` means it
   is not. A document that says otherwise is refused by the schema.
10. **A run that got past its refusals always carries `package`, `tasks`,
    `deviations` and `open_items`**, each list possibly empty; `summary`
    and `run` are required for the two `complete` outcomes and for
    `gate_failed`, where every task is done and the record exists, and are
    optional for `parked` and `push_failed`.
11. **A task's `outcome` in the result is its state**: `done`, `dropped`,
    `in_progress` for the parked one, or `pending` for those after it. A
    done task carries its `commit` and its gate `verdict`, one of `pass`,
    `pass_baseline_failing`, `regressed`, `still_failing` and
    `gate_failed`: the five rows of the table in §5 of the skill.
12. **`run` is the `AF-Run:` trailer's object**: `gate`, as `speca
    --gate-report` gives it, and `open_items`, whose `opened` counts the
    result's open items by kind, a defect as `defect:<severity>`, and whose
    `closed`, `passed_on` and `reclassified` are empty, because the run
    reads and writes no issue.
13. **`gate` is `speca`'s `result.gate` object, copied**: the schema
    requires its `verdict` and its `commands`, each with `name`,
    `exit_code` and `seconds`, and lets the object carry what else `speca`
    puts there, so that a new member of the gate's answer does not make a
    result invalid. Everywhere else the schema refuses a member it does not
    name.
14. **`parked` is `{task, output}`**: the task's number and the output of
    the check that failed.
15. **An open item's `ids` and `evidence` are lists of strings**, `ids`
    non-empty; `adr` is the ADR's path; `awaits` is the receiving package's
    id as its directory spells it (`"31"`).
16. **A deviation always names its ADR.** The skill records every declared
    deviation in one (§5, step 8); where the repository's instructions put
    decisions elsewhere, `adr` is that document's path.
17. **In a package that requires evidence, the run takes `speca <package>
    --verify-evidence` on the tip after the full gate**, as a landing does
    on the merged tree. A `fail` is a verdict that is not landable: the
    outcome is `gate_failed`, and `summary` names the tests that failed.
18. **`--base` names a branch of `origin`**, and the base is used to tell
    the work branch from it, to measure the diff of the conformance pass
    and for the gate's base; the run fetches it and never checks it out.
19. **The package is "on the branch" when `speca <package>` resolves it in
    the checked-out tree.** Otherwise the run is refused with
    `refused_package`, as for a missing file.
20. **`refused_package` and `refused_dependency` name the package they
    looked at** when they could read it, so `package` may be present on a
    refusal, with any lifecycle status.
21. **The reference documentation is `docs/controller.md`**, which
    `29_af_spec_under_controller` creates: this package adds `--no-merge`,
    `--base`, the outcomes and the schema to it, and `docs/architecture.md`
    lists the schema file.

## Dependencies

- **`29_af_spec_under_controller`** (`draft`): the result file's common
  members and the conventions of its schema and tests, which this package
  follows.
- **`27_unattended_mode`** (`active`): `af-impl --unattended`, which
  `--no-merge` combines with and does not change.
- **`21_open_item_labels`**, **`22_open_items_read_back`** (`active`): the
  kinds of an open item, which the result's `open_items` carry, and the
  survey's read of open items, which `--no-merge` leaves out.
- **`19_speca_gate`** (`active`): the full gate and its answer, which the
  result copies.
- **`20_specrun_driver`** (`active`): unchanged; it calls `af-impl` without
  the new arguments.
- **nightshift** (`agent-fox-dev/nightshift`,
  `docs/prd/02-drive-work-items-through-the-hub.md`): the first caller of
  this mode. It depends on this package; nothing here depends on it.

## Verified External API

- **`claude -p`**, read in `docs/specrun.md`: `claude -p "/af-impl
  <spec_dir> --unattended" --output-format stream-json --verbose`, the
  call `specrun` makes and a controller makes with the new arguments.
- **`speca`**, read in `docs/speca.md` and the skills:
  `speca <package> --validate`, `speca <package> --gate task|full`,
  `speca <package> --gate-report --since <commit>`,
  `speca <package> --verify-evidence`,
  `speca <package> --task <N> --state <S>`. `evidence_required` is `true`
  for an active package with a `contract_hash`.
- **`github.com/santhosh-tekuri/jsonschema/v6`** v6.0.2, in `go.mod`:
  `jsonschema.NewCompiler()`, `(*Compiler).Compile(path)`,
  `jsonschema.UnmarshalJSON(reader)`, `(*Schema).Validate(value)`.

## Glossary

- **controller**: a program that starts skill sessions, reads their result
  files and lands their work itself; nightshift is one.
- **work branch**: the branch a controller checked out for one piece of
  work, which a skill run commits to and pushes.
- **result file**: the JSON document a run writes at the path `--result`
  names, saying how it ended.
- **open item, `unmet`, kind, severity**: as `af-impl`'s conformance pass
  defines them (`skills/af-impl/references/conformance.md`).
- **package, draft, active, `lite`, gate, evidence**: as the spec format
  and `docs/speca.md` define them.
