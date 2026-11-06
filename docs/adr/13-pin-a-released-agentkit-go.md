# 13. Pin a released agentkit-go

**Status:** accepted
**Date:** 2026-11-06
**Supersedes:** [ADR 06](06-build-against-the-latest-agentkit-go.md)
**Related:** [ADR 01](01-run-the-spec-command-on-the-agentkit-driver.md)

## Context

[ADR 06](06-build-against-the-latest-agentkit-go.md) had every make target that
compiles first run `make update-agentkit`, which resolved
`github.com/agent-fox-dev/agentkit-go@main`, rewrote the `replace` and the
`codesearch` requirement to that revision, and ran `go mod tidy`. It was the
right answer while agentkit-go had no tags: a pin only moves when somebody
moves it, nobody did, and a `spec` built here sent strict tools the Anthropic
API refused, four hours after agentkit-go had fixed it.

Three things followed from it that were always costs rather than benefits:

- **`go.mod` carried a `replace`.** The `require` was the unresolvable
  `v0.0.0` and the `replace` pointed it at a pseudo-version of `main`. Because
  `go install <pkg>@<version>` ignores a module's own `replace` directives,
  `go install github.com/agent-fox-dev/spec/cmd/spec@<version>` could not work
  at all.
- **`make` needed the network**, for every target that compiles, including
  `make test`.
- **Builds were not reproducible from the source tree**, because the pin moved
  underneath them.

agentkit-go now has a release, `v0.9.0`.

## Decision

Pin the released version and carry no `replace`.

```
require (
	github.com/agent-fox-dev/agentkit-go v0.9.0
	github.com/agent-fox-dev/agentkit-go/codesearch v0.0.0-20261026174114-de24b69a5bad
)
```

- **No target moves the pin.** `build`, `test`, `test-fast`, `coverage`, `vet`,
  `lint` and `doc` no longer depend on `update-agentkit`, so every target
  builds the committed pin, and `AGENTKIT_UPDATE` is gone with the dependency
  it controlled. No make target reaches the network any more.
- **`make update-agentkit` stays, as an explicit move.** It runs
  `go get github.com/agent-fox-dev/agentkit-go@latest` and `go mod tidy`, and
  writes no `replace`. Somebody runs it when they want the pin moved, and the
  move is committed as its own change.
- **`codesearch` stays on a pseudo-version of `main`.** It is a nested module,
  so a release of it would be tagged `codesearch/v0.9.0`, and no tag of that
  form exists: `go list -m -versions` reports no version for it at all. A
  pseudo-version in `require` is resolvable from the proxy, unlike the
  `replace` it replaces, so this does not block `go install`.
  `make update-agentkit` deliberately leaves it alone: the two modules are
  developed together, and moving `codesearch` to `main` while agentkit-go sits
  on a release is how they would come apart. It is moved by hand, with
  `go get github.com/agent-fox-dev/agentkit-go/codesearch@main`, until it is
  tagged too.

## Consequences

- `go install github.com/agent-fox-dev/spec/cmd/spec@main` works. Verified
  against the commit that merged this change: it installs as
  `v1.4.2-0.20261106104458-0693d2f6e842` and runs. `@latest` still does not,
  but no longer because of the `replace`: the newest tag, `v1.4.1`, predates
  the Go commands and holds no `cmd/` directory, so no release contains them.
  Removing the `replace` is what made either possible, and it is the first half
  of cutting a release (`docs/spec-kit-comparison.md` §7.1, item 6); the second
  half is a tag.
- `make check` and `make test` run offline.
- A build is reproducible from the source tree, because the pin only moves when
  a commit moves it.
- **A fix on agentkit-go's `main` no longer arrives by itself.** That is the
  cost ADR 06 was paying the network and the `replace` to avoid, and it is now
  deliberate: a fix arrives when agentkit-go is released and somebody runs
  `make update-agentkit`. If a break like the strict-tools one recurs, it will
  show up in a user's run and not in this repository's next `make`, so the pin
  is worth moving whenever agentkit-go cuts a release.
- The pin is `v0.9.0`, which is pre-1.0: a release of it may still break this
  repository, and nothing here holds it to compatibility.
