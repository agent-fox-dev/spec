# spec

A specification format for work done by coding agents, in which a spec is a
contract that a validator can prove complete, and the tooling that writes,
renders and administers packages of it.

## Why this exists

Coding agents are good at building what they are told and bad at noticing
what they were not told. A feature described in prose has gaps the agent
fills on its own, tests the agent treats as optional, and a plan nobody can
check for holes. The first version of this format was written to fix that,
and it did not go far enough: every archived version 1 spec had edge-case and
property tests that no task owned, so no coder ever implemented them. The
spec said they existed; nothing proved that anybody would build them.

That failure is the reason for everything in this repository. A spec is not
useful because it is written down. It is useful when something other than a
model can say whether it is complete, whether it was followed, and what is
left to do.

## What is different

Most spec-driven kits for agents are a set of prompt templates and Markdown
files: the method lives in the prompts, and the artifacts are whatever the
model wrote. This one is a **machine-validated contract format**, and that
changes what the tools can promise.

- **A plan is proven complete, or it is invalid.** Every acceptance criterion
  carries the check that verifies it. Every end-to-end path carries a smoke
  check that names the real components it may not mock. Every criterion is
  owned by a task. The schemas and eight cross-file rules make those facts
  properties the validator proves, not conventions the model is asked to
  follow. A package that breaks one is invalid, not warned about.

- **Tests are the contract, not an option.** Because the check is part of the
  criterion, there is no test to skip and no "add tests later". The
  integration task that runs every path's smoke check is derived from the
  package, so it cannot be forgotten or written differently each time. A task
  closes on evidence, not on a green suite: `speca` runs the tests mapped to
  each criterion, records the run in which each failed before the task was
  implemented, and closes the task only when every one of them passes.

- **State is a state machine, not a checkbox.** A task is claimed through a
  compare-and-set, so two agents cannot both take it. Readiness follows the
  dependencies. Activation hashes the spec's intent and its contract files,
  and every later task move is refused while either differs, so the spec
  cannot be edited to match the code. Progress lives in its own small file, so
  recording work never rewrites the contract.

- **Context is engineered for the coder.** An agent implementing task 3 is
  given task 3 in full, with its criteria, checks and the paths they name, and
  everything else by id, cut to a token budget if asked. It is not handed the
  whole package and told to find its part.

- **It runs without a person.** From an issue URL or a sentence to a
  validated, activated package in one unattended run, with a cost estimate
  before any model call, budgets, per-phase models, an adversarial review of
  the PRD and of the plan, and a split into several packages when the idea is
  too big for one. The result is one JSON object with the next call to make.

- **Specs relate to each other.** A package names the packages it depends
  on; the validator checks that they exist, that there is no cycle, and that
  the two share an actor in their execution paths. The packages of a split are
  generated as a pipeline so each one sees the ones before it.

- **A spec describes a change, not the system.** Once implemented, a package
  is sealed and archived; the code, its tests and the architecture decisions
  are the record. The format keeps no living description of the system, on
  purpose: that description goes stale, and the kit would rather have a
  contract that was true when it was built than one that claims to be true
  now.

## What it costs

The same choices have a price, and it is worth knowing before adopting it.

- The generator runs on Claude only, and the commands need a Go toolchain.
  The skills render for Claude Code, Codex, Cursor, Copilot, pi and
  opencode, but the unattended run is one vendor's.
- Three of a package's files are JSON. A rendered `spec.md` is committed
  beside them so a reviewer can read the diff, but the contract itself is
  data, not prose.
- A package is heavier than a Markdown spec of the same feature. Version 3
  of the format made it about a third smaller, and a `lite` profile fits a
  small change, but a validated contract is more than a page of bullets.
- Drift between sealed packages and the code is not detected after the fact.
  The implementation skill checks conformance once, at pull-request time.
- The method is not customisable: one format, one set of prompts, no hooks.

A longer and more critical comparison with GitHub Spec Kit, OpenSpec and
Google's Elephant-Goldfish Model, including what this kit has borrowed from
each, is in [docs/spec-kit-comparison.md](docs/spec-kit-comparison.md).

## How it is used

Three steps, and a command or skill for each.

1. **Write the PRD.** Prose with a hashed `## Intent`, goals, non-goals and
   the decisions already taken. The `af-prd` skill writes it with a person;
   the `spec` command writes it itself from an idea, an issue or a file,
   records every open question it had to settle, and decides whether the
   change fits a `lite` package.
2. **Generate the package.** `spec` turns the PRD into `requirements.json`,
   where every criterion carries its check and every path its smoke check,
   and `tasks.json`, the flat list of tasks that own the criteria. Each file
   is validated before it is written, and the package is activated only on a
   clean verdict. The `af-spec` skill does the same inside a session, and on
   request has a fresh subagent review the plan before activation.
3. **Implement it, task by task.** The `af-impl` skill claims a task with
   `speca`, reads it with `specr --task N`, writes the task's checks as
   failing tests, makes them pass, records the task done, and opens a pull
   request when the derived integration task has run every path end to end.

```bash
spec https://github.com/org/repo/issues/42    # idea → validated package under .specs/
specr 16 --task 3                             # task 3 in full, the rest by id
speca 16 --task 3 --state in_progress         # claim it; refused if another agent has it
speca 16 --task 3 --evidence map.json         # its tests, seen failing before the code exists
speca 16 --task 3 --state done                # seen passing, then recorded; progress.json is the only file that changes
speca 16 --verify-evidence                    # every recorded test again, on the merged tree
speca 16 --validate                           # the format's full verdict, rule by rule
speca --next                                  # the package to implement next, in dependency order
specrun                                       # implement every open package, one /af-impl session each
```

Every command writes one JSON object to stdout, with a `next` field naming
the call to make after it, so an agent or a program can drive the whole
loop without parsing prose.

## What is in this repository

| Path | What it is |
| --- | --- |
| [`specification/`](specification/spec-format-v3.md) | The format: the reference document and the canonical JSON Schemas |
| [`afspec/`](afspec/README.md) | The Go library: load, validate, mutate, save, render and migrate a package |
| [`envelope/`](docs/api.md#envelope) | The commands' JSON output as Go types, for a program that calls them |
| `cmd/spec/`, `specgen/` | The `spec` command and its generation pipeline |
| `cmd/specr/`, `cmd/speca/` | The `specr` render and the `speca` admin commands |
| `cmd/specrun/`, `internal/specrun/` | The `specrun` loop: `/af-impl` on every open package, each in a `claude -p` session, checked from the repository |
| `skills/` | The `af-prd`, `af-spec` and `af-impl` skills, one source rendered for six agents |
| [`examples/`](examples/README.md) | Programs that create, validate and work through a package with the library |
| `docs/` | The reference docs, the decisions (ADRs), the errata and this kit's comparison with others |

## Getting started

```bash
git clone https://github.com/agent-fox-dev/spec && cd spec
make build              # spec, specr, speca and specrun into $GOBIN
make install-skills     # the three skills into ~/.claude/skills (AGENT=codex|cursor|copilot|pi|opencode for the others)
```

`spec` reaches Claude through `ANTHROPIC_API_KEY`, Vertex AI or Amazon
Bedrock; `specr` and `speca` make no model call at all, and `specrun` runs
every session through `claude -p`. Building without a
checkout, the library, the make targets and the rules a coding session
follows are in [docs/development.md](docs/development.md).

## Read more

- [The format](specification/spec-format-v3.md) — every field, the EARS patterns, the checks, the validation rules, rendering and the migration from version 3
- [The `spec` command](docs/cli.md), [the `specr` command](docs/specr.md) and [the `speca` command](docs/speca.md) — input, flags, the output envelope, errors and warnings
- [The `specrun` command](docs/specrun.md) — the implementation loop: the landing checks, the state and resume, the stall, the logs and the ledger
- [Architecture](docs/architecture.md), [configuration](docs/configuration.md) and [the public Go API](docs/api.md)
- [Comparison with other spec kits](docs/spec-kit-comparison.md) — where this kit is stronger, where it is weaker, and what it borrowed
- [Decisions](docs/adr/) — one ADR per choice, from running the generator on the AgentKit driver to migrating the format to version 3
