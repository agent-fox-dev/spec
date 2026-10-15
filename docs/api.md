# API

This repository has no REST API. Its API is the Go library `afspec` and the
JSON object the `spec` and `specr` commands write to stdout.

The authoritative reference is the package documentation: run `make doc` to
write `go doc -all` output for every public package to `docs/generated/` (committed), or read it with
`go doc -all github.com/agent-fox-dev/spec/afspec`. This file is the map.

| Package | Purpose |
|---|---|
| `afspec` | The reference library for the version 2 specification format: load (`LoadSpec`), create (`CreateSpec`), mutate, validate (`ValidateArtifactSchema`, `ValidateCrossSpec`), lint, render and compute the dependency graph of a spec package. A fuller tour is in [`afspec/README.md`](../afspec/README.md). |
| `specgen` | The generation pipeline behind `spec`: phases, prompts and the split of a large spec. |
| `cmd/spec`, `cmd/specr` | The commands; see [`cli.md`](cli.md) and [`specr.md`](specr.md). |

The format itself, with its schemas and validation rules, is in
[`specification/spec-format-v2.md`](../specification/spec-format-v2.md); the
canonical JSON Schemas are in `specification/schemas/`.

How the packages fit together is in [`architecture.md`](architecture.md); the
configuration of the commands is in [`configuration.md`](configuration.md).
