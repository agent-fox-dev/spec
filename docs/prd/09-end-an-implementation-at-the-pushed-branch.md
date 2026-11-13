# End an implementation at the pushed branch

## Intent

af-impl owns the landing: it merges its branch into the base branch, pushes
it, and keeps the forge's issues in line with what it left open. That fits a
repository where the session is the only one landing. On a hub such as
Parley, landing is the hub's: a change lands under policy, after checks and
a review the session does not make, and the session holds a token that can
push one branch and nothing else. af-impl gets a mode for that setting. It
implements the package as it does today and ends once the branch is pushed;
what it would have written into the merge commit and onto the forge it
hands to its caller as a file.

## Goals

- **A mode that merges nothing.** `/af-impl <package> --push-only --result
  <file>` implements the package on its branch, pushes the branch, and
  ends on that branch. It never checks out, merges into or pushes the base
  branch.
- **No forge call.** In this mode af-impl reads no issue and writes none.
  It needs no forge token.
- **The record goes to the caller.** The session writes one JSON file, at
  the path the caller named, with the outcome, the branch and its commit,
  each task's result, the gate report, the open items in full, and the
  text af-impl would have put in the merge commit.
- **A result in every case.** The file is written whether the session
  pushed, stopped early or could not push, and its `outcome` says which.
- **The other runs are unchanged.** Without `--push-only`, af-impl runs as
  it does today, and `specrun` starts the same sessions with the same
  prompts.

## Non-goals

- **Landing the branch.** Opening a change or a pull request for it,
  reviewing it, landing it and verifying the base branch afterwards are the
  caller's: sparrow (`agent-fox-dev/sparrow`,
  `docs/prd/01-drive-work-items-through-the-hub-under-mandates.md`).
- **Acting on the open items.** Whether an open item becomes an issue, a
  work item or nothing is the caller's decision. The mode reports them.
- **Closing or passing on issues.** What the branch resolves is true only
  once it has landed; the mode reports it and the caller acts on it.
- **Reading back the open items of earlier packages.** The survey's read of
  the forge is left out in this mode; a caller that tracks open items
  elsewhere has no issues for it to read. Handing such items to the session
  is a later PRD.
- **A reader or validator for the result file in this repository.** The
  owner decided that nothing here checks the file; its shape is fixed by
  this document and the skill's reference, and the caller parses it.
- **`specrun`.** It keeps landing through af-impl's own merge. It does not
  use this mode.
- **The reconcile and repair runs** (`--reconcile`, `--repair`). They do
  not take `--push-only`.
- **A spec package.** The change is skill text, which the steering forbids
  testing; it is made directly, with this PRD as its record (decision 5).

## Background

`skills/af-impl/SKILL.md` describes one run in eight steps: read the
package, refuse early, survey, branch, each task, conformance, land,
report. Its argument hint lists four forms: a package, `--reconcile <adopt
file> --report <report file>`, `--repair <issue>`, and `<package>
--unattended`.

Step 4 creates `feature/<NN>-<slug>` from the base branch. Step 7, with
`skills/af-impl/references/landing.md`, pushes the branch, opens an issue
for every entry of the `unmet` list with `issue` from `aflib`, writes the
merge commit's message (open items, deviations, one entry per task, the
verification with the gate report, the conformance findings, and the
trailers `Spec:` and `AF-Run:`), merges with `--no-ff` on a landable full
gate of the merged tree, then closes the issues the branch resolved,
relabels the items that awaited the package, and pushes the base branch.
The survey (`references/survey.md`) reads the open items for the package
from the forge with `issue --list`.

The `--reconcile` run already takes a file the caller names (`--report
<report file>`) and writes its account there; `specrun` passes a path in
its run directory. `--unattended` (`references/unattended.md`) moves each
implement task into a subagent and leaves the survey, the claims, the
commits, the conformance pass and the landing with the orchestrating
session.

The skills are written once under `skills/` and rendered for each agent by
`internal/skillgen`; `make build` embeds the SHA-256 of the rendered af-impl
`SKILL.md` in `specrun`, which reports the skill a session loads.

sparrow, the controller that replaces `specrun` for a project on a Parley
hub, starts the implementer in a clone of a task fork, with a token bound
to that fork and limited to paths. The first push of a new branch opens
the change on the hub. sparrow then gates and reviews the change and asks
the hub to land it. Its PRD names this mode as a dependency that does not
exist yet.

## Requirements

Constraint: the change is to the text of the af-impl skill and its
references under `skills/af-impl/`, rendered for every agent as the others
are. No command of this repository changes its behaviour.

### The arguments

`--push-only` follows the package and needs `--result <file>` with it; it
may be combined with `--unattended`, in any order after the package. The
file's path is outside the repository's working tree.

The session refuses, before the survey and without creating a branch, when:

- `--push-only` is given without `--result`, or `--result` without
  `--push-only`: it says which one is missing;
- `--push-only` is given with `--reconcile` or `--repair`: it says the
  mode implements a package;
- the result path is inside the working tree: it says so and names the
  path.

In the first two cases no result file is written, since there is no path
or no mode to write it for; in the third none is written either, since the
path is the fault. Every other refusal of step 2 (a missing artefact, an
archived package, a failing verdict, an unmet dependency) happens as today
and is written to the result file as a `stopped` outcome.

### What the run does differently

Steps 1 to 6 run as they do today, with one difference: the survey does
not read open items from the forge, and its brief says that it did not.

Step 7 becomes:

1. Run `speca <package> --gate full` on the branch's tip, then the
   repository's thorough target where it has one (such as `make check`),
   then, in a package that requires evidence, `speca <package>
   --verify-evidence`. These are the checks today's landing runs on the
   merged tree; here they run on the branch.
2. On a landable verdict of all of them, push the branch to `origin`
   (`git push -u origin <branch>`), never forced.
3. Write the result file.
4. Stay on the branch.

A verdict that is not landable pushes nothing; the outcome is `stopped`
and the result carries what each check returned.

At no point does the session check out the base branch after step 4 has
created the feature branch, merge, push the base branch, or call `issue`
or another forge tool. It does not seal the package.

With `--unattended`, the subagents work as `references/unattended.md`
says; the orchestrating session does the new step 7.

The report of step 8 is given as today, with the path of the result file
and without a merge commit or issue URLs.

### The result file

One JSON object, UTF-8, written once, at the end of the run:

| Member | Type | Meaning |
|---|---|---|
| `schema` | string | `af-impl-result/1` |
| `outcome` | string | `pushed`, `stopped` or `push_failed` |
| `reason` | string or null | For `stopped` and `push_failed`: one line saying what stopped the run, in the words the report uses (`unmet dependency 12_cache`, `task 3 parked after its second failed attempt`, `full gate not landable`, `push rejected: <git's first line>`). Null for `pushed` |
| `spec` | object | `id`, `name`, `dir` (the `spec_dir` `speca` reports) and `status` |
| `branch` | string or null | The feature branch; null when the run stopped before step 4 |
| `base` | object or null | `branch` and `commit`: the base branch and the commit the feature branch started from; null when the run stopped before step 4 |
| `commit` | string or null | The branch's tip. For `pushed`, the commit that was pushed |
| `tasks` | array | One entry per task of the package, the integration task included: `id`, `title`, `state` (as `speca` reports it at the end), `commit` (null when it has none) and `verdict` (the gate's verdict for the task, null when it did not run) |
| `gate` | object or null | What `speca <package> --gate-report --since <baseline commit>` returns: `full` and `task`, each with `runs`, `seconds` and `reused`, and `baseline_full_seconds` and `last_full_seconds`; null when no gate ran |
| `evidence` | object or null | In a package that requires evidence, the `evidence_check` of `--verify-evidence`; else null |
| `open_items` | array | One entry per open item the branch leaves (below); empty when there are none |
| `resolves` | array | One entry per open item the session knows the branch resolves; always empty in this version, since the survey reads none (see *Non-goals*) |
| `message` | string | The text af-impl would have written as the merge commit's body: open items first, the deviations, one entry per task, the verification, the conformance findings. Without the `Spec:` and `AF-Run:` trailers; empty when the run stopped before the first task |

An entry of `open_items` holds:

| Member | Type | Meaning |
|---|---|---|
| `kind` | string | `awaits`, `defect`, `untested` or `deviation` |
| `severity` | string or null | For a `defect`: `security`, `data-loss`, `correctness` or `minor`; else null |
| `awaits` | string or null | For `awaits`: the spec id of the package that must take it up; else null |
| `title` | string | What is missing, in under 60 characters, without the package's name in front |
| `body` | string | What today's issue body holds: what is missing and why, the ids it concerns with each criterion's text, the evidence (`file:line`, the check's output), the ADR that records the decision when there is one, and what would close it |
| `criteria` | array of strings | The requirement and criterion ids the item concerns; may be empty |

The kinds and severities are the ones af-impl gives an issue today. An
outbound handoff whose receiver does not take it yet is an `awaits` entry,
as today; when the receiving package is sealed or has its last task
`done`, it is a `defect` of severity `correctness`, as today.

What each outcome guarantees:

- **`pushed`**: every task is `done`, every check of step 7 was landable,
  and `origin` holds `branch` at `commit`.
- **`stopped`**: nothing was pushed by step 7. `tasks` shows how far the
  run got, and the branch, when there is one, holds its commits locally.
- **`push_failed`**: every task is `done` and every check was landable,
  and the push was rejected or could not reach `origin`. The branch and
  its commits are left in place.

When the result file cannot be written (the directory does not exist, the
path is not writable), the session says so as the last line of its report,
with the path and the error, and its report carries the same members in
prose. A push that succeeded is not undone.

### Continuing a run

A session started again with the same arguments, after a gap, continues
the branch as step 4 says today. Tasks already `done` are skipped. When
`origin` already holds the branch at the local tip and every task is
`done`, the session runs no task, runs the checks of step 7 again (the
gate is reused where `speca` reuses it), and writes the result file with
outcome `pushed`. An existing result file at the path is overwritten.

### End-to-end flows

**A package implemented for a hub.** A caller starts, in a clone whose
`origin` is a task fork, `claude -p "/af-impl .specs/31_greeting
--push-only --result /run/7/result.json --unattended"`. The session reads
the package, surveys without the forge, creates
`feature/31-greeting`, implements each task in a subagent, runs the
conformance pass, runs the full gate and `make check` on the branch tip,
pushes the branch and writes `/run/7/result.json` with outcome `pushed`,
two `open_items` and the message. The working tree is on
`feature/31-greeting`; the base branch has not moved, locally or on
`origin`.

**A run that stops.** Task 3 fails twice and is parked. The session pushes
nothing, writes the file with outcome `stopped`, the reason naming the
task, tasks 1 and 2 `done` with their commits and task 3 with its state,
and ends on the branch.

**A push the remote refuses.** The token may not push a path the branch
touches, and `origin` rejects the push. The session writes the file with
outcome `push_failed` and git's first line as the reason, and leaves the
branch as it is.

## Design Decisions

1. **The argument is `--push-only`.** The owner decided it: it names what
   the session does and fits a pull-request workflow as well as a hub.
2. **The record goes into a result file the caller names.** The owner
   decided it; `--reconcile` already takes `--report <file>` the same way.
3. **The mode makes no forge call and reports instead.** The owner decided
   it. Reading issues is left out as well as writing them, because a
   session on a hub holds no forge token.
4. **Nothing in this repository checks the result file.** The owner
   decided it.
5. **No spec package.** It follows from decision 4: with no program to
   check, every requirement is skill text, and a package would hold no
   criterion with a check the steering allows. The change is made
   directly on a branch, with this PRD as its record.
6. **The checks of the merged tree run on the branch tip.** Chosen here:
   the session cannot see the tree its change will land on, and the caller
   gates the change again on the hub.
7. **The trailers are left out of `message`.** Chosen here: `Spec:` and
   `AF-Run:` belong to a merge commit `specrun` reads; the same numbers
   are members of the file (`spec`, `gate`, `open_items`).
8. **`resolves` is in the schema and empty.** Chosen here: a caller that
   later hands open items to the session gets them back in a member that
   already exists, without a second schema.
9. **A result file is required with the mode.** Chosen here: a session
   that pushes and leaves no record gives its caller nothing to read but
   the transcript, which is what `specrun` learned not to trust.

## Dependencies

- The af-impl skill (`skills/af-impl/SKILL.md`, `references/landing.md`,
  `references/survey.md`, `references/unattended.md`), which gains the
  mode.
- `27_unattended_mode` (active, every task done): the mode it combines
  with. Unchanged.
- `20_specrun_driver` (active, every task done): its sessions and prompts.
  Unchanged.
- `docs/adr/12-render-the-skills-for-every-agent-from-one-source.md`: the
  text is written once under `skills/` and rendered.
- sparrow (`agent-fox-dev/sparrow`,
  `docs/prd/01-drive-work-items-through-the-hub-under-mandates.md`): the
  caller this mode is for. Its PRD describes an implementer run with
  `--unattended` only and is to name `--push-only --result <file>` once
  this lands.

## Glossary

- **push-only mode**: af-impl run with `--push-only`, ending once its
  branch is pushed, with no merge and no forge call.
- **result file**: the JSON file a push-only run writes at the path its
  caller named.
- **caller**: the program that starts the session and reads the result
  file; sparrow is the first.
- **open item**: something a branch leaves undone or decided against the
  spec, of kind `awaits`, `defect`, `untested` or `deviation`, as af-impl
  uses the term.
- **landable**: a verdict of the gate table in af-impl's step 5 that
  allows a landing.
