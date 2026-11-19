# Open items and their labels

An **open item** is something a package's implementation did not finish: a
gap between what its spec asks and what landed on the base branch. Every open
item becomes an issue on the forge (GitHub or GitLab), and its `af:` labels
say what kind of gap it is, where it came from and what happens to it next.
`specrun` decides what the loop does from those labels alone, so a label is
both a description and an instruction. A person steers the loop by changing
labels on the issue.

This page explains every label, who sets it and how an item labelled with it
is resolved. The mechanics of each rule (state, comments, ledger, dry run)
are in [specrun.md](specrun.md); the decisions are
[PRD 07](prd/07-keep-the-implementation-loop-fast-and-close-its-open-items.md),
§3 and §5.

## Where open items come from

- **A landing.** Before `/af-impl` merges a package, its conformance pass
  lists everything the branch does not do or could not show. A fault found in
  the branch's own code is fixed there, not filed; what remains is the
  `unmet` list, and each entry gets one issue, titled
  `<NN_name>: <what is missing>`. Its body says what is missing and why, the
  criteria it concerns, the evidence (`file:line`, a check's output), the ADR
  that records the decision when there is one, and **what closes it**.
- **Adoption.** An issue opened before the label scheme existed, with no
  `af:` label and a title that starts with a package's `NN_name:`, is handed
  to the next reconcile session, which reads its body and labels it.
- **A rule of `specrun`.** The rules below relabel an item whose labels are
  incomplete, whose awaited package has finished, or that a person settled.

An issue whose title does not start with a package's `NN_name:` and that
carries no `af:` label is a person's issue: no session or rule ever labels,
comments on or closes it.

## A complete set of labels

Every open item carries:

| Label | Required |
|---|---|
| exactly one kind: `af:awaits`, `af:defect`, `af:untested` or `af:deviation` | always |
| `af:from-<NN>`, the package that opened it | always |
| `af:awaits-<MM>`, the package that must take it up | for `af:awaits` |
| exactly one `af:severity-<s>` | for `af:defect` |

`af:accepted`, `af:rejected` and `af:repair-failed` are added later, by a
person or a repair session.

An item that lacks any required label, or carries two kinds, is unclear, and
an unclear item goes to a person: the rule `incomplete-labels` removes its
other kind labels, makes it `af:deviation`, adds `af:from-<NN>` when it is
missing, and comments, naming what was missing. `specrun` applies this rule
after every landing and at every reconcile. A person who relabels an item by
hand therefore changes the kind in one edit: adding `af:defect` without
removing `af:deviation` leaves two kinds, and the rule turns the item back
into a deviation.

## Kinds

The kind says whose move it is.

### `af:awaits` — a later package must take it up

The branch offers something (an exported function, an endpoint, an event)
that a named later package must use or wire. It is not a fault of the package
that opened it. `af:awaits-<MM>` names the receiving package, and the body's
"What closes it" names it by id.

How it is resolved:

1. When package `MM` is implemented, its survey lists every item labelled
   `af:awaits-<MM>` and maps each to the task that satisfies it, or records
   why none will.
2. When `MM` lands, each item its branch resolved is closed, with the
   production caller and the test as evidence.
3. An item `MM` did not resolve is passed on: relabelled `af:awaits-<later>`
   when a later package owns it, or turned into a `correctness` defect when
   none does.

**Wiring nobody owns is a defect.** An item can never await a package that
has already finished: `/af-impl` files it as a `correctness` defect from the
start ("handoff to a package that has already landed"), and the rule
`awaits-finished` turns an open `af:awaits` item into one once every package
it awaits is finished, or when it names none.

A person rarely needs to act on an `awaits` item. To hand it to another
package, replace its `af:awaits-<MM>` label.

### `af:defect` — the delivered code is wrong

The code a branch delivered behaves differently from what its spec asks, or
loses data, races or exposes something, and it could not be fixed on the
branch: the fix was tried and the gate failed, the fix needs a person's
decision or belongs to code outside the branch, or it is a flaky test of code
the branch did not touch. Every defect carries one severity (below), and the
severity decides whether the loop stops for it and whether it is repaired
automatically.

How it is resolved: a repair. After every reconcile, `specrun` queues the open
defects whose severity is in `--repair-on` (by default every severity), most
severe first and then oldest first, and runs one `/af-impl --repair` session
per issue: it reproduces the fault with a failing test, fixes it on a branch
of its own, merges and closes the issue. A repair that cannot reproduce or fix
the fault in two attempts marks the issue `af:repair-failed` instead. A
defect closed by a person, or by a later package's branch, is resolved too.

### `af:untested` — it works, but nothing proves it

The behaviour is believed to work, but a criterion of the spec has no test
that asserts it.

How it is resolved: a repair, when `untested` is in `--repair-on` (it is by
default). The repair session writes the missing test and runs it on the
unchanged code:

- the test passes: the behaviour was right, the test is committed and merged,
  and the issue is closed;
- the test fails: the behaviour was wrong, so the issue becomes a
  `correctness` defect, with the failure in a comment, and the same session
  repairs it.

### `af:deviation` — a person must decide

The code deliberately does something other than what the spec says, and the
decision is recorded in an ADR. The implementing session made the call; a
person must approve or overrule it. A deviation never stops the loop and is
never repaired automatically: it stays open, and every reconcile lists the
unsettled deviations, oldest first, in `result.open_deviations` and on
stderr.

A deviation that is recorded in an ADR and still leaves every criterion met is
not an open item: it is reported with the landing and gets no issue.

`af:deviation` is also where an **unclear** item ends up: one adopted from
before the label scheme whose body does not settle its kind (rule
`adoption-unsettled`), or one with incomplete labels (rule
`incomplete-labels`). Such an item usually cites no ADR; its comment names
the rule that put it there.

How it is resolved: a person reads the issue and the ADR it cites, then adds
**one** label. The next reconcile does the rest:

| The person adds | Meaning | At the next reconcile |
|---|---|---|
| `af:accepted` | The deviation stands. | The issue is closed, with a comment naming the ADR it cites (or saying it cites none). The spec is not edited: the ADR and the closed issue are the record. |
| `af:rejected` | The code must do what the spec says. | `af:deviation` and `af:rejected` are removed, `af:defect` and `af:severity-correctness` added, with a comment. From then on it is a defect like any other and is repaired as one. |

An issue with both labels is nobody's decision: it is left as it is, with a
`deviation_conflict` warning, until one of the two is removed.

For an unclear item that is not really a deviation, the person gives it its
real kind instead: remove `af:deviation` and add the right kind, with
`af:awaits-<MM>` or `af:severity-<s>` where that kind needs one, in the same
edit.

Do not fix a rejected deviation by hand while its label says otherwise: label
it, and let the reconcile and the repair close it, so that the issue, the
ledger and the base branch agree.

## Severities

A `defect` carries exactly one severity. It says how much harm the fault
does, and it sets how `specrun` treats the defect:

| Label | The fault | Stops the loop by default (`--stop-on`) | Repaired automatically |
|---|---|---|---|
| `af:severity-security` | exposes something it must not: data, credentials, access | yes | only with `--repair-security`; otherwise left to a person |
| `af:severity-data-loss` | loses or corrupts data | yes | yes |
| `af:severity-correctness` | gives a wrong result or behaves differently from the spec, a race included; also every rejected deviation, every untested item whose new test fails, and every handoff nobody owns | no | yes |
| `af:severity-minor` | harms no result, for example a test suite that has grown past its time budget (`test-cost`) | no | yes |

When a package lands with an open defect whose severity is in `--stop-on`,
the loop stops after it (stage `defect`, reason `serious defect`), and
starting `specrun` again stops at the same place. To go on, a person closes
the issue, changes its severity label, or runs with a `--stop-on` that no
longer holds it. With `--repair`, such defects are repaired before the next
package instead, short of a security defect without `--repair-security`.

## Origin and destination

| Label | Meaning |
|---|---|
| `af:from-<NN>` | The package that opened the item. It is how `specrun` finds a package's items after it lands, and how a repair session finds the spec and code the defect concerns. |
| `af:awaits-<MM>` | For an `af:awaits` item, the package that must take it up. An item may await more than one; it becomes a defect only when every one of them is finished. |

`specrun` creates both for every package under the spec root and its
`archive/` before its first step.

## Decisions and outcomes

| Label | Added by | Meaning |
|---|---|---|
| `af:accepted` | a person | Settles a deviation: it stands. See [`af:deviation`](#afdeviation--a-person-must-decide). |
| `af:rejected` | a person | Settles a deviation: the code must follow the spec. See [`af:deviation`](#afdeviation--a-person-must-decide). |
| `af:repair-failed` | a repair session | The repair could not reproduce or fix the fault; a comment says what was tried and what each attempt returned. The issue stays open and is never queued for repair again. |

An open item with `af:repair-failed` is a person's. They fix it by hand and
close the issue, or remove `af:repair-failed` to let the next reconcile queue
it again, for example after correcting the issue's body. When its severity is
in `--stop-on`, the loop stops on it (reason `repair failed`) until then.

## When an item is closed

An item is closed, always with a comment giving the evidence, when:

- a package's branch resolves it, and the landing closes it after the merge;
- a reconcile session finds that the condition in its "What closes it" holds
  on the base branch, with the production caller and the test as evidence;
- a repair fixes it, or finds it already fixed;
- a person settles a deviation with `af:accepted`;
- a person closes it.

A closed issue is never relabelled by any rule.
