# Migrate the format to version 3

## Intent

A version 2 package says one behaviour up to five times and keeps its
progress in a contract file. `docs/spec-format-simplification.md` measured
that cost and named the changes that take it out: a criterion carries its own
check and a path its own smoke check, so there is no test artifact (S1);
progress lives in its own file, so the contract is never rewritten by a task
move (S8); the fields nothing uses go (S3); ids drop the package prefix (S4);
the integration task is derived from the package, not authored (S6); and a
small change fits a `lite` package with no paths (O2). Version 3 of the format
is those six items, done as one migration that is deterministic from version
2, as the move from version 1 was, so that every committed `spec.md` is
rendered again once and not four times.

## Goals

- A version 3 package is `prd.md`, `requirements.json`, `tasks.json`, an
  optional `architecture.md`, `progress.json` written by the tools, and
  `spec.md`. There is no `test_spec.json`.
- Every criterion carries a `check` and every execution path a `smoke` check,
  required by the schema, so that four of the eleven cross-file rules of
  version 2 become shapes a package cannot be written without.
- A task move writes `progress.json` and nothing else. After activation the
  three contract files and `spec.md` change only by a lifecycle move.
- The integration task is rendered from the package and has a state, but is
  not in `tasks.json`.
- A package of at most three requirements may carry `profile: lite` and no
  execution path, and validates under the same rules minus the path rules.
- `afspec` reads and writes version 3 only. `speca <package> --migrate`
  converts a version 2 package in place, deterministically, whatever its
  status, and `speca --migrate --all` converts every package of the spec root
  and its archive.
- Every version 2 package under `.specs/archive/` is migrated in this change
  and validates afterwards. The version 1 packages there are left as they are.
- `spec`, `specr`, `speca`, the three skills and the documentation speak
  version 3.

## Non-goals

- One JSON file for requirements and tasks (O1). The identity header stays on
  each file, and rule C1 stays.
- Priorities (A1), stored open questions (A2), structured contracts (O3), the
  living view (O4) and the review render (O5). Each adds a field, and the
  document says to measure first.
- A map from criterion to the test that covers it in `progress.json`. The
  steering keeps the traceability in the commit.
- Reading version 2 packages at runtime. A version 2 package loads as a
  version 1 package does today: it is reported with one `schema_version`
  error that names the migration, and nothing else is read of it.
- Converting the version 1 packages under `.specs/archive/`. They were never
  converted to version 2 and stay history.
- Changing what the intent hash covers. It stays the hash of `## Intent`, so
  that the migration of an active package changes no hash.
- Changing the lifecycle, the task state machine or the compare-and-set
  claim.

## Background

### What the ten packages say

The measurements are in `docs/spec-format-simplification.md` §1. Of 442
criteria, 404 have a test, one test per criterion in 93 % of cases, each test
about twice the size of its criterion. Of 35 smoke tests 18 list criteria
beside their path. `guard` is on 14 criteria, `optional` on no task, `owner`
and `tags` on no package. Every id carries the package's prefix and rule C2
checks it. The two packages implemented since the render landed show seven
commits each that touch `tasks.json` and `spec.md` for a two-line state move.

### What the library does today

`afspec.LoadSpec` reads the four artifacts into the generated version 2
types; `Save` writes all four, `architecture.md` and `spec.md` through
temporary files; `ValidateCrossFile` runs C1 to C11; `RenderCombined` renders
the PRD, the architecture, the requirements, the tests and the tasks;
`RenderIndividualScoped` scopes a task's render through its `tests`. Task
state lives in `tasks.json`: `TransitionTask`, `IsReady` and `NextTask` are
methods of `TasksV2Json`. The generator (`specgen`) runs three artifact
phases, requirements, test_spec and tasks, each validated with
`ValidateGenerationStepComplete`; the plan review and revision take the three
artifacts. `speca --task` saves through `Spec.Save`. A package whose
`schema_version` is not 2 is reported with one `schema_version` error.

### What the migration of version 1 was

Format v2 §13 maps each version 1 construct to its version 2 form and says the
conversion is deterministic. No code performed it in this repository: the
version 1 packages under `.specs/archive/` still declare `schema_version: 1`.
This migration is the same table, with code behind it, run once over the
version 2 packages.

## Requirements

### 1. The files of a package

- A package holds `prd.md`, `requirements.json` and `tasks.json`, which are
  its contract; `progress.json`, which the tools write; `architecture.md`,
  optional; and `spec.md`, the render. `test_spec.json` does not exist in
  version 3, and a save does not write one.
- `progress.json` holds `$schema`, `spec_id`, `spec_name`, `schema_version`,
  `updated_at` and `tasks`: one `{id, state}` per task of the plan and one for
  the integration task when the package has one, in id order, `state` one of
  `pending`, `in_progress`, `done` and `dropped`. A package without the file
  loads with every task `pending` and `updated_at` empty. Every save writes
  it. `Spec.SaveProgress` writes it alone, atomically, and nothing else.
- The PRD frontmatter holds `spec_id`, `spec_name`, `title`, `status`,
  `created_at`, `intent_hash`, `schema_version` (`3`), and optionally
  `profile` (`full` or `lite`; absent means `full`), `steering_version`,
  `source` and `supersedes`. `updated_at`, `owner` and `tags` are gone:
  `updated_at` lives in `progress.json`.
- `spec.md` renders the contract and never the progress, so a task move
  leaves it byte for byte as it was. Its header line names `prd.md`,
  `requirements.json`, `tasks.json` and `architecture.md`.

### 2. requirements.json

- The file holds `$schema`, `spec_id`, `spec_name`, `schema_version`,
  `requirements` (at least one), `execution_paths` (optional; a `full`
  package has at least one, a `lite` package none) and `external_apis`
  (optional, as in version 2). `introduction` and `glossary` are gone; a
  glossary is a `## Glossary` section of the PRD.
- A requirement holds `id` (`REQ-{N}`), `title`, `rationale` (optional),
  `system` (optional: the acting component of every criterion that names
  none) and `criteria` (at least one).
- A criterion holds `id` (`REQ-{N}.{C}`), `pattern` (`ubiquitous`,
  `event_driven`, `state_driven`, `unwanted`, `optional`), `condition`
  (absent for `ubiquitous`, required otherwise), `system` (optional, over the
  requirement's), `action`, `contract` (optional, required for `unwanted`)
  and `check` (required). There is no `guard` and no `complex_event`: a
  second condition is written into `condition`.
- A `check` is either its own, `{kind, setup, pseudocode}` with `kind` one of
  `unit`, `integration` and `property` and `setup` and `pseudocode` optional,
  or shared, `{same_as}`, naming a criterion of this package whose check is
  its own, or a path of this package whose smoke check verifies it. The two
  forms do not mix.
- An execution path holds `id` (`PATH-{N}`), `title`, `steps` (at least two)
  and `smoke` (required): `real_components` (at least one), `setup`
  (optional), `end_state` (at least one) and `pseudocode` (optional).
- The rendered sentence of a criterion is as in version 2, with `system`
  resolved from the criterion, else its requirement, else `system`. A check
  renders under its criterion: its kind, its setup, its pseudocode, or the id
  it is the same as. A smoke check renders under its path.

### 3. tasks.json

- The file holds `$schema`, `spec_id`, `spec_name`, `schema_version`,
  `test_commands`, `dependencies` and `tasks` (at least one).
- A task holds `id`, `title`, `criteria` (at least one requirement or
  criterion id), `steps` (at least one), and optionally `touches`,
  `depends_on` and `done_when`. There is no `kind`, no `tests`, no `state`
  and no `optional`. Every task is an implement task.
- The integration task is derived. In a `full` package it is task `N+1`,
  where `N` is the highest task id, titled "Verify every execution path end
  to end", with no criteria, `depends_on` every task of the plan, `touches`
  the union of every task's `touches` in order, and steps that say: run each
  path's smoke check with its real components and confirm its end state, and
  trace the path through the real code; confirm that the result of every
  criterion with a contract is consumed in production code; search every path
  in `touches` for stub markers; and, when the package depends on other
  packages, confirm the entry points those packages own are called from
  production code. Its done-when is the test commands. A `lite` package has
  none. Every tool that lists, renders, counts or moves tasks includes it as
  task `N+1` with kind `integration`; the tasks of the plan have kind
  `implement`.
- A task's state is in `progress.json`. `IsReady`, `NextTask`,
  `TransitionTask`, `CompleteTaskStates` and `ResetTaskStates` are methods of
  `Spec`, read the progress and return a new progress artifact. The
  integration task is ready when every task of the plan is done or dropped.
  The state machine is version 2's.

### 4. Validation

- Schema validation runs `prd-frontmatter.v3.json`, `requirements.v3.json`,
  `tasks.v3.json` and, when the file is present, `progress.v3.json`. A
  package whose `schema_version` is neither absent nor `3` is reported with
  one `schema_version` error that names the version and
  `speca <package> --migrate`, and nothing else of it is validated.
- The cross-file rules, all errors:

  | # | Rule |
  |---|---|
  | C1 | `spec_id` and `spec_name` agree across `prd.md`, `requirements.json`, `tasks.json`, `progress.json` when present, and the folder name |
  | C2 | every id matches its format (`REQ-N`, `REQ-N.C` under requirement `REQ-N`, `PATH-N`, a positive task id) and is unique in its family |
  | C3 | every `check.same_as` names a criterion of this package whose check is its own and is not the criterion itself, or a path of this package |
  | C4 | every `task.criteria` entry names a requirement or a criterion; every `depends_on` entry names a lower task id |
  | C5 | every criterion is owned by at least one task, directly or through its requirement |
  | C6 | every `unwanted` criterion has a non-empty contract |
  | C7 | a `full` package has at least one execution path; a `lite` package has no execution path and at most three requirements |
  | C8 | every entry of `progress.json` names a task of the plan or the integration task, each at most once |

- The warnings: `scope_limit` (more than 10 requirements, 8 criteria in a
  requirement, 12 tasks, 12 steps in a task), `missing_contract` as today,
  and `rendered` as today. `glossary_hint` is gone.
- The cross-spec rules: an unknown dependency and a cycle as today; the actor
  rule applies to a dependency edge only when both packages have execution
  paths; the glossary warning is gone.
- `ValidateGenerationStep` runs, after requirements: the schema, C1, C2, C3,
  C6, C7; after tasks: the above plus C4, C5. `GenerationSteps` is
  requirements, then tasks.

### 5. Rendering

- The combined render is the PRD, the architecture, the requirements (each
  criterion with its sentence, contract and check; each path with its steps
  and smoke check; the external APIs) and the tasks (the test commands, the
  dependencies, every task of the plan, then the integration task). No task
  state is rendered.
- The scoped render of a plan task is as in version 2 with the checks in the
  place of the tests: the requirements that own the task's criteria in full,
  the paths a `same_as` of those criteria names in full, every other
  requirement and path one line each, the external APIs, the task in full and
  every other task, the integration task included, one line each.
- The scoped render of the integration task holds every path in full with its
  smoke check, every requirement one line each, the criteria that carry a
  contract listed with their contracts, the external APIs, the test commands
  and dependencies, and the integration task in full.
- The budget drops the architecture first, then the `setup` and `pseudocode`
  of every check and smoke check; `specr` reports the second as
  `check_detail`.
- `specr`'s sections are `prd`, `architecture`, `requirements` and `tasks`.
  `specr <package> --task N+1` renders the integration task. `result.tasks`
  lists the plan's tasks and the integration task with their states from
  `progress.json`.

### 6. speca

- The task form writes `progress.json` through `Spec.SaveProgress`, setting
  its `updated_at`, and no other file. Its checks are version 2's, over the
  plan's tasks and the integration task.
- The report's `tasks`, `task_counts`, `next_task` and the list's entries
  include the integration task. `updated_at` is the progress file's; `tags`
  is gone. A package that validates with every task done, the integration
  task included, is offered `--status sealed`; sealing with the integration
  task open is refused without `--force`.
- `speca <package> --migrate [--dry-run]` converts a version 2 package to
  version 3 in place, whatever its status, and `speca --migrate [--all]
  [--dry-run]` does it for every package of the spec root, then of
  `archive/`. `result.migrated` holds one entry per package: `spec_dir`,
  `spec_id`, `spec_name`, `status`, `from_version`, `migrated` (`written`,
  `unchanged` for a package already at version 3, or `failed`) and `reason`.
  A package that fails is an entry, not a failure of the sweep. The form
  takes no other operation and refuses `--force`.
- A package in another format version is refused by the task form and the
  status form, and reported by the report and the list, naming
  `speca <package> --migrate` as the way to convert it.

### 7. The migration

- `afspec.LoadV2(dir)` reads a version 2 package and returns the version 3
  package it converts to, in memory; `afspec.MigrateV2(dir)` writes that
  package to `dir`, every file through the same temporary files and renames a
  save uses, removes `test_spec.json` and writes `spec.md`. Both refuse a
  package whose `schema_version` is not 2, a version 2 package that does not
  load, and one with a criterion no test verifies or a path no smoke test
  verifies, naming the gap.
- The conversion, which is the same for every package:
  - every `REQ` and `PATH` id loses its `{spec_id}-` prefix, in every file;
  - `updated_at` moves to `progress.json`; a set `owner` becomes the
    extension key `x-owner` and a non-empty `tags` becomes `x-tags`;
    `schema_version` becomes `3`; `profile` is not written;
  - `introduction` is dropped; a non-empty `glossary` becomes a `## Glossary`
    section appended to the PRD body, one `- **term**: definition` line per
    term in term order;
  - a `complex_event` criterion becomes `event_driven` with the condition
    `{condition} and {guard}`;
  - when every criterion of a requirement carries the same `system`, it
    moves to the requirement;
  - a criterion's check comes from the tests that verify it, in artifact
    order, smoke tests aside. The criterion owns a test when it is the first
    criterion of this package in the test's `verifies`. A criterion that owns
    tests gets its own check: the kind of the first, the `given` lines of all
    in order without repeats as `setup`, their `pseudocode` joined by a
    newline as `pseudocode`; a criterion without a contract takes the first
    owned test's `then` lines joined by `; ` as its contract. A criterion
    that owns none but is verified by a test gets `same_as` the owner of the
    first such test; one verified by smoke tests alone gets `same_as` the
    first path of the first such test;
  - a path's smoke check comes from the smoke tests that verify it: their
    `real_components`, `given` and `then` lines in order without repeats as
    `real_components`, `setup` and `end_state`, their `pseudocode` joined by
    a newline;
  - a task loses `kind`, `tests`, `state` and `optional`; the integration
    task is removed from the plan and its state becomes the progress entry of
    task `N+1`; every other task's state becomes its progress entry;
  - extension keys of `prd.md`, `requirements.json` and `tasks.json` are kept
    on their objects, under the new ids; those of `test_spec.json` and of the
    removed integration task are lost, which the format reference says.
- The migration changes no `## Intent`, no `intent_hash` and no status.
- The ten version 2 packages under `.specs/archive/` are migrated in this
  change with `speca --migrate --all`, and `speca --list --all` reports each
  as valid afterwards.

### 8. The generator

- `spec` runs two artifact phases, `requirements` and `tasks`; the phase
  `test_spec` and its tier entries are gone, and `--phase-model test_spec=`
  is a usage error naming the phases.
- The PRD phase may submit `profile: lite` when the work is at most three
  requirements and no flow crosses components end to end; the package's
  frontmatter records it. The requirements prompt is told the profile: a
  `lite` package writes no execution path and at most three requirements, a
  `full` one at least one path.
- The requirements prompt asks for a check on every criterion and a smoke
  check on every path, with the rules of version 2's test prompt where they
  still apply: one check per behaviour, variants as a table, a property check
  only for a real invariant, a smoke check that asserts the end of its path,
  no artifact checks, and the existing tests of the repository named in
  `pseudocode`.
- The tasks prompt asks for tasks with `criteria` and `steps` and no tests,
  kind, state or integration task, and says the integration task is derived.
- The plan review and revision take the two artifacts; the rendered plan is
  the requirements and the tasks; the revision tool's frozen fields are the
  identity and the actors.
- The generator's hints (`vague_language`, `line_reference`) and
  `omitDefaultDependencies` are unchanged. The canary test still passes.
- The result's `tests` count is gone; `traceability` reports
  `criteria_covered`, `criteria_uncovered`, `paths_covered`,
  `paths_uncovered` and `criteria_unowned`.

### 9. The skills

- `af-spec` writes `prd.md`, `requirements.json` and `tasks.json`, takes the
  verdict after each, renders, validates and activates as today; it never
  writes `progress.json` or an integration task; it chooses `lite` with the
  person for a package of at most three requirements and no end-to-end flow.
  Its references describe the version 3 fields.
- `af-impl` reads the three artifacts, works through the plan's tasks and
  then the integration task `N+1`, claims and records each through `speca`,
  writes each criterion's check as the project's test and names the criterion
  id in the commit's traceability line, and expects `git diff` after a task
  move to hold `progress.json` alone.
- `af-prd` and the shared references name the three artifacts and the
  profile.
- Every skill body stays within the 24,576-byte ceiling.

### 10. Documentation and reference

- `specification/spec-format-v3.md` is the reference: the files, the
  frontmatter, `requirements.json` with checks and smoke checks,
  `tasks.json`, `progress.json`, the derived integration task, the profiles,
  the ids, the rules C1 to C8, the warnings, rendering, the generation
  contract, and a §13 that maps every version 2 construct to its version 3
  form and states the conversion above. `spec-format-v2.md` stays, as the
  superseded reference, as `spec-format.md` stayed.
- `specification/schemas/*.v3.json` are canonical and `afspec/schemas/`
  holds copies of the four; `make json-gen` generates the Go types from them.
  The version 2 schemas stay in `specification/schemas/`.
- `README.md`, `docs/api.md`, `docs/cli.md`, `docs/speca.md`,
  `docs/specr.md`, `docs/configuration.md`, `docs/architecture.md`,
  `afspec/README.md` and `examples/README.md` describe version 3; the three
  examples build, validate and work through a version 3 package.
- An ADR records the migration and the decisions below. `make doc` is run.

## Design Decisions

1. **The library speaks one version.** Reading two formats doubles every
   rule and every render. A version 2 package is a legacy package, as a
   version 1 package is today, and the migration is the one path forward.
2. **The migration lives in the library and in `speca`.** `speca` already
   writes packages in every status and sweeps the spec root; a migration is
   a format move, not an edit, so it writes a sealed or archived package.
3. **The check keeps three fields.** `kind`, `setup` and `pseudocode` are
   what a test adds that its criterion lacks; `then` is the contract, and a
   criterion without one takes it from the test, so nothing a test asserted
   is lost. `title` and `when` restate the criterion and are dropped.
4. **`same_as` never chains.** A shared check is on the criterion that owns
   the test and every other criterion points at it, so a reader follows one
   link.
5. **The integration task is task `N+1`.** Every tool addresses a task by its
   id, so giving the derived task the next id keeps `--task`, `next_task`,
   the claim and the render unchanged. Its state is a progress entry like any
   other.
6. **Progress is a separate file, the status is not.** The status is a
   property of the contract: activation hashes the intent and sealing closes
   the work. A lifecycle move rewrites `prd.md`; a task move never does.
7. **`spec.md` does not show progress.** Showing it would re-render the file
   on every task move, which S8 removes; the JSON of `speca` and `specr`
   carries the states.
8. **`updated_at` is the progress file's.** It changes with every task move
   and never with the contract.
9. **`system` moves up only when every criterion agrees.** A default that a
   criterion without `system` would inherit changes that criterion's
   sentence, so the migration never introduces one.
10. **The glossary moves to the PRD.** It is documentation, and the PRD is
    where documentation lives; the migration appends it after the last
    section, outside `## Intent`.
11. **The migration refuses a package with a gap** rather than inventing a
    check: a criterion no test verifies or a path no smoke test verifies has
    no deterministic check, and the ten packages have none.
12. **`lite` is the PRD phase's choice.** It already sizes the work for a
    split, and the profile is a size decision; a validator rule then holds
    the package to it, so a `full` package with no path is a defect and a
    `lite` package with four requirements is one.
13. **The version 1 packages are not migrated.** No code read them before
    this change and none will after; a second migration for history nobody
    runs is weight.

## Dependencies

| Spec or document | Reason |
|---|---|
| `docs/spec-format-simplification.md` | The source: §3 S1, S3, S4, S6, S8, §5 O2, §6. |
| `docs/prd/05` and ADR 18 | The first half of §6, already landed: the scoped render, the generator hints, the extension keys, which this change keeps. |
| ADR 14 | `spec.md` beside the artifacts; this change keeps it and keeps the progress out of it. |
| ADR 10 and 11 | `speca`'s forms and the verdict across the spec root, which gain the migrate form and lose the glossary rule. |
| ADR 17 | The plan review, which takes two artifacts after this change. |

## Verified External API

None. The change uses the standard library, `goccy/go-yaml` and
`santhosh-tekuri/jsonschema/v6` as `afspec` already does, and
`atombender/go-jsonschema` to generate the types, as `make json-gen` already
does.
