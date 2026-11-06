# 14. Commit the rendered package beside its JSON

**Status:** accepted
**Date:** 2026-11-06
**Related:** [ADR 05](05-render-a-package-with-its-own-command.md), [ADR 10](10-report-and-change-the-state-of-a-package-with-its-own-command.md), [ADR 11](11-validate-a-package-against-the-spec-root.md)

## Context

A specification package is `prd.md`, three JSON files and an optional
`architecture.md`. `specr <package>` renders all of them as one Markdown
document (ADR 05), but nobody committed the render, so a pull request that
added or changed a package showed its reviewer 60 to 126 KB of JSON that no
one reads as prose. `docs/spec-kit-comparison.md` §7.1 item 2 recommended
committing the render.

A render that a separate step writes is a render somebody forgets. A render
that is committed and stale is worse than none, because a reviewer reads it
believing it is the package.

## Decision

Every package carries `spec.md`, its render, beside its artifacts.

- **Every save writes it, in the same atomic write.** `saveToDisk` — reached by
  `Save`, `Transition` and `Supersede`, so by every `spec` run and every
  `speca` task and status move — writes `spec.md` through the same temporary
  files and renames as the artifacts. A failed save changes none of them. The
  render changes with every task and status move, which is right: the diff
  shows the state a reviewer is approving.
- **Its bytes are fixed.** One header line, a blank line, and exactly what
  `specr <package>` returns in `result.markdown`: the whole package, with no
  scope and no budget. `afspec.RenderedFileName`, `RenderedHeader` and
  `(*Spec).RenderedBytes` expose them, for a program that writes a package's
  files itself. The same package always renders to the same bytes; nothing in
  it comes from the clock.
- **It is derived, never read as data.** `LoadSpec` does not read it. The JSON
  and `prd.md` stay the package.
- **The verdict reports a stale render, by status.** The rule `rendered`, in
  the package's own validation, compares `spec.md` with the render for a
  package read from disk (not for one held in memory, such as a dry run's,
  whose `Dir` names a directory it never wrote). Missing is a warning — the
  ordinary state of a package written before this change. Different, or there
  and unreadable, is an error while the package is draft or active, since one
  call repairs it, and a warning once it is sealed, superseded or archived: a
  change of the renderer must not make history invalid. Because the rule is
  in the package's own validation, `speca --validate`, `speca --list`, `specr`
  and the activation check all agree on it.
- **`speca` renders, not `specr`.** `specr` reads and never writes into a
  package; `speca` is the command that changes one (ADR 10). `speca <package>
  --render` writes `spec.md` in any status, because it is outside the
  package's contract and touches no intent hash; `speca --render [--all]`
  does it for every package, and reports a package it cannot render as an
  entry with its reason, as `--list` reports one it cannot load.

## Consequences

- A pull request that adds or changes a package can be reviewed by reading
  `spec.md` in the diff.
- A committed render adds about 85 % to a package's size in the repository.
- A change to the renderer makes every committed render stale at once.
  `speca --render --all` repairs them; nothing re-renders them by itself, and
  until someone does, the draft and active packages do not validate.
- A writer that does not go through `afspec` — the `af-spec` skill writes the
  four files by hand — must run `speca <package> --render` before its verdict.
- A `speca` task move now changes `spec.md` too, so the diff of a recorded
  task holds the task's state, `updated_at` and the same change in `spec.md`.
