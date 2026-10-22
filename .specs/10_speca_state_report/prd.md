---
spec_id: "10"
spec_name: "speca_state_report"
title: "speca: report a spec package's state from the shell"
status: "active"
created_at: "2026-10-22T07:23:01.807482Z"
updated_at: "2026-10-22T07:23:01.807482Z"
intent_hash: "72a28f44629e25835193cef851f8a7f699d3f0bb567baa252077ecd8d1da5c51"
schema_version: 2
source: "docs/prd/02-change-the-state-of-a-package-with-its-own-command.md"
---
## Intent

A spec package has two kinds of state: its lifecycle status and the state of each task. An agent working from a shell needs to read both through the library, not by grepping the files. This spec adds `speca`, the spec admin command, with its read-only forms. It reports one package's state, gives the format's full verdict on a package, and lists every package under the spec root. It uses the same single-input, single-envelope interface as `spec` and `specr`, and it lays the shared groundwork that the commands which change state will build on.

## Goals

- `speca <spec>` returns one JSON envelope with the package's identity, its status, whether it validates (naming the failing rules when it does not), whether its intent has drifted, every task with its state and readiness, task counts, the next task to work on, the lifecycle moves allowed from its status, and the packages it depends on and that depend on it.
- `speca <spec> --validate` returns the format's verdict on the package: schema errors, the cross-file rules C1–C11, and the cross-spec rules that concern it. Each finding names its rule. The derived traceability uses the same shape as `spec`'s result. An invalid package exits 0.
- `speca --list` returns one entry for every package under the spec root, in number order, each with its status, validity, task counts and next task. `--all` adds the packages under `archive/`. A package that fails to load is listed with its error and does not fail the call.
- `afspec` gains `(*TasksV2Json).IsReady` and `(*TasksV2Json).NextTask`. `speca`, `specr` and `examples/tasks` all use them, so "ready" and "next" mean the same thing everywhere.
- The package-reference resolver exists once, in a package that `specr` and `speca` both import. `specr`'s behaviour is unchanged apart from the new `ready` field on its tasks.
- `speca` follows the interface of `spec` and `specr`: exactly one positional input (none for `--list`), exactly one JSON object on stdout on every program-driven path, help on stderr, `--schema` (checked in as a golden file), `--version`, and the exit codes 0/1/2. It writes no file, makes no model or network call, and reads no credential.
- `make build` installs `speca` beside `spec` and `specr`, stamped the same way.

## Non-goals

- Changing a task's state (`--task N --state S`), `--force`, `--dry-run`, the `lifecycle` error category and the task warnings. That is the next scope, `speca_task_states`.
- Changing a package's lifecycle status (`--status active|sealed|archived|superseded`, `--by`), the library move under `archive/` that keeps `superseded`, and recording `supersedes` on the replacing package. That is scope `speca_lifecycle`.
- Making `spec --help`, `specr --help` and their `--schema` descriptions name the other commands, having `specr`'s `next` suggest `speca` calls, and adding the claim and done calls to the `af-impl` "one task at a time" example. That is scope `state_command_cross_links`. `speca`'s own help does name its siblings in this scope.
- Editing package content (requirements, tests, tasks, PRD body, `architecture.md`), or creating a package.
- Rendering, which stays with `specr`.
- Reading format version 1 packages beyond what `LoadSpec` already does. As with `specr`, a v1 package loads, but its tests and tasks are not read.
- A smaller binary. Like `specr`, `speca` links `internal/cli` and, through it, the model stack (ADR 05, Consequences), but it never uses that stack.

## Background

`afspec` implements both state machines. `ValidTransition` and `(*Spec).Transition` in `afspec/lifecycle.go` cover the lifecycle table (`draft → active|archived`, `active → sealed`, `sealed → superseded|archived`, `superseded → archived`). `ValidTaskTransition` and `(*TasksV2Json).TransitionTask` in `afspec/task.go` cover §8.3.1. Validation is `(*Spec).Validate` (schema, then C1–C11) and `ValidateCrossSpec(specs, graph)` (§10.4). Discovery and the graph are `DiscoverSpecs` and `BuildDependencyGraph`. None of this can be reached from a shell. `README.md` and `docs/architecture.md` both say lifecycle management "has no CLI surface". So an agent reads `tasks.json` by hand to find out which task is next, and it cannot ask whether a package validates without running a Go program.

The library cannot compute "the task to work on now". `examples/tasks/main.go` does it in `pick`: the first `in_progress` task, otherwise the first `pending` task whose `depends_on` are all `done`. Format v2 §8.3 says an absent `depends_on` means "the previous task", and `pick` ignores that rule: it treats a task without `depends_on` as having no dependencies.

ADR 05 built `specr` (`cmd/specr`) for the read side of rendering, on `internal/cli` (`NewRun`, `Run.Warn`, `Run.Envelope`, `Emit`, `SplitArgs`, `EncodeJSON`, `FlagsDocument`, `EnvelopeDocument`). The resolver it uses for a package reference (`locate`, `pathCandidates`, `isSpecDir`, `specDirs`, `matching` in `cmd/specr/locate.go`) is private to the command, and so is its task summary (`taskInfos` and `taskInfo` in `cmd/specr/render.go`). `spec` builds its validation and traceability report with the private helpers `reportOf`, `itemOf`, `traceOf` and `unownedTests` in `specgen/pipeline.go`. A third command that needs the same things would have to copy all of them.

The public output types live in `envelope`, which imports only the standard library: `RenderResult`, `TaskInfo`, `ValidationReport`, `TraceReport`, `Decode[R]`, `DecodeSpec`, `DecodeSpecr`.

## Requirements

### 1. Readiness and the next task in `afspec`

- `(*TasksV2Json).IsReady(id int) bool` reports whether the task with that id exists, is `pending`, and has every dependency `done`. A task's dependencies come from its `depends_on` when the field is present, and an explicitly empty list means none. When the field is absent, the dependency is the task immediately before it in plan order, and the first task has none (format v2 §8.3). A dependency id the plan does not have counts as not done, and so does a `dropped` dependency. An unknown task id, or a nil receiver, gives false.
- `(*TasksV2Json).NextTask() (*Task, bool)` returns a copy of the first `in_progress` task in plan order. Failing that, it returns the first task for which `IsReady` is true. Otherwise it returns `nil, false`, including on a nil receiver or an empty plan.
- `examples/tasks`'s `pick` uses `NextTask` when called with id 0. It keeps its two distinct messages, "every task is done or dropped" and "no pending task has all its dependencies done", and its existing tests still pass.

### 2. A shared package for what the commands know about a package on disk

A new package, `internal/specpkg`, holds the following, so that `specr`, `speca` and `specgen` share one copy:

- **The resolver**, moved from `cmd/specr/locate.go` with its behaviour unchanged. A reference is tried as a path (to the package directory or a file in it, relative to the working directory and then to `--dir`), and then as a directory name, number or name under the spec root. An archived package is reached only by its path, and the "no match" message names one that matches. The resolver refuses `-`, a URL and a multi-line reference. Every refusal is a usage error, and its message names the calling tool (`specr …` or `speca …`), which the caller passes in.
- **The listing** of `NN_name` directories under a root, which the resolver already uses.
- **The display rule** for a package path: relative to `--dir` when the path is inside it, absolute otherwise.
- **The task summary** builder, which produces `envelope.TaskInfo` for each task in plan order, including the new `ready` field (from `IsReady`).
- **The conversion** of an `afspec.ValidationResult` into `envelope.ValidationReport`, and of a spec's derived traceability into `envelope.TraceReport`. These move from `specgen/pipeline.go`, and `specgen` calls them. `spec`'s output does not change.
- **The rule names** of a set of validation errors: the distinct `Check` of each error (or its `Category` when `Check` is empty), in order. This is `specr`'s `rules` helper.

`specr` switches to the shared resolver, display rule and task summary. Its output is unchanged except that each entry of `result.tasks` and `result.task` carries `ready`. `cmd/specr/testdata/schema.golden.json` is regenerated for the new field.

### 3. The command and its interface

- `cmd/speca` builds a `speca` binary invoked as `speca [flags] <spec>` or `speca --list [--all]`. Flags may come before or after the input (`cli.SplitArgs`).
- The flags in this scope are:
  - `--validate`: the full verdict.
  - `--list`: every package under the spec root.
  - `--all`: with `--list`, also the packages under `archive/`.
  - `--specs-dir` and `--dir`: the same meaning as for `specr`.
  - `--schema` and `--version`.
- On every program-driven path, stdout receives exactly one envelope, built by `internal/cli`, with `tool` set to `speca`.
- `-h` and `--help` print the help to stderr and exit 0.
- A bare invocation prints the help and exits 2, and it also writes a usage envelope when stdout is not a terminal. An invocation that has no positional input and is not `--list` counts as bare.
- `--version` prints `speca <identity>` and exits 0.
- `--schema` prints a self-description and exits 0 without reading anything. The description holds `tool`, `description`, `input` with examples, `flags` from `cli.FlagsDocument`, `result` from `cli.EnvelopeDocument` (leaving out `side_effects`, `input`, `model` and `usage`), and `exit_codes`. It is checked in at `cmd/speca/testdata/schema.golden.json` and guarded by a golden test that `UPDATE_GOLDEN=1` regenerates.
- The help names the forms and examples. Near the top, one line names the sibling commands: `spec` writes a package from an idea, `specr` renders one, `speca` reports its state. The `--schema` `description` carries the same sentence.
- These are usage errors (exit 2, stage `usage`, category `usage`, nothing read):
  - more than one positional input;
  - a positional input with `--list`;
  - `--validate` together with `--list` (two operations);
  - `--all` without `--list`;
  - `-`, a URL, or a multi-line input;
  - `--dir` that is not a directory;
  - a reference that names no package or names two;
  - an undefined flag.

  The message is also printed to stderr, as `specr` does.
- `speca` never writes a file. It makes no model call and no network call, and it reads no credential or model environment variable.

### 4. The report: `speca <spec>`

The command resolves the reference, loads the package with `afspec.LoadSpec`, and returns these fields in `result`:

- **Identity.** `spec_dir` (by the display rule), `spec_id`, `spec_name`, `title`, `status`, `created_at` and `updated_at`. Also `intent_hash` when it is set, and `supersedes` and `tags` when they are not empty.
- **`valid`.** The package's own verdict from `(*Spec).Validate`, always present in this form. When it is false, `failing_rules` lists the failing rule names.
- **`intent_drift`.** True when the package is `active`, has an `intent_hash`, and its PRD body either no longer hashes to that value with `afspec.ComputeIntentHash` or has no `## Intent` section. Absent otherwise.
- **`tasks`.** Every task in plan order as `TaskInfo` (`id`, `title`, `kind`, `state`, `optional`, `depends_on`, `ready`). It is an empty list when the plan has no tasks.
- **`task_counts`.** `pending`, `in_progress`, `done`, `dropped` and `total`.
- **`next_task`.** The task `NextTask` returns, as `TaskInfo`. Absent when there is none.
- **`allowed_status`.** Each of the five lifecycle states, in the order `draft`, `active`, `sealed`, `superseded`, `archived`, for which `afspec.ValidTransition(status, target)` holds. It is an empty list for a terminal status.
- **`depends_on`.** The `spec_id`s in the package's own `tasks.json` `dependencies`, in file order with duplicates removed. It is an empty list when there are none.
- **`dependents`.** The `spec_id`s of other packages under the spec root whose dependencies name this package, found with `afspec.DiscoverSpecs` and `afspec.BuildDependencyGraph`, sorted with duplicates removed.

### 5. Report edge cases

- **The spec root cannot be read.** This covers a root that is missing, not a directory, or unreadable. `dependents` is then absent and the call raises a `low` `specs_dir_unreadable` warning naming the root. A dangling dependency reported by `BuildDependencyGraph` does not stop the graph from being used.
- **The package cannot be loaded.** Exit 1 at stage `load`, category `input`. The message names the directory and the library's reason, and `result` is absent.
- **A package in another format version.** This means a PRD `schema_version` that is neither absent nor 2. It is reported with whatever loaded and a `high` `spec_invalid` warning saying the package's tests and tasks are not read, as `specr` warns. A version 2 package that is invalid raises no warning, because `valid` and `failing_rules` already give the answer.

### 6. The verdict: `speca <spec> --validate`

- The result carries:
  - the identity fields of the report;
  - `intent_drift`;
  - `valid`: the combined verdict;
  - `validation`: an `envelope.ValidationReport` holding `valid`, `error_count`, `warning_count`, and each error and warning with its `check`, `artifact`, `entity` and `message`;
  - `traceability`: an `envelope.TraceReport` holding `criteria_covered`, `criteria_uncovered`, `paths_covered`, `paths_uncovered` and `tests_unowned`.

  The fields that are specific to the report are absent.
- `validation` combines `(*Spec).Validate` with the cross-spec rules. To apply those rules, every package under the spec root that loads is loaded, together with the target package when it is not already among them (an archived package, for example). A graph is built from those packages, and `afspec.ValidateCrossSpec` runs over them.
- From the cross-spec result, these findings are kept:
  - the errors whose entity is the package's `spec_id`;
  - the errors that name no entity (the dependency cycle), but only when the package has at least one edge in the graph;
  - the warnings whose entity is a term defined in the package's own glossary.
- `valid` is true exactly when no error was kept from either source.
- If the spec root cannot be read, the cross-spec rules are skipped with a `low` `specs_dir_unreadable` warning, and the per-package verdict is reported on its own.
- Packages under the root that fail to load are left out of the cross-spec rules without failing the call.
- An invalid package exits 0. A package that cannot be loaded exits 1 at stage `load`, category `input`. A package in another format version raises the same `high` `spec_invalid` warning as in the report.

### 7. The list: `speca --list [--all]`

- The command lists the `NN_name` directories under the spec root. It sorts them by their numeric prefix compared as a number, with ties broken by the full name, and it loads each one with `LoadSpec`.
- Each entry of `packages` holds `spec_dir`, `spec_id`, `spec_name`, `title`, `status`, `valid` (from `Validate`), `task_counts`, and `next_task` when there is one.
- A package that fails to load becomes an entry with only `spec_dir` and `load_error` (the library's message), and the call continues.
- `--all` then appends the directories under `<root>/archive/` in the same order, each marked `archived: true`. The flag refers to the directory, not the status: a superseded package under `archive/` is listed as `archived: true` with `status: superseded`. A missing `archive/` adds nothing and raises no warning.
- A spec root that does not exist or cannot be read fails the call: exit 1, stage `load`, category `input`, with a message naming the root and `fix_hint.flag` `--specs-dir`. A root that exists but holds no packages gives an empty `packages` list and exits 0.

### 8. `summary` and `next`

`summary` gives one sentence per form, capped by the shell at 200 characters:

- **Report.** The package directory name, its status, done tasks out of the total, and the next task when there is one. It says so when the package does not validate. Example: `speca: 16_indexed_code_search active, 3 of 7 tasks done; next task 4`.
- **`--validate`.** For example, `speca: 16_indexed_code_search validates` or `speca: 16_indexed_code_search does not validate (3 errors: C3, C7, cross_spec_actor)`.
- **`--list`.** The number of packages, with non-zero counts per status in lifecycle order, plus the number unreadable. Example: `speca: 9 packages (2 draft, 5 active, 2 sealed)`.

`next` suggests only calls that exist once this scope is done:

- **Report.** When there is a `next_task` N, `specr <spec_dir> --task N`. Its `why` says whether the task is in progress or is the next ready task.
- **`--list`.** For the first active package, in list order, that has no task in progress and has a ready task N: `specr <that spec_dir> --task N`.
- **`--validate`.** No suggestion.

### 9. The public output types

`envelope` gains the following, documented with `description` tags:

- `AdminResult`, the single result type for every form of `speca`. A field that a form does not produce is absent: `valid` is a pointer, and the slices that must show as empty in their own form use `omitzero`.
- `AdminEnvelope = Envelope[*AdminResult]` and `DecodeSpeca(data []byte) (AdminEnvelope, error)`.
- `TaskCounts`.
- `PackageInfo`, one entry of `packages`.
- `ready` on `TaskInfo`: true for a pending task whose dependencies are all done, absent otherwise.

`envelope` keeps importing only the standard library, and nothing already in it changes name or meaning. `cmd/speca`'s `Result` embeds `envelope.AdminResult` and adds `Summary()` and `Next()`, as `cmd/specr` does.

Errors in this scope:

| `stage` | `category` | When | Exit |
|---|---|---|---|
| `usage` | `usage` | any case in Requirement 3 | 2 |
| `load` | `input` | `LoadSpec` refused the package; `--list` on a spec root that cannot be read | 1 |
| `schema` | `internal` | the `--schema` document could not be encoded | 1 |

`retryable` and `resumable` are always false.

### 10. Build and documentation

- `make build` installs `./cmd/speca` with the same `LDFLAGS` as the other two commands, and the `build` target's help line names all three.
- Per the steering, the following are updated in the same change:
  - a new `docs/speca.md`, modelled on `docs/specr.md`, covering the forms, the input, the report, the output, errors, warnings, flags, `--schema` and calling it from an agent;
  - `README.md`: a section on `speca`, with the "no CLI surface" sentence corrected to say that reading state has a command while changing it is still library API;
  - `docs/architecture.md`: the layout (`cmd/speca`, `internal/specpkg`), a section on the speca command with its package graph, and the Consumers paragraph;
  - `docs/api.md`: `AdminResult`, `AdminEnvelope`, `DecodeSpeca`, `PackageInfo`, `TaskCounts`, and `TaskInfo.ready`;
  - `docs/specr.md`: `ready` in the task fields;
  - `afspec/README.md`: `IsReady` and `NextTask`;
  - a new ADR, `docs/adr/10-report-and-change-the-state-of-a-package-with-its-own-command.md`, recording the decision to build `speca`;
  - `docs/generated/`, regenerated with `make doc`.
- No test checks the content of the docs.

## Design Decisions

1. **Split into four scopes, with the read-only command first.** The full idea (report, task moves, lifecycle moves, cross-command links) is more than one spec's worth. The read-only forms establish the command, the shared resolver, the output type and the library's readiness rule, which every later scope builds on, and they can be tested without any write.
2. **Forms are flags, and a bare `speca <spec>` is the report.** This is the interface ADR 05 fixed for `specr`. It means that an accidental call never changes anything, and that an agent knowing only the command's name learns the package's state from it.
3. **`next` only suggests calls that exist after this scope.** A suggestion such as `speca --task N --state in_progress`, made before that form exists, would send an agent into a usage error. The task-states and lifecycle scopes add the claim, activation and seal suggestions when they add the forms.
4. **Readiness follows the format, not `pick`.** An absent `depends_on` means the previous task in plan order, as §8.3 says. An explicit empty list means no dependencies (`MarshalJSON` and `deepCopy` already keep nil and empty apart). Only `done` satisfies a dependency, because the format says "must be `done` first". For every existing fixture this gives the same next task as `pick`, since the first task never has `depends_on`.
5. **`NextTask` returns `(*Task, bool)` and `IsReady` takes an id.** This matches `GetTask`'s idiom of returning a copy plus a found flag. Readiness depends on the other tasks' states, so it is a method on the plan, not on `Task`.
6. **The shared code goes in a new `internal/specpkg`, not in `internal/cli` or `envelope`.** `envelope` must stay standard-library-only (`docs/architecture.md`). `internal/cli` does not import `afspec` today. A small package of its own keeps "a package on disk" apart from "the shell".
7. **`specgen`'s report converters move into `internal/specpkg`.** `spec` and `speca` then describe validation and traceability identically. The functions are private and no test calls them directly, so moving them is a pure refactor.
8. **The report's `valid` is the package's own verdict, and `--validate` adds the cross-spec rules.** `Validate` is what `spec` and `specr` call "valid". The cross-spec rules need every package under the root loaded, which is the job `--validate` is asked to do.
9. **How cross-spec findings are attributed.** The library tags unknown-dependency and actor errors with the downstream `spec_id`, which decides them. The cycle error names no single package, so it is attached to any package with an edge in the graph, because a package with no edges cannot be on a cycle. Glossary warnings carry the term, so they are attached when the package defines that term.
10. **`--list` lists directories itself instead of calling `DiscoverSpecs`.** `DiscoverSpecs` drops a package whose `prd.md` is missing or malformed, and such a package must still be listed with its `load_error`. Sorting is numeric because `100_x` sorts before `99_x` as text.
11. **`--list` on a spec root that cannot be read fails at `load`.** An empty list would hide a mistyped `--specs-dir`. A root that exists and is empty is a true "no packages". A missing `archive/` is normal.
12. **`depends_on` comes from the package's own `tasks.json`, and `dependents` from the root's graph.** The package's own dependencies are known even when the root cannot be read or the package sits outside it. Only the reverse direction needs the scan.
13. **`specs_dir_unreadable` keeps its declared stage, `preflight`.** `internal/cli`'s `warnStages` gives each code exactly one stage, and `spec` already raises this code. Changing the stage to `discover`, as the input suggested, would change an existing meaning.
14. **An invalid version 2 package raises no warning in the report or the verdict.** `valid` with `failing_rules`, or `validation`, is the answer that was asked for. A package in another format version gets `specr`'s `high` `spec_invalid` warning, because its tasks are silently missing.
15. **`--validate` exits 0 on an invalid package.** The question was whether it validates, and the answer is in `valid`. Exit 1 is for a call that could not do its job.
16. **One `AdminResult` serves every form.** Fields a form does not produce are absent, with a pointer for `valid` and `omitzero` for slices that must show as empty in their own form. One decoder, `DecodeSpeca`, then covers every form, and the later scopes add `changes` and `dry_run` to the same type.
17. **`specr` sets `ready` too.** Both commands build `TaskInfo` with one function, so a field on a shared type never means something different depending on which command wrote it.
18. **Archived packages are reached by path only.** This is the resolver's existing rule. It stops a name lookup from finding an archived package that shares its number with a live one.
19. **The ADR is written in this scope and covers the whole command.** The decision to build `speca` belongs to the change that creates it. The later scopes add forms under that decision.
20. **No smaller binary.** `speca` links `internal/cli` as `specr` does. ADR 05 left splitting the envelope out of the model stack for later, and nothing here needs it.
