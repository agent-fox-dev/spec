# Spec Format Specification

Version 2.0 — Draft

Supersedes version 1.3 (`spec-format.md`). Section 13 maps every v1
construct to its v2 equivalent.

## 0. Why version 2

Version 1 optimised for referential integrity: thirteen ID families, four
kinds of test, two kinds of criterion, three parallel traceability
mechanisms and twenty-odd cross-file rules. Specs generated under v1 were
at the same time over-complicated (65–100 KB per artifact, 12–17 task
groups, most of it restating the same requirement in three places) and
under-specified (in every archived spec, none of the edge-case tests and
none of the property tests were owned by any task, so no coder ever
implemented them).

Version 2 optimises for one thing: **a complete, executable plan.** Every
criterion is verified by a test, every test is owned by a task, every path
is exercised by a smoke test, and a spec that violates any of those is
invalid. Everything that can be derived is derived, not authored.

Design rules for v2:

1. One concept, one place. No section may restate another section.
2. Derived data is never stored. Coverage and traceability are computed.
3. Structure over string matching. Validation never greps for words.
4. The plan is the contract. Validation proves the plan covers the spec.
5. Fewer, longer items beat many short ones. Token budgets are finite.

---

## 1. Scope

This document defines the on-disk format for a specification package
("spec"): the durable artifact that captures design intent, acceptance
criteria, verification contracts and the implementation plan for one
cohesive feature.

A spec consists of four required artifacts and one optional artifact:

| File | Format | Required | Purpose |
|---|---|---|---|
| `prd.md` | Markdown with YAML frontmatter | yes | Narrative intent: the "why" and "what" |
| `requirements.json` | JSON (schema-validated) | yes | What the system must do, as EARS criteria, plus end-to-end execution paths |
| `test_spec.json` | JSON (schema-validated) | yes | One flat list of tests; each test verifies one or more criteria or paths |
| `tasks.json` | JSON (schema-validated) | yes | One flat list of tasks; each task owns criteria and tests |
| `architecture.md` | Markdown (free-form) | no | Modules, interfaces, data models, technology choices |

---

## 2. Terminology

| Term | Definition |
|---|---|
| Spec | A package of the artifacts above representing one feature |
| Spec root | The directory containing all specs, e.g. `.specs/` |
| Operator | A human who authors PRDs and reviews specs |
| Coordinator | The agent that generates and mutates the JSON artifacts |
| Coder | The agent that executes one task at a time |
| Criterion | One testable EARS statement inside a requirement |
| Path | An execution path: an end-to-end scenario from entry point to side effect |
| Test | One entry of `test_spec.json` |
| Task | One entry of `tasks.json`; the unit of dispatch to a coder |
| EARS | Easy Approach to Requirements Syntax |

---

## 3. Folder layout, naming, completeness

Unchanged from v1.3 §3: `<spec_root>/{NN}_{snake_case_name}/`, monotonically
increasing `NN`, `archive/` for superseded and archived specs, all four
required files must exist for the spec to be valid, bootstrap mode during
creation.

---

## 4. `prd.md`

### 4.1 Frontmatter

```yaml
---
spec_id: "05"
spec_name: "my_feature"
title: "Human-readable title"
status: "draft"                 # draft | active | sealed | superseded | archived
created_at: "2026-05-18T12:00:00Z"
updated_at: "2026-05-18T12:00:00Z"
intent_hash: null               # set at draft → active
schema_version: 2
owner: "author-name"            # optional
source: "docs/prds/feature.md"  # optional
supersedes: []                  # optional
tags: []                        # optional
---
```

| Field | Type | Required | Description |
|---|---|---|---|
| `spec_id` | string | yes | Numeric prefix as string. Must match the folder prefix. |
| `spec_name` | string | yes | Snake-case slug. Must match the folder suffix. |
| `title` | string | yes | Human-readable title. |
| `status` | enum | yes | Lifecycle state (§9). |
| `created_at` | ISO 8601 | yes | Immutable after creation. |
| `updated_at` | ISO 8601 | yes | Updated on every save. |
| `intent_hash` | string or null | yes | SHA-256 of the `## Intent` body. Null while `draft`. |
| `schema_version` | integer | yes | `2`. |
| `owner`, `source`, `supersedes`, `tags` | | no | As in v1.3. Omitted means empty. |

### 4.2 Body

Unchanged from v1.3 §4.2: `# {title}` followed by a mandatory `## Intent`
section (the only machine-read section, hashed at activation), then any
operator-discretion sections. Architectural content belongs in
`architecture.md`.

**Scope rule (generation instruction).** The PRD describes intent, goals,
non-goals and behaviour. It does not enumerate functions, flags and file
paths per feature; when it does, the coordinator must still compress it to
at most 10 requirements (§6.5). A PRD that cannot be compressed is split
into several specs before generation.

---

## 5. `architecture.md`

Unchanged from v1.3 §5: optional, free-form, no frontmatter, not validated,
may name concrete modules and files, may be edited at any lifecycle stage.

---

## 6. `requirements.json`

### 6.1 Top-level schema

```json
{
  "$schema": "https://agent-fox.dev/schemas/requirements.v2.json",
  "spec_id": "05",
  "spec_name": "my_feature",
  "schema_version": 2,
  "introduction": "One or two sentences describing the system being specified.",
  "glossary": {},
  "requirements": [],
  "execution_paths": [],
  "external_apis": []
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `$schema` | string | yes | Schema URI. |
| `spec_id`, `spec_name` | string | yes | Must match `prd.md`. |
| `schema_version` | integer | yes | `2`. |
| `introduction` | string | yes | What is being specified. |
| `glossary` | object | no | Term → definition for project-specific vocabulary a newcomer would not know. Not cross-validated (§10.3). |
| `requirements` | array | yes | Requirement objects (§6.2). At least one. |
| `execution_paths` | array | yes | Path objects (§6.3). At least one. |
| `external_apis` | array | no | Verified external symbols (§6.4). |

Removed from v1: `correctness_properties` (now ordinary criteria verified
by `property` tests), `error_handling` (now `unwanted` criteria with a
`contract`).

### 6.2 Requirements and criteria

```json
{
  "id": "05-REQ-1",
  "title": "Agent mode output",
  "rationale": "Agents parse stdout; any non-JSON byte breaks them.",
  "criteria": [
    {
      "id": "05-REQ-1.1",
      "pattern": "state_driven",
      "condition": "AF_AGENT=1 is set in the environment",
      "system": "spec binary",
      "action": "write only JSON to stdout and route all progress output to stderr",
      "contract": "stdout parses as a single JSON object"
    },
    {
      "id": "05-REQ-1.2",
      "pattern": "unwanted",
      "condition": "an unhandled error occurs while AF_AGENT=1 is set",
      "system": "spec binary",
      "action": "write {\"ok\": false, \"error\": <message>} to stdout and exit",
      "contract": "exit code 1; stdout is {\"ok\": false, \"error\": string}"
    },
    {
      "id": "05-REQ-1.3",
      "pattern": "unwanted",
      "condition": "stdout is a closed pipe while writing the JSON result",
      "system": "spec binary",
      "action": "suppress the write error and exit with the command's own exit code",
      "contract": "no panic; exit code unchanged by the write failure"
    }
  ]
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | `{spec_id}-REQ-{N}`, N sequential from 1. |
| `title` | string | yes | Short descriptive title. |
| `rationale` | string | no | One sentence: why this requirement exists. Replaces the v1 `user_story` object. |
| `criteria` | array | yes | Criterion objects. At least one. |

There is **no separate edge-case list**. An edge case is a criterion,
usually with pattern `unwanted` or `complex_event`. The generation
instruction in §6.5 still requires edge-case coverage; it just lives in the
same list with the same ID scheme.

#### 6.2.1 Criterion

Each criterion is one EARS sentence decomposed into fields. The rendered
sentence is derived; the fields are the source of truth.

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | `{spec_id}-REQ-{N}.{C}`, C sequential from 1 within the requirement. |
| `pattern` | enum | yes | `ubiquitous`, `event_driven`, `complex_event`, `state_driven`, `unwanted`, `optional`. |
| `condition` | string | all but `ubiquitous` | The clause after the pattern keyword: the trigger, state, error condition or feature. Must be absent for `ubiquitous`. |
| `guard` | string | `complex_event` only | The additional condition joined with AND. Must be absent for other patterns. |
| `system` | string | no | The component that acts. Defaults to `"system"` when rendering. |
| `action` | string | yes | What the system must do. Testable and unambiguous. |
| `contract` | string | `unwanted` | What the caller observes: return value, exit code, status code, emitted event. Required for `unwanted`; recommended whenever the result is consumed by something else. |

| `pattern` | Rendered sentence |
|---|---|
| `ubiquitous` | THE {system} SHALL {action} |
| `event_driven` | WHEN {condition}, THE {system} SHALL {action} |
| `complex_event` | WHEN {condition} AND {guard}, THE {system} SHALL {action} |
| `state_driven` | WHILE {condition}, THE {system} SHALL {action} |
| `unwanted` | IF {condition}, THEN THE {system} SHALL {action} |
| `optional` | WHERE {condition}, THE {system} SHALL {action} |

When `contract` is present the renderer appends a second line:
`→ {contract}`. The contract is always rendered; a contract the coder cannot
see does not exist.

Correctness properties ("for any X, Y holds") are written as `ubiquitous`
criteria whose action starts with "for any …" and are verified by a test
of kind `property` (§7).

### 6.3 Execution paths

```json
{
  "id": "05-PATH-1",
  "title": "Operator creates a spec from a PRD file",
  "steps": [
    { "actor": "operator",    "action": "runs `spec new prd.md --name feature`" },
    { "actor": "spec binary", "action": "validates the PRD path and the name" },
    { "actor": "Campaign",    "action": "allocates the next prefix and creates the spec directory" },
    { "actor": "spec binary", "action": "writes prd.md and the three JSON scaffolds" },
    { "actor": "spec binary", "action": "prints {\"ok\": true, \"spec_dir\": …} to stdout" }
  ]
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | `{spec_id}-PATH-{N}`. |
| `title` | string | yes | The end-to-end scenario. |
| `steps` | array | yes | At least two `{actor, action}` steps. |

Path rules (unchanged in substance from v1.3 §6.4.1): a path starts at a
user action, CLI command, API call or scheduled trigger; ends at a concrete
side effect; uses logical actors; and, when several specs form a dependency
chain, the last spec in the chain owns a path that crosses every spec
boundary.

### 6.4 External APIs (optional)

As v1.3 §6.6 with one addition: each package entry carries a boolean
`verified`. `verified: false` means the signatures are assumptions from the
PRD and the coder must confirm them before use. The coordinator must never
write "unverified" into the `version` field or a note; it sets the flag.

```json
{
  "package": "github.com/spf13/cobra",
  "version": "v1.10.2",
  "verified": true,
  "symbols": [
    { "name": "Command", "import_path": "github.com/spf13/cobra", "signature": "type Command struct{…}" }
  ]
}
```

### 6.5 Requirement quality rules

Each rule is tagged with its enforcement mechanism.

1. **[schema]** Every criterion has a non-empty `action`.
2. **[validator, error]** Every `unwanted` criterion has a `contract`.
3. **[validator, warning]** A spec has at most 10 requirements; a requirement has at most 8 criteria. Beyond that, split the spec.
4. **[validator, warning]** Criterion fields avoid vague words (`appropriate`, `properly`, `correctly`, `reasonable`, `as needed`, `etc`).
5. **[generation instruction]** For every requirement the coordinator considers, and where applicable writes a criterion for: empty or null input; boundary values; operation failure; authorization failure; concurrent operation. For anything that spawns processes, loops, retries or calls a service: timeout, resource cleanup on failure, iteration cap, and the rule that library code returns errors instead of terminating the process.
6. **[generation instruction]** Prefer measurable constraints over qualitative language.

---

## 7. `test_spec.json`

One flat list. There are no separate arrays for edge-case, property or
smoke tests; the `kind` field carries that distinction and the `verifies`
field carries the link.

### 7.1 Top-level schema

```json
{
  "$schema": "https://agent-fox.dev/schemas/test_spec.v2.json",
  "spec_id": "05",
  "spec_name": "my_feature",
  "schema_version": 2,
  "tests": []
}
```

There is no `coverage` object. Coverage is computed by the validator and
reported by `spec validate`; it is never stored.

### 7.2 Test

```json
{
  "id": "TS-05-3",
  "kind": "unit",
  "verifies": ["05-REQ-1.2"],
  "title": "Unhandled error in agent mode is reported as JSON on stdout",
  "given": ["AF_AGENT=1 is set", "the spec directory does not exist"],
  "when": "spec status 99_missing is executed",
  "then": [
    "exit code is 1",
    "stdout parses as JSON with ok == false and a non-empty error string",
    "stderr is empty"
  ],
  "pseudocode": "r = run(env={AF_AGENT:1}, 'spec status 99_missing'); assert r.code == 1; j = parse_json(r.stdout); assert j.ok == false; assert len(j.error) > 0"
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | `TS-{spec_id}-{N}`, N sequential from 1. One number series for all kinds. |
| `kind` | enum | yes | `unit`, `integration`, `property`, `smoke`. |
| `verifies` | array of strings | yes | Non-empty. Criterion IDs (`…-REQ-N.C`) for `unit`, `integration`, `property`; path IDs (`…-PATH-N`) for `smoke`. A smoke test may additionally list criterion IDs. |
| `title` | string | yes | One sentence: what is verified. |
| `given` | array of strings | yes | Preconditions. May be empty. |
| `when` | string | yes | The action or input. For `property`: the input domain ("for any non-empty list of …"). |
| `then` | array of strings | yes | Non-empty. Observable outcomes. For `smoke`: the side effects at the end of the path. |
| `pseudocode` | string | no | Language-agnostic assertion pseudocode. May name concrete functions and files. Recommended for `unit`, `integration`, `property`. |
| `real_components` | array of strings | `smoke` | Components that must not be mocked. Required and non-empty for `smoke`, absent otherwise. |

Kinds:

- `unit` — exercises one component in isolation; collaborators may be stubbed.
- `integration` — exercises two or more real components; only external I/O may be stubbed.
- `property` — a property-based test; `when` names the generator, `then` states the invariant.
- `smoke` — traverses a full execution path with real components; `verifies` must contain the path ID.

### 7.3 Coverage rules

1. **[validator, error]** Every criterion is listed in `verifies` of at least one test.
2. **[validator, error]** Every path is listed in `verifies` of at least one `smoke` test.
3. **[validator, error]** Every ID in `verifies` resolves to a criterion or a path of this spec.
4. **[validator, warning]** A test verifies at most 4 criteria. Tests that verify more are usually two tests.
5. **[generation instruction]** For every `unwanted` criterion, the test asserts the caller-observable outcome named in the `contract`, not merely "an error occurred".

---

## 8. `tasks.json`

One flat, ordered list of tasks. A task is the unit of dispatch: one coder
session, one task. Tests are written and made to pass inside the task that
owns them; there are no separate "write tests" tasks.

### 8.1 Top-level schema

```json
{
  "$schema": "https://agent-fox.dev/schemas/tasks.v2.json",
  "spec_id": "05",
  "spec_name": "my_feature",
  "schema_version": 2,
  "test_commands": {
    "all_tests": "go test ./... -count=1",
    "linter": "go vet ./...",
    "spec_tests": "go test ./cmd/spec/... -count=1"
  },
  "dependencies": [],
  "tasks": []
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `test_commands` | object | yes | `all_tests` and `linter` required; `spec_tests` optional. Must use the project's real tooling. |
| `dependencies` | array | yes | Cross-spec dependencies (§8.2). May be empty. |
| `tasks` | array | yes | Task objects (§8.3). At least two: one `implement`, one final `integration`. |

Removed from v1: `traceability` (derived, §8.5), task groups and
subtasks (merged into tasks), `verification` subtasks (replaced by
`done_when`), `from_group`/`to_group`/`sentinel` on dependencies.

### 8.2 Dependencies

```json
{ "spec": "01", "reason": "uses the afspec loader and validator" }
```

| Field | Type | Required | Description |
|---|---|---|---|
| `spec` | string | yes | The `spec_id` of the upstream spec. Must exist in the spec root (checked by `spec validate --cross`). |
| `reason` | string | yes | What this spec consumes from it. |

Ordering is at spec granularity: every task of this spec runs after the
upstream spec is sealed or its integration task is done. Group-level
dependencies were never populated with real values in practice and are
gone.

### 8.3 Task

```json
{
  "id": 3,
  "kind": "implement",
  "title": "Agent mode: JSON-only stdout and error envelope",
  "criteria": ["05-REQ-1"],
  "tests": ["TS-05-3", "TS-05-4", "TS-05-5"],
  "steps": [
    "Add isAgentMode() reading AF_AGENT and thread it through root.go PersistentPreRun",
    "Route the banner and spinner to stderr; make emit() the only stdout writer",
    "Wrap the Execute() error path: in agent mode print {ok:false,error} and exit 1",
    "Treat EPIPE from emit() as success so a closed pipe never changes the exit code"
  ],
  "touches": ["golang/cmd/spec/root.go", "golang/cmd/spec/emit.go", "golang/cmd/spec/banner.go"],
  "depends_on": [1],
  "done_when": ["AF_AGENT=1 spec list | jq . succeeds with no banner text on stdout"],
  "state": "pending",
  "optional": false
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | integer | yes | Sequential from 1. Order in the array is the default execution order. |
| `kind` | enum | yes | `implement` or `integration`. Exactly one `integration` task, and it is last. |
| `title` | string | yes | Imperative, specific. |
| `criteria` | array of strings | `implement` | Requirement IDs or criterion IDs this task satisfies. Non-empty for `implement`; a requirement ID means all of its criteria. May be empty for `integration`. |
| `tests` | array of strings | yes | Test IDs this task must make pass. Non-empty. |
| `steps` | array of strings | yes | Non-empty. What to do, in order. Concrete enough that a coder with the scoped render (§11) needs nothing else. |
| `touches` | array of strings | no | Files, packages or modules the task is expected to create or modify. Anchors the coder and gives the integration task a checklist. |
| `depends_on` | array of integers | no | Task IDs that must be `done` first. Absent means "the previous task". Enables parallel dispatch of independent tasks. Must reference lower IDs. |
| `done_when` | array of strings | no | Extra completion checks beyond the implicit ones. |
| `state` | enum | yes | `pending`, `in_progress`, `done`, `dropped`. |
| `optional` | boolean | no | Default `false`. |

**Implicit definition of done for every task:** the tests listed in `tests`
exist, are executable and pass; `test_commands.all_tests` passes;
`test_commands.linter` passes; every `done_when` entry holds. The coder
works test-first: write the listed tests from the test spec, run them and
see them fail, then implement `steps` until they pass.

#### 8.3.1 Task state machine

```
pending ──→ in_progress ──→ done
   │             │            │
   │             └──→ pending ┘   (released, or reopened after an upstream change)
   └──→ dropped
```

| State | Allowed next states |
|---|---|
| `pending` | `in_progress`, `dropped` |
| `in_progress` | `done`, `pending` |
| `done` | `pending` |
| `dropped` | (terminal) |

Runtime metadata (run IDs, agents, timestamps) does not belong in
`tasks.json`.

### 8.4 Integration task (final task)

The last task has `kind: "integration"`. It exists to catch wiring gaps
that component-level tests cannot see.

**[validator, error]** Its `tests` contain every test of kind `smoke`.

**[generation instruction]** Its `steps` cover, in the project's own
vocabulary:

1. Trace every execution path through the real code; confirm each step
   calls the next and no stub remains.
2. For every criterion with a `contract`, confirm the producer's result is
   consumed by a caller in production code, not only by tests.
3. Search the files listed in `touches` across all tasks for stub markers
   appropriate to the language (`panic("not implemented")`, `TODO`,
   `NotImplementedError`, bare `return nil` on non-trivial paths, …).
4. For paths whose entry point belongs to another spec, confirm the entry
   point is called from production code.

An execution path that is not live in production code fails this task.
Errata or deferrals do not satisfy it.

### 8.5 Derived traceability

The validator can emit a traceability matrix on demand:

```
criterion → tests (from test.verifies) → tasks (from task.tests)
path      → smoke tests                → integration task
```

No traceability array is stored, so it cannot drift.

### 8.6 Plan quality rules

1. **[validator, error]** Every test is listed in `tests` of at least one task.
2. **[validator, error]** Every criterion is covered by `criteria` of at least one `implement` task, directly or via its requirement ID.
3. **[validator, error]** Every ID in `criteria`, `tests` and `depends_on` resolves; `depends_on` forms a DAG over lower IDs.
4. **[validator, warning]** A task owns at most 10 tests and at most 12 steps. Larger tasks are split.
5. **[validator, warning]** A spec has at most 12 tasks.
6. **[generation instruction]** Group by requirement, not by layer: a task delivers one or two requirements end to end (model, logic, interface, tests), so that each task leaves the system in a working state.

---

## 9. Lifecycle

Unchanged from v1.3 §9: `draft → active → sealed`, optional `superseded`
and `archived`; intent hash computed at `draft → active` and enforced
afterwards; superseding adds a deprecation banner and moves the old spec to
`archive/`.

---

## 10. Validation

### 10.1 Schema validation

Per-file JSON Schema validation (`*.v2.json`). Rejects unknown fields,
missing required fields, bad enums. The EARS field constraints of §6.2.1
are expressed in the schema with a single conditional per pattern on the
`condition`/`guard` fields.

### 10.2 Cross-file integrity

All rules are errors unless stated otherwise.

| # | Rule | Artifacts |
|---|---|---|
| C1 | `spec_id` and `spec_name` agree across `prd.md`, all three JSON files and the folder name. | all |
| C2 | Every ID matches its format (§Appendix A), carries this spec's prefix, and is unique within its family. | all |
| C3 | Every `test.verifies` entry resolves to a criterion or a path. | test_spec → requirements |
| C4 | Every criterion is verified by at least one test. | requirements ← test_spec |
| C5 | Every path is verified by at least one `smoke` test, and every `smoke` test verifies at least one path. | requirements ↔ test_spec |
| C6 | Every `task.criteria` entry resolves to a requirement or criterion; every `task.tests` entry resolves to a test; every `depends_on` entry resolves to a lower task ID. | tasks → requirements, test_spec |
| C7 | Every test is owned by at least one task. | test_spec ← tasks |
| C8 | Every criterion is owned by at least one `implement` task. | requirements ← tasks |
| C9 | Exactly one task has kind `integration`, it is the last task, and its `tests` include every `smoke` test. | tasks, test_spec |
| C10 | Every `unwanted` criterion has a non-empty `contract`. | requirements |
| C11 | `real_components` is present and non-empty exactly when `kind` is `smoke`. | test_spec |

Warnings (never block): scope limits (§6.5.3, §7.3.4, §8.6.4–5), vague
language (§6.5.4), glossary hints (§10.3), criteria without a `contract`
whose action mentions an error outcome.

### 10.3 Glossary

The glossary is documentation, not a validation target. The v1 backtick
rule generated dozens of definitions for identifiers and produced most of
the repair churn. In v2 the validator emits at most a **warning** listing
backtick-wrapped terms that appear in three or more criteria and have no
glossary entry.

### 10.4 Cross-spec

`spec validate --cross` checks: every `dependencies[].spec` exists; the
dependency graph is acyclic; glossary terms shared between specs have the
same definition (warning); and, for each dependency edge, the downstream
spec has at least one path step whose actor also appears in an upstream
path (error, unchanged from v1).

---

## 11. Rendering

Rendering is deterministic: same JSON in, same Markdown out.

### 11.1 Targets

| Target | Content |
|---|---|
| Per-file | Markdown for one artifact. |
| Combined | PRD body, then `architecture.md` if present, then requirements, tests, tasks. |
| Scoped to task `N` | PRD body and architecture unfiltered; the requirements that own the task's criteria in full and all others as one line each; the task's tests in full; the paths verified by those tests; task `N` in full and all other tasks as one line each. |

The scoped render is what a coder receives. Because C7 and C8 guarantee
that every task lists its tests and criteria, the v1 inference chain
(traceability lookup, text matching, full-spec fallback) is not needed and
does not exist in v2.

### 11.2 Rules

- Every criterion renders as its EARS sentence followed by `→ {contract}`
  when a contract exists.
- Every test renders all of its fields, whatever its kind. A test rendered
  as a title alone is a v1 bug, not a feature.
- Every task renders `steps`, `touches`, `criteria`, `tests`, `done_when`
  and the implicit definition of done.

---

## 12. Generation contract

This section binds the coordinator (the generation pipeline). A pipeline
that does not follow it will produce valid-looking specs with gaps, which
is exactly what v1 did.

1. **Sequential with full context.** Artifacts are generated in the order
   requirements → tests → tasks. The tests step receives the complete
   requirements artifact. The tasks step receives the complete
   requirements artifact and the complete test artifact. "Complete" means
   every ID and every sentence; a compact Markdown rendering (§11) is
   acceptable, an ID-only summary is not.
2. **Validate inline, repair inline.** After each step the pipeline runs
   schema validation plus every cross-file rule that is decidable with the
   artifacts produced so far (C2–C5 after tests, C6–C9 after tasks) and
   sends violations back to the model for repair before continuing. A
   generation that ends with an invalid spec is a failed generation; it
   does not write partial output as "warnings".
3. **Examples match the schema.** Every example in a prompt validates
   against the v2 schema of the artifact it illustrates.
4. **Scope first.** Before writing requirements the coordinator decides
   whether the PRD fits in 10 requirements; if not, it reports that the
   PRD must be split rather than generating an oversized spec.
5. **Language from the project.** Test commands, stub markers and
   pseudocode idioms come from the project's manifest, never from a
   default.

---

## 13. Migration from v1.3

| v1.3 construct | v2 |
|---|---|
| `acceptance_criteria[]` + `edge_cases[]` (`.C` and `.EC` IDs) | `criteria[]` with `.C` IDs; edge cases are `unwanted`/`complex_event` criteria |
| `ears_pattern`, `trigger`/`state`/`error_condition`/`feature`, `condition` | `pattern`, `condition`, `guard` |
| `return_contract` (required, nullable) | `contract` (optional; required for `unwanted`; always rendered) |
| `user_story {role, goal, benefit}` | `rationale` (optional string) |
| `correctness_properties[]` (`PROP` IDs) | `ubiquitous` criteria + tests of kind `property` |
| `error_handling[]` (`ERR` IDs) | dropped; `unwanted` criteria carry the behaviour and the contract |
| `external_apis[].version: "unverified…"` | `external_apis[].verified: false` |
| `test_cases`, `edge_case_tests`, `property_tests`, `smoke_tests` (`TS-N`, `TS-EN`, `TS-PN`, `TS-SMOKE-N`) | `tests[]` with `kind`, single `TS-N` series |
| `requirement_id` / `property_id` / `execution_path_id` on tests | `verifies[]` |
| `preconditions`, `input`, `expected`, `assertion_pseudocode` | `given`, `when`, `then`, `pseudocode` |
| `trigger`, `mockable`, `expected_effects` on smoke tests | `when`, (dropped), `then` |
| `coverage {}` | computed, not stored |
| `task_groups[].subtasks[]` (`G.N` IDs), `kind: tests/standard/checkpoint` | `tasks[]` (`N` IDs), `kind: implement`; tests are written inside the task that owns them |
| `kind: wiring_verification` + "stub/dead" word check | `kind: integration` + rule C9 |
| `verification {id: "G.V", checks[]}` | implicit definition of done + optional `done_when[]` |
| `details[]` | `steps[]` |
| `requirement_refs`, `test_spec_refs` | `criteria`, `tests` (both mandatory) |
| — | `touches[]`, `depends_on[]` (new, optional) |
| `traceability[]` | derived (§8.5) |
| `dependencies[].from_group/to_group/sentinel` | `dependencies[].spec/reason` |
| subtask states `queued`, `pending_reevaluation` | dropped; `pending`, `in_progress`, `done`, `dropped` remain |

A deterministic converter (`spec migrate`) can produce a v2 spec from a
v1 spec for everything except the missing task ownership of edge-case and
property tests (C7/C8), which v1 never had; the converter attaches those
tests to the task that owns the parent requirement.

---

## Appendix A: ID formats

| Entity | Format | Example |
|---|---|---|
| Requirement | `{spec_id}-REQ-{N}` | `05-REQ-3` |
| Criterion | `{spec_id}-REQ-{N}.{C}` | `05-REQ-3.2` |
| Execution path | `{spec_id}-PATH-{N}` | `05-PATH-1` |
| Test | `TS-{spec_id}-{N}` | `TS-05-12` |
| Task | `{N}` (integer) | `3` |

Five formats, down from thirteen.

## Appendix B: Schema files

| Schema | Validates |
|---|---|
| `prd-frontmatter.v2.json` | YAML frontmatter of `prd.md` |
| `requirements.v2.json` | `requirements.json` |
| `test_spec.v2.json` | `test_spec.json` |
| `tasks.v2.json` | `tasks.json` |

The `$schema` URIs are informational; the library validates against its
bundled copies.
