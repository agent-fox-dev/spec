# 06. Build against the latest agentkit-go

**Status:** accepted
**Date:** 2026-10-15
**Related:** [ADR 01](01-run-the-spec-command-on-the-agentkit-driver.md)

## Context

agentkit-go has no tags, so this module pins a pseudo-version of its `main`
branch: once in the `require` of `agentkit-go/codesearch`, and once in the
`replace` that ADR 01 describes. A pin only moves when somebody moves it,
and nobody did: a `spec` built from this repository sent every tool to the
Anthropic API as strict and got a 400 (`Schemas contains too many parameters
with union types`) from its first model call. agentkit-go had fixed that
(`keep the strict tools of an Anthropic request within the API's limits`)
four hours after the revision this module pinned. No test here could notice:
they run against a scripted model, never the API.

The two modules are developed together, and a fix in one is wanted in the
other as soon as it lands.

## Decision

Every make target that compiles first runs `make update-agentkit`, which
resolves `github.com/agent-fox-dev/agentkit-go@main`, rewrites the `replace`
and the `codesearch` requirement to that revision, and runs `go mod tidy`.
`build`, `test`, `test-fast`, `coverage`, `vet`, `lint` and `doc` depend on
it, so `make check` and `make build` use the latest agentkit-go.

- **The pin stays in `go.mod`.** Go has no floating version, and the
  resolved revision is what the build records. When the update moves it,
  `go.mod` and `go.sum` change in the working tree and are committed with
  the change they were built with.
- **`AGENTKIT_UPDATE=0 make <target>` skips the update**, for working offline
  or reproducing a build of the committed pin. The update needs the network;
  when it cannot resolve `main` it fails and says so, rather than building
  silently against a stale pin.
- Plain `go build` and `go test` do not run the update. They use the pin in
  `go.mod`, which is the revision the last `make` left there.

## Consequences

- A breaking change on agentkit-go's `main` breaks this repository's next
  `make`, which is the point: the break shows up here, at the change that
  causes it, and not in a user's first run.
- Builds are not reproducible from the source tree alone until the moved pin
  is committed. A change that bumps the pin says so in its commit.
- `make` needs the network by default.
