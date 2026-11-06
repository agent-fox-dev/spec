# 15. Version the steering

**Status:** accepted
**Date:** 2026-11-06
**Related:** [ADR 02](02-choose-the-model-per-phase.md), [ADR 04](04-hand-on-what-a-phase-learned.md), [ADR 11](11-validate-a-package-against-the-spec-root.md), [ADR 14](14-commit-the-rendered-package-beside-its-json.md)

## Context

`.specs/steering.md` holds the project's directives to every agent, and every
phase of a `spec` run is given it. It had no version, so no package recorded
which directives it was written under, and when the project changed its mind
nothing said which packages were written before the change and might now
contradict it. Its history was in git alone. `docs/spec-kit-comparison.md`
§4.9 and §7.1 item 4 recommended versioning it.

## Decision

- **The version and its record live in YAML frontmatter.** `steering.md` may
  open with a block holding `version` (`MAJOR.MINOR.PATCH`) and `amendments`,
  a newest-first list of `version`, `date` and `change`. It is parsed with
  `goccy/go-yaml`, as the PRD frontmatter already is, and checked by
  `project.ReadSteering` against a fixed list of rules. A file without
  frontmatter is unversioned and behaves as it always did.
- **A malformed record is a warning, not a stop.** One high
  `steering_malformed` warning, at stage `preflight`, names every rule broken,
  on `spec` and on `speca --validate`. The body is used all the same and the
  version is treated as absent.
- **The record is never sent to a model.** It grows with every amendment and a
  phase needs the directives, not their history. Every phase is given the body
  without the frontmatter and, when the file is versioned, the line
  `Steering version <v>`. The run reads the steering once and puts the body and
  the version on the phase requests; the record is on none of them.
- **A package records its steering version.** The PRD frontmatter gains an
  optional `steering_version`, which `spec` writes into every package it writes
  under a versioned steering. It is outside the hashed `## Intent`, so it may
  change after activation.
- **A stale version is a warning a person clears.** The rule
  `steering_version` names, for a draft or active package written under an
  older version, both versions and every amendment since, and says when one of
  them was a MAJOR bump. Whether the package still holds is a judgment, so the
  rule is a warning and never makes a package invalid; the person who has
  checked it changes the field.
- **The rule belongs to the verdict across the spec root.** It reads the spec
  root's steering, and `afspec.(*Spec).Validate` is given no spec root, so it
  runs in `specpkg.Verdict` beside the cross-spec rules (ADR 11).
- **Nothing is refused because of the steering.** There is no gate on
  generating or activating a package; alignment with the directives is a
  reviewer's job, which the machine does not judge.

## Consequences

- A maintainer who amends the steering adds an amendment at the top of the
  record, and `speca <package> --validate` then tells each draft or active
  package written before it what it missed.
- Clearing the warning is a hand edit of `prd.md`, which leaves the package's
  `spec.md` behind it (ADR 14); `speca <package> --render` brings it up to
  date.
- The packages already under `.specs/` carry no `steering_version`: nothing
  recorded which steering they were written under, and a recorded version
  they were not written under would make the warning lie. This repository's
  `steering.md` gets its record reconstructed from git, back to the
  standardisation of 2026-10-15: 1.0.0, 1.1.0, 2.0.0 and 2.0.1.
- What a MAJOR, MINOR or PATCH bump means is documented for the person who
  writes the amendment; no machine checks it.
