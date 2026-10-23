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

This repository holds the format, its reference library, the command that
writes packages of it, the command that renders them and the command that
reports their state:

| Path | What it is |
| --- | --- |
| `specification/` | The format: the reference documents and the canonical JSON Schemas |
| [`afspec/`](afspec/README.md) | The `afspec` Go library — load, validate, mutate, save and render spec packages. It embeds a copy of the v2 schemas in `afspec/schemas/` |
| [`envelope/`](docs/api.md#envelope) | The JSON `spec`, `specr` and `speca` write to stdout, as Go types for a program to decode it into; standard library only |
| `cmd/spec/`, `specgen/` | The `spec` command and its generation pipeline, built on `afspec` and [agentkit-go](https://github.com/agent-fox-dev/agentkit-go) |
| `cmd/specr/` | The `specr` command: a package rendered as Markdown, whole or scoped to one task |
| `cmd/speca/` | The `speca` command: a package's state, the format's verdict on it, or the list of every package |

See [docs/architecture.md](docs/architecture.md) for how the pieces fit
together.

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
validates it against the other specs of the spec root (an unknown dependency, a
cycle, a dependency with no actor in common), and activates it if it validates.
A package that fails either is still written to disk, in `draft`, with the
broken rules named in the result. It is the verdict `speca <spec> --validate` gives.

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
| `--dry-run` | Write nothing to disk or to the forge; report what would be written |
| `--model`, `--effort` | A tier (`SIMPLE`, `STANDARD`, `ADVANCED`) or Claude model id, and its reasoning effort |
| `--phase-model` | Run one phase on another model and effort, e.g. `tasks=claude-haiku-4-5:medium`; a tier already runs the later phases lower |
| `--budget`, `--max-turns`, `--total-budget` | Per-phase and whole-run ceilings |
| `--max-tokens` | The output token cap of one model response, thinking included (default 65536) |
| `--preflight` | Run every check that would refuse the run, then stop before any model call |
| `--max-parallel` | How many packages of a split are generated at once (default 3) |
| `--adversarial`, `--adversarial-rounds` | Have a fresh agent look for flaws in every PRD, and another revise it, before the package is generated; up to 1–5 rounds (default 1) |

The model is Claude, reached through `ANTHROPIC_API_KEY`, Vertex AI or Amazon
Bedrock. Every flag, the envelope, the error categories and the warning codes
are in [docs/cli.md](docs/cli.md); the environment variables in
[docs/configuration.md](docs/configuration.md).

A Go program that calls one of the commands decodes its output with the
[`envelope`](docs/api.md#envelope) package (`envelope.DecodeSpec`,
`envelope.DecodeSpecr`, `envelope.DecodeSpeca`) instead of writing structs of its own.

Rendering a package, reading its state, moving its tasks from one state to
another and changing its lifecycle status — activating, sealing, archiving or
superseding it — have a command each, below. The lifecycle moves are also library
API in [`afspec`](afspec/README.md) for an embedder to call.

## Rendering a Spec Package

The `specr` CLI renders an existing package as Markdown with `afspec`'s render
functions, for an agent to read — called from a prompt, or from a skill's
instructions such as agent-fox's `af-impl`. It takes one reference to a
package (its directory, a file in it, or its number or name under the spec
root) and changes nothing:

```bash
specr .specs/16_indexed_code_search    # the whole package
specr 16 --task 3                      # scoped to task 3: what it owns in full, the rest by id
specr 16 --section tasks               # only the named sections
specr 16 --max-tokens 8000             # drop detail until it fits
specr --schema                         # the flags and the output, as JSON Schema
```

Like `spec`, it writes one JSON object to stdout: the Markdown is
`result.markdown`, beside the spec's identity, the sections rendered, what a
budget dropped, and the plan's tasks with their states; `next` names the next
task not done and, where the package can take the change, the `speca` call that
claims the task to work on or records the task rendered as done. `--markdown-file` also writes the Markdown to a file. Exit
codes: `0` rendered, `1` the package could not be read, `2` a usage error.
Everything is in [docs/specr.md](docs/specr.md); the decision is
[ADR 05](docs/adr/05-render-a-package-with-its-own-command.md).

## Reporting a Spec Package's State

The `speca` CLI is the spec admin command. It reports a package's state, gives
the format's full verdict on it, lists every package under the spec root, and
moves the state of a package's tasks. It takes one reference to a package (none
with `--list`) and writes one JSON object to stdout, as `spec` and `specr` do:

```bash
speca 16                  # status, validity, every task with its state and readiness, the next task,
                          # the lifecycle moves allowed, the packages it depends on and that depend on it
speca 16 --validate       # schema errors, the cross-file rules C1–C11 and the cross-spec rules, by rule
speca --list              # every package under the spec root, in number order
speca --list --all        # and those under archive/
speca 16 --task next --state in_progress   # claim the task to work on now
speca 16 --task 4 --state done             # finish task 4 (--task 1,2 or all, --dry-run, --force)
speca 16 --status active                   # activate a draft; also sealed, archived, or superseded --by 12
speca --schema            # the flags and the output, as JSON Schema
```

`result.next_task` is the task to work on — the one in progress, else the first
whose dependencies are done — and `next` names the `specr` call that renders
it, after the claim of the task where it can be claimed. A package that does not
validate is reported with exit 0, in `valid`; exit codes `0`, `1` and `2` are
those of the other two commands. The task form moves tasks through the library's
task state machine, every one or none, so a second claim of a task that is taken
is refused with the states it may move to, and it writes through the library's
`Spec.Save`, which writes every artifact of the package again. The status form
moves a package through its lifecycle with the library's `Transition` and
`Supersede`, refuses a move the lifecycle does not allow with the way to get
there, moves an archived or superseded package under `archive/`, and, when that
move fails after the status was written, says so and is resumable: the same call
moves the package and changes nothing else. The other forms write no file. `speca`
makes no model or network call and reads no credential. Everything is in
[docs/speca.md](docs/speca.md); the decision is
[ADR 10](docs/adr/10-report-and-change-the-state-of-a-package-with-its-own-command.md).

## Agent Skills

`skills/` holds two Claude Code skills that put the three commands to work in
a person's own session, one on each side of `spec`:

| Skill | Step | What it does |
| --- | --- | --- |
| `af-prd` | before `spec` | writes a PRD with the user, in the shape `spec`'s own PRD phase writes, checked against what the format demands, and saves it where `spec <prd>` takes it |
| `af-impl` | after `spec` | implements a package task by task on one branch, claiming and finishing each task with `speca`, rendering it with `specr`, and opening a pull request, with an issue for each item it leaves open (through aflib's `issue` command) |

`make install-skills` copies each one to `~/.claude/skills/<name>/SKILL.md`;
`make uninstall-skills` removes them.

## Installation

Build the `spec`, `specr` and `speca` commands from a checkout:

```bash
git clone https://github.com/agent-fox-dev/spec && cd spec
make build          # installs spec, specr and speca into $GOBIN
```

`make build` stamps all three commands with `git describe`, the commit and the
build time (the variables in [version.go](version.go)), which `--version`
prints, e.g. `spec v1.4.1 (commit 94f3890, built 2026-10-14T06:33:13Z)`.

`go install …/cmd/spec@version` (or `…/cmd/specr@version`, `…/cmd/speca@version`) does not work yet: the module carries a
`replace` for agentkit-go until that module is tagged (see
[ADR 01](docs/adr/01-run-the-spec-command-on-the-agentkit-driver.md)).

`make build`, `make test` and the other targets that compile first move the
agentkit-go pin in `go.mod` to the latest `main` ([ADR 06](docs/adr/06-build-against-the-latest-agentkit-go.md));
`AGENTKIT_UPDATE=0 make build` skips that, e.g. offline.

### Go library

```bash
go get github.com/agent-fox-dev/spec/afspec
```

## Development

The library and the command need Go 1.27 or later. The tests need no network
and no API key: the command's tests run against a scripted model.

| Command | What it does |
| --- | --- |
| `make help` | List all targets |
| `make check` | `make fmt`, `vet`, `lint` and `test` |
| `make build` | Install the `spec`, `specr` and `speca` commands, stamped with the version |
| `make test` | `go test ./... -count=1` |
| `make test-fast` | The tests with `-short` |
| `make fmt` | `gofmt -l -w .` |
| `make vet` | `go vet ./...` |
| `make lint` | `golangci-lint` if installed (config in `.golangci.yml`) |
| `make coverage` | Test coverage per function |
| `make doc` | Write `go doc -all` for each package to `docs/generated/` (committed) |
| `make update-agentkit` | Point `go.mod` at the latest agentkit-go `main` (the targets that compile run it first; `AGENTKIT_UPDATE=0` skips it) |
| `make tidy` | `go mod tidy` |
| `make clean` | Remove `build/` and the coverage profile |
| `make json-gen` | Regenerate the Go artifact types from `afspec/schemas/` |
| `make install-skills`, `make uninstall-skills` | Install the skills under `skills/` into `~/.claude/skills`, or remove them |

## Documentation

- [Spec Format Reference v2](specification/spec-format-v2.md) — field-level schemas, EARS patterns, validation rules, and rendering
- [Spec Format Reference v1.3](specification/spec-format.md) — the superseded format
- [afspec](afspec/README.md) — the Go library's API
- [Examples](examples/README.md) — programs that create, validate and work through a spec with `afspec`
- [Public Go API](docs/api.md) — the packages and where their reference lives
- [Architecture](docs/architecture.md) and [configuration](docs/configuration.md)
- [The spec command](docs/cli.md) — input, flags, the output envelope, errors and warnings
- [The specr command](docs/specr.md) — the forms of a render, the reference it takes, the output and `--schema`
- [The speca command](docs/speca.md) — the report, the verdict and the list, the reference it takes, the output and `--schema`
- [Configuration](docs/configuration.md) — environment variables and limits
- [Architecture](docs/architecture.md) — repository layout, the schema copies, the command's packages and a run
- [ADR 01](docs/adr/01-run-the-spec-command-on-the-agentkit-driver.md) — why the command is built here, on the AgentKit driver
- [ADR 06](docs/adr/06-build-against-the-latest-agentkit-go.md) — why every build moves to the latest agentkit-go
- [ADR 10](docs/adr/10-report-and-change-the-state-of-a-package-with-its-own-command.md) — why a package's state has a command of its own, and what it reports