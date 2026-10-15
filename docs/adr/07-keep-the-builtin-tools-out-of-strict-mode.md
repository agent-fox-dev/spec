# 07. Keep the built-in tools out of strict mode

**Status:** accepted
**Date:** 2026-10-15
**Related:** [ADR 04](04-hand-on-what-a-phase-learned.md),
[ADR 06](06-build-against-the-latest-agentkit-go.md)

## Context

Every tool of an Anthropic request is declared strict, so the API guarantees
the arguments match the schema. The API totals its strict-mode limits over the
request: 20 strict tools, 16 union parameters (each optional property is one),
and a compiled grammar size it does not publish. A request past any is refused
whole, with a 400.

agentkit-go counts the first two and declares tools strict in the order they
are sent, while they fit. That fixed the first failure of a real run (the
union parameters of the read-only file tools, see ADR 04). It could not see
the third: the requirements phase, with `submit_requirements`,
`patch_requirements` and four file tools strict, was refused ("The compiled
grammar is too large"). A probe of the real API found:

- `submit_requirements` alone, with `patch_requirements`, and with one file
  tool: accepted. The same with four file tools: refused. The file tools
  alone, and `patch_requirements` with them: accepted.
- A request with the same 16 unions in simpler tools (the PRD phase) was
  accepted, so no count of tools or unions predicts it.
- The PRD, test spec and tasks phases were accepted, with no known margin.
- With the file tools first, so `submit_requirements` was the tool left out of
  strict mode: accepted.

## Decision

A phase's built-in read-only tools are sent without the strict flag
(`anthropic.Options.NonStrictTools`, set in `agentrun.Runner`), so the strict
budget is spent on the phase's own tools. They are the largest schemas of the
request and the ones whose first call must be valid: a rejected submission
costs a repair turn and a correction of a whole artifact. The built-ins take a
path and a line range; their arguments are validated when they arrive, and a
bad one costs one cheap tool error.

## Consequences

- The requirements phase sends two strict tools (`submit_requirements`,
  `patch_requirements`), the combination the probe found accepted. Every phase
  has the same margin: its own schemas, and nothing else.
- A model may send a built-in tool a malformed argument that strict mode would
  have prevented. The tool answers with an error naming the field.
- A phase's own tools can still outgrow the budget, alone or together. The
  same probe is how to find out: it builds each phase's request and posts it
  with `max_tokens` 1.
- `spec` depends on an agentkit-go with the option; ADR 06 keeps it on the
  latest.
