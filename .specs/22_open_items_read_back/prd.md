---
spec_id: "22"
spec_name: "open_items_read_back"
title: "Read the open items back, close what a branch resolves and stop on a serious defect"
status: "active"
created_at: "2026-11-12T18:25:00Z"
intent_hash: "1c617280bf4a8ad22138c0c8e14a47e3633ec205aa9023ccbafa4c4cd47a2ced"
schema_version: 3
source: "docs/prd/07-keep-the-implementation-loop-fast-and-close-its-open-items.md"
---

# Read the open items back, close what a branch resolves and stop on a serious defect
## Intent

The issues af-impl opens are written and never read. A gap meant for a later
package is not handed to it, a gap a later package closes stays open, an
item keeps pointing at a package that has long landed, and the loop builds
the next package on top of a data-loss defect it just filed. The loop reads
its open items back: a package's survey lists what awaits it and the known
defects in the files it touches, its landing closes what it resolved and
passes on what it did not, no item is ever left awaiting a package that is
done, and `specrun` does not start the next package on top of a serious
defect.

## Goals

- **No dead handoffs.** An `awaits` item never names a package that has
  already landed: af-impl files such a gap as a `correctness` defect, and
  `specrun` relabels any open `af:awaits-<NN>` whose package is done.
- **Open items are read back.** A package's survey lists the open items that
  await it and the open defects and untested items in the files it touches.
- **Landing closes what the branch resolved**, each item with the merge
  commit and the evidence, and passes on or reclassifies the items that
  awaited the package and are still open.
- **Stop on serious defects.** After a package lands with an open defect
  whose severity is in `--stop-on` (default `security,data-loss`), `specrun`
  stops and names it. The next package is not started on top of it.
- **A dry run shows the label changes.** `specrun --dry-run` prints what the
  label rules would change, and changes nothing.

## Non-goals

Left to the later scopes of
`docs/prd/07-keep-the-implementation-loop-fast-and-close-its-open-items.md`:

- **Repairing instead of stopping** (`--repair`, the repair queue):
  `specrun_repair`.
- **The reconcile**: adopting older issues, checking every closing condition
  against the base branch, and settling deviations by label:
  `specrun_reconcile`.
- **Declared handoffs in the format**: `package_handoffs`. Here the survey
  reads the spec's prose about wiring.

## Background

`21_open_item_labels` gives every open item a kind and labels, and
`specrun` checks them after a landing. Nothing reads them yet:

- af-impl's survey (`skills/af-impl/references/survey.md`) never lists
  issues;
- af-impl's rules forbid it to "close, edit or comment on an issue this run
  did not open" (`SKILL.md`, *Never*);
- `specrun` starts the next package whatever the last one filed.

On the measured repository, three issues were resolved by a later package
and stayed open, two were orphaned when the package meant to wire them
landed without doing so, and a package's `tasks.json` handed work to two
packages that had landed before it. A defect left in place was worked around
by the packages after it, and four packages each rewrote two checks that an
earlier one had exported and nobody called.

`speca <NN>` reports a package's status and the state of its tasks; `afspec`
answers the same in process.

## Requirements

### 1. No `awaits` toward a landed package

**af-impl text.** Before filing an `awaits` item, af-impl asks `speca <NN>`
about the receiving package. If that package is sealed, or its last task is
`done`, the item is not `awaits`. It is a `defect` of severity
`correctness`, with the note "handoff to a package that has already landed".
The same check runs when the survey reads a spec's prose about wiring
("package N registers …"): prose that hands work to a landed package is
recorded as a survey decision, either "this package wires it" or "filed as a
defect".

**`specrun`.** After every landing it lists the open issues labelled
`af:awaits`. For each one whose `af:awaits-<NN>` names a package that is
finished (sealed, archived, or with no task open), or names no package under
the spec root, it removes `af:awaits` and `af:awaits-<NN>`, adds `af:defect`
and `af:severity-correctness`, and comments that the package it awaited has
landed. That covers the item af-impl passed on wrongly and the one a session
left behind.

### 2. The survey reads the open items back (af-impl text)

The survey lists, with `issue --list --state open`: every item labelled
`af:awaits-<this package>`; and every open `af:defect` and `af:untested`
item whose evidence names a file that this package's tasks touch. The brief
gets a section "Open items for this package". Each `awaits` item is either
mapped to the task that will satisfy it, or recorded as a numbered decision
with the reason it will not be. Each defect in the touched area is listed as
a known fault, so the plan does not build on it unknowingly or copy the code
around it. An exported entry point that an `awaits` item names is used, not
re-implemented.

### 3. Landing closes what the branch resolved (af-impl text)

Before the merge, for each item the survey listed, af-impl decides whether
the branch closes it, with evidence: the production caller (`file:line`) and
the test that exercises it. After the merge is pushed, it closes each
resolved item with `issue <ref> --close --body`; the comment names the merge
commit, the evidence and this package. af-impl's *Never* rule changes
accordingly: it may comment on and close an issue labelled `af:*` whose
closing condition this branch meets, and still never closes, edits or
comments on any other issue.

### 4. Nothing is left awaiting a package that is done (af-impl text)

An `af:awaits-<this package>` item the branch did not resolve is passed on
by af-impl after the merge: relabelled to the later package that now owns
it, with a comment saying why, or relabelled `af:defect` with severity
`correctness`, with a comment saying that no package owns it. The trailer
and the report count the items closed, passed on and reclassified.
`specrun` then enforces the result with the rule of section 1.

### 5. Stop on serious defects (`specrun`)

After a package lands and the label rules have run, `specrun` lists the open
`af:defect` issues labelled `af:from-<NN>`. If any carries a severity in
`--stop-on` (default `security,data-loss`), it stops with exit code 1 and
names them. Started again, it makes the same check for the last package that
landed before it chooses a step, so a restart does not build on the defect
either: the person closes the issue, changes its labels, or changes
`--stop-on`. An empty `--stop-on` never stops. A `deviation`, an `untested`
item and a defect of another severity never stop `specrun`.

### 6. The dry run

`specrun --dry-run` prints, beside the next step, what the label rules of
`21_open_item_labels` and of this package would change as the labels now
stand, and whether the stop rule would fire. It writes nothing to the forge.

## Design Decisions

1. **The run that resolves an item closes it** (PRD decision 7). Only the
   run that wired the caller can cite the caller. The `af:` label marks the
   issues that are af-impl's own, so the *Never* rule keeps it away from a
   person's issues.
2. **Data-loss defects stop the loop by default, correctness defects do
   not** (PRD decision 10). A correctness gap is usually local; a lost write
   is the kind a later package silently builds on.
3. **`security` stops by default** (PRD decision 9): a person should see it
   before the loop builds further on it.
4. **Passing on is af-impl's, enforcing is `specrun`'s.** Choosing the new
   owner takes judgement; that nothing still awaits a finished package is a
   rule checked the same way every time.
5. **The relabel rule covers every finished package, not only the one that
   just landed.** An item filed toward a package that had already landed is
   the wiring nobody owns; one rule catches it whenever it appears.
6. **An `af:awaits-<NN>` that names no package is a defect too.** Nothing
   will ever land to close it.
7. **The stop check is repeated at the next start.** Otherwise starting
   `specrun` again would be the way past a stop.
8. **An empty `--stop-on` means never stop.** The flag decides only when
   `specrun` halts; a project that wants no halt says so explicitly.

## Dependencies

- `21_open_item_labels` (active): the kinds, the labels, the forge client in
  `specrun` and the trailer whose counts af-impl now fills.
- `aflib/issuex`, for the listing, the labels and the comments; the `issue`
  command, which the sessions use to list, comment on, relabel and close.
- `afspec`, for whether a package is finished.
- The af-impl skill (`skills/af-impl`), whose text changes.

## Glossary

- **finished package**: one that is sealed, superseded or archived, or has
  no task pending or in progress; what `speca --next` treats as done.
- **passing on**: relabelling an `awaits` item to the later package that now
  owns it.
- **stop rule**: the check that ends the loop after a package opened a
  defect of a severity in `--stop-on`.
