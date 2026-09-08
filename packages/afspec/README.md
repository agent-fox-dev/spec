# afspec

Standalone Python library for the agent-fox specification format (v1.3). Load,
validate, mutate, and render specs — the structured artifact format used by
[spec-format](https://github.com/agent-fox-dev/spec) for spec-driven
development.

Requires Python 3.12+. Dependencies: `pydantic`, `PyYAML`, `jsonschema`.

## Installation

Install from git:

```bash
pip install "afspec @ git+https://github.com/agent-fox-dev/spec.git#subdirectory=packages/afspec"
```

Pin to a release tag:

```bash
pip install "afspec @ git+https://github.com/agent-fox-dev/spec.git@v1.4.1#subdirectory=packages/afspec"
```

In `pyproject.toml`:

```toml
[project]
dependencies = [
    "afspec @ git+https://github.com/agent-fox-dev/spec.git@v1.4.1#subdirectory=packages/afspec",
]
```

## Spec Format Overview

A spec is a directory (`NN_name/`) containing four required artifacts and
one optional:

| File | Format | Purpose |
|------|--------|---------|
| `prd.md` | Markdown with YAML frontmatter | Product requirements — intent, goals, tech stack, design |
| `requirements.json` | JSON (schema v1) | EARS-syntax acceptance criteria, correctness properties, execution paths, external API contracts, error handling, glossary |
| `test_spec.json` | JSON (schema v1) | Test contracts — unit, property, edge-case, and smoke tests |
| `tasks.json` | JSON (schema v1) | Implementation plan with subtask states, dependencies, test commands, traceability |
| `architecture.md` | Markdown (optional) | Detailed architecture for complex specs |

## Quick Start

```python
from pathlib import Path
from afspec import load_spec, validate, discover_specs, Status

# Load a single spec
spec = load_spec(Path(".specs/01_foundation"))
print(spec.prd.frontmatter.title)  # "Foundation"
print(spec.prd.frontmatter.status)  # Status.DRAFT
print(len(spec.requirements.requirements))  # number of requirements
print(len(spec.tasks.task_groups))  # number of task groups

# Validate a spec (schema + cross-file consistency)
result = validate(spec)
if result.errors:
    for err in result.errors:
        print(f"{err.file}:{err.path} — {err.message}")

# Discover all specs in a directory
metas = discover_specs(Path(".specs"))
for meta in metas:
    print(f"{meta.spec_id}: {meta.spec_name} ({meta.status})")
```

## API Reference

All symbols are importable from the top-level package: `from afspec import <symbol>`.

### I/O

| Symbol | Signature | Description |
|--------|-----------|-------------|
| `load_spec` | `(dir: str \| Path) -> Spec` | Load all artifacts from a spec directory. Raises `LoadError`. |
| `save` | `(spec: Spec, dir: str \| Path) -> None` | Atomic save with lifecycle guards. Raises `SaveError`, `LifecycleError`. |
| `marshal_json` | `(model: object) -> str` | Deterministic JSON serialization (2-space indent, sorted dicts). |

### Core Types (Pydantic Models)

| Type | Key Fields |
|------|------------|
| `Spec` | `prd: PRDDocument`, `requirements: Requirements`, `test_spec: TestSpec`, `tasks: Tasks`, `architecture: str \| None` |
| `PRDDocument` | `frontmatter: PRDFrontmatter`, `body: str` |
| `PRDFrontmatter` | `spec_id`, `spec_name`, `title`, `status: Status`, `created_at`, `updated_at`, `owner`, `source`, `tags`, `intent_hash` |
| `Requirements` | `spec_id`, `spec_name`, `introduction`, `glossary`, `requirements: list[Requirement]`, `correctness_properties`, `execution_paths`, `error_handling`, `external_apis` |
| `Requirement` | `id`, `title`, `user_story: UserStory`, `acceptance_criteria: list[Criterion]`, `edge_cases: list[Criterion]` |
| `Criterion` | `id`, `ears_pattern: EARSPattern`, `system`, `action`, `return_contract`, plus pattern-specific fields |
| `TestSpec` | `spec_id`, `spec_name`, `test_cases`, `property_tests`, `edge_case_tests`, `smoke_tests`, `coverage: Coverage` |
| `TestCase` | `id`, `requirement_id`, `kind`, `description`, `preconditions`, `input`, `expected`, `assertion_pseudocode` |
| `Tasks` | `spec_id`, `spec_name`, `test_commands: TestCommands`, `dependencies`, `task_groups: list[TaskGroup]`, `traceability` |
| `TaskGroup` | `id: int`, `kind: TaskGroupKind`, `title`, `subtasks: list[Subtask]`, `verification` |
| `Subtask` | `id`, `title`, `details`, `test_spec_refs`, `requirement_refs`, `state: SubtaskState`, `optional: bool` |
| `SpecMeta` | `spec_id`, `spec_name`, `status: Status`, `dir: str` — lightweight metadata for discovery |
| `UserStory` | `role`, `goal`, `benefit` |
| `ExternalAPI` | `package`, `version`, `symbols: list[ExternalAPISymbol]` |
| `ExternalAPISymbol` | `name`, `import_path`, `signature`, `notes` |
| `CorrectnessProperty` | `id`, `title`, `for_any`, `invariant`, `validates` |
| `ExecutionPath` | `id`, `title`, `steps: list[PathStep]` |
| `PathStep` | `actor`, `action` |
| `ErrorHandlingEntry` | `id`, `condition`, `behavior`, `requirement_id` |
| `PropertyTest` | `id`, `property_id`, `validates`, `description`, `for_any_strategy`, `invariant_check` |
| `EdgeCaseTest` | `id`, `requirement_id`, `kind`, `description`, `preconditions`, `input`, `expected`, `assertion_pseudocode` |
| `SmokeTest` | `id`, `execution_path_id`, `description`, `trigger`, `real_components`, `mockable`, `expected_effects` |
| `VerificationSubtask` | `id`, `checks: list[str]` |
| `TestCommands` | `spec_tests`, `all_tests`, `linter` |
| `TaskDependency` | `depends_on_spec`, `from_group`, `to_group`, `relationship`, `sentinel` |
| `TraceabilityEntry` | `requirement_id`, `test_spec_id`, `task_id`, `test_path` |
| `Coverage` | `requirements_covered`, `properties_covered`, `paths_covered`, `gaps` |
| `DependencyEdge` | `from_spec`, `to_spec`, `from_group`, `to_group`, `relationship` |

### Enums

| Enum | Values |
|------|--------|
| `Status` | `draft`, `active`, `sealed`, `superseded`, `archived` |
| `EARSPattern` | `ubiquitous`, `event_driven`, `complex_event`, `state_driven`, `unwanted`, `optional` |
| `SubtaskState` | `pending`, `queued`, `in_progress`, `done`, `pending_reevaluation`, `dropped` |
| `TaskGroupKind` | `tests`, `standard`, `checkpoint`, `wiring_verification` |

### Validation

| Symbol | Signature | Description |
|--------|-----------|-------------|
| `validate` | `(spec: Spec) -> ValidationResult` | Full validation (schema + cross-file + wiring semantics). |
| `validate_schema` | `(spec: Spec) -> list[ValidationError]` | Schema-only validation against bundled JSON Schemas plus EARS constraints and task group structure. |
| `validate_cross_file` | `(spec: Spec) -> list[ValidationError]` | Cross-file consistency checks (dangling refs, coverage gaps, glossary, ID formats). |
| `validate_cross_spec` | `(specs: dict[str, Spec], graph: DependencyGraph) -> list[ValidationError]` | Cross-spec interface consistency (API symbols, glossary conflicts, contract mismatches). |
| `validate_structured` | `(spec: Spec) -> dict[str, Any]` | Full validation reshaped as a categorised dict for CLI consumption. |
| `ValidationResult` | `.valid: bool`, `.errors: list[ValidationError]`, `.warnings: list[ValidationWarning]` | Validation outcome. `valid` is `True` when `errors` is empty, regardless of warnings. |
| `ValidationError` | `.file`, `.path`, `.message`, `.rule`, `.value` | A validation failure. |
| `ValidationWarning` | `.message`, `.entity_id` | A non-blocking warning (sizing, complexity, vague language). |

### Lifecycle

| Symbol | Signature | Description |
|--------|-----------|-------------|
| `transition` | `(spec: Spec, target: Status, dir: str \| Path) -> Spec` | Transition lifecycle state and persist to disk. Raises `LifecycleError` for invalid transitions. |
| `valid_transition` | `(current: Status, target: Status) -> bool` | Check if a transition is allowed. |
| `supersede` | `(spec: Spec, superseding_spec_id: str, dir: str \| Path) -> Spec` | Mark a sealed spec as superseded, prepend deprecation banner, and persist. |
| `move_to_archive` | `(spec_dir: str \| Path, root: str \| Path) -> None` | Transition to archived (if needed) and move the spec directory to `{root}/archive/`. |

### Discovery and Dependencies

| Symbol | Signature | Description |
|--------|-----------|-------------|
| `discover_specs` | `(root: str \| Path) -> list[SpecMeta]` | Find all spec directories and return lightweight metadata from `prd.md` frontmatter. |
| `build_dependency_graph` | `(metas: list[SpecMeta], root: str \| Path) -> DependencyGraph` | Build the inter-spec dependency graph from `tasks.json` declarations. |
| `DependencyGraph` | `.edges()`, `.dependencies(spec_id)`, `.dependents(spec_id)`, `.topological_sort()` | Queryable directed dependency graph. |
| `is_spec_dir_name` | `(name: str) -> bool` | Check whether a name matches the canonical `{NN}_{snake_case}` pattern. |
| `parse_spec_dir_name` | `(name: str) -> tuple[int, str] \| None` | Parse a spec directory name into `(prefix, spec_name)`, or `None`. |
| `load_spec_landscape` | `(spec_root: str \| Path, *, include_archive: bool, current_spec_id: str \| None) -> list[dict]` | Collect metadata for all specs (active and optionally archived) for landscape views. |

### Subtask Mutation

| Symbol | Signature | Description |
|--------|-----------|-------------|
| `transition_subtask` | `(tasks: Tasks, subtask_id: str, target: SubtaskState) -> Tasks` | Transition a single subtask's state via the state machine. |
| `complete_subtask_states` | `(tasks: Tasks, group_ids: list[int]) -> Tasks` | Mark all subtasks in the specified groups as `done` (bypasses state machine). |
| `reset_subtask_states` | `(tasks: Tasks, group_ids: list[int]) -> Tasks` | Reset all subtasks in the specified groups to `pending`. |

### Rendering

| Symbol | Signature | Description |
|--------|-----------|-------------|
| `render_requirements` | `(req: Requirements) -> str` | Render requirements as Markdown. |
| `render_test_spec` | `(ts: TestSpec) -> str` | Render test spec as Markdown. |
| `render_tasks` | `(t: Tasks) -> str` | Render tasks as Markdown with checkbox-formatted subtasks. |
| `render_combined` | `(spec: Spec) -> str` | Render all artifacts as a single Markdown document. |
| `render_individual` | `(spec: Spec) -> dict[str, str]` | Render each artifact separately. |
| `render_individual_scoped` | `(spec: Spec, target_group: int) -> dict[str, str]` | Render each artifact scoped to a target task group's refs. |
| `render_ears_sentence` | `(criterion: Criterion) -> str` | Render a single EARS criterion as a natural-language sentence. |

### EARS Criterion Builders

Convenience constructors for creating `Criterion` objects:

| Builder | Signature |
|---------|-----------|
| `ubiquitous_criterion` | `(id, system, action) -> Criterion` |
| `event_driven_criterion` | `(id, trigger, system, action) -> Criterion` |
| `complex_event_criterion` | `(id, trigger, condition, system, action) -> Criterion` |
| `state_driven_criterion` | `(id, state, system, action) -> Criterion` |
| `unwanted_criterion` | `(id, error_condition, system, action) -> Criterion` |
| `optional_criterion` | `(id, feature, system, action) -> Criterion` |

### Exceptions

| Exception | Base | When |
|-----------|------|------|
| `SpecError` | `Exception` | Base for all afspec errors |
| `LoadError` | `SpecError` | Missing files, malformed JSON/YAML |
| `SaveError` | `SpecError` | Write failures |
| `LifecycleError` | `SpecError` | Invalid lifecycle transitions, mutation guards |
| `IntentError` | `SpecError` | Missing `## Intent` section in PRD body |
| `BootstrapError` | `SpecError` | Spec bootstrapping failures |

### Other

| Symbol | Signature | Description |
|--------|-----------|-------------|
| `schemas` | `() -> dict[str, bytes]` | Returns bundled JSON Schema files for external validation. |
| `compute_intent_hash` | `(body: str) -> str` | SHA-256 of the normalized `## Intent` section from a PRD body. Raises `IntentError` if section is missing. |
| `compute_coverage` | `(ts: TestSpec, req: Requirements) -> Coverage` | Compute test coverage by scanning test entries against requirements, properties, and paths. |
| `create_spec` | `(spec_id: str, spec_name: str) -> Spec` | Create a new Spec with initialized sub-artifacts in draft state. |
| `BootstrapSpec` | Class: `__init__(spec_id, spec_name)` | Incremental spec population with deferred cross-file validation via `finalize()`. |
