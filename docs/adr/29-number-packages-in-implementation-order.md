# 29. Number packages in implementation order

**Status:** accepted
**Date:** 2026-11-15
**Related:** [ADR 11](11-validate-a-package-against-the-spec-root.md), [ADR 24](24-declare-handoffs-between-packages.md), [ADR 28](28-size-a-package-by-the-time-it-takes-to-land.md)

## Context

A package's number is assigned when it is written: the highest number under
the spec root and `archive/`, plus one. Nothing said what the numbers mean
beyond that. `speca --next` and `specrun` pick the next package by the
`dependencies` of `tasks.json`, with ties broken by number, so the numbers
were the implementation order only where the dependencies happened to agree
with them.

Two things could break that agreement. The PRD phase of `spec` reported the
scopes of a split "first one first", with only the first said to be
foundational, so a later scope could be listed ahead of one it builds on. And
a package written in a session with `af-spec` could name any package of the
spec root as a dependency, one with a higher number included. The validator
accepted both as long as the graph had no cycle.

A reader then cannot take the listing of the spec root as the plan of work,
and a package that depends forward waits on work nobody sees coming when
reading in order. The ten live packages of this repository, `19` to `28`,
all depend on lower numbers only.

## Decision

- **The numbers are the implementation order, near enough.** A package is
  numbered after everything it builds on. Ties and independent packages are
  free: the order is rough, not total.
- **A dependency SHOULD name a package with a lower number.** One that names
  a higher number is the cross-spec warning `cross_spec_dependency_order`,
  of the package that declares it, on `tasks.json`
  (`afspec.CheckDependencyOrder`). `speca --validate` and `spec` both report
  it, as they report every cross-spec finding.
- **A split is ordered by implementation.** The PRD phase of `spec` reports
  the scopes in the order they are to be implemented, each building only on
  the scopes before it and on the specs that exist; a follow-on scope's PRD
  and its review are told never to depend on a later scope. `af-spec`
  proposes a split in the same order and recommends writing the first scope
  not yet written.
- **It is a warning, not an error.** The dependency graph decides what can
  be implemented when, and a forward edge with no cycle still lands. An
  active package is closed to edits and cannot be renumbered, so an error
  would make existing spec roots invalid with no repair.

## Consequences

- `speca --list` reads as the plan of work, and `speca --next` rarely has to
  pick a package ahead of a lower-numbered one that is still open.
- Work that a later package delivers cannot be required by an earlier one.
  The shared part moves into an earlier package, or the earlier package
  offers an extension point that the later one depends on and fills, declared
  as a handoff once the later package exists
  ([ADR 24](24-declare-handoffs-between-packages.md)). A handoff already goes
  from a package to one that depends on it, so the two rules agree.
- A spec root written before this rule may now carry the warning. Nothing
  needs to change in it.
- The order of a split is still the model's judgement. The warning catches
  a forward dependency once it is written; it cannot tell that two scopes
  are in the wrong order when neither names the other.
