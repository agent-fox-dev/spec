---
spec_id: "23"
spec_name: "specrun_reconcile"
title: "Reconcile every open item against the base branch every few packages"
status: "active"
created_at: "2026-11-12T18:45:00Z"
intent_hash: "b70d88731a2d7befc8f202a2bac8eef7c205bfbd4eaf87e7e0d1be856b0981fa"
schema_version: 3
source: "docs/prd/07-keep-the-implementation-loop-fast-and-close-its-open-items.md"
---

# Reconcile every open item against the base branch every few packages
## Intent

Closing an item in the run that resolves it is the main way open items
close, but it misses things: an item resolved in passing by a package that
never listed it, the issues opened before any of this existed, and the
deviations that wait for a person. Every few packages, and once at the end,
the loop reconciles every open item against the base branch. A session does
the part that needs judgement: it classifies the older issues and decides
whether each closing condition now holds. `specrun` does the rest by rule: it
chooses which issues the session may adopt, settles the deviations a person
has labelled, and checks what the session left. The list of open items stays
at most a few hours out of date, and every item on it has a path to closed.

## Goals

- **A reconcile every few packages.** After every `--reconcile-every`
  (default 5) landed packages, and once more when every package is done,
  `specrun` runs a reconcile step.
- **Older issues are adopted.** An open issue with no `af:` label whose
  title starts with `<NN_name>:` of a package under the spec root is handed
  to the session, which gives it a kind, its origin, the package it awaits
  and a severity. No other issue is ever handed over.
- **Resolved items are closed.** For every open `af:*` issue the session
  decides whether the condition in its "What closes it" holds on the base
  branch, and closes those that hold, with evidence. It changes no code.
- **A person settles a deviation with one label.** `af:accepted` closes it
  at the next reconcile; `af:rejected` turns it into a `correctness` defect.
  A deviation with neither stays open and is listed, oldest first.
- **The session is checked.** After it, `specrun` verifies that the
  repository is as it was and applies every label rule again.

## Non-goals

- **Repairs.** The repair queue that is built after each reconcile, and the
  repair sessions, are `specrun_repair`. Until then a rejected deviation
  becomes a defect that waits.
- **Editing a spec.** Accepting a deviation edits no package: the ADR and
  the closed issue are the record.
- **Touching a person's issues.** An issue whose title does not match a
  package is never handed to a session, labelled, commented on or closed.

## Background

`22_open_items_read_back` has the run that resolves an item close it, and
`specrun` relabel what awaits a finished package. What still slips through,
from the measured repository: 15 issues resolved in part by later packages,
which no single run could close; 46 issues opened before kinds existed, all
titled `<NN_name>: <what is missing>` with a body that says what is missing,
why, the evidence and what closes it; and deviations, which are decided and
recorded in an ADR but need a person's acceptance.

`specrun` (`20_specrun_driver`) has one kind of step, implementing a
package, and a state file with the steps done and the step running. Its
label rules are functions from the issues to a list of changes
(`21_open_item_labels`). af-impl takes a package as its argument and has no
other mode.

## Requirements

### 1. When a reconcile runs

A reconcile is a step of the loop. It runs after every `--reconcile-every`
landed packages (default 5), and once more when every package is done,
unless a reconcile was the last step and no package landed since. The count
of packages since the last reconcile is in the state file, so it survives a
restart. `--reconcile-every 0` leaves only the reconcile at the end. A
reconcile step recorded as running when `specrun` starts is run again.

### 2. What `specrun` hands to the session

Before the session, `specrun` selects the issues to adopt: open issues that
carry no label starting with `af:` and whose title starts with `<NN_name>:`,
where `NN_name` is the directory name of a package under the spec root or
its archive. It writes them (number, title, URL, the package) to
`<step>-adopt.json` in the log directory and starts
`claude -p "/af-impl --reconcile <adopt file> --report <report file>"` with
the flags of every session. The report file is `<step>-report.md` in the log
directory. The stall rule applies.

### 3. What the session does (af-impl text)

`/af-impl --reconcile` does only what needs a model.

**Adopting.** For each issue in the adopt file it reads the body and
assigns its kind, `af:from-<NN>`, `af:awaits-<NN>` when it awaits a package,
and a severity when it is a defect, adding those labels with a comment
saying the issue was adopted and why it got that kind. An issue whose kind
the body does not settle is labelled `af:deviation`, so a person decides it.
It adopts no issue that is not in the file.

**Checking closing conditions.** For every open `af:*` issue it decides
whether the condition in "What closes it" holds on the base branch, and
closes those that hold, with evidence (`file:line`, a test). It changes no
code, creates no branch and commits nothing.

**The report.** It writes Markdown to the report file: what it adopted, what
it closed, and the rest by kind and severity, deviations oldest first.

### 4. What `specrun` does after the session

It checks that the base branch is checked out, the tree is clean and `HEAD`
is where it was; a session that changed the repository stops the loop. A
missing report is a warning. Then, by rule, each change with a comment that
names the rule and the commit of the base branch:

- **Unsettled adoptions.** An issue from the adopt file that is still open
  and still has no kind gets `af:deviation` and `af:from-<NN>`.
- **The label rules** of the earlier packages run again over every open
  `af:` issue.
- **Accepted deviations.** An open issue with `af:deviation` and
  `af:accepted` is closed, with a comment naming the ADR the issue cites.
- **Rejected deviations.** One with `af:deviation` and `af:rejected` loses
  both labels and gets `af:defect` and `af:severity-correctness`, with a
  comment saying a person rejected the deviation.
- An issue with both `af:accepted` and `af:rejected` is left alone, with a
  warning.

The ledger line of a reconcile holds the numbers adopted, closed and
settled; the result and stderr list the open deviations with neither label,
oldest first.

### 5. The dry run

`specrun --dry-run` says when the next step is a reconcile, lists the
issues it would hand over for adoption, and adds the deviation changes to
what the label rules would change.

## Design Decisions

1. **A model runs only where judgement is needed** (PRD decision 22).
   Selecting by title, settling labelled deviations and checking the
   session follow from labels and git; classifying an issue from its prose
   and deciding whether a closing condition holds do not.
2. **Older issues are adopted by their title, and only by their title** (PRD
   decision 20). The title keeps the reconcile away from every issue a
   person opened; `deviation` puts an unclear one in front of a person.
3. **A person settles a deviation where it is tracked, with a label** (PRD
   decision 19). Accepting needs no spec edit; rejecting makes an ordinary
   defect.
4. **Deviations are listed, not stopped on** (PRD decision 13).
5. **Reconcile every 5 packages by default** (PRD decision 15): five
   packages took 4 to 6 hours in the measured run.
6. **The adopt list and the report path are arguments of the session.** The
   PRD's `/af-impl --reconcile` leaves open how the session learns which
   issues `specrun` selected; a file it is told to read is checked the same
   way every time, and a report at a path `specrun` chose can be found.
7. **`specrun` labels what the session left unsettled.** The PRD has the
   session label an unclear issue `af:deviation`; a session that skipped one
   would leave it invisible, so the rule runs after it too.
8. **The reconcile session must not move `HEAD`.** It changes no code, so a
   commit on the base branch is a session that did something else.
9. **Both `af:accepted` and `af:rejected` is nobody's decision.** The issue
   stays as it is and the warning names it.
10. **No final reconcile right after a reconcile.** When the last package
    happens to land on a multiple of `--reconcile-every`, one reconcile
    serves as both.
11. **`--reconcile-every 0` keeps the final reconcile.** Nothing in this
    plan is switched off entirely by a default or by zero.

## Dependencies

- `22_open_items_read_back` (active): the label rules that run again, the
  closing of items by af-impl and the dry run's `would_change`.
- `aflib/issuex` for listing, labelling, commenting and closing
  (`Client.CloseIssue(ctx, ref IssueRef, comment string) error`, read in a
  checkout at commit `6b70ef7`); the `issue` command, which the session
  uses.
- The af-impl skill (`skills/af-impl`), which gains the `--reconcile` mode.

## Glossary

- **reconcile**: one step of the loop that brings every open item in line
  with the base branch: a session for the judgement, rules for the rest.
- **adopting**: giving an issue opened before the label scheme its kind and
  labels.
- **settling**: a person deciding a deviation with `af:accepted` or
  `af:rejected`.
