# Spec Format

## Why a spec format?

Natural-language requirements are ambiguous, untestable, and drift from
implementation. The spec format solves this by defining a structured,
machine-readable package that turns design intent into verifiable contracts.
Every requirement maps to a test, every test maps to a task — nothing is
specified without verification and nothing is built without a requirement.

## Overview

A specification package ("spec") is the durable artifact that captures design
intent, acceptance criteria, verification contracts, and implementation plans
for one cohesive feature. Every spec lives in a numbered directory
(`{NN}_{snake_case_name}/`) and contains four required artifacts plus one
optional artifact:

| Artifact | Format | Purpose |
| --- | --- | --- |
| `prd.md` | Markdown + YAML frontmatter | Narrative intent — the "why" and "what." Human-authored. Contains a hashed `## Intent` section that is protected after approval. |
| `requirements.json` | JSON (schema-validated) | What the system must do: EARS acceptance criteria, correctness properties, execution paths, and error handling. |
| `test_spec.json` | JSON (schema-validated) | How each requirement is verified: unit tests, property tests, edge-case tests, and smoke tests with computed coverage. |
| `tasks.json` | JSON (schema-validated) | What work to do, in what order: task groups, subtasks with a state machine, cross-spec dependencies, and requirement-to-test traceability. |
| `architecture.md` | Markdown (free-form) | Optional. Architectural context — modules, interfaces, data models, technology choices. No schema, not cross-validated. |

**Key properties:**

- **EARS patterns.** Requirements use the Easy Approach to Requirements Syntax — six
  structured patterns (`ubiquitous`, `event_driven`, `complex_event`,
  `state_driven`, `unwanted`, `optional`) that produce testable, unambiguous
  acceptance criteria from decomposed fields.
- **Two-layer validation.** Schema validation (per-file, sub-millisecond) plus
  cross-file integrity checks (referential integrity of IDs, requirement-to-test
  coverage, glossary completeness) run on every mutation.
- **Lifecycle.** A spec progresses through `draft → active → sealed`, with
  optional `superseded` and `archived` terminal states. The `## Intent` section
  is hashed at the `draft → active` transition and protected thereafter.
- **Traceability.** Bidirectional links connect every requirement through its
  test spec and task to an executable test, ensuring nothing is specified without
  verification and nothing is built without a requirement.

The full specification — field-level schemas, EARS pattern definitions, ID
formats, validation rules, task state machine, and rendering — is at
**[spec-format-v2.md](specification/spec-format-v2.md)**. Version 1.3 is kept
for reference at [spec-format.md](specification/spec-format.md).

JSON Schemas for all artifacts are available in `specification/schemas/`
(`prd-frontmatter.v2.json`, `requirements.v2.json`, `test_spec.v2.json`,
`tasks.v2.json`, plus the v1 set). These schemas can be used for external
validation or code generation.

## Implementations

This repository is the home of the **format** only — it contains no
implementation code. The reference implementation — the `afspec` Go library,
the `specgen` generation pipeline and the `spec` CLI — lives in
[agent-fox](https://github.com/agent-fox-dev/agent-fox); it bundles a copy of
the schemas above.

## Generating a Spec Package

The `spec` CLI turns a product idea into a complete, validated package in one
unattended run — there is no session state and no interactive refinement
loop. It takes exactly one positional input:

```bash
spec path/to/prd.md                                   # a file's contents are the idea
spec https://github.com/org/repo/issues/42             # the issue and its comments are the idea
spec "add dark mode to the settings screen"            # any other text is the idea
spec -                                                  # read the idea from stdin
```

Nobody is waiting to answer questions, so the run resolves every open
question itself, records each in a `## Design Decisions` section of the PRD,
and reports the ones it is least sure of as `open_questions` in the result.
It then generates `requirements.json`, `test_spec.json` and `tasks.json` in
that order — each validated against the format's schema and its cross-file
rules before it is written — writes the package under `.specs/{NN}_{name}/`,
and activates it if it validates. A package that fails cross-file validation
is still written to disk, with the broken rules named in the result.

Output is one JSON object on stdout — spec id, artifacts written, requirement
and test counts, the validation report, derived traceability coverage, and
any `open_questions` — with progress on stderr. Exit codes: `0` a valid
package was written, `1` the run failed (the stage is named in the JSON),
`2` a usage error.

| Flag | Effect |
| --- | --- |
| `--specs-dir` | Where `NN_name` packages live (default `.specs/`, or `$AF_SPEC_DIR`) |
| `--name` | Override the spec name the model chooses |
| `--architecture` | Also write the optional `architecture.md` |
| `--no-activate` | Leave a valid package in `draft` instead of activating it |
| `--comment` | Post the finished PRD back to the source issue |
| `--dry-run` | Write nothing to disk or GitHub; report what would be written |

Lifecycle management for an existing package — activating, sealing,
archiving, superseding, validating, rendering — has no CLI surface; it's
library API in `afspec` for an embedder to call.

## Installation

Install the spec CLI via the install script:

```bash
curl -fsSL https://raw.githubusercontent.com/agent-fox-dev/agent-fox/refs/heads/main/install.sh | sh
```

### Go library

```bash
go get github.com/agent-fox-dev/agentfox
```

## Documentation

- [Spec Format Reference v2](specification/spec-format-v2.md) — field-level schemas, EARS patterns, validation rules, and rendering
- [Spec Format Reference v1.3](specification/spec-format.md) — the superseded format