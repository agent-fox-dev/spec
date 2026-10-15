# 08. Send a call that failed mid-stream again

**Status:** accepted
**Date:** 2026-10-15
**Related:** [ADR 01](01-run-the-spec-command-on-the-agentkit-driver.md),
[ADR 03](03-pipeline-the-scopes-of-a-split.md)

## Context

ADR 01 dropped agent-fox's `middleware.Retry` for the Anthropic SDK's own
retries. Those cover a request whose response has not begun: a refused
connection, a 429 or a 5xx status. They do not cover a stream that fails
after its 200 — a connection that goes dead, a body that stops, an
`overloaded_error` event — and agentkit-go's loop ends the run on the first
turn that fails.

A run resuming a five-scope split of agentkit-go's PRD 12 failed that way
without writing a package. Twenty turns into the second scope's PRD phase and
two into the first scope's requirements, the one HTTP/2 connection both phases
shared stopped answering; after four minutes the kernel gave up on it
(`read: operation timed out`) and both phases ended with it. The run
reported a retryable `api` failure and wrote nothing. The first scope's PRD,
ten minutes and $3.54 of it, was thrown away, for the next run to write
again.

## Decision

`agentrun` wraps the provider of every phase in one that sends a failed call
again when the failure is transient: a network error, a truncated body, an
HTTP 408, 409, 429 or 5xx, or an `overloaded_error`, `api_error`,
`rate_limit_error` or `timeout_error` event. It waits 2, 8 and 30 seconds
before the three repeats (`agentrun.DefaultRetryDelays`). A cancellation, a
credential, a refusal or an invalid request is not repeated, and a run
cancelled during a wait ends as aborted.

The failed attempt's events are forwarded as they arrive; the two that end
it — the terminal error and the failed message — are held back until the
attempt is known to be the last. A repeat is traced under `--verbose`. The
message that ends the stream carries the failed attempts' usage, which was
billed.

The repeat lives here and not in agentkit-go because it needs nothing of the
driver's: a `core.ProviderClient` wrapping another is all it takes.

## Consequences

- A connection that drops is survived without the phase, or the scopes of a
  split sharing the connection, starting over.
- A failure that persists still fails the run, about forty seconds later.
- A repeated call re-streams its message: under `--show-text` and
  `--verbose` the partial text or tool calls of the failed attempt show
  before the repeat's.
- A dead connection is still only noticed when the operating system gives up
  on it, which can take minutes; an idle timeout on the stream would notice
  sooner.
