---
spec_id: "15"
spec_name: "rendered_package_markdown"
title: "Commit the rendered package beside its JSON"
status: "active"
created_at: "2026-11-06T12:17:17Z"
updated_at: "2026-11-06T14:02:24Z"
intent_hash: "940185862cce1a404a2ed345c4b0d24c4a8aa7bfb64715c2c53ea223aa361561"
schema_version: 2
source: "docs/prd/04-review-the-plan-commit-the-render-and-version-the-steering.md"
---

# Commit the rendered package beside its JSON

## Intent

Nobody reads a specification package in a pull request, because three of its
four files are JSON and the Markdown that `specr` renders is never committed.
A pull request that adds a package shows a reviewer 60 to 126 KB of JSON that
no one reads as prose.

This change makes the render part of the package on disk. Every write of a
package through `afspec` also writes the render as `spec.md`, in the same
all-or-nothing write, so the file is never one a maintainer forgets. The
verdict reports a `spec.md` that is missing or no longer matches, and
`speca --render` brings it up to date for one package or for every package
without touching anything else. The JSON and `prd.md` stay the package;
`spec.md` is derived from them and is never read as data.

## Goals

- Every package that `afspec` writes carries a `spec.md`. Its bytes are a
  fixed header line followed by what `specr <package>` renders. A pull request
  that adds or changes a package can be reviewed by reading `spec.md` in the
  diff.
- `speca <package> --validate` reports a missing `spec.md` as a warning. A
  `spec.md` that differs from the render is an error for a draft or active
  package, and a warning for a sealed, superseded or archived one.
- `speca --render` brings `spec.md` up to date for one package or for every
  package, without changing any other file.
- The same package always renders to the same bytes. `spec.md` holds nothing
  that is not in the package, and in particular no time of rendering.
- The packages already under `.specs/` carry their `spec.md`, committed.

## Non-goals

- Making `spec.md` canonical, or parsing it. The JSON and `prd.md` stay the
  package. `spec.md` is derived from them and is never read as data.
- `.gitattributes` entries that collapse or expand files in a diff.
- Re-rendering every `spec.md` automatically when the renderer changes.
  `speca --render` does it on request.
- Rendering a scope or a budget into `spec.md`. It is the whole package, with
  no `--task`, no `--section` and no `--max-tokens`.
- Suggesting `speca <package> --render` in the `next` of another form.
- The three other scopes of the input PRD, each its own package, written by
  its own run:
  - **`steering_version`** — the steering's frontmatter version and amendment
    record, the `steering_malformed` warning, the versioned steering block
    given to every phase, the `steering_version` field of the PRD frontmatter
    and the verdict rule that names the amendments a package missed.
  - **`review_triage_convergence`** — the four dispositions (`fixed`,
    `contested`, `escalated`, `declined`), the counter-evidence check, the
    repeat rules, the two-clean-rounds stopping rule, the breaking change to
    `envelope.Review`, and the `steering` rubric in the PRD review.
  - **`adversarial_plan_review`** — `--adversarial-plan`, the in-memory plan
    validation, the grounding facts, the `zero_decision` and `grounding`
    rubrics, the plan reviewer and reviser, the plan review's failure
    handling, splits, and the `plan_review` and `plan_revision` phase names
    with their models.

## Background

### What a reviewer sees today

A package is `prd.md`, three JSON files and an optional `architecture.md`.
`specr <package>` renders all of them as one Markdown document
(`afspec.(*Spec).RenderCombined`, reached through `cmd/specr/render.go`, whose
unscoped branch is the library's combined render byte for byte). The render is
deterministic, and leaves out nothing but the `$schema` URIs and the
artifacts' copies of the spec's identity. Nobody commits the render, so a pull
request that adds a package shows a reviewer the JSON. Measured on the
packages in `.specs/`:

| Package | On disk | `specr` render |
|---|---|---|
| 09 | 151 KB | 127 KB |
| 10 | 128 KB | 107 KB |
| 13 | 77 KB | 65 KB |
| 14 | 101 KB | 88 KB |

A committed render therefore adds about 85 % to a package's size in the
repository.

### How a package is written

Every write of a package goes through `afspec`. `(*Spec).Save` writes draft
and active packages and refuses the others; `Transition` and `Supersede`
write in any state. All three reach the internal `saveToDisk`
(`afspec/afspec.go`), which renders `prd.md`, marshals the three JSON
artifacts, writes every artifact to a temporary file with `writeTempFile`,
fsyncs each, and only then renames them all — so a failed save changes none
of them, and no `*.tmp.*` file is left behind. `architecture.md` is in that
list only when the package has one.

`speca`'s task form and status form and `spec`'s write all reach these
functions. `speca --status active` goes through `Transition`, which computes
the intent hash. Because the render includes the PRD frontmatter as a list —
`renderMetadata` writes **Status**, **Updated** and **Intent hash** among the
fields — the render changes with every status move and every task move, which
is what makes a committed render show the state a reviewer is approving.

The `af-spec` skill is the one writer that does not go through `afspec`: it
writes the four files itself and validates them with `speca --validate`. Its
source lives at `skills/af-spec/SKILL.md` with its reference files beside it
(spec 14).

### How a package is validated

`afspec.(*Spec).Validate` is the package's own verdict: the four JSON Schemas
(`ValidateSchema`), and then, only if those pass, the cross-file rules C1 to
C11 (`ValidateCrossFile`). `internal/specpkg.Verdict` adds the rules across
specs, which need the other packages of the spec root, and is what both
`speca <package> --validate` and `spec`'s validation of what it wrote call.

The package's own validation is what the commands ask when they ask whether a
package validates: `speca --list` fills each entry's `valid` from it, `specr`
fills `result.valid` from it, and `cmd/speca/status_checks.go`
`checkActivation` refuses an activation on it. The rules across specs are in
no such check: an activation is not refused today for an unknown dependency.

### `speca`'s forms

`speca` has five forms: the report, `--validate`, `--list` (with `--all`), the
task form (`--task`, `--state`) and the status form (`--status`, `--by`).
`cmd/speca/forms.go` `execute` picks one: `--list` branches before the
reference is resolved, because it takes none; every other form resolves the
reference with `specpkg.Locate` first. `options.check` refuses the
combinations that cannot hold before anything is read, and today it refuses
`--all` without `--list` and `--force` or `--dry-run` without the task or
status form. `cmd/speca/list.go` lists the spec root with `specpkg.List` and
the archive under it, loading each package itself so that one which does not
load is still an entry, with its reason, rather than a failure of the call.

## Requirements

### 1. Writing the render with every save

- Every write of a package through `afspec` also writes `spec.md` in the
  package directory, in the same all-or-nothing write as the other files.
  That covers `Save`, `Transition` and `Supersede`, and through them every
  `spec` run, every `speca` task move and every `speca` status move.
- `spec.md` is one header line, a blank line, and then exactly what
  `specr <package>` returns in `result.markdown`: the whole package, with no
  scope and no budget. The header line is:

  ```
  <!-- Generated by afspec from prd.md, requirements.json, test_spec.json, tasks.json and architecture.md. Do not edit: change those files, or run speca <package> --render. -->
  ```

- The same package always renders to the same bytes. `spec.md` holds nothing
  that is not in the package. In particular, it holds no time of rendering.
- `afspec` exposes the file name and the expected bytes for a package, so
  that a program which writes the files itself can write `spec.md` too.
- `LoadSpec` does not read `spec.md` as data. The package is what it was.

### 2. Checking it

The verdict gains a rule, `rendered`. It applies to a package read from
disk, and not to a package held only in memory, such as one in a dry run.

| `spec.md` | Draft or active | Sealed, superseded or archived |
|---|---|---|
| absent | warning: `spec.md is missing; run speca <package> --render` | the same warning |
| differs from the render | error | warning |
| equal to the render | nothing | nothing |

The message for a difference names the first line that differs, and
`speca <package> --render`. An error counts like any other: a draft package
whose `spec.md` is stale cannot be activated until it is rendered again.

### 3. `speca <package> --render`

- `speca <package> --render` writes `spec.md` for one package, whatever its
  status. It changes no other file, and does not touch `updated_at`.
- The result lists the package with `rendered`: `written`, `unchanged` or
  `failed`. A failed package also carries the reason, for example that it
  cannot be loaded.
- `--dry-run` reports what would change and writes nothing.

### 4. `speca --render` over every package

- `speca --render` with no package renders every package under the spec
  root. With `--all`, it also renders those under `archive/`.
- A package that fails does not stop the others from being rendered.
- The exit codes are `speca`'s own.

### 5. `--render` is a form of its own

- Combining `--render` with `--validate`, `--list`, `--task` or `--status` is
  a usage error.
- `--dry-run` and `--all` are accepted with `--render`; `--force` is not.

### 6. Documentation and build

- `docs/speca.md`: the `--render` form, and the rule `rendered` in the
  verdict.
- `docs/specr.md`: `spec.md` is `specr`'s render, committed.
- `docs/api.md`: the `afspec` functions for `spec.md`.
- `docs/architecture.md`: `spec.md` in the write.
- `docs/cli.md`: `spec.md` among the artifacts a `spec` run reports it wrote.
- `specification/spec-format-v2.md`: `spec.md` as a generated file of the
  package that is not canonical, and the rule `rendered`.
- `README.md`: `spec.md` in the package table.
- An ADR for committing `spec.md`.
- `make doc` for the changed exported identifiers.
- The `af-spec` skill runs `speca <package> --render` before
  `speca <package> --validate`, in the skill source layout of spec 14.
- The packages under `.specs/` are rendered with `speca --render` and their
  `spec.md` files committed.

## Design Decisions

1. **`spec.md` is written by every save, in the same atomic write.** A render
   written by a separate step is a render someone forgets. It changes with
   every task move, which is right: the diff then shows the state a reviewer
   is approving.
2. **A stale `spec.md` is an error only while the package can be saved.** A
   draft or active package is fixed by one `speca --render`. A sealed package
   is history, and a renderer change must not make history invalid. Its stale
   render is still reported, as a warning.
3. **`speca` renders, not `specr`.** `specr` reads and never writes into a
   package. `speca` is the command that changes a package (ADR 10). The new
   form writes one derived file in any status, because `spec.md` is not part
   of the contract and touches no intent hash.
4. **`spec.md` holds the full render, the PRD included.** A reviewer reads
   one file. A render without the PRD would be cheaper, but it would send the
   reviewer back to `prd.md` for the reason behind every requirement.
5. **The `rendered` rule belongs to the package's own validation, not only to
   the verdict across specs.** The input PRD asks that a draft package with a
   stale render cannot be activated, and `checkActivation` asks the package's
   own validation, as `speca --list` and `specr` do. Putting the rule
   anywhere else would make `speca --validate` and `speca --status active`
   disagree about whether the same package validates.
6. **"Read from disk" is what `LoadSpec` did, not a non-empty `Dir`.** A dry
   run of `spec` sets `Dir` on a package it never wrote
   (`specgen/pipeline.go`), so `Dir` alone would make a dry run report a
   missing `spec.md` for a directory that holds nothing. The library records
   that a package came from `LoadSpec`, and the rule reads that.
7. **A `spec.md` that exists and cannot be read counts as a difference, not
   as absent.** Absent is the ordinary state of a package written before this
   change, and its warning says to render. A file that is there and
   unreadable is a fault, and reporting it as the milder case would hide it.
8. **A failed package in the sweep is an entry, not a failure of the call.**
   `speca --list` already reports a package that does not load as an entry
   with its reason, so that one broken package does not hide the rest. The
   sweep follows it: exit 0 with every package's outcome listed. The
   one-package form has nothing else to report, so there a failure fails the
   call.
9. **The documentation, the `af-spec` skill's change and the committed
   renders of `.specs/` are task steps, not criteria.** All three are one-off
   changes to this repository, not behaviour of the system under
   specification, and the steering forbids a test of a documentation file's
   content, so a criterion for them could be verified by no test this project
   allows. A test over the repository's own `.specs/` tree would also fail for
   every package written after this one until someone rendered it, which makes
   the test a tripwire on unrelated work rather than a check of this change.
   Each step carries the file it changes, and the integration task's
   `done_when` names the checks that cover them.
10. **`--render` takes no `--force`.** `--force` overrides a refusal, and
    `--render` has none to override: it writes one derived file in any
    status. Accepting it silently would suggest it did something.
11. **A `spec` run lists `spec.md` among the artifacts it wrote.**
    `envelope.SpecResult` defines `artifacts` as the files written, and the
    run writes this one. A dry run writes none of them, so it lists none.

## Dependencies

- **Spec 10, `speca`: report a spec package's state from the shell.**
  `--render` is a new form of that command, and its result, its envelope and
  its exit codes are the ones spec 10 defined; the sweep follows the shape of
  `--list`.
- **Spec 11, `speca`: change the state of a package's tasks.** The task form
  saves the package through the library, and that write now writes `spec.md`
  too.
- **Spec 12, `speca`: change a spec package's lifecycle status.** The status
  form writes the package through `Transition` and `Supersede`, and an
  activation is refused while `spec.md` is stale.
- **Spec 14, render the skills for every agent from one source** (active, all
  tasks done). The `af-spec` change is made in its source layout,
  `skills/af-spec/SKILL.md`.
- **ADR 05, render a package with its own command**, and **ADR 10, report and
  change the state of a package with its own command**: `spec.md` is
  `specr`'s render, and `speca` is the command that writes it.
- **ADR 11, validate a package against the spec root.** The rule `rendered`
  joins the verdict beside the rules across specs.
- **`docs/spec-kit-comparison.md`** §7.1 item 2: the recommendation this
  package carries out.

## Verified External API

- No new dependency. The render is `afspec.(*Spec).RenderCombined`, already in
  the library, and the write uses `os.CreateTemp`, `(*os.File).Sync` and
  `os.Rename`, as `afspec/afspec.go` `writeTempFile` does today.
- **`internal/cli.WriteAtomic`**, read in `internal/cli/output.go`:
  `func WriteAtomic(path string, data []byte) error` — creates the parent
  directory when missing, writes a temporary file in it, syncs, closes and
  renames over `path`. It is what `specr --markdown-file` writes with, and
  what the `--render` form writes `spec.md` with.
