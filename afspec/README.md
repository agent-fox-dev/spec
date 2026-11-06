# afspec

Go library for the agent-fox specification format, version 3. Load, validate,
mutate, save, render and migrate specs with byte-for-byte round-trip fidelity.

The format itself is specified in
[`specification/spec-format-v3.md`](../specification/spec-format-v3.md). The
JSON Schemas this library compiles and embeds, in `schemas/`, are copies of the
canonical ones in [`specification/schemas/`](../specification/schemas/):
`//go:embed` cannot reach outside the package directory. Change a schema in
`specification/schemas/` first, copy it here, then run `make json-gen`.

## Installation

```bash
go get github.com/agent-fox-dev/spec/afspec
```

## Quick start

```go
package main

import (
    "fmt"
    "log"

    "github.com/agent-fox-dev/spec/afspec"
)

func main() {
    spec, err := afspec.LoadSpec(".specs/01_foundation")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(spec.Title, spec.Status)

    // Schema validation plus the cross-file rules C1 to C8.
    result := spec.Validate()
    if !result.Valid {
        for _, e := range result.Errors {
            fmt.Printf("[%s] %s: %s\n", e.Check, e.Artifact, e.Message)
        }
    }

    fmt.Println(spec.RenderCombined())
}
```

Runnable programs that create a spec, validate one and work through its
tasks are in [`examples/`](../examples/README.md).

## What version 3 changes

| Area | Version 2 | Version 3 |
|---|---|---|
| Tests | a fourth artifact, `test_spec.json`, one `tests` list with `kind` and `verifies` | gone; every criterion carries its `check` (`kind`, `setup`, `pseudocode`, or `same_as` another's) and every path its `smoke` check, required by the schema |
| Criteria | six patterns, `guard` on `complex_event`; `system` on every criterion | five patterns (a second condition is written into `condition`); `system` optional on the criterion, with the requirement's as the default |
| Progress | `state` on every task of `tasks.json`; `updated_at` in the PRD frontmatter | `progress.json`, written by the tools, holding every task's state and `updated_at`; a task move writes that file alone |
| The integration task | authored, last in the plan, owning every smoke test | derived from the package as task `N+1`; not stored |
| Tasks | `kind`, `tests`, `state`, `optional` | `id`, `title`, `criteria`, `steps`, `touches`, `depends_on`, `done_when` |
| IDs | `{spec_id}-REQ-N`, `{spec_id}-REQ-N.C`, `{spec_id}-PATH-N`, `TS-NN-N`, task ids | `REQ-N`, `REQ-N.C`, `PATH-N`, task ids |
| Profiles | — | `full` (at least one path, an integration task) or `lite` (at most three requirements, no path) |
| Dropped fields | | `introduction`, `glossary` (a `## Glossary` section of the PRD), `owner`, `tags` |
| Cross-file rules | C1 to C11 | C1 to C8 |

## Key types

| Type | Description |
|------|-------------|
| `Spec` | PRD frontmatter fields (`Profile` among them), PRD body, the two JSON contract artifacts, the progress (`Progress`, nil for a package without `progress.json`) and `UpdatedAt`, from the progress |
| `RequirementsV3Json` | Requirements artifact (generated from the JSON Schema); `Check` and `Smoke` are its check types |
| `TasksV3Json` | Tasks artifact (generated from the JSON Schema) |
| `ProgressV3Json`, `TaskProgress` | Progress artifact (generated from the JSON Schema): one `{Id, State}` per task |
| `PlannedTask` | One task of the plan: a `Task` with its `Kind` (`TaskKindImplement` or `TaskKindIntegration`) and its `State` |
| `TaskState` | `TaskStatePending`, `TaskStateInProgress`, `TaskStateDone`, `TaskStateDropped` |
| `ValidationResult` | `Valid`, `Errors`, `Warnings` |
| `ValidationEntry` | One error or warning: category, check, artifact, message, entity |
| `SpecMeta` | Lightweight metadata for discovery |
| `DependencyGraph`, `DependencyEdge` | Inter-spec dependency graph from `tasks.json` |
| `CoverageReport` | Derived coverage: `Covered` and `Uncovered` ID lists |
| `TraceabilityMatrix`, `TraceLink` | Derived criterion → check → tasks matrix |
| `PartialSpec` | The artifacts a generation run has produced so far, and the profile |
| `BootstrapSpec` | Incremental builder with deferred validation via `Finalize()` |

The constants `SchemaVersion` (`3`), `ProfileFull`, `ProfileLite`,
`RequirementsFileName`, `TasksFileName`, `ProgressFileName`,
`RenderedFileName` and `IntegrationTaskTitle` name what the format fixes.

## API overview

### I/O

| Function | Description |
|----------|-------------|
| `LoadSpec(dir) (*Spec, error)` | Load all artifacts from a spec directory; `progress.json` may be absent, and every task is then `pending` |
| `(*Spec).Save(dir) error` | Atomic save of every artifact, `progress.json` and `spec.md`, with lifecycle guards |
| `(*Spec).SaveProgress(dir) error` | Write `progress.json` alone, atomically, with the same guards: what a task move writes |
| `(*Spec).IsScaffold() bool` | Report whether the spec has no requirements or tasks yet |
| `(*Spec).IsLite() bool` | Whether the package's profile is `lite` |
| `CreateSpec(specID, specName) *Spec` | Create a draft scaffold |
| `MarshalJSON(v) ([]byte, error)` | Deterministic serialization in schema field order |

### Validation

| Function | Description |
|----------|-------------|
| `(*Spec).Validate() ValidationResult` | Schema validation, then the cross-file rules |
| `(*Spec).ValidateSchema() ValidationResult` | The bundled JSON Schemas: the PRD frontmatter, the two contract artifacts and, when present, the progress |
| `(*Spec).ValidateCrossFile() ValidationResult` | Rules C1 to C8 plus the never-blocking warnings |
| `ValidateCrossSpec(specs, graph) ValidationResult` | Dependencies exist, the graph is acyclic, actors are shared where both packages have paths |
| `(*Spec).ValidateStructured() map[string]any` | Structured output for CLI consumption |
| `ValidateArtifactSchema(artifact, schema, name)` | Validate one decoded artifact |

The cross-file rules, all errors:

| Rule | What it requires |
|---|---|
| C1 | `spec_id` and `spec_name` agree across the PRD, the two contract artifacts, `progress.json` when present, and the folder name |
| C2 | Every ID matches its format (`REQ-N`, `REQ-N.C` under `REQ-N`, `PATH-N`, a positive task id) and is unique in its family |
| C3 | Every `check.same_as` names a criterion of this spec whose check is its own, and not the criterion itself, or a path of this spec |
| C4 | Every `task.criteria` entry resolves to a requirement or a criterion; every `depends_on` entry resolves to a lower task id |
| C5 | Every criterion is owned by at least one task, directly or through its requirement |
| C6 | Every `unwanted` criterion has a non-empty contract |
| C7 | A `full` package has at least one execution path; a `lite` package has none and at most three requirements |
| C8 | Every entry of `progress.json` names a task of the plan or the integration task, each at most once |

A criterion without a check and a path without a smoke check are schema
errors, not rules. A package that declares another `schema_version` gets one
`schema_version` error that names `speca <package> --migrate`.

### Generation

| Function | Description |
|----------|-------------|
| `GenerationSteps` | The mandatory order: requirements, tasks |
| `DecodeArtifact(step, content)` | Decode a model's tool input into the typed artifact |
| `ValidateGenerationStep(step, partial)` | The artifact's schema plus every rule decidable at that step: after requirements C1, C2, C3, C6 and C7; after tasks C4 and C5 as well |
| `ValidateGenerationStepComplete(step, partial)` | The same, without stopping at a schema failure: every rule runs, so a repair loop hears of every problem at once |
| `FormatValidationEntries(entries) string` | Render violations as repair feedback |

### Lifecycle

| Function | Description |
|----------|-------------|
| `(*Spec).Transition(target, dir) (*Spec, error)` | Transition status, persist, return a new copy |
| `(*Spec).Supersede(specID, dir) (*Spec, error)` | Mark a sealed spec superseded with a deprecation banner |
| `ValidTransition(current, target) bool` | Whether a status transition is allowed |
| `MoveToArchive(specDir, root) error` | Transition to archived (a superseded spec too) and move under `archive/`; a taken destination is found before anything is written |
| `ArchiveDestination(specDir, root) (string, error)` | `<root>/archive/<name>`, or a `*SaveError` when it is taken or cannot be checked; writes nothing |
| `MoveUnderArchive(specDir, root) (string, error)` | Move an archived or superseded package under `archive/` and rewrite no file, so its status stays; the status is written first, with `Transition` or `Supersede` |
| `ComputeIntentHash(body) (string, error)` | SHA-256 of the `## Intent` section |
| `HasIntent(body) bool` | Whether `ComputeIntentHash` can hash the body: `## Intent`, spelled exactly so, on a line of its own |

### Migration

| Function | Description |
|----------|-------------|
| `LoadV2(dir) (*Spec, error)` | Read a version 2 package and return the version 3 package it converts to, in memory, by format v3 §13; nothing is written |
| `MigrateV2(dir) error` | Write that package in place, whatever its status, through the temporary files and renames a save uses, and remove `test_spec.json` |

Both refuse a package whose `schema_version` is not 2, one that does not
load, and one with a criterion no test verifies or a path no smoke test
verifies, naming the gap. The conversion is deterministic and changes no
`## Intent`, no `intent_hash` and no status; `speca <package> --migrate` runs
it, and `speca --migrate --all` over a spec root and its archive.

### Discovery

| Function | Description |
|----------|-------------|
| `DiscoverSpecs(root) ([]SpecMeta, error)` | Scan a root for spec directories |
| `BuildDependencyGraph(metas, root) (*DependencyGraph, error)` | Build the graph from each `tasks.json` |
| `IsSpecDirName(name) bool` / `ParseSpecDirName(name)` | The `{NN}_{snake_case_name}` pattern |
| `IsSpecName(name) bool` | The name part of that pattern, `[a-z][a-z0-9]*(_[a-z0-9]+)*`: a name it accepts always makes a valid directory |
| `LoadSpecLandscape(root, includeArchive, currentSpecID)` | Metadata for landscape views |
| `UnknownFields(raw, target) []string` | The keys in a decoded JSON value that the target's type has no field for, as JSON pointers. `DecodeArtifact` refuses an artifact that has any; `LoadSpec` keeps them aside and `ValidateSchema` reports each as an `unknown_field` error, so the schemas' `additionalProperties: false` holds although the decode would drop the key. An extension key (`x-…`) is not listed |
| `IsExtensionKey(key) bool`, `ExtensionPrefix` | A key that begins with `x-` is an extension key, a team's own metadata, which every schema allows on every object. `LoadSpec` keeps the extension keys of the JSON artifacts and the PRD frontmatter, and every save writes each back on the object it came from, found by its `id` or its position; `DecodeArtifact` refuses one in a model's artifact |

### Rendering

| Function | Description |
|----------|-------------|
| `(*Spec).RenderCombined(opts…) string` | One concatenated Markdown document: the PRD, the architecture, the requirements, the tasks |
| `(*Spec).RenderIndividual(opts…) map[string]string` | Per-section Markdown, keyed `prd`, `architecture`, `requirements`, `tasks` |
| `(*Spec).RenderIndividualScoped(taskID, opts…)` | Scoped to one task; what a coder receives. The PRD is there without its `## Background` and `## Requirements`, which the task's criteria restate, and one line names what was left out. Scoped to the integration task, every path is in full and the criteria with a contract are listed under `## Contracts` |
| `(*Spec).RenderTasks() string` | The plan as Markdown: the test commands, the dependencies, every stored task, then the integration task |
| `(*TasksV3Json).Render()`, `(*RequirementsV3Json).Render()` | One artifact as Markdown; the tasks render holds the stored tasks only |
| `(*Spec).RenderedBytes() []byte` | What `spec.md` holds: `RenderedHeader`, a blank line and the whole `RenderCombined()`; every save writes it |
| `RenderedFileName`, `RenderedHeader` | `spec.md`, the file every save writes beside the artifacts, and its first line, which names `prd.md`, `requirements.json`, `tasks.json` and `architecture.md` |
| `WithMaxTokens(n) RenderOption` | Cap the estimated size, truncating progressively |
| `(Criterion).RenderEARSSentence() string` | The EARS sentence |
| `(Criterion).RenderEARS() string` | The sentence plus `→ contract` when there is one |

Without a budget, a render drops nothing from the contract. The `prd` part
opens with the frontmatter, one field per line. Every artifact field follows:
each criterion renders as its sentence, its contract and its check, each path
with its steps and its smoke check, and the tasks render begins with the test
commands (`spec_tests` included) and the cross-spec dependencies and ends
with the integration task. Two things are left out because they add nothing
to the metadata: the artifacts' `$schema` URIs and their own copies of the
spec's identity. A criterion's `pattern` is carried by its EARS sentence. The
progress is not the contract and is never rendered, so a task move leaves
`spec.md` byte for byte as it was.

A scoped render shows the frontmatter, every external API, the test commands
and the dependencies, since all of them belong to the whole spec. The
requirements that own the task's criteria, and the paths a `same_as` of those
criteria names, are in full; every other requirement, path and task, the
integration task included, is listed one line each, by id and title.
`WithMaxTokens` is the one way to drop detail: it removes the architecture
first, then the `setup` and `pseudocode` of every check and smoke check.

### Derivation

| Function | Description |
|----------|-------------|
| `(*RequirementsV3Json).ComputeCoverage() CoverageReport` | Which criteria carry a check that resolves, and which paths a smoke check |
| `(*Spec).ComputeTraceability() TraceabilityMatrix` | criterion → check → tasks, and path → smoke check → the integration task; each `TraceLink` has `Kind`, `ID`, `Check`, `Tasks`, `Covered` and `Owned` |

Neither is stored: format v3 §8.5 makes both derived.

### Criterion builders

| Function | Sentence |
|----------|----------|
| `UbiquitousCriterion(id, system, action)` | THE system SHALL action |
| `EventDrivenCriterion(id, condition, system, action)` | WHEN condition, THE system SHALL action |
| `StateDrivenCriterion(id, condition, system, action)` | WHILE condition, THE system SHALL action |
| `UnwantedCriterion(id, condition, system, action, contract)` | IF condition, THEN THE system SHALL action |
| `OptionalCriterion(id, condition, system, action)` | WHERE condition, THE system SHALL action |

An empty `system` leaves the field unset, so the requirement's `system`, then
`"system"`, is rendered. Every criterion needs a check:

| Function | Description |
|----------|-------------|
| `(Criterion).WithContract(contract)` | A copy carrying the contract |
| `(Criterion).WithCheck(kind, setup, pseudocode)` | A copy carrying its own check; `kind` is `CheckKindUnit`, `CheckKindIntegration` or `CheckKindProperty` |
| `(Criterion).WithSameCheckAs(id)` | A copy whose check is the same as the named criterion's, or as a path's smoke check |
| `OwnCheck(kind, setup, pseudocode) Check`, `SameAs(id) Check` | The two forms of a check, for a `Criterion` built by hand |
| `(Check).IsShared()`, `KindText()`, `PseudocodeText()` | Read a check |
| `(Requirement).Resolve(c) Criterion` | A copy of `c` with the requirement's `system` filled in when `c` names none |

### Mutation

Each function takes an artifact by value and returns an updated copy; the
input is never modified. The `Add…` functions fail on a duplicate ID
(`AddExternalAPI` on a duplicate package), except `AddDependency`, which
appends unconditionally.

| Function | Description |
|----------|-------------|
| `AddRequirement` / `GetRequirement` / `RemoveRequirement` | Requirements in `requirements.json` |
| `AddCriterion` / `GetCriterion` | Criteria within one requirement |
| `AddExecutionPath`, `AddExternalAPI` | Execution paths (each with its `Smoke`) and external API contracts |
| `AddTask`, `AddDependency` | Tasks and cross-spec dependencies in `tasks.json` |
| `NextRequirementID`, `NextCriterionID`, `NextExecutionPathID`, `NextTaskID` | The next free ID of each of the four formats |
| `LoadDependentInterfaces(specID, root)` | The upstream specs' interfaces; specs that fail to load are skipped |

### Tasks

A task's state lives in `progress.json`, so the plan and its moves are
methods of `Spec`. None of them changes the receiver: a move returns the
progress to write, which the caller assigns to `spec.Progress` and persists
with `SaveProgress`.

| Function | Description |
|----------|-------------|
| `(*Spec).Plan() []PlannedTask` | Every task in id order: the stored tasks, kind `implement`, then the integration task of a `full` package, kind `integration`, each with its state |
| `(*Spec).IntegrationTaskID() (int, bool)` / `IntegrationTask() (PlannedTask, bool)` | The derived integration task, task `N+1`: it depends on every stored task, touches what they touch, and its steps run every smoke check and confirm every contract is consumed (format v3 §8.4). False for a `lite` package and a package with no task |
| `(*Spec).GetTask(id) (*PlannedTask, bool)` | A copy of one task of the plan |
| `(*Spec).TaskState(id) TaskState` | The task's state from the progress, `pending` when it has no entry |
| `(*Spec).IsReady(id) bool` | Whether the task is in the plan, is `pending`, and has every dependency `done`. A task's dependencies are its `depends_on` when present (an empty list means none), otherwise the task before it in plan order (§8.2); the integration task is ready when every stored task is `done` or `dropped`. A dependency the plan lacks, or a `dropped` one, is not done |
| `(*Spec).NextTask() (*PlannedTask, bool)` | A copy of the task to work on: the first `in_progress` task in plan order, else the first ready one; `nil, false` when there is none |
| `(*Spec).TransitionTask(id, target) (*ProgressV3Json, error)` | Move one task through the state machine of §8.1 and return the progress to write |
| `(*Spec).CompleteTaskStates(ids)` / `ResetTaskStates(ids)` | The progress with the named tasks `done` or `pending`, bypassing the machine |
| `ValidTaskTransition(current, target) bool` | Whether a task transition is allowed |

### Lint

| Function | Description |
|----------|-------------|
| `RunLintSpecs(specsDir, lintAll) (LintResult, error)` | Validate every spec under a root and collect the findings; specs whose tasks are all done are skipped unless `lintAll` |
| `DiscoverLintSpecs(specsDir, filterSpec)` | The specs a lint run would look at |
| `SortFindings(findings)` / `ComputeExitCode(findings)` | Order findings by severity, and the exit status they imply |

### Older versions

The library reads version 3 only. A spec that declares an older
`schema_version` fails validation with a single `schema_version` error rather
than one schema error per difference; for a version 2 package the error names
`speca <package> --migrate`, which converts it in place with `MigrateV2`
([Migration](#migration)). Section 13 of the
[format reference](../specification/spec-format-v3.md) maps each version 2
construct to its version 3 form. A version 1.3 package has no conversion.

### Error types

| Type | When returned |
|------|---------------|
| `SpecError` | Base error; every afspec error unwraps to it via `errors.As` |
| `LoadError` | Missing or malformed spec files (carries `File`); also a package the migration refuses |
| `SaveError` | Disk write failures |
| `LifecycleError` | Invalid spec or task state transitions |
| `IntentError` | Missing `## Intent` section, or intent drift on an active spec |
| `BootstrapError` | `BootstrapSpec.Finalize` failures |

## Concurrency

This package provides no goroutine-safety guarantees. `Spec` instances are not
safe for concurrent use. Synchronize externally, or confine each `Spec` to one
goroutine.

## Code generation

`RequirementsV3Json`, `TasksV3Json`, `ProgressV3Json` and
`PrdFrontmatterV3Json` are generated from the schemas in `schemas/` with
[go-jsonschema](https://github.com/atombender/go-jsonschema), run from the
repository root:

```bash
make json-gen
```

They are generated with `--only-models`. Structural checking belongs to
`Validate`, which runs the compiled schemas and reports violations with the
rule that failed; generated `UnmarshalJSON` methods would duplicate that and
would also reject the empty scaffold `CreateSpec` writes, which `Save` and
`LoadSpec` have to round-trip before the artifacts are generated.
