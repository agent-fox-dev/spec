---
spec_id: "26"
spec_name: "package_handoffs"
title: "Declare handoffs between packages and check their direction"
status: "active"
created_at: "2026-11-12T19:45:00Z"
intent_hash: "aee98f739f3ed0d85f3386418a4c431053f00927a86d9cd78ef14e5feebeb0ad"
schema_version: 3
source: "docs/prd/07-keep-the-implementation-loop-fast-and-close-its-open-items.md"
---

# Declare handoffs between packages and check their direction
## Intent

A package often leaves an extension point for another package to fill: a
hook to register into, an entry point to call. Today that handoff exists
only as prose in a PRD or a task, and no tool reads prose. So packages have
handed work to packages that land before them, where nothing could ever wire
it; receiving packages have landed without knowing they were expected to
wire anything; and the integration task, which fails any path that is not
live, was bypassed because it had no way to tell a gap from a part left
unwired on purpose. A handoff becomes data in the format: the sending
package declares what it leaves, to whom, and which criteria depend on it;
the receiving package's tasks say which handoffs they take. `speca` refuses
a handoff to a package that lands earlier and warns when the receiver does
not take it, and the integration task tells a declared handoff from a gap.

## Goals

- **Cross-package wiring is declared.** `requirements.json` gets an optional
  `handoffs` array (`what`, `to`, `criteria`), and a task an optional
  `takes_handoffs` list.
- **A handoff goes to a package that lands later.** `speca --validate`
  refuses a handoff whose `to` is unknown, is the package itself, or does
  not depend, directly or transitively, on the sending package.
- **The receiver is told.** Validating the receiving package warns for each
  inbound handoff no task takes; `--strict-handoffs` makes each an error,
  and `specrun --strict-handoffs` asks for that before it starts a package.
- **The coder sees what to wire.** `specr <package> --task <N>` renders a
  task's handoffs with the sending package's text.
- **The integration task tells a declared handoff from a gap.** A step that
  is unwired only because a declared outbound handoff is not yet taken does
  not fail the task and is filed as `awaits`; the receiver's integration
  task confirms that production code wires each inbound handoff.

## Non-goals

- **Changing packages already written.** Spec roots written before handoffs
  existed still validate; both fields are optional.
- **A stored strictness setting.** `--strict-handoffs` stores nothing; the
  caller decides each time.
- **Handoffs to an archived package.** A package under `archive/` has
  landed; a handoff to it is the wrong direction by definition.
- **Any other change to the v3 format.**

## Background

Dependencies in the format point only backwards: `tasks.json`
`dependencies` names the packages a package builds on, and
`afspec.BuildDependencyGraph` and `afspec.ValidateCrossSpec` check them
(an unknown dependency, a cycle, no shared actor). A handoff forwards has no
field. On the measured repository a package's `tasks.json` said "package 24
registers its landing entry point, package 16 its fork activity hook"; both
had landed before it, so nothing would ever wire them.

`skills/af-impl/references/test-first.md` says that in the integration task
"a path that is not live in production code fails this task, and a recorded
decision or a deferral does not satisfy it". In practice a package with an
extension point lands anyway, through the unmet list, with an issue that
says "lacks production callers".

The integration task is derived by `afspec` (`Spec.IntegrationTask`,
`afspec/task.go`); `specr <package> --task N+1` renders every path and a
`## Contracts` section (`afspec/render.go`). `speca --validate` gives the
verdict through `internal/specpkg.Verdict`, which reads every package of the
spec root for the cross-spec rules. `21_open_item_labels` defines the
`awaits` kind and the `af:awaits-<NN>` label.

## Requirements

### 1. A `handoffs` field

`requirements.json` gets an optional `handoffs` array. Each entry has an
`id` (`H-1`, `H-2`, in order), and names:

- `what`: the extension point or entry point that is left for another
  package (`internal/land` after-landing hooks, `change.Stacks.OnLanded`);
- `to`: the receiving package's id;
- `criteria`: the criteria of this package that depend on the handoff being
  wired.

A task may list the handoffs it takes in `takes_handoffs`, each as the
sending package's id and the handoff's id (`"24:H-1"`). af-spec and the
`spec` generator write a handoff wherever the PRD says that another package
will call, register or fill something this package offers.

### 2. A handoff goes to a package that lands later

`speca --validate` refuses a package with a handoff whose `to` is unknown,
is the package itself, or does not depend, directly or transitively, on
this package. In dependency order such a package can land first, and then
nothing is left to wire it. It also refuses a `takes_handoffs` entry that
names no handoff, or one that is addressed to another package.

### 3. The receiving package takes the handoff

`speca --validate` on the receiving package warns for each inbound handoff,
found by reading the other packages under the spec root, that no task
takes. The warning names the sending package and the handoff. By default a
warning does not make the package invalid.
`speca --validate --strict-handoffs` turns each such warning into an error.
The flag stores nothing.

`specrun --strict-handoffs` validates the next package that way before it
starts its session, and stops when the package is not valid.

### 4. The renders

`specr <package>` and the package's `spec.md` list the package's own
handoffs with the requirements, and each task's `takes_handoffs`.
`specr <package> --task <N>` renders the handoffs the task takes with the
sending package's `what` and the text of the criteria that depend on it, so
the coder sees what to wire. The integration render lists each inbound
handoff in its `## Contracts` section, and each outbound one.

### 5. The integration task

For a package with outbound handoffs, the derived integration task says
that a step unwired only because a declared outbound handoff is not yet
taken does not fail the task, is filed as `awaits` labelled with the
handoff's `to`, and has the criteria it lists covered by a test that calls
the entry point directly. Every other path that is not live still fails the
task. For a package with inbound handoffs, the task confirms that
production code wires each one, by `file:line`; a handoff the package takes
but does not wire fails the task.

## Design Decisions

1. **A handoff is format data, checked by `speca`** (PRD decision 8). The
   check that matters, "the receiver lands later", is a graph question
   `afspec` already answers. An `x-` key could carry the data, but the
   format gives `x-` keys no meaning.
2. **An untaken handoff is a warning, and an error only on request** (PRD
   decision 12). Spec roots written before handoffs existed must still
   validate.
3. **A handoff has an id, `H-N`.** `takes_handoffs` (`"24:H-1"`) needs one
   to point at; it follows the `REQ-N` and `PATH-N` ids.
4. **`spec.md` holds only the package's own data.** The committed render is
   compared with the package on every validation; if it carried the sending
   package's text, an edit to one package would make another one's render
   stale. `specr` adds the sender's text when it renders a task or the
   integration task.
5. **The direction rule is a cross-spec rule.** It needs the other packages,
   like the unknown dependency and the cycle, and is reported for the
   sending package.
6. **`specrun` validates only under `--strict-handoffs`.** af-impl already
   refuses a package that does not validate; the flag adds the one question
   af-impl does not ask.
7. **The `schema_version` stays 3.** Both fields are optional additions,
   and a reader that does not know them is a reader of an older schema
   copy, which the schema's closed objects already refuse.

## Dependencies

- `21_open_item_labels` (active): the `awaits` kind and `af:awaits-<NN>`
  label an untaken outbound handoff is filed with.
- `20_specrun_driver` (active): `specrun`, which gains `--strict-handoffs`.
- The v3 format (ADR 19, ADR 11 for the cross-spec rules): additions to
  `requirements.v3.json`, `tasks.v3.json` and
  `specification/spec-format-v3.md`.
- The af-spec and af-impl skills and the `spec` generator's prompts
  (`specgen/templates`).

## Glossary

- **handoff**: something a package offers and leaves for a named later
  package to call, register into or fill.
- **outbound**, **inbound**: a handoff seen from the package that declares
  it, and from the package it is addressed to.
- **taking** a handoff: a task of the receiving package listing it in
  `takes_handoffs`.
