---
spec_id: "21"
spec_name: "open_item_labels"
title: "Give every open item a kind and labels a program can read"
status: "active"
created_at: "2026-11-12T18:05:00Z"
intent_hash: "9108433f6e169eecd3498d37548611bd2d6dd3fa937f66cd4d71cd2d5eecd22c"
schema_version: 3
source: "docs/prd/07-keep-the-implementation-loop-fast-and-close-its-open-items.md"
---

# Give every open item a kind and labels a program can read
## Intent

af-impl opens an issue for every open item a package leaves, and then nobody
reads it: an extension point waiting for a later package, a data-loss defect
and a criterion without a test all look the same in the list, and none says
who must close it. Every open item gets a kind, the package that opened it,
the package that must close it when it awaits one, and a severity when it is
a defect, as labels a program can filter on. `specrun` keeps the label scheme
in place and checks, after every landing, that the issues the session opened
carry it. The merge commit says in one machine-readable line what the run
measured and what it left open, so the ledger records it without parsing
prose.

## Goals

- **Classified open items.** Every open item is filed with one kind
  (`awaits`, `defect`, `untested`, `deviation`), the package that opened it,
  for `awaits` the package that must close it, and for `defect` a severity
  (`security`, `data-loss`, `correctness`, `minor`).
- **The labels exist before they are needed.** `specrun` creates every label
  of the scheme the repository lacks before its first step.
- **An issue without its labels reaches a person.** After every landing,
  `specrun` gives an issue of that package that lacks a kind, its origin or a
  defect's severity the kind `deviation`, with a comment.
- **Counts by kind.** The report and the merge commit say how many open items
  the package opened, per kind and severity, with the issue links.
- **A machine-readable trailer.** The merge commit ends with an `AF-Run:`
  trailer, one line of JSON with the gate counts and the open-item counts,
  and the ledger line of the package holds them.

## Non-goals

Left to the later scopes of
`docs/prd/07-keep-the-implementation-loop-fast-and-close-its-open-items.md`:

- **Reading the items back.** The survey listing them, landing closing what
  the branch resolved, passing on, the no-dead-handoff rule and stopping on
  serious defects (`open_items_read_back`). Until then the counts of closed,
  passed-on and reclassified items in the trailer are zero.
- **Settling deviations and adopting older issues** (`specrun_reconcile`),
  and **repairs** (`specrun_repair`). This package creates their labels
  (`af:accepted`, `af:rejected`, `af:repair-failed`) and nothing more.
- **Declared handoffs** (`package_handoffs`).

## Background

`skills/af-impl/references/conformance.md` builds the `unmet` list: every
open item, with what is missing, why, the ids it concerns and its evidence.
`references/landing.md` opens one issue per entry with
`issue --create --title "<NN_name>: <what is missing>"`, with no label.
Over 25 packages on one repository that made 46 issues, none ever closed:
13 defects (one security, two data loss) sat among "no production caller
yet", and nothing could tell them apart without reading every body.

The `issue` command (`aflib/cmd/issue`) supports `--create --label`,
`--create-label`, `--list --label` and `--edit --add-label/--remove-label`.
`aflib/issuex` is the Go client it is built on. `specrun`
(`20_specrun_driver`) has no forge access yet; its ledger line holds a
step's kind, subject, times and outcome. `speca <package> --gate-report`
(`19_speca_gate`) gives the gate counts of a package's run.

## Requirements

### 1. Kinds (af-impl text)

The conformance pass assigns each entry of the `unmet` list exactly one kind:

- `awaits`: the branch offers something that a named later package must use
  or wire. It is not a fault of this package.
- `defect`: code this branch delivered behaves differently from what its
  spec asks, or loses data, races, or exposes something. It carries a
  severity: `security`, `data-loss`, `correctness` or `minor`.
- `untested`: the behaviour is believed to work, but a criterion has no test
  that asserts it.
- `deviation`: the code deliberately does something other than the spec,
  recorded in an ADR, and a person must accept it or change the spec.

### 2. Labels (af-impl text, and `specrun`)

The issue for an open item carries `af:<kind>`; `af:from-<NN>`, the package
that opened it; for `awaits`, `af:awaits-<NN>`, the receiving package; for
`defect`, `af:severity-<s>`. The body's "What closes it" section stays, and
for `awaits` it names the receiving package by id. af-impl run on its own
creates a missing label on first use, with `issue --create-label`.

Before its first step `specrun` creates every label of the scheme that the
repository lacks: the four kinds, the four severities, `af:accepted`,
`af:rejected`, `af:repair-failed`, and `af:from-<NN>` and `af:awaits-<NN>`
for every package under the spec root, the archive included. A label that
exists is left as it is. `specrun` reaches the forge through `aflib/issuex`,
for the repository of the origin remote, with the token the `issue` command
uses. Without a forge client (no token, no origin remote, a forge that is
not supported) it refuses to start; `--dry-run` goes on, with a warning, and
writes nothing to the forge.

### 3. The check after every landing (`specrun`)

After a package lands, `specrun` lists the open issues of that package: the
ones labelled `af:from-<NN>`, and the ones whose title starts with
`<NN_name>:`. Each must carry exactly one kind, `af:from-<NN>`, for a
`defect` exactly one severity, and for `awaits` one `af:awaits-<MM>`. An
issue that lacks any of these gets `af:deviation` as its only kind, gets
`af:from-<NN>` when it lacks it, and a comment, so a person decides it.

Every change `specrun` makes to an issue carries a comment that says which
rule made it and on which commit of the base branch. The rule changes
nothing on a second run. A forge call that fails stops the loop: the rules
after it would decide on labels that are not known.

### 4. Counts and the trailer (af-impl text, and `specrun`)

The report and the merge commit say how many open items the package opened,
closed, passed on and reclassified, per kind and severity, with the issue
links. The merge commit ends with an `AF-Run:` trailer: one line of JSON
holding

- `gate`: `full` and `task`, each with `runs`, `seconds` and `reused`, and
  `baseline_full_seconds` and `last_full_seconds`, copied from
  `speca <package> --gate-report`;
- `open_items`: `opened`, `closed`, `passed_on` and `reclassified`, each an
  object of counts keyed by kind, a defect keyed `defect:<severity>`.

After a landing `specrun` reads the trailer from the merge commit and puts
both objects into the package's ledger line; the short form it prints holds
the full gate runs and seconds, the reused gates and the items opened. A
merge commit with no trailer, or one that is not JSON, is a warning: the
package still counts as landed, and its ledger line holds neither object.

## Design Decisions

1. **Classify at creation, by the run that knows** (PRD decision 6). The
   conformance reviewer has the evidence when it builds the unmet list;
   labels let `specrun` and the next survey filter without reading bodies.
2. **`specrun` checks its sessions and does not trust them** (PRD decision
   25): the label check reads the forge, not the session's final message.
3. **The forge is required from this package on.** The label rules decide
   what the loop does next; a loop that cannot read labels would build on
   defects it cannot see. The sessions need the same token to open issues.
4. **A label that exists is found by creating it.** `issuex` has no call
   that lists labels; `CreateLabel` answers a conflict for one that exists,
   and that is not an error.
5. **The package's issues are found by label or by title.** An issue that
   lacks every label is exactly the one the check exists for, and af-impl
   has always titled an open item `<NN_name>: <what is missing>`.
6. **An incomplete issue becomes a deviation, with its other kind removed.**
   One kind per issue is what every later rule filters on; `deviation` is
   the kind a person settles.
7. **A failed forge call stops the loop.** Every rule is safe to apply
   again, so the next start repeats it.
8. **The trailer's keys are fixed here**, since af-impl writes it and
   `specrun` reads it. A key `specrun` does not know is ignored, so later
   packages can add to it.
9. **The labels of the later scopes are created now.** One place owns the
   scheme; the rules that read them come with their packages.

## Dependencies

- `20_specrun_driver` (active): the loop, the landing checks and the ledger
  this package adds to.
- `19_speca_gate` (active): `speca --gate-report`, whose counts the trailer
  carries.
- `aflib/issuex` (`github.com/agent-fox-dev/aflib`), for every forge
  operation.
- The `issue` command from `aflib`, which the sessions use.
- The af-impl skill (`skills/af-impl`), whose text changes.

## Verified External API

`github.com/agent-fox-dev/aflib/issuex`, read with `go doc` in a checkout at
commit `6b70ef7`:

- `NewWithOptions(o Options) (Client, error)`, with `Options.Dir`,
  `Options.Repo`, `Options.UserAgent`; `NewNoOp() Client`;
  `DetectRepo(dir string) (Repo, bool)`.
- `Client.ListIssues(ctx, repo Repo, filter IssueFilter) (IssueList, error)`,
  with `IssueFilter{State, Labels, Sort, Direction, Limit}` and
  `IssueList{Issues, Incomplete}`.
- `Client.AddLabels(ctx, ref IssueRef, labels []string) error`,
  `Client.RemoveLabel(ctx, ref IssueRef, label string) error`,
  `Client.CreateLabel(ctx, repo Repo, label Label) error`,
  `Client.AddComment(ctx, ref IssueRef, body string) (string, error)`,
  `Client.Authenticated() bool`.
- `Issue{Number, Title, Body, State, Labels, CreatedAt, HTMLURL}`,
  `IssueRef{Repo, Number}`, `Label{Name, Color, Description}`;
  `IsConflict(err) bool`, `IsNoToken(err) bool`.

## Glossary

- **open item**: an entry of af-impl's `unmet` list: what the branch does not
  do that the spec asks, or what the run could not show.
- **kind**: one of `awaits`, `defect`, `untested`, `deviation`.
- **label scheme**: every `af:` label this plan uses.
- **trailer**: the `AF-Run:` line at the end of a package's merge commit.
