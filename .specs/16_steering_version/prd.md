---
spec_id: "16"
spec_name: "steering_version"
title: "Version the steering and record which version a package was written under"
status: "sealed"
created_at: "2026-11-06T12:39:19Z"
updated_at: "2026-11-06T15:38:02Z"
intent_hash: "ba0d73c0321f7fea64059d3368983dd14dfc7f389d101ca901e80d0c6accfe91"
schema_version: 2
source: "docs/prd/04-review-the-plan-commit-the-render-and-version-the-steering.md"
---

# Version the steering and record which version a package was written under

## Intent

Nothing holds a package to the steering. `steering.md` is fed to every phase,
but it has no version, so no package records which directives it was written
under. When the project changes its mind, nothing says which packages were
written before the change and might now contradict it.

This change gives the steering a version and a dated record of its
amendments, in YAML frontmatter the body does not see. Every phase still gets
the directives, now with the version named. A package records the version it
was generated against in its PRD frontmatter, and the verdict names, for a
draft or active package written under an earlier version, the amendments made
since. The record is read by the verdict, never sent to a model, and nothing
is refused because of it: a package that has fallen behind is reported, and a
person who has checked it clears the warning by changing the field.

## Goals

- `.specs/steering.md` may carry a version and a dated record of its
  amendments as YAML frontmatter. A file without frontmatter keeps behaving
  exactly as it does today, with no warning.
- Frontmatter that breaks one of the well-formedness rules is reported as a
  `high` warning, `steering_malformed`, by both `spec` and
  `speca --validate`, naming every broken rule. The directives are still
  used; the version is treated as absent.
- Every phase is given the body without the frontmatter. When the file is
  versioned, the steering block names the version. The amendment record is
  never sent to a model.
- The PRD frontmatter gains an optional `steering_version`. `spec` writes the
  steering's version into every package it writes when the steering is
  versioned, and leaves the field out when it is not.
- `speca <package> --validate` gains a rule, `steering_version`: for a draft
  or active package written under an earlier version, the verdict names both
  versions and every amendment made since, and says when one of them removed
  or changed a directive. It never makes a package invalid.
- `.specs/steering.md` of this repository gets its frontmatter, its record
  reconstructed from the file's git history. Its body does not change, and
  the packages already under `.specs/` get no `steering_version`.

## Non-goals

- Checking a package against the steering by machine. The directives are
  prose, and design rule 3 of the format keeps the validator from judging
  prose. Alignment is a reviewer's job. The machine checks only the version.
- Steering alignment in the adversarial reviews — the `steering` rubric, its
  evidence rules and the finding that cannot be declined. That is the
  `review_triage_convergence` package for the PRD review and the
  `adversarial_plan_review` package for the plan review.
- A gate that refuses to generate or activate a package because of the
  steering (Spec Kit's Constitution Check as a gate). A contradiction is
  reported, as a warning, not refused.
- A Sync Impact Report: listing, after an amendment, every document and
  package to update. The verdict's warning lists the amendments a package
  missed. That is the whole of it.
- Checking that the right number was bumped. The meaning of MAJOR, MINOR and
  PATCH is documented for the person who writes the amendment; no machine
  reads it.
- Changing the steering files of the other agent-fox repositories. This
  repository's copy is versioned. The others take the same text when they
  next copy it.
- Backfilling `steering_version` into the packages already under `.specs/`.
  Nothing recorded which steering they were written under.
- A semver library, pre-release or build-metadata suffixes, or version ranges.
- `docs/spec-kit-comparison.md` itself, which is an analysis dated 2026-10-28
  and stays as written.
- The two later scopes of the input PRD, each its own package, written by its
  own run:
  - **`review_triage_convergence`** — the four dispositions (`fixed`,
    `contested`, `escalated`, `declined`), the counter-evidence check, the
    repeat rules, the two-clean-rounds stopping rule, the breaking change to
    `envelope.Review`, and the `steering` rubric in the PRD review.
  - **`adversarial_plan_review`** — `--adversarial-plan`, the in-memory plan
    validation, the grounding facts, the `zero_decision` and `grounding`
    rubrics, the plan reviewer and reviser, splits, and the `plan_review` and
    `plan_revision` phase names with their models.

## Background

### The steering today

`internal/project.Steering(specsDir)` reads `<spec root>/steering.md` whole
and returns it as one string. A file that is missing, a symlink, empty, over
`maxSteeringBytes` (24 KB), or that holds the placeholder marker
`<!-- steering:placeholder -->` or only the bare `# Steering` heading is
nothing, and the result is `""`. The checks are made over the whole file,
because the file has no frontmatter.

Four call sites pass it into a phase: `specgen/pipeline.go` at the split
decision, the artifact phases and the architecture phase, and
`specgen/rounds.go` for the PRD review. Each puts it in a prompt through
`steeringBlock` in `specgen/prompts.go`, which wraps the trimmed text in a
`## Steering` heading between `--- BEGIN STEERING ---` and
`--- END STEERING ---`, and returns `""` for an empty steering so that no
block appears.

The file has no version. Its history is in git alone:

| Commit | Date | What changed |
|---|---|---|
| `832b86e` | 2026-10-15 | the file was standardised across the agent-fox repositories |
| `051c49f` | 2026-10-15 | *Generated Docs* was added |
| `1eea238` | 2026-10-25 | *Tests and Spec Traceability* was added |

Two commits before those, on 2026-08-06 and 2026-10-13, predate the
standardisation.

### The PRD frontmatter today

`prd-frontmatter.v2.json` holds twelve keys and `additionalProperties:
false`; eight are required and `owner`, `source`, `supersedes` and `tags` are
optional. It has no field that records the steering. Three places read or
write those keys and all three must learn a new one:

- `afspec/prd.go` — `prdFrontmatter` parses the YAML with `goccy/go-yaml`,
  and `renderPRD` writes it back with a hand-written renderer in a **fixed
  field order**, so that the output is byte-stable; the optional keys are
  omitted when empty.
- `afspec/prd-frontmatter.v2.go` — the generated Go type, written by
  `make json-gen` from the copy under `afspec/schemas/`, never by hand.
- `afspec/validate.go` — `prdFrontmatterForSchema` carries exactly the
  schema's keys, so that a field of `Spec` which is not in the schema cannot
  produce a spurious `additionalProperties` error.

`specgen/pipeline.go` builds the package: `afspec.CreateSpec` then
`spec.CreatedAt`, `spec.UpdatedAt`, `spec.Title`, `spec.Source` and
`spec.PRDBody`.

### The verdict today

`afspec.(*Spec).Validate` is the package's own verdict and knows nothing of
the spec root. `internal/specpkg.Verdict(specsDir, spec, siblings...)` adds
the rules that need the other packages (ADR 11), and is what both
`speca <package> --validate` and `spec`'s validation of what it wrote call. A
rule that has to read the spec root's `steering.md` belongs there, beside the
rules across specs — it cannot live in the library's own validation, which is
given no spec root.

`spec` raises its preflight warnings in `specgen/pipeline.go` around the
landscape discovery and the project profile — `specs_dir_unreadable` and
`project_language_unknown` — before the first phase. `speca` raises
`specs_dir_unreadable` from `cmd/speca/forms.go`. Every warning code is
declared in `envelope/warn.go`, re-exported in `internal/cli/warn.go`, and
given exactly one stage in `warnStages`; `preflight` is the stage of both
existing codes above.

### What lands beside this

The `rendered_package_markdown` package (spec 15, active) adds a different
rule, `rendered`, to the package's own validation, and both packages add a
row to the validation tables of `docs/speca.md` and
`specification/spec-format-v2.md` §10. Whichever is implemented second
rebases on the first. Neither needs the other's behaviour.

## Requirements

### 1. Reading the steering's version and its record

- `steering.md` may open with YAML frontmatter that holds two keys:

  ```yaml
  ---
  version: 2.0.1
  amendments:
    - version: 2.0.1
      date: 2026-11-06
      change: The file carries a version and this record.
    - version: 2.0.0
      date: 2026-10-25
      change: Tests are organised by behaviour; no per-spec, smoke, property, phase or group test files.
  ---
  ```

- A version is `MAJOR.MINOR.PATCH`: three non-negative integers with no
  leading zeros. Versions compare by number, not as text.
- Well-formed frontmatter:
  - has only the keys `version` and `amendments`;
  - has a `version` in that form;
  - has at least one amendment, newest first, each with a `version`, a
    `date` (`YYYY-MM-DD`) and a non-empty `change`;
  - has amendment versions that strictly decrease and dates that do not
    increase;
  - has a first amendment whose version is `version`.
- A steering file with no frontmatter is **unversioned**. It behaves as it
  does today, with no warning.
- The 24 KB bound, the placeholder and the bare-heading checks apply to the
  body after the frontmatter.

### 2. A steering file whose frontmatter breaks a rule

- A steering file whose frontmatter breaks a rule gets a `high` warning,
  `steering_malformed`, that names every broken rule. Both `spec` and
  `speca --validate` raise it. Its body is still used, and its version is
  treated as absent.
- A frontmatter block that never closes is malformed in the same way.
- Neither command fails because of it, and neither changes its exit code.

### 3. What every phase is given

- Every phase is given the body without the frontmatter. When the file is
  versioned, the steering block names the version (`Steering version 2.0.1`).
- The amendment record is not sent to the model.
- A steering with nothing to say puts no block in any prompt, as today.

### 4. The package's record of the version

- The PRD frontmatter gains `steering_version`, an optional string in the
  version form. The field is added to
  `specification/schemas/prd-frontmatter.v2.json` and its copy in
  `afspec/schemas/`, to the Go types, and to
  `specification/spec-format-v2.md`.
- `spec` writes the steering's version into every package it writes when the
  steering is versioned, and leaves the field out when it is not.
- A `steering_version` that is not in the version form is a schema error.
- The field is frontmatter, outside the hashed `## Intent`, so it can be
  changed after activation.

### 5. The `steering_version` rule in the verdict

The verdict (`speca --validate`, and `spec`'s validation of what it wrote)
gains a rule, `steering_version`. The rule applies to a draft or active
package that has the field, when the spec root's steering is versioned:

- Equal versions raise nothing.
- A steering newer than the package raises a warning. It names both versions
  and every amendment after the package's version, newest first, each with
  its date and change. When one of them is a MAJOR bump, the warning says
  that a directive the package was written under has been removed or changed.
- A package version newer than the steering, or one that is not in the
  amendment record, raises a warning that says so.

The rule does not apply to a sealed, superseded or archived package, to a
package without the field, or under an unversioned steering. It never makes a
package invalid.

### 6. Documentation and build

- `docs/configuration.md`: the steering frontmatter, its rules and the
  meaning of each version number.
- `docs/cli.md`: the `steering_malformed` warning.
- `docs/speca.md`: the rule `steering_version` in the verdict.
- `docs/api.md`: the `steering_version` field of the public package type.
- `specification/spec-format-v2.md`: the field `steering_version` in §4.1 and
  the rule `steering_version` in §10.
- An ADR for versioning the steering.
- `make doc` for the changed exported identifiers.
- `.specs/steering.md` gets its frontmatter, with the record reconstructed
  from the file's git history. The body does not change.
- The packages under `.specs/` do not get a `steering_version`.

## Design Decisions

1. **The steering version lives in frontmatter, the record as YAML.** The
   PRD's frontmatter is already parsed with `goccy/go-yaml`. A YAML list is
   checked without parsing Markdown headings, which is the brittleness the
   format chose JSON to avoid.
2. **The amendment record is not sent to the model.** It grows with every
   amendment, and a phase needs the directives, not their history. The
   verdict's warning is where the history is read.
3. **A stale `steering_version` is a warning, and the person clears it.**
   Whether a package still holds under a new directive is a judgment. The
   warning says which amendments to check against, and changing the field
   records that someone checked.
4. **The packages in `.specs/` get no `steering_version`.** Recording a
   version they were not written under would make the warning lie about them.
5. **The `steering_version` rule belongs to the verdict across the spec root,
   not to the library's own validation.** The rule has to read the spec
   root's `steering.md`, and `afspec.(*Spec).Validate` is given no spec root.
   `internal/specpkg.Verdict` is where ADR 11 put every rule that needs more
   than the package itself.
6. **A steering whose body says nothing is unversioned, whatever its
   frontmatter.** `steering_version` records which directives a package was
   written under, and a file that is empty, is only the placeholder, is only
   the bare heading or whose body is over the bound has no directives to
   record. Such a file is nothing, as it is today, and a package written
   against it records no version.
7. **A malformed frontmatter is one warning, raised once per command, at
   stage `preflight`.** That is the stage of `specs_dir_unreadable` and
   `project_language_unknown`, the other warnings about the ground a run
   stands on, and raising it once keeps a run that reads the steering at four
   phases from repeating itself.
8. **`steering_version` is written after `schema_version` and before
   `owner`** in `renderPRD`'s fixed field order, in the schema's properties
   and in the format's §4.1 table: it belongs with the fields the format
   owns, not among the four a person fills in.
9. **This repository's record is numbered 1.0.0, 1.1.0, 2.0.0 and 2.0.1.**
   The standardisation of 2026-10-15 is the baseline; *Generated Docs* added
   a directive the same day, so MINOR; *Tests and Spec Traceability* on
   2026-10-25 changed how tests are organised so that packages and code
   written under the earlier file may now break it, so MAJOR; and this change
   adds no directive to the body, so PATCH. The input PRD's illustration
   numbered this change 2.1.0, which its own rule — MINOR when a directive is
   added — does not support, so the rule wins.
10. **The record is reconstructed only as far back as the standardisation.**
    The two commits of 2026-08-06 and 2026-10-13 predate it, and an amendment
    list that reaches behind the baseline would describe a file this one does
    not continue.
11. **An empty `steering_version` on disk is read as the field being absent,
    not as a bad version.** The field is a plain string, as `owner` and
    `source` are, and those are omitted when empty rather than written as
    `""`. Making an empty value an error would need the field to be a pointer
    everywhere, only so that one spelling of "no version" could be refused
    differently from the other.
12. **The documentation, the ADR and this repository's own frontmatter are
    task steps, not criteria.** They are one-off changes to this repository
    rather than behaviour of the system under specification, and the steering
    forbids a test of a documentation file's content, so a criterion for them
    could be verified by no test this project allows. Each step names the
    file it changes.

## Dependencies

- **Spec 09, review the PRD adversarially** (active, all tasks done). The
  review phase is one of the four that is handed the steering
  (`specgen/rounds.go`), and this change alters what that hand-over carries.
- **Spec 10, `speca`: report a spec package's state from the shell** (active,
  all tasks done). `--validate` is the form the rule `steering_version`
  appears in, and `steering_malformed` is a warning of its envelope.
- **ADR 02, choose the model per phase**, and **ADR 04, hand on what a phase
  learned**: the steering reaches a phase through the same request structs.
- **ADR 11, validate a package against the spec root.** The rule
  `steering_version` reads the spec root's steering, beside the rules across
  specs.
- **Spec 15, commit the rendered package beside its JSON** (active, no task
  done). Not a dependency: neither package needs the other's behaviour. Both
  add a row to the validation tables of `docs/speca.md` and
  `specification/spec-format-v2.md` §10, so whichever is implemented second
  rebases on the first.
- **`docs/spec-kit-comparison.md`** §4.9 and §7.1 item 4: the recommendation
  this package carries out.

## Verified External API

- **`github.com/goccy/go-yaml` v1.19.2**, pinned in `go.mod` and already
  imported by `afspec/prd.go`:
  `func Unmarshal(data []byte, v interface{}) error`. It parses the PRD
  frontmatter today and parses the steering frontmatter here.
- **`github.com/atombender/go-jsonschema`**, installed by `make json-gen` at
  `@latest` and not a module dependency. It regenerates
  `afspec/prd-frontmatter.v2.go` from `afspec/schemas/prd-frontmatter.v2.json`
  with `go-jsonschema --only-models -p afspec`.
- No new dependency. Versions are compared as three integers parsed with
  `strconv.Atoi`, and `MAJOR.MINOR.PATCH` with no pre-release needs no semver
  library. A date is checked with
  `time.Parse(time.DateOnly, s)` — `time.DateOnly` is `"2006-01-02"`, the
  `YYYY-MM-DD` form the record uses.
