# Architecture

This repository holds the agent-fox specification format, its reference
implementation (the `afspec` Go library), the `spec` command, which turns a
product idea into a package of that format, and the `specr` command, which
renders a package as Markdown. It is one Go module,
`github.com/agent-fox-dev/spec`.

## Layout

```
specification/              # The format (no code)
  spec-format-v2.md         #   The current format reference
  spec-format.md            #   Version 1.3, kept for reference
  schemas/                  #   Canonical JSON Schemas, v1 and v2
afspec/                     # The reference library, package afspec
  schemas/                  #   Embedded copies of the v2 schemas
  testdata/                 #   Spec package fixtures for the tests
cmd/spec/                   # The spec command: flags, help, the entry point
cmd/specr/                  # The specr command: a package rendered as Markdown
specgen/                    # The generation pipeline: phases, prompts, split
  templates/                #   The prompts, as Markdown, embedded
internal/
  agentrun/                 #   One model phase on the AgentKit driver
  cli/                      #   The shell: input, envelope, flags, progress
  issuex/                   #   GitHub and GitLab: read an issue, post a comment
  project/                  #   Language detection, the test-command audit, steering
  repomap/                  #   The token-budgeted repository map
docs/                       # Repository documentation
.specs/                     # Spec packages for work on this repository
```

Tests sit next to the code they test, as `*_test.go` files.
`make doc` writes the Go package reference to `build/docs/`, which is not
committed.

## The format and the library

`specification/` defines the format; `afspec` implements it. A change to the
format lands in both in the same change.

### The schema copies

`afspec` embeds its schemas with `//go:embed schemas/*.json`, and an embed
pattern cannot reach outside the package directory, so `afspec/schemas/` holds
copies of the four v2 files in `specification/schemas/`. The two sets must be
byte-identical. To change a schema:

1. Edit it in `specification/schemas/` and update the format reference.
2. Copy it over the file of the same name in `afspec/schemas/`.
3. Run `make json-gen` to regenerate `requirements.v2.go`, `test_spec.v2.go`,
   `tasks.v2.go` and `prd-frontmatter.v2.go`.

The v1 schemas have no copy in `afspec/`: the library reads version 2 only.

### What the library does

| Area | Files |
| --- | --- |
| Load and save, atomically and round-trip exact | `afspec.go`, `prd.go`, `marshal.go`, `create.go` |
| Schema validation and the cross-file rules C1–C11 | `validate.go`, `schemas.go`, `unknown.go` |
| Lifecycle (`draft → active → sealed`, superseded, archived) and the intent hash | `lifecycle.go`, `intent.go` |
| Task state machine | `task.go` |
| Mutation and ID allocation | `mutate.go`, `nextid.go`, `bootstrap.go` |
| Discovery, the cross-spec dependency graph, lint | `discover.go`, `dirname.go`, `specsdir.go`, `graph.go`, `lint.go` |
| Derived coverage and traceability | `coverage.go` |
| Markdown rendering, with a token budget | `render.go`, `render_budget.go`, `estimate_tokens.go`, `ears.go` |
| Stepwise validation for a generation pipeline | `generation.go` |

The full API is listed in [afspec/README.md](../afspec/README.md).

## The spec command

`spec` is documented for callers in [cli.md](cli.md) and
[configuration.md](configuration.md); the decision to build it here, on the
cut-down AgentKit, is [ADR 01](adr/01-run-the-spec-command-on-the-agentkit-driver.md).

### Package graph

```
cmd/spec ──> internal/cli ──> internal/agentrun ──> agentkit-go (driver, tools,
   │              │                                  catalog, codesearch,
   │              └──> internal/issuex                provider/anthropic)
   └──> specgen ──> afspec, internal/{agentrun, cli, issuex, project, repomap}
```

Nothing under `internal/` imports `specgen`, and `afspec` imports nothing
from this repository. AgentKit is consumed for the driver (`agentkit.New`),
the read-only file tools (`tools.All`), outlines (`outline`), the model
catalog (`catalog.Lookup`), the Anthropic client (`anthropic.Resolve`), the
schema combinators (`schema`) and the indexed search (`codesearch`).

### A run

1. **`internal/cli`** parses the flags, classifies and reads the one input (a
   file, text, stdin, or a GitHub/GitLab thread through `internal/issuex`),
   resolves the model (a tier or a catalog id, and its effort) and the
   Anthropic client, builds the run's one code-search index, and builds an
   `agentrun.Runner`. Every refusal up to here happens before a token is spent,
   and `--preflight` stops right after it.
2. **`specgen.Run`** looks for an unfinished split plan for this input,
   detects the project's language (`internal/project`), lists the existing
   specs, and builds the repository map once (`internal/repomap`).
3. The **PRD phase** runs, unless a plan is being resumed. It may report a
   split, which is recorded as `<first_scope>.split.json` in the spec root.
4. For each package: a spec number is reserved by creating its directory; the
   **requirements**, **test spec** and **tasks** phases run in that order, each
   a fresh agent that sees the PRD and the artifacts before it; the optional
   **architecture** phase runs; the package is saved with `afspec`, read back,
   validated, activated, and its PRD posted under `--comment`.
   A split pipelines its scopes (`specgen/split_run.go`): the PRD phases run
   in order, and each scope's package is built on its own goroutine as soon as
   its PRD exists, up to `--max-parallel` at once. The numbering, the
   landscape of existing specs and the split plan are shared between those
   goroutines under one lock; numbers are reserved by the coordinator, in
   scope order.
5. **`internal/cli`** builds the envelope from the result and the run's
   bookkeeping (warnings, phases, forge writes) and prints it.

### A phase

`agentrun.Runner.Run` builds one `agentkit.Agent` per phase with:

- the phase's system prompt, the project's `AGENTS.md` and `CLAUDE.md` under
  `--trust-project`, and a closing note naming the granted tools;
- the phase's optional `Prefix`, sent as the driver's cached prefix ahead of
  the task (the PRD phase's input);
- the phase's own terminating tools, whose handlers validate the submission
  and either return `Terminate` or an error the model reads and repairs (a
  generation phase has two: `submit_<step>` and `patch_<step>`);
- the read-only built-ins and, with an index, `code_search` — never a shell,
  so no guard is needed and `agentkit.New` would refuse one without it;
- `MaxTurns`, `MaxCostUSD` and `Timeout` from the per-phase bounds, and
  `Prune` so old tool results leave the request before the window fills.

Each phase declares its kind (`prd`, `requirements`, `test_spec`, `tasks`,
`architecture`), and the runner builds its agent on that kind's model and
effort from the run's `agentrun.Plan`: the tier's choice for the phase, or a
`--phase-model` override. A submit tool's rejection runs
`afspec.ValidateGenerationStepComplete` and the project audit together, so it
names every problem in one pass, and holds the rejected submission for the
step's patch tool (`specgen/patch.go`) to correct in place.

### What a phase is sent, and what is cached

A request is the tools, then the system prompt, then the messages, and the
provider caches it by prefix: whatever two requests share from the start is
paid for once at the full rate. The prompts are laid out for that
(ADR 04):

| Part | PRD phase | Generation and architecture phases |
|---|---|---|
| system | the mandate, then the run context: language, `steering.md`, repository map | the format's rules, then the same run context |
| prefix | the input and `--context` | — |
| task | the landscape of specs and, in a split, the scope block | the PRD, the codebase notes, the landscape, the relevant files, the artifacts before it, the step's instructions |

The run context (`runContextBlock`) is the same for every phase of a run, so
the packages of a split that run the same step on the same model read it,
and the 6000-token map in it, from the cache; every scope's PRD phase shares
the input as well. Tool sets differ between steps, and tools come first, so
the steps of one package do not share a prefix with each other.

Every tool is declared strict while the request stays within the API's strict
limits (20 tools, 16 union parameters); agentkit-go decides which. The
artifact schemas are kept strict-expressible: the identity fields the handler
writes are not declared, and a dictionary such as the glossary is asked for as
a list of entries.

The driver's event stream feeds the progress lines and the per-phase counts
(`tool_calls`, `tool_errors`); its `RunResult` is the phase's usage and stop
reason, which `agentrun` maps onto the envelope's error categories.

## The specr command

`specr` is documented for callers in [specr.md](specr.md); the decision is
[ADR 05](adr/05-render-a-package-with-its-own-command.md).

```
cmd/specr ──> afspec
    └──────> internal/cli   (the envelope, SplitArgs, WriteAtomic, the --schema generator)
```

It does not use `cli.App`, which resolves a model, a forge and a code-search
index before it hands over: `specr` has none of those. `cmd/specr` parses its
own flags with `cli.SplitArgs`, resolves the reference (`locate.go`), loads the
package with `afspec.LoadSpec`, validates it for a warning, renders it with
`RenderCombined`, `RenderIndividual` or `RenderIndividualScoped`
(`render.go`), and emits a `cli.Envelope`. `--schema` is built by
`cli.FlagsDocument` and `cli.EnvelopeDocument` (`internal/cli/schemadoc.go`),
which describe a flag set and a Go type by reflection, reading each field's
`description` tag.

## Consumers

agent-fox's tools import `afspec` as a library. Lifecycle management has no
CLI surface; it is library API for an embedder to call.
