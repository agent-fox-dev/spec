---
spec_id: "24"
spec_name: "specrun_repair"
title: "Repair defects and untested criteria as units of work of their own"
status: "active"
created_at: "2026-11-12T19:05:00Z"
intent_hash: "baef226688b8d0a0d078268659a85f70554a43c817dbfd3872e90e3fcf77c6d9"
schema_version: 3
source: "docs/prd/07-keep-the-implementation-loop-fast-and-close-its-open-items.md"
---

# Repair defects and untested criteria as units of work of their own
## Intent

A defect the loop files today stays filed. Each package's scope rules stop
it from touching an earlier package's code, so a later package that meets a
defect works around it and leaves it in place, and the backlog only grows.
A repair is a unit of work of its own: one session, one issue, one branch,
one merge. It proves the defect with a test before it fixes anything, and
the same run shows when a later package already fixed it. `specrun` runs
repairs at two moments: instead of stopping, after a package opened a
serious defect, and in batches after every reconcile, most severe first, so
that correctness and minor defects and untested criteria get closed without
halting the loop. It chooses what to repair from labels and checks from the
forge and the repository what each session left.

## Goals

- **A repair queue from labels.** After each reconcile `specrun` queues the
  open defects whose severity is in `--repair-on` and the open untested
  items, most severe first, then oldest first, at most `--repair-max` per
  reconcile, leaving out issues labelled `af:repair-failed` and, unless
  `--repair-security` is set, security defects.
- **Repair instead of stopping.** With `--repair`, a package that opened
  defects of a severity in `--stop-on` gets each of them repaired before the
  next package.
- **A repair proves the defect first.** The regression test is written and
  run on the unchanged code; a test that passes there is committed alone and
  closes the issue as already fixed.
- **An untested criterion is closed by a test**, or becomes a defect and is
  repaired as one in the same session.
- **A failed repair is marked and left.** It gets `af:repair-failed` and a
  comment; later queues leave it out until a person removes the label.
- **Every repair session is checked**: back on a clean base branch, the
  issue closed or labelled `af:repair-failed`.

## Non-goals

- **Repairing security defects without a person.** A repair session may fix
  one, but only with `--repair-security`; the default is to stop.
- **A recipe for a slow test suite.** A `test-cost` finding is a minor
  defect and is repaired like one; how a suite is made cheaper is decided
  in that repair or by a person.
- **Moving task state.** A repair is not a package task and writes no
  `progress.json`.

## Background

`22_open_items_read_back` stops the loop after a package opened a defect of
a severity in `--stop-on`. `23_specrun_reconcile` runs a reconcile every few
packages, turns a rejected deviation into a `correctness` defect and leaves
the labels as they should stand. Nothing fixes a defect: on the measured
repository 13 were open after 25 packages, and none of the later packages
addressed one. af-impl has a package mode and a reconcile mode and no way to
work on a single issue. `specrun` has two kinds of step, a package and a
reconcile, each with a ledger line and a check of what the session left.

## Requirements

### 1. The repair queue

At each reconcile, after its rules, `specrun` builds the queue from the
labels as they stand: every open `af:defect` whose severity is in
`--repair-on` (default `security,data-loss,correctness,minor,untested`), and
every open `af:untested` item when `--repair-on` holds `untested`. It leaves
out every issue labelled `af:repair-failed`, and every `security` defect
unless `--repair-security` is set. It orders the queue most severe first
(`security`, `data-loss`, `correctness`, `minor`, then `untested`), then
oldest first, and takes at most `--repair-max` (default 5). It repairs them
one session each, in order, before the next package. Issues not taken stay
for the next reconcile. The queue is in the state file.

### 2. A repair step

`specrun` starts `claude -p "/af-impl --repair <issue number>"` with the
flags of every session; the stall rule applies. When the session ends it
checks that the base branch is checked out and the tree is clean, and that
the issue is closed or labelled `af:repair-failed`. A session that leaves
anything else stops the loop.

The outcome goes into the ledger: `fixed`, `already_fixed` or `failed`. A
failed repair whose issue has a severity in `--stop-on` stops the loop;
otherwise the loop goes on. A repair step recorded as running when
`specrun` starts counts as done when its issue is closed, and is run again
otherwise.

### 3. Repair instead of stopping

With `--repair`, after a package that opened defects whose severity is in
`--stop-on`, `specrun` repairs each of them, most severe first, before the
next package, instead of stopping. A `security` defect is repaired only
when `--repair-security` is also set; without it `specrun` stops as before.
After the repairs it makes the stop check again: a defect that is still
open stops the loop.

### 4. What a repair session does (af-impl text)

A repair of a **defect**:

- branches `fix/<issue>-<slug>` from the base branch;
- writes the regression test first and runs it on the unchanged code;
- when the test fails, fixes the code until the test passes;
- when the test passes on the unchanged code, fixes nothing: the defect is
  already gone. It commits only the test, with a `test:` commit that says
  so, and closes the issue with that test as evidence;
- passes the full gate (`speca --gate full`) and, when the issue's package
  has `test_commands`, that package's commands;
- merges `--no-ff` and closes the issue with the merge commit as evidence.

A repair of an **untested** item writes the test the criterion is missing,
on a `fix/<issue>-<slug>` branch, and runs it on the unchanged code. When it
passes, the repair commits the test (`test:`), passes the full gate, merges
and closes the issue with the test as evidence. When it fails, the repair
relabels the issue `af:defect` with severity `correctness`, with a comment
holding the failure, and goes on as a repair of that defect, in the same
session.

A repair touches only what the fix needs, and may edit a landed package's
code, because the defect is its scope. It fails when its test cannot
reproduce the issue as described, in which case it does not guess, or when
it cannot fix the defect in two attempts. Either way it comments on the
issue with what it tried, adds `af:repair-failed`, leaves the issue open and
returns to a clean base branch. The merge commit of a repair ends with an
`AF-Run:` trailer whose `repair` object holds the issue and the outcome.

### 5. Configuration and the dry run

`--repair`, `--repair-on`, `--repair-max` and `--repair-security` are flags,
each with its `SPECRUN_` variable. `specrun --dry-run` prints the repair
queue as the labels now stand.

## Design Decisions

1. **Repairs are separate merges, and `security` stops by default** (PRD
   decision 9). A repair edits code another package landed; inside a
   package's branch it would break that package's scope and traceability.
2. **A repair proves the defect before it fixes it** (PRD decision 14).
3. **Stopping and repairing are configured separately** (PRD decision 17).
   Batching repairs at reconciles, with a cap, keeps their time bounded and
   visible in the ledger.
4. **An untested criterion is closed by a test** (PRD decision 18).
5. **`specrun` reads the outcome from the forge and the trailer.** Closed
   means repaired, `af:repair-failed` means failed, and the trailer's
   `repair.outcome` tells `already_fixed` from `fixed`; a closed issue with
   no trailer is `fixed`.
6. **`untested` is a value of `--repair-on`.** The PRD's default list and
   its "when `--repair-on` holds `untested`" make it one; it sorts after
   every severity.
7. **A failed repair of a serious defect stops the loop, and so does a
   serious defect the repairs left open.** `--repair` replaces a stop only
   when the repair succeeds.
8. **`--repair-max 0` repairs nothing at a reconcile.** The repairs that
   `--repair` runs instead of stopping are not capped: each of them stands
   between the loop and the next package.
9. **The queue is rebuilt at every reconcile.** What the state file holds is
   only the rest of the current batch, so labels a person changed in between
   are honoured.

## Dependencies

- `23_specrun_reconcile` (active): the reconcile after which the queue is
  built, and the rejected deviations that become defects.
- `22_open_items_read_back` (active): `--stop-on` and the stop rule that
  `--repair` replaces.
- `19_speca_gate` (active): `speca --gate full`, which a repair runs.
- `aflib/issuex`; the `issue` command, which the session uses.
- The af-impl skill (`skills/af-impl`), which gains the `--repair` mode.

## Glossary

- **repair**: one session that closes one defect or one untested item on a
  branch and a merge of its own.
- **repair queue**: the issues `specrun` will repair after a reconcile, in
  order.
- **already fixed**: a defect whose regression test passes on the unchanged
  code.
