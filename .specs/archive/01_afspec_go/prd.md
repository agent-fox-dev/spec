---
spec_id: '01'
spec_name: afspec_go
title: Afspec Go
status: draft
created_at: '2026-08-05T09:55:57.962704+00:00'
updated_at: '2026-08-05T09:59:42.706798+00:00'
owner: ''
source: docs/prds/afspecgo.md
schema_version: 1
---
# afspec-go: Go Implementation of the afspec Library

## Intent

The Python `afspec` library provides the canonical implementation for loading, validating, mutating, and rendering agent-fox specification packages. Go consumers (CLIs, servers, CI tools) currently cannot work with spec packages natively — they must shell out to Python or reprocess specs from scratch.

This project creates an idiomatic Go library (`afspec`) that provides the same public API surface as the Python package, enabling Go programs to be first-class citizens in the spec-driven development workflow. Byte-for-byte round-trip fidelity with the Python implementation is a hard requirement.

## Goals

- Provide a Go package that can load, validate, mutate, save, and render spec packages with the same semantics as the Python `afspec` library.
- Reuse the auto-generated Go structs from `make json-go` as the foundation for data types.
- Produce idiomatic Go code: exported methods on structs where the struct is the primary operand, `(result, error)` return signatures, standard library patterns, Go naming conventions.
- Maintain byte-for-byte round-trip fidelity: a spec loaded and saved by the Go library must produce identical output to the Python library.
- Share the same JSON Schema files for schema validation, embedded via `//go:embed`.
- Include `BootstrapSpec` for incremental spec population with deferred cross-file validation.

## Non-goals

- Porting `agentspec` (the AI-powered spec creation library).
- Porting the `spec` CLI.
- Porting `lint.py` (not part of the public API).
- Implementing a Go `pydantic`-equivalent model layer — the auto-generated structs from JSON Schema are sufficient.

## Functional Requirements

### I/O (load, save, marshal)

- `LoadSpec(dir)` loads all artifacts (`prd.md`, `requirements.json`, `test_spec.json`, `tasks.json`, optional `architecture.md`) from a spec directory and returns a `Spec` struct. Returns a `LoadError` on missing required files or malformed content. Standalone function (constructs the struct).
- `spec.Save(dir)` atomically writes all artifacts to disk using write-to-temp-then-rename, with lifecycle guards (sealed specs cannot be saved). Returns `SaveError` or `LifecycleError`.
- `MarshalJSON(v)` produces deterministic JSON serialization matching Python's output byte-for-byte. Struct fields are serialized in declaration order (matching the generated Go struct field order, which matches the JSON Schema property order). Any `map[string]T` values are serialized with keys sorted alphabetically. Implemented via a custom `MarshalJSON` function that walks struct fields via reflection and recursively sorts map keys. Go's `encoding/json` already preserves struct field declaration order. Standalone function.
- PRD loading must parse YAML frontmatter (delimited by `---`) and extract the markdown body separately. Edge-case behavior (missing closing delimiter, malformed YAML) must match the Python implementation.
- PRD frontmatter is written using a hand-written renderer with a fixed field order and explicit value formatting (mirroring Python's `_render_yaml_value()` and `_render_prd()`), not `yaml.Marshal`, to guarantee byte-for-byte fidelity.

### Validation

- `spec.Validate()` returns a `ValidationResult` containing errors and warnings. `Valid` is true when `Errors` is empty.
- `spec.ValidateSchema()` checks each artifact against the bundled JSON Schema files (embedded via `//go:embed`) plus EARS constraints and task group structure rules.
- `spec.ValidateCrossFile()` checks cross-file consistency: dangling references, coverage gaps, glossary completeness, ID format validation.
- `ValidateCrossSpec(specs, graph)` checks cross-spec interface consistency: API symbols, glossary conflicts, contract mismatches. Standalone function (operates across multiple specs).
- `spec.ValidateStructured()` returns validation results reshaped as a categorized map for CLI consumption. The return type is `map[string]any` with the following structure:
  - `"valid"` (`bool`): `true` when there are no errors.
  - `"errors"` (`[]map[string]any`): always present. Each entry has:
    - `"category"` (`string`): `"schema"` or `"integrity"`.
    - `"message"` (`string`): human-readable description.
    - Schema errors additionally include `"artifact"` (`string`), and optionally `"path"` and `"value"`.
    - Integrity errors additionally include `"check"` (`string`, the rule name), and optionally `"requirement_id"`.
  - `"warnings"` (`[]map[string]any`): present only when non-empty. Each entry has:
    - `"category"` (`string`): `"warning"`.
    - `"message"` (`string`): human-readable description.
    - `"entity_id"` (`string`): identifier of the entity that triggered the warning.
- JSON Schema validation uses `github.com/santhosh-tekuri/jsonschema/v6`.

### Lifecycle

- `spec.Transition(target, dir)` transitions a spec's lifecycle state (`draft → active → sealed`, plus `superseded` and `archived` terminals), persists to disk, and returns the updated `Spec`. Returns `LifecycleError` for invalid transitions.
- `ValidTransition(current, target)` checks whether a state transition is allowed (pure function, no side effects). Standalone function.
- `spec.Supersede(supersedingSpecID, dir)` marks a sealed spec as superseded, prepends a deprecation banner to the PRD body, and persists.
- `MoveToArchive(specDir, root)` transitions to archived (if needed) and moves the spec directory to `{root}/archive/`. Standalone function.

### Discovery and Dependencies

- `DiscoverSpecs(root)` scans a directory for spec directories (`NN_name/` pattern) and returns lightweight `SpecMeta` entries parsed from PRD frontmatter. Standalone function.

  **`SpecMeta` struct fields:**

  | Field | Go Type | Source |
  |---|---|---|
  | `SpecID` | `string` | `spec_id` key in PRD YAML frontmatter |
  | `SpecName` | `string` | `spec_name` key in PRD YAML frontmatter |
  | `Status` | `Status` (enum) | `status` key in PRD YAML frontmatter |
  | `Dir` | `string` | Filesystem path to the spec directory |

- `BuildDependencyGraph(metas, root)` builds the inter-spec dependency graph from `tasks.json` declarations. Returns a `DependencyGraph`. Standalone function.

  **`DependencyGraph` method signatures:**

  | Method | Signature | Description |
  |---|---|---|
  | `Edges` | `() []DependencyEdge` | Returns all edges in the graph |
  | `Dependencies` | `(specID string) []DependencyEdge` | Returns edges where `ToSpec == specID` (upstream dependencies) |
  | `Dependents` | `(specID string) []DependencyEdge` | Returns edges where `FromSpec == specID` (downstream dependents) |
  | `TopologicalSort` | `() ([]string, error)` | Returns spec IDs in topological order using Kahn's algorithm; returns error if a cycle is detected |

  **`DependencyEdge` struct fields:** `FromSpec` (`string`), `ToSpec` (`string`), `FromGroup` (`int`), `ToGroup` (`int`), `Relationship` (`string`).

- `IsSpecDirName(name)` and `ParseSpecDirName(name)` validate and parse directory names matching `{NN}_{snake_case}`. Standalone functions.
- `LoadSpecLandscape(root, includeArchive, currentSpecID)` collects metadata for all specs for landscape views. Standalone function.

### Subtask Mutation

- `tasks.TransitionSubtask(subtaskID, target)` transitions a single subtask's state via the state machine. Method on `Tasks`.

  **Valid subtask state transitions (complete list):**

  | From | To |
  |---|---|
  | `pending` | `queued` |
  | `pending` | `dropped` |
  | `queued` | `in_progress` |
  | `queued` | `pending` |
  | `queued` | `dropped` |
  | `in_progress` | `done` |
  | `in_progress` | `pending_reevaluation` |
  | `done` | `pending_reevaluation` |
  | `pending_reevaluation` | `pending` |
  | `pending_reevaluation` | `dropped` |

  No other transitions are permitted. Any other transition returns a `LifecycleError`.

- `tasks.CompleteSubtaskStates(groupIDs)` marks all subtasks in specified groups as `done` (bypasses state machine). Method on `Tasks`.
- `tasks.ResetSubtaskStates(groupIDs)` resets all subtasks in specified groups to `pending`. Method on `Tasks`.

### Rendering

- `spec.RenderCombined()` renders all artifacts as a single Markdown document. Method on `Spec`.
- `spec.RenderIndividual()` renders each artifact separately, returning a `map[string]string`. Method on `Spec`.
- `spec.RenderIndividualScoped(targetGroup int)` renders each artifact scoped to a target task group's refs. Method on `Spec`. Scoping algorithm:
  1. Collect all `requirement_refs` and `test_spec_refs` from every subtask in `targetGroup`.
  2. **Requirements:** Render only requirements whose IDs, acceptance criteria IDs, or edge case IDs appear in the collected `requirement_refs`. Include a Spec Overview listing all requirements for context.
  3. **Test spec:** Render only test entries whose IDs appear in the collected `test_spec_refs`.
  4. **Tasks:** Render the target group with full subtask detail; render all other groups as one-line summaries showing completion counts.
  5. **PRD body and architecture:** Included as-is (unfiltered).
  6. **Fallback:** If the target group has no refs, falls back to full unscoped rendering.
- `req.Render()` renders requirements as Markdown. Method on `Requirements`.
- `ts.Render()` renders test spec as Markdown. Method on `TestSpec`.
- `t.Render()` renders tasks as Markdown with checkbox-formatted subtasks. Method on `Tasks`.
- `criterion.RenderEARSSentence()` renders a single EARS criterion as a natural-language sentence. Method on `Criterion`.

### EARS Criterion Builders

Standalone convenience constructors:

- `UbiquitousCriterion(id, system, action)`.
- `EventDrivenCriterion(id, trigger, system, action)`.
- `ComplexEventCriterion(id, trigger, condition, system, action)`.
- `StateDrivenCriterion(id, state, system, action)`.
- `UnwantedCriterion(id, errorCondition, system, action)`.
- `OptionalCriterion(id, feature, system, action)`.

### Bootstrap

- `BootstrapSpec` struct with `NewBootstrapSpec(specID, specName)` constructor. Supports incremental population of spec artifacts with deferred cross-file validation.
- `Finalize()` method on `BootstrapSpec`: assembles a `Spec` from all set artifacts, then runs full validation (schema + cross-file). It does **not** call `Save()`. Return signature: `(*Spec, []ValidationError)`.
  - On success: returns `(spec, nil)` (empty slice).
  - On failure (missing required artifacts or validation errors): returns `(nil, errors)`.
  - Missing artifacts produce `ValidationError` entries with `rule = "bootstrap"` and `message = "Missing artifact: {name}"`.

### Other

- `Schemas()` returns the bundled JSON Schema files as a `map[string][]byte` (embedded via `//go:embed`). Standalone function.
- `ComputeIntentHash(body)` computes SHA-256 of the normalized `## Intent` section from a PRD body. Returns `IntentError` if section is missing. Standalone function.
- `ts.ComputeCoverage(req)` computes test coverage by scanning test entries against requirements, properties, and paths. Method on `TestSpec`.
- `CreateSpec(specID, specName)` creates a new Spec with initialized sub-artifacts in draft state. Standalone function.

### Error Types

- `SpecError` — base error type (Go error wrapping conventions). All specific error types embed or wrap `SpecError` such that `errors.As(err, &SpecError{})` works through the chain.
- `LoadError`, `SaveError`, `LifecycleError`, `IntentError`, `BootstrapError` — specific error types wrapping `SpecError`. The wrapping chain guarantees that `errors.As` and `errors.Is` traverse from specific to base type as expected by Go conventions. For example, a `LoadError` wraps a `SpecError`, so both `errors.As(err, &LoadError{})` and `errors.As(err, &SpecError{})` return true.

## Technical Boundaries

- Go 1.26.5 (as specified in the existing `go.mod`; this is an intentionally forward-looking version already set in the project).
- Module path: `github.com/agent-fox-dev/spec`.
- Package name: `afspec` — all public functions and methods in project root, internal helpers in `internal/` subpackages.
- JSON Schema files copied to `schemas/` in the project root as the source of truth, embedded via `//go:embed`.
- JSON Schema validation: `github.com/santhosh-tekuri/jsonschema/v6`.
- YAML parsing: `github.com/goccy/go-yaml` (already a dependency).
- Atomic file writes via write-to-temp-then-rename.
- **Concurrency model:** No goroutine-safety guarantees are provided. Callers must synchronize access to `Spec` instances externally. `Spec` structs are treated as value types — mutation methods return new copies rather than modifying in place, matching the Python immutable Pydantic model pattern.

## External API Surface

### `github.com/santhosh-tekuri/jsonschema/v6`

Used exclusively for `spec.ValidateSchema()`. No network access is performed; schemas are loaded from embedded bytes via `jsonschema.SchemeURLLoader`.

| Function / Method | Purpose | Return / Failure Mode |
|---|---|---|
| `jsonschema.UnmarshalJSON(io.Reader)` | Load a schema document from embedded bytes | Returns parsed schema value; returns `error` on malformed JSON |
| `compiler.Compile(url)` | Compile a schema URL against the loader | Returns `*jsonschema.Schema`; returns `error` if `$ref` resolution fails or schema is structurally invalid |
| `schema.Validate(instance)` | Validate a JSON value against the compiled schema | Returns `nil` on success; returns `*jsonschema.ValidationError` on failure — a tree of errors each carrying `InstanceLocation`, `KeywordLocation`, and `Message` |

**Failure modes handled:**
- Invalid schema (bad `$ref`, malformed structure) → `Compile()` error → wrapped in `LoadError` at startup.
- Validation failure → `*jsonschema.ValidationError` → mapped to `ValidationResult.Errors` entries with path and message.

### `github.com/goccy/go-yaml`

Used exclusively for parsing YAML frontmatter in `prd.md`. YAML marshaling is **not** used — PRD frontmatter is written by a hand-written renderer to guarantee byte-for-byte fidelity with Python.

| Function | Purpose | Return / Failure Mode |
|---|---|---|
| `yaml.Unmarshal([]byte, interface{})` | Parse frontmatter bytes into a Go struct or `map[string]interface{}` | Returns `error` with line/column info on syntax errors or type mismatches |

**Failure modes handled:**
- Malformed YAML (syntax error, type mismatch) → `yaml.Unmarshal` returns error with line/column info → wrapped in `LoadError`.
- Missing required fields → detected post-parse by inspecting the populated struct → wrapped in `LoadError`.
- Missing closing `---` delimiter or other edge cases → behavior matches Python implementation exactly.

## Dependencies

- **JSON Schema files** — copied from `packages/afspec/afspec/schemas/` to `schemas/` in the project root. These are the source of truth for both Python and Go. The Go library version tracks the schema version (currently v1), not the Python library version. When the Python library updates its schemas, the Go library is updated by copying new schemas and regenerating structs via `make json-go`.
- **Auto-generated Go structs** — produced by `make json-go`, used as the data model foundation.
- **Python test fixtures** — static spec directories (each containing `prd.md`, `requirements.json`, `test_spec.json`, `tasks.json`) extracted from the Python test suite and stored under `testdata/` in the Go repository.

## Test Strategy

- **Framework:** Standard library `testing` package.
- **Assertion / diff library:** `github.com/google/go-cmp` (already in `go.mod`) — used for structured diffs on test failures.
- **Unit tests:** Table-driven tests for all pure functions and methods.
- **Golden-file / fidelity tests:** Each fixture in `testdata/` is loaded via `LoadSpec`, saved to a temporary directory, and the output is compared byte-for-byte against the original fixture files using `go-cmp`. Any byte-level difference fails the test with a full human-readable diff. This verifies the byte-for-byte round-trip fidelity hard requirement.
- **Discrepancy reporting:** On mismatch, `go-cmp` outputs the full diff (expected vs. actual) to the test log, making it straightforward to identify which field or formatting rule diverges from the Python implementation.
- No third-party test runners or BDD frameworks are used.

## Design Decisions

- **BootstrapSpec included** — needed by downstream consumers for incremental spec building.
- **lint.py excluded** — not part of the public API in Python.
- **Schema embedding** — `//go:embed` bundles schemas into the binary, avoiding filesystem dependencies at runtime.
- **Atomic saves** — write-to-temp-then-rename pattern for safe concurrent access.
- **Subtask state machine** — transition rules are hardcoded in the library (see the complete transition table in the Subtask Mutation section). These rules mirror `spec-format.md` and are not configurable.
- **Cycle detection** — `DependencyGraph.TopologicalSort()` uses Kahn's algorithm to detect and report cycles in addition to matching Python behavior.
- **PRD parsing edge cases** — match Python behavior exactly for malformed frontmatter.
- **Methods over functions** — any function whose primary operand is a struct is a method on that struct. Standalone functions are reserved for constructors, discovery, multi-entity operations, and pure utilities.
- **MarshalJSON determinism** — struct fields are serialized in declaration order (matching JSON Schema property order); `map[string]T` values are serialized with keys sorted alphabetically. Implemented via a custom marshaler using reflection. Go's `encoding/json` already preserves struct field declaration order, so no special handling is needed for structs.
- **PRD frontmatter rendering** — uses a hand-written renderer (fixed field order, explicit value formatting) rather than `yaml.Marshal`, mirroring Python's `_render_yaml_value()` and `_render_prd()` functions to achieve byte-for-byte fidelity.
- **Schema versioning** — the Go library tracks schema version (v1), not the Python library version. Schema updates are applied by copying new files and regenerating structs.
- **Concurrency** — no goroutine-safety guarantees; callers synchronize externally. Mutation methods return new `Spec` copies rather than mutating in place, matching the Python immutable Pydantic model pattern.
- **Error wrapping** — `SpecError` is the base type; all specific error types (`LoadError`, `SaveError`, `LifecycleError`, `IntentError`, `BootstrapError`) wrap it such that `errors.As` traverses the full chain.
- **Owner** — `agent-fox-dev`.
