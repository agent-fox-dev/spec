# 24. Declare handoffs between packages

## Status

Accepted

## Context

A package often leaves an extension point for another package to fill: a
hook to register into, an entry point to call. That handoff existed only as
prose in a PRD or a task, which no tool reads. Packages handed work to
packages that had landed before them, where nothing could ever wire it;
receivers landed without knowing they were expected to wire anything; and the
integration task, which fails any path that is not live, was bypassed through
the unmet list because it could not tell a gap from a part left unwired on
purpose.

## Decision

A handoff is data in the version 3 format, checked by the tools.

- `requirements.json` holds an optional `handoffs` array: `{id: "H-N", what,
  to, criteria}`. A task holds an optional `takes_handoffs` list of
  `"<spec id>:H-N"`. Both are optional, and the `schema_version` stays 3.
- The direction is a cross-spec rule, `cross_spec_handoff_direction`, an
  error of the sender: `to` must name a package of the spec root, not the
  package itself, that depends on the sender directly or transitively. In
  dependency order any other package can land first. A package under
  `archive/` has landed, so a handoff to one is the wrong direction.
- A take must name a handoff its package declares to the taker
  (`cross_spec_handoff_taken`, an error of the taker).
- An inbound handoff no task takes is the warning `handoff_not_taken`, and an
  error only on request: `speca --validate --strict-handoffs`, and
  `specrun --strict-handoffs` before it starts a package. Spec roots written
  before handoffs must still validate, and the flag stores nothing.
- `spec.md` holds only the package's own data, its handoffs and its takes:
  the committed render is compared with the package on every validation, and
  another package's text in it would make it stale whenever that package
  changed. `specr --task` adds the sender's text (`afspec.InboundHandoffs`)
  when it renders a task or the integration task.
- The derived integration task gains one step per outbound handoff: a path
  step unwired only because that handoff is not yet taken does not fail the
  task, is filed as `awaits` for the receiver, and has its criteria covered by
  a test that calls the entry point directly. The rendered integration task of
  a receiver confirms by `file:line` that production code wires each inbound
  handoff.
- The verdict `specrun` needs is `afspec.ValidateInRoot`, which runs the
  package's rules and the cross-spec rules that concern it over the spec
  root; `internal/specpkg` and `speca` share its cross-spec part
  (`afspec.CrossSpecFindings`), so the loop imports neither command.

## Consequences

- A handoff in the wrong direction is caught when the sending package is
  written, not when the receiver lands.
- A receiver's coder sees what to wire in the task's render, with the
  sender's criteria.
- The integration task no longer has to be bypassed for a declared
  extension point; every other path that is not live still fails it.
- A spec root that names handoffs without strict checking can still land a
  receiver that does not take one; the warning says so on every verdict.
