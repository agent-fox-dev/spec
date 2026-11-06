# 19. Migrate the format to version 3

**Status:** accepted
**Date:** 2026-11-06
**Related:** [ADR 10](10-report-and-change-the-state-of-a-package-with-its-own-command.md), [ADR 11](11-validate-a-package-against-the-spec-root.md), [ADR 14](14-commit-the-rendered-package-beside-its-json.md), [ADR 17](17-review-the-plan-adversarially.md), [ADR 18](18-allow-extension-keys-and-move-word-checks-out-of-the-validator.md)

## Context

`docs/spec-format-simplification.md` measured the ten version 2 packages
and found most of their weight in restatement: a `test_spec.json` whose
`given`, `when` and `then` repeat the criterion each test verifies, two
ids and a traceability array to tie them together, a task state inside
`tasks.json` that re-renders the committed `spec.md` on every move, an
integration task that every package writes the same way, and frontmatter
and glossary fields nothing reads. It recommended one migration to a
version 3 rather than four minor versions, each re-rendering every
committed `spec.md`. `docs/prd/06` specifies the migration.

## Decision

- **One `requirements.json` carries the checks.** A criterion has a `check`:
  its own (`kind`, `setup`, `pseudocode`) or `same_as` a criterion whose
  check is its own, or a path. A path has a `smoke` check (`real_components`,
  `setup`, `end_state`, `pseudocode`). `test_spec.json`, the test ids, the
  `tests` of a task and the `traceability` array are gone; coverage and
  traceability are derived.
- **The integration task is derived.** A package with an execution path has
  one, task `N+1` after the stored tasks, that depends on every task,
  touches what they touch, runs every smoke check and confirms every
  contract. `tasks.json` holds implement tasks only.
- **Progress is a file of its own.** `progress.json` holds `updated_at` and
  one state per task of the plan, the integration task included; the tools
  write it and a task move writes nothing else (`Spec.SaveProgress`). The
  lifecycle status stays in the PRD frontmatter, because activation hashes
  the intent and sealing closes the contract. `spec.md` never shows a state.
- **Ids drop the spec prefix** (`REQ-1`, `REQ-1.2`, `PATH-1`): the package is
  the namespace. `guard` and `complex_event` go into `condition`; `system`
  is optional and a requirement's applies to its criteria; the glossary is a
  `## Glossary` section of the PRD; `updated_at`, `owner` and `tags` leave
  the frontmatter.
- **A `lite` profile** (at most three requirements, no path, no integration
  task) is the PRD phase's choice, recorded in the frontmatter and held by
  rule C7.
- **The library speaks one version.** A version 2 package loads as a legacy
  package, as a version 1 package did, with one `schema_version` error that
  names the fix: `speca <package> --migrate`. `afspec.LoadV2` and
  `MigrateV2` convert deterministically; `speca --migrate [--all]` sweeps a
  spec root. The migration refuses a package with a criterion no test
  verifies or a path no smoke test verifies rather than invent a check; the
  ten packages have none. Version 1 packages are not migrated.
- **The generator has two steps.** Requirements, with every check, then
  tasks; the plan review and revision work over the two artifacts, and the
  integration task is never generated.

## Consequences

- A package is roughly half the size, and a task move changes one small
  file instead of re-rendering the whole package.
- Every tool that addressed a task by id still does: the integration task is
  `N+1`, claimed, rendered and reported like any other.
- The ten archived version 2 packages are migrated in this change and
  validate as version 3; their `spec.md` files are re-rendered once.
- A test of a criterion is written in the test file of the code it
  exercises, and the coder keeps the map from criterion to test outside the
  code; the skills say so.
- `make json-gen` generates the version 3 types from the version 3 schemas;
  the version 2 schemas stay in `specification/` as the reference of what
  the migration reads.
