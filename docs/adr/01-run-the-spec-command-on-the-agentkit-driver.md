# 01. Run the spec command on the AgentKit driver

**Status:** accepted
**Date:** 2026-10-13
**Related:** agent-fox ADR 02 (build the spec pipeline on AgentKit), ADR 04
(implement a spec as a tool), ADR 05 (write every scope of a split);
agentkit-go PRD 10 (cut AgentKit down to the hands and one driver)

## Context

The `afspec` library moved to this repository. The `spec` command that uses
it — `cmd/spec` and the `specgen` pipeline — stayed in agent-fox, on top of
two layers agent-fox shares between its four tools: `internal/toolio` (the
shell: input, envelope, events, report file, `--schema`) and
`internal/agentrun` (the phase runner: model tiers, guard, policy, scratch
directories, compaction).

AgentKit has since been cut down by its PRD 10. Its module is now
`github.com/agent-fox-dev/agentkit-go`, and the packages agentrun was built on
are gone: `compaction`, `middleware`, `stop`, `skills`, `jsonx`, the non-Claude
providers, `core.AgentConfig`, `NewAgentWithHistory`, `ThinkingLevel`,
`ConstrainedSampling`, `catalog.ResolveModel`. What is left is the hands (the
file tools, outlines, symbols, the indexed search module, code mode, the MCP
pool, the guard) and one minimal driver, `agentkit.New(Config)`, over the
official Anthropic SDK. agent-fox's `spec` does not build against it.

## Decision

Re-implement `spec` in this repository, with the same intent, pipeline, flags,
result and exit codes, on the driver as it now is:

- **`specgen` moves unchanged in substance.** The prompts, the submit tools
  and their validation, the split and its resumable plan, the number
  reservation, the project audit and the result types are ported as they were.
  Its tests come with it and drive the real loop against `provider/faux`.
- **`internal/agentrun` is rewritten, not ported.** One `agentkit.New` per
  phase. Every spec phase is read-only and has no shell, so the guard, the
  tool policy, the scratch directory and the read roots are not needed —
  `agentkit.New` itself refuses a reachable shell with no guard, and the
  runner refuses to grant anything but the read-only tools. What agentrun
  owned and AgentKit now does is deleted from it:

  | Was | Is |
  |---|---|
  | `stop.AfterTurns`, `stop.OverBudget`, the phase timeout | `Config.MaxTurns`, `MaxCostUSD`, `Timeout` |
  | `middleware.Retry` (three attempts) | the SDK's retries (two) |
  | `compaction` (summarising at 60% of the window) | `Config.Prune` (eliding old tool results at 35%, last four turns whole) |
  | `ThinkingLevel` and the tier table's three vendors | `Effort`; the tier table's Claude rows |
  | `catalog.ResolveModel`, credential checks per vendor | `catalog.Lookup`, `anthropic.Resolve` |
  | skills and context-file discovery | `AGENTS.md` and `CLAUDE.md` read by agentrun under `--trust-project` |
  | `ConstrainedSampling`, `Temperature` | none: every tool is sent `strict`, and `Config` has no temperature |

- **`internal/cli` is a lean shell for one command.** It keeps what a caller
  of `spec` reads — one envelope with `ok`, `status`, `exit_code`, `summary`,
  `error` (with `fix_hint`), `warnings`, `result` under `--detail`,
  `artifacts`, `side_effects`, `next`, `input`, `model`, `usage` — and
  `--output`, `--preflight`, `--dry-run`, `--context`, `--input-kind`. It drops
  what existed to keep four tools uniform or observable from outside: the
  events file and `--emit-events`, the report file, `--schema`,
  `schema_version`, `session_id`, `timings`, `x-trust` and
  `untrusted_fields`, `needs_human`, and the cross-tool flag messages.
- **`internal/issuex` keeps reading and commenting.** The GitHub and GitLab
  client is trimmed to `ReadIssue`, `ListComments`, `AddComment`,
  `Authenticated` and `Close`, with the shared transport, forge detection and
  host checks unchanged.
- **Code search stays.** The run builds one `codesearch` index, offers
  `code_search` to every phase, and closes it on every exit path.
- **`jsonx` is replaced by a 100-line ordered decoder** on the standard
  library's `encoding/json/jsontext`. `schema.Parse` is not used: it refuses
  boolean subschemas, and the converter has to inline `$defs`, drop keywords
  only in keyword position and drop property names the API cannot declare,
  which a parse-then-walk would have to undo.
- **An unlisted model id is refused.** AgentKit serves an id its catalog does
  not list with no price, so `--budget` could never stop it.

## Consequences

- `spec` is buildable and testable from this repository alone, and agent-fox
  can delete its copy.
- agentkit-go has no tags yet, so `go.mod` requires a pseudo-version.
  `agentkit-go/codesearch` requires `agentkit-go v0.0.0` (its own `go.mod`
  resolves that with a local `replace`), so this module carries one
  `replace` from that version to the same pseudo-version. Until agentkit-go
  tags a release that codesearch requires, `go install …/cmd/spec@version`
  refuses the module; the command is built from a checkout (`make build`).
  The pin follows agentkit-go's `main` ([ADR 06](06-build-against-the-latest-agentkit-go.md)).
- The envelope is smaller than agent-fox's version 3.1 and carries no
  `schema_version`. A caller that read `report_file`, `session_id`,
  `timings` or the events stream has to stop.
- The test IDs in the ported tests (`TS-06-57`, `TS-14-28`, …) name the
  agent-fox specs that introduced the behaviour; those specs are not in this
  repository.
