# Run af-spec and af-impl under a controller

## Intent

`af-spec` and `af-impl` assume one of two callers: a person in the session,
or `specrun`, which works in one checkout and lets `af-impl` merge into the
base branch itself. A third caller is coming: a controller, such as
nightshift, that starts each skill as a headless session on a work branch,
has nobody in the session to answer a question, lands the work itself
through a hub, and sends the work back when a gate, a reviewer or a person
objects. Today the skills cannot serve it: `af-spec` stops to ask and picks
its own package number, `af-impl` ends by merging and by opening forge
issues, and neither can take findings back on finished work. This PRD gives
both skills the modes a controller needs, and one way to tell it what
happened: a result file.

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
- **`af-impl` can stop at the pushed branch.** With `--no-merge` it
  implements the package on the branch it was started on, pushes that
  branch, and neither merges, nor touches the base branch, nor calls the
  forge.
- **Finished work can be sent back.** `af-impl <package> --rework
  <findings file>` fixes findings on a package whose tasks are all done,
  without moving a task state, and answers each finding.
- **Every such run leaves a result file.** With `--result <file>` each of
  the modes above writes one JSON document that says how the run ended,
  what it produced and what it left open. A caller reads that file and
  nothing else of the session.

## Non-goals

- **The controller.** What starts these sessions, where they run, what it
  does with a result and how the work lands is the controller's:
  `agent-fox-dev/nightshift`, `docs/prd/02-drive-work-items-through-the-hub.md`.
- **Changes to `specrun`.** It keeps calling `af-impl` as it does, and
  `af-impl` without the new arguments behaves as it does today.
- **Changes to the package format, `afspec`, `speca` or `specr`.** The
  modes use the commands as they are.
- **An unattended `af-prd`.** A controller hands `af-spec` the text it has;
  no separate PRD file is written for it.
- **Reading or writing forge issues in the new `af-impl` modes.** The open
  items go to the result file; whoever lands the work files them.
- **Proving a defect while planning.** `af-spec --defect` writes no test
  and runs none. The failing run is recorded when the package is
  implemented, by the evidence rule of `28_close_task_on_evidence`.
- **Writing several packages in one `af-spec` run.** One run, one package.
- **Rework of a package with open tasks.** `--rework` is for work whose
  tasks are all done; an unfinished package is continued by running
  `af-impl` on it again, as today.

## Background

**`af-spec`** (`skills/af-spec/SKILL.md`) writes a package in the session.
It stops for the person's answers when it reads its input (§1), when the
work needs a split (§4) and for an escalated finding of its review. It
takes the next free number itself (§3: the highest prefix in the spec root
and under `archive/`, plus one). It activates a package that validates
(§10), and leaves it a draft only when the person asks. Its input may be a
draft package to finish or repair. It commits as the repository's
instructions say.

**`af-impl`** (`skills/af-impl/SKILL.md`) implements a package and runs to
the merge. It refuses to start on the work branch (§2), creates
`feature/<NN>-<slug>` from the base branch (§4), reads open items from the
forge with `issue` in its survey (`references/survey.md`), opens an issue
for every open item, merges into the base branch on a full gate of the
merged tree, pushes the base branch and closes and relabels issues
(`references/landing.md`). It never force-pushes. Its `--unattended`
(`references/unattended.md`) runs each implement task in a subagent and
changes nothing else. Its `--repair <issue>` fixes one forge issue and
merges; its `--reconcile` changes no code. A package whose tasks are all
`done` or `dropped` is refused in §2: there is nothing to implement. The
open items a run leaves are the `unmet` list of
`references/conformance.md`, each with one kind (`awaits`, `defect` with a
severity, `untested`, `deviation`).

**What tells a caller how a run went.** `specrun` reads git: the merge
commit on the base branch and its `AF-Run:` trailer. The reconcile mode
writes a Markdown report to a path given with `--report`. There is no
machine-readable result of a run that did not merge.

**Activation freezes a package.** `speca <package> --status active` hashes
the `## Intent` section and, since `28_close_task_on_evidence`,
`requirements.json` and `tasks.json`; while one differs from its hash every
task write is refused. A package that a person may still send back for
revision therefore has to stay a draft until they approved it. An active
package closes its tasks on evidence: each criterion's test is recorded
failing before the implementation.

**The controller this is written for.** nightshift starts a planner session
(`af-spec`) and implementer sessions (`af-impl`) with `claude -p` in a
clone of a task fork on a Parley hub. The session's credential can fetch
and push in the fork and nothing else: it cannot write the base branch and
has no forge token. The hub records every push of the branch as a version
of a change, refuses to land a merge commit, and lands the change itself
after nightshift's gate and a reviewer passed it. nightshift gives each
planner its package number, because several planners run at once. When the
gate fails, the reviewer objects, the person rejects, or the landing
conflicts, nightshift starts the implementer again with what was found.

The skills are rendered for every agent from the one source under
`skills/` (ADR 12); every argument this PRD adds is added there.

## Requirements

Constraint: Linux and macOS; Windows is best effort. Constraint: a skill
run without any argument this PRD adds behaves exactly as it does today.
Constraint: in the modes below, the mode's rules on branches, merging and
pushing take the place of the repository's own agent instructions
(`CLAUDE.md`, `AGENTS.md`) on those points; every other instruction of the
repository still holds.

### The result file

`--result <file>` is accepted by `af-spec` with `--unattended`, and by
`af-impl` with `--no-merge` or `--rework`. Given with neither, the skill
says `--result needs --unattended, --no-merge or --rework` and does
nothing.

The file is one JSON document, written once, as the last thing the run
does, outside the repository's working tree when the path is outside it.
Every document has:

- `skill`: `af-spec` or `af-impl`; `mode`: `unattended`, `no_merge` or
  `rework`;
- `outcome`: one of the values the mode's section names;
- `reason`: one sentence, present when the outcome is a refusal or a
  failure;
- `package`: `spec_dir`, `spec_id`, `spec_name`, `title`, `status`,
  `profile`, absent when the run refused before it knew the package;
- `branch` and `head`: the branch the run worked on and its last commit,
  and `pushed`: whether that commit is on `origin`;
- the members the mode's section names.

A run that refuses before it changes anything still writes the file, with
the refusal as `outcome` and `reason`. A path that cannot be written is
said in the session's last message (`result file not written: <error>`);
the run's work is left as it is. A caller treats a session that left no
file, or one that does not parse, as failed.

Without `--result` the modes behave the same and report in the session
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
  as `decisions`.
- A finding its review would escalate to the person is decided the same
  way, and listed in the result as `escalated`, with the finding and the
  decision.
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
`speca <NN> --status active`.

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
`--no-activate` also work in a watched run.

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

### af-impl: rework

`af-impl <package> --rework <findings file>` changes finished work in
answer to findings. It implies everything `--no-merge` says about the
branch, the base, the forge and the end, and takes `--base` and
`--unattended` (with which each finding's fix runs in a subagent, one at a
time).

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
- `nothing_changed`: no finding led to a commit (all `declined` or
  `not_reproduced`); nothing is pushed;
- `rework_failed`: a finding is `failed`, or the gate is not landable
  after the fixes were tried twice. The commits stay local, nothing is
  pushed, and `gate` holds what each command returned;
- `push_failed`, and the refusals above.

The result lists `replies`, one per finding in the file's order: the
finding's `id`, `action` (`fixed`, `declined`, `not_reproduced` or
`failed`), `text` (what was done, or why not), and `commit` and `test`
where there are any. It also holds `open_items`, in the shape of
*af-impl: stop at the pushed branch*, for what the rework itself leaves
open.

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

**An implementation handed over.** After the person approved the plan, the
controller activates the package on the branch and starts `af-impl
.specs/62_<name> --unattended --no-merge --result impl.json`. The run
surveys without the forge, implements each task in a subagent with its
evidence recorded, writes the documentation, runs the conformance pass and
the full gate, pushes the branch and writes `impl.json` with `outcome:
complete_with_open_items` and one `untested` item. The base branch is as
it was, and no issue exists.

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

1. **A rework mode of `af-impl` is part of this PRD.** The owner decided
   it: without it a controller cannot send work back.
2. **A defect is proven when it is implemented, not when it is planned.**
   The owner decided it: `af-spec` writes no code, and an active package
   already records each test failing first.
3. **A planned package stays a draft until a person approved it; the
   caller activates it.** The owner decided it: activation freezes the
   contract, and a plan may be sent back.
4. **Unattended, work too large for one package is written as its first
   scope, and the rest is reported.** The owner decided it.
5. **The result is a file, at a path the caller gives.** Chosen here: a
   session's last message is prose a caller would have to parse, and
   `--report <file>` of the reconcile mode is the precedent.
6. **A refusal writes the result too.** Chosen here: a caller should not
   have to tell a refusal from a crash by the absence of a file.
7. **`--number` must be above every existing number.** Chosen here:
   packages are implemented in number order (ADR 29), and an archived
   number is never reused.
8. **The unattended `af-spec` works on the checked-out branch and pushes
   it.** Chosen here: the controller made the branch for this work; a
   skill that created another would leave the controller's branch empty.
9. **`--no-activate` and `--number` are arguments of their own, not part
   of `--unattended`.** Chosen here: a person in a watched run has the
   same two needs, and `spec --no-activate` already exists.
10. **`--no-merge` runs the full gate on the branch tip, not on a merged
    tree.** Chosen here: there is no merged tree; the caller that lands
    the work gates what it lands.
11. **A parked or gate-failed branch is pushed in `--no-merge`; a failed
    rework is not.** Chosen here: unfinished first work is lost with the
    clone unless it is pushed, while a failed rework would replace a
    version that passed the gate with one that does not.
12. **Rework rebases for a conflict and never merges the base in.** Chosen
    here: the hub this is written for refuses a change that holds a merge
    commit. It is the one exception to "never force-push", limited to the
    work branch and to a push that replaces only what the run fetched.
13. **A finding from a person, or marked binding, cannot be declined.**
    Chosen here: a person's answer outranks the implementer's judgement;
    an implementer that cannot follow it says so and fails.
14. **The mode's rules on branches and merging outrank the repository's
    agent instructions.** Chosen here: those instructions commonly say
    "merge into main and push it", which is exactly what the caller asked
    the skill not to do.
15. **Size.** Five areas beside the result file; expected to be split into
    three packages: the result file with the unattended `af-spec` and its
    arguments first, then `af-impl --no-merge`, then `af-impl --rework`.

## Dependencies

- **`14_render_skills_from_one_source`** (archived): the skills' source
  under `skills/` and its render per agent, which every new argument goes
  through.
- **`27_unattended_mode`** (`active`): `af-impl --unattended`, which
  `--no-merge` and `--rework` combine with and do not change.
- **`28_close_task_on_evidence`** (`active`): the contract hash that makes
  a draft necessary before approval, and the evidence that proves a defect
  at implementation.
- **`21_open_item_labels`**, **`22_open_items_read_back`** (`active`): the
  kinds of an open item, which the result's `open_items` carry, and the
  survey's read of open items, which `--no-merge` leaves out.
- **`20_specrun_driver`**, **`23_specrun_reconcile`**,
  **`24_specrun_repair`** (`active`): unchanged; they call `af-impl`
  without the new arguments.
- **ADR 29**, `number-packages-in-implementation-order`: the rule
  `--number` keeps.
- **nightshift** (`agent-fox-dev/nightshift`,
  `docs/prd/02-drive-work-items-through-the-hub.md`): the first caller of
  these modes. It depends on this PRD; nothing here depends on it.

## Verified External API

- **`claude -p`**, read in `docs/specrun.md`: `claude -p "/af-impl
  <spec_dir> --unattended" --output-format stream-json --verbose`, the
  call `specrun` makes and a controller makes with the new arguments.
- **`speca`**, read in `docs/speca.md` and the skills:
  `speca <package> --status active`, `speca <package> --validate`,
  `speca <package> --gate task|full`, `speca <package> --gate-report
  --since <commit>`, `speca <package> --task <N> --state <S>`,
  `speca --list --all`. `evidence_required` is `true` for an active
  package with a `contract_hash`.
- **Parley's hub**, read in `docs/api.md` of `agent-fox-dev/parley` at
  commit `7eabfb9`: a push to a branch that is the source of a change
  records its next version; a change that holds a merge commit is refused
  (`merge_commits_unsupported`); a land replays the change's commits onto
  the target. These are why the rework rebases.

## Glossary

- **controller**: a program that starts skill sessions, reads their result
  files and lands their work itself; nightshift is one.
- **work branch**: the branch a controller checked out for one piece of
  work, which a skill run commits to and pushes.
- **result file**: the JSON document a run writes at the path `--result`
  names, saying how it ended.
- **finding**: one objection to a package or to finished work, from a
  gate, a reviewer, a person, a health command or a landing conflict.
- **reply**: what a run did about one finding, and why.
- **rework**: a run of `af-impl` that fixes findings on a package whose
  tasks are all done.
- **open item, `unmet`, kind, severity**: as `af-impl`'s conformance pass
  defines them (`skills/af-impl/references/conformance.md`).
- **package, draft, active, `lite`, gate, evidence**: as the spec format
  and `docs/speca.md` define them.
