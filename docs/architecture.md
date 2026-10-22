# Architecture

This repository holds the agent-fox specification format, its reference
implementation (the `afspec` Go library), the `spec` command, which turns a
product idea into a package of that format, the `specr` command, which
renders a package as Markdown, and the `speca` command, which reports a
package's state. It is one Go module, `github.com/agent-fox-dev/spec`.

## Layout

```
specification/              # The format (no code)
  spec-format-v2.md         #   The current format reference
  spec-format.md            #   Version 1.3, kept for reference
  schemas/                  #   Canonical JSON Schemas, v1 and v2
afspec/                     # The reference library, package afspec
  schemas/                  #   Embedded copies of the v2 schemas
  testdata/                 #   Spec package fixtures for the tests
envelope/                   # The commands' JSON output as Go types (stdlib only)
cmd/spec/                   # The spec command: flags, help, the entry point
cmd/specr/                  # The specr command: a package rendered as Markdown
cmd/speca/                  # The speca command: a package's state, its verdict, the list
specgen/                    # The generation pipeline: phases, prompts, review, split
  templates/                #   The prompts, as Markdown, embedded
internal/
  agentrun/                 #   One model phase on the AgentKit driver
  cli/                      #   The shell: input, envelope, flags, progress
  issuex/                   #   GitHub and GitLab: read an issue, post a comment
  project/                  #   Language detection, the test-command audit, steering
  repomap/                  #   The token-budgeted repository map
  specpkg/                  #   What the commands share about a package on disk
examples/                   # Programs using afspec: create, validate, work through tasks
docs/                       # Repository documentation
.specs/                     # Spec packages for work on this repository
```

Tests sit next to the code they test, as `*_test.go` files.
`make doc` writes the Go package reference of the public packages to
`docs/generated/`, which is committed so an agent can orient itself with it.

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
| Task state machine, readiness (`IsReady`) and the next task (`NextTask`) | `task.go` |
| Mutation and ID allocation | `mutate.go`, `nextid.go`, `bootstrap.go` |
| Discovery, the cross-spec dependency graph, lint | `discover.go`, `dirname.go`, `specsdir.go`, `graph.go`, `lint.go` |
| Derived coverage and traceability | `coverage.go` |
| Markdown rendering, with a token budget | `render.go`, `render_budget.go`, `estimate_tokens.go`, `ears.go` |
| Stepwise validation for a generation pipeline | `generation.go` |

The full API is listed in [afspec/README.md](../afspec/README.md).

## The output contract

The commands write one JSON object to stdout. Its Go types are the public
package `envelope`, which imports the standard library only: the envelope, the
error and warning vocabulary, `spec`'s result (`SpecResult`), `specr`'s
(`RenderResult`) and `speca`'s (`AdminResult`). A program that reads the output
imports it and calls `envelope.DecodeSpec`, `DecodeSpecr` or `DecodeSpeca`; it
pulls in neither the model SDK nor the `replace` that `go.mod` carries for
agentkit-go.

The commands do not keep a second definition. `internal/cli` names the types
as aliases (`cli.Envelope` is `envelope.Envelope[any]`, since the shell does
not know which result it carries) and adds what only a producer needs: the
`Run` that accumulates warnings and cost, the `warnStages` table, the schema
generator behind `specr --schema` and `speca --schema`. `specgen.Result` embeds
`envelope.SpecResult`, `cmd/specr`'s `Result` embeds `envelope.RenderResult` and
`cmd/speca`'s embeds `envelope.AdminResult`, adding the methods the shell asks
of a result (`Summary`, `Next`, `Artifacts`). Nothing under
`envelope/` imports another package of this module.

## The spec command

`spec` is documented for callers in [cli.md](cli.md) and
[configuration.md](configuration.md); the decision to build it here, on the
cut-down AgentKit, is [ADR 01](adr/01-run-the-spec-command-on-the-agentkit-driver.md).

### Package graph

```
cmd/spec ──> internal/cli ──> internal/agentrun ──> agentkit-go (driver, workspace,
   │              │                                  tools, catalog, codesearch,
   │              └──> internal/issuex                provider/anthropic)
   └──> specgen ──> afspec, internal/{agentrun, cli, issuex, project, repomap, specpkg}
```

Nothing under `internal/` imports `specgen`, and `afspec` imports nothing
from this repository. AgentKit is consumed for the driver (`agentkit.New`),
the workspace a run is rooted at (`workspace.Open`), the read-only file tools
built over it (`tools.For`), outlines (`outline`), the model catalog
(`catalog.Lookup`), the Anthropic client (`anthropic.Resolve`), the schema
combinators (`schema`) and the indexed search (`codesearch`).

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
   With `--adversarial`, the PRD then goes through its **review rounds**
   (`specgen/rounds.go`, [ADR 09](adr/09-review-the-prd-adversarially.md)):
   a `prd_review` phase finds its flaws, a `prd_revision` phase answers them,
   and the package is built from the last complete PRD. A split runs each
   scope's rounds in the coordinator, after its PRD phase and before its
   number is reserved, so the next scope's PRD phase is shown the revised PRD.
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
  `Prune` so old tool results leave the request before the window fills;
- `MaxTokens`, the cap on one response: 65,536 tokens for every phase
  (`specgen/phases.go`), or `--max-tokens`, which `Bounds.MaxTokens` carries
  and which replaces the phase's own cap. The model's thinking is spent from
  the same cap as its reply, and a phase's reply is one tool call carrying the
  whole document, so at `xhigh` and `max` a smaller cap is gone before the call
  begins. The driver goes on only while a response holds a tool call, so such
  a response ends the run as an `end_turn` with no result;
  `agentrun.Result.LastReason` keeps the response's own stop reason, which is
  how the `no_result` error tells that from a model that stopped on its own
  and names `--max-tokens` and the effort rather than a ceiling that was never
  reached;
- a provider that sends a model call again when its stream failed for a
  transient reason — a dropped or stalled connection, a truncated body, an
  overloaded or rate-limited API — after 2, 8 and 30 seconds
  ([ADR 08](adr/08-send-a-call-that-failed-mid-stream-again.md)). The SDK
  retries only a request whose response has not begun, and the driver ends
  the run on the first failed turn.

Each phase declares its kind (`prd`, `prd_review`, `prd_revision`,
`requirements`, `test_spec`, `tasks`, `architecture`), and the runner builds its agent on that kind's model and
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

| Part | PRD phase | Review and revision phases | Generation and architecture phases |
|---|---|---|---|
| system | the mandate, then the run context: language, `steering.md`, repository map | the review's or the revision's mandate, then the same run context | the format's rules, then the same run context |
| prefix | the input and `--context` | — | — |
| task | the landscape of specs and, in a split, the scope block | the input and `--context`, the PRD with its open questions and relevant files, the codebase notes as the author's claims, the landscape, in a split the scope block without the PRD-writing instructions, earlier rounds' findings; the revision also the round's findings | the PRD, the codebase notes, the landscape, the relevant files, the artifacts before it, the step's instructions |

The run context (`runContextBlock`) is the same for every phase of a run, so
the packages of a split that run the same step on the same model read it,
and the 6000-token map in it, from the cache; every scope's PRD phase shares
the input as well. Tool sets differ between steps, and tools come first, so
the steps of one package do not share a prefix with each other.

A phase's own tools (the PRD, review, revision or artifact submit tool, and
for the artifact steps the patch tool) are declared strict; its built-in read-only tools are
not ([ADR 07](adr/07-keep-the-builtin-tools-out-of-strict-mode.md)).
agentkit-go declares strict the tools it is not told to leave out, in the
order they are sent, while they fit the API's counted limits (20 tools, 16
union parameters). The artifact schemas are kept strict-expressible: the identity fields the handler
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
    ├──────> envelope          (its result: RenderResult)
    ├──────> internal/specpkg  (the resolver, the display rule, the task summary)
    └──────> internal/cli      (the Run, SplitArgs, WriteAtomic, the --schema generator)
```

It does not use `cli.App`, which resolves a model, a forge and a code-search
index before it hands over: `specr` has none of those. `cmd/specr` parses its
own flags with `cli.SplitArgs`, resolves the reference (`specpkg.Locate`), loads
the package with `afspec.LoadSpec`, validates it for a warning, renders it with
`RenderCombined`, `RenderIndividual` or `RenderIndividualScoped`
(`render.go`), and emits a `cli.Envelope`. `--schema` is built by
`cli.FlagsDocument` and `cli.EnvelopeDocument` (`internal/cli/schemadoc.go`),
which describe a flag set and a Go type by reflection, reading each field's
`description` tag.

## The speca command

`speca` is documented for callers in [speca.md](speca.md); the decision is
[ADR 10](adr/10-report-and-change-the-state-of-a-package-with-its-own-command.md).

```
cmd/speca ──> afspec
    ├───────> envelope        (its result: AdminResult)
    ├───────> internal/specpkg
    └───────> internal/cli    (the Run, SplitArgs, the --schema generator)

cmd/specr ──> internal/specpkg
specgen   ──> internal/specpkg
internal/specpkg ──> afspec, envelope, internal/cli   (for the usage error)
```

Like `specr`, it does not use `cli.App`, and it links `internal/cli` and so the
model stack, which it never uses (ADR 05, Consequences). `internal/specpkg`
holds what `spec`, `specr` and `speca` each need and none should copy: the
resolver of a package reference (`Locate`, `List`), the display rule for a path
(`Display`), the task summary with its `ready` (`TaskInfos`), the converters of
afspec's verdict and derived coverage into the envelope's `ValidationReport` and
`TraceReport` (`ReportOf`, `TraceOf`), and the names of the rules a verdict
breaks (`Rules`). It is a package of its own because `envelope` must stay
standard-library only and `internal/cli` does not import `afspec`.

`cmd/speca` has five forms, each in a file of its own (the status form in three),
chosen by the flags in `forms.go`:

| Form | File | Reads | Writes |
|---|---|---|---|
| the report, `speca <spec>` | `report.go` | the package, and the spec root's dependency graph for `dependents` | nothing |
| the verdict, `speca <spec> --validate` | `verdict.go` | the package and every package of the spec root, for the cross-spec rules | nothing |
| the list, `speca --list` | `list.go` | every package directory of the spec root, and `archive/` with `--all` | nothing |
| the task form, `speca <spec> --task T --state S` | `task.go`, `plan.go`, `taskflags.go` | the package | every artifact of the package, through `Spec.Save`; the content that changes is the task states and the `updated_at` of `prd.md` |
| the status form, `speca <spec> --status S` | `status.go`, `status_checks.go`, `status_supersede.go` | the package, and the replacing package with `--by` | every artifact of the package, through `Transition` or `Supersede`, and of the replacing package, through `Save`; then the directory, renamed under `archive/` by `MoveUnderArchive`, which rewrites no file |

The forms that write do so only through the library. The task form:
`plan.go` chooses the moves and applies them with `TransitionTask` (and, under
`--force`, `CompleteTaskStates` and `ResetTaskStates`), so the state machine is
`afspec`'s and not a copy; `task.go` sets `updated_at`, which `Save` does not, and
calls `Spec.Save`, which writes every artifact through temporary files and then
renames them, so that all of them are in the library's canonical encoding and
have mode `0600` afterwards. A call that is refused, and a dry run, write nothing. Nothing
outside the files that write may call `Save`, `Transition` or `Supersede` (`task.go`
and the status form's files); a test of the sources checks it, and checks that the
other forms leave the file tree as it was.

The status form is a list of checks and a write (`status.go`, `statusChecks`). The
checks run in the order the spec fixes (load, the move, the target's own, the keys
a write would drop, the archive destination), and the first to refuse ends the
call with no result. The writes come in the order that puts the step most likely to
fail last: the status (`Transition` or `Supersede`), the id in the replacing
package's `supersedes` (`Save`), then `MoveUnderArchive`. The library calls and the
read-back are variables, as `Save` is in the task form, so that a test can fail them. A
move that fails after the status was written returns a result and the call that
finishes it, and `Result.Resumable` says so to the shell, which reports it in the
error. `cmd/speca` shares `unknownKeys` and `frontmatterUnknownKeys` with the task
form, since the library does not report the keys of the `prd.md` frontmatter that
every write drops.

Readiness is not decided here: `IsReady` and `NextTask` are
`afspec`'s. `speca` uses both; `specr` reports `IsReady` on each task it lists,
through `specpkg.TaskInfo`; and `examples/tasks` picks its task with `NextTask`.
So the three agree on what "ready" and "next" mean.

The three commands name one another. `cli.SiblingCommands` (`internal/cli/siblings.go`)
is the one sentence that does, and each command's help and each `--schema`
description (`spec` has none) carry it. `specr`'s `next` also suggests the `speca` call
that claims a task or records it done, but only where that call would succeed, and
`internal/specpkg` holds the rule both commands ask: `Writable` (a draft, or an active
package whose intent has not drifted, `IntentDrift`). `speca` accepts a write and
suggests a claim by it, `specr` suggests a call of `speca` by it, and it keeps the answer
in a field of its result that is never written.

## Consumers

agent-fox's tools import `afspec` as a library. Reading a package's state,
moving its tasks and changing its lifecycle status (activating, sealing,
archiving or superseding a package) have a command, `speca`; the same moves are
library API for an embedder to call.
