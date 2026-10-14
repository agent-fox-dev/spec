# 05. Render a package with its own command, built for an agent to call

**Status:** accepted
**Date:** 2026-10-14
**Related:** [ADR 01](01-run-the-spec-command-on-the-agentkit-driver.md),
agent-fox's ADR 03 (rebuild the skills as tools) and ADR 06 (version the
envelope interface)

## Context

`afspec` renders a package as Markdown, whole (`RenderCombined`) or scoped to
one task (`RenderIndividualScoped`), and agent-fox's `impl` builds its prompts
from those renders. An agent working without `impl` — a person driving
agent-fox's `af-impl` skill, or a prompt that says "implement task 3 of spec
16" — had no way to call them. It read the four artefacts as JSON and scoped
them by hand, which is the work the scoped render exists to do, done less
reliably: a coder that has to find a task's criteria, tests and verified paths
across three files misses some.

The README said rendering, like the rest of lifecycle management, had no CLI
surface and was library API for an embedder. That holds for the operations
that change a package. A render changes nothing, and its caller is often not
an embedder but an agent with a shell.

## Decision

A second command, `specr`, in `cmd/specr`, that renders a package and changes
nothing. Its interface is the one `spec` and agent-fox's tools share, because
an agent that can drive one of them can drive all of them:

- **One positional input**, a reference to a package: a path to it or to a
  file in it, or its directory name, number or name under the spec root —
  the same references agent-fox's `impl` resolves.
- **One JSON envelope on stdout** on every program-driven path, from
  `internal/cli`, so `ok`, `status`, `exit_code`, `error` (with `fix_hint`),
  `warnings` and `next` mean what they mean for `spec`. The Markdown is
  `result.markdown`, beside the facts a caller decides on: the scope, the
  sections, what a budget dropped, and the plan's tasks with their states.
- **Forms are flags, not verbs**: `--task`, `--section`, `--max-tokens`. A
  verb would be a second interface to learn; agent-fox's ADR 03 left no room
  for one.
- **The renders are the library's.** The whole package is `RenderCombined`
  byte for byte, so `specr` and `impl` show a coder the same document; a
  scoped render is `RenderIndividualScoped` under the same headings. The
  command adds a one-line note naming the task, and nothing else to the text.
- **Refuse rather than fall back.** `RenderIndividualScoped` renders the whole
  package for an unknown task id. The command refuses one as a usage error
  listing the ids, because a caller that asked for one task and silently got
  twelve has been misled.

### Discoverable

`--help` carries the forms and examples. `--schema`, which `spec` left behind
when it moved here, returns for `specr`: one JSON object holding the flags as
a JSON Schema generated from the flag set, the envelope's schema generated
from the Go types with each field described, and the exit codes — what a
framework needs to declare `specr` as a tool without reading this repository.
A golden file makes any change to it a reviewed diff. The generator lives in
`internal/cli`, where `spec` can adopt it.

`next` points at the next task not done, so a caller that rendered the whole
package learns the `--task` form from the output itself.

### What it does not carry

There is no `schema_version`: the envelope in this repository has none (ADR
01), and versioning `specr`'s alone would describe half an interface. There is
no `--output`, `--detail`, `--preflight` or progress stream: they serve a long
model run, and a render takes milliseconds.

`--markdown-file` is the one concession to callers without a JSON parser: it
writes the Markdown to a file of its own, while stdout stays the envelope.
Printing raw Markdown on stdout instead was rejected: a command whose stdout
is Markdown on success and JSON on failure forces its caller to guess which
it got.

### Escaping

The shared emitter stops escaping `<`, `>` and `&` as `<`-style
sequences. Markdown is full of them (`<br>` in table cells, `&` in
pseudocode), the envelope is read by programs and models and never embedded
in HTML, and the decoded value is the same either way. `spec`'s envelopes
change in the same way.

## Consequences

- `make build` builds both commands. `specr` links `internal/cli` and with it
  the model stack, so its binary is as large as `spec`'s; it makes no model
  call and needs no credential. Moving the envelope into a package of its own
  would shrink it and is left for when a third command wants the same.
- A change to a render function changes `specr`'s output, which is the point:
  there is one renderer.
- The warning codes `spec_invalid`, `section_absent` and `over_budget`, and the
  artifact kind `markdown_file`, join the shared sets in `internal/cli`.
