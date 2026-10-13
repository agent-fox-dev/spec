# Architecture

This repository holds the agent-fox specification format and its reference
implementation, the `afspec` Go library. It is one Go module,
`github.com/agent-fox-dev/spec`.

## Layout

```
specification/              # The format (no code)
  spec-format-v2.md         #   The current format reference
  spec-format.md            #   Version 1.3, kept for reference
  schemas/                  #   Canonical JSON Schemas, v1 and v2
afspec/                     # The reference library, package afspec
  schemas/                  #   Embedded copies of the v2 schemas
  legacy/                   #   Version 1.3 types and a read-only loader
docs/                       # Repository documentation
.specs/                     # Spec packages for work on this repository
```

Tests sit next to the code they test, as `*_test.go` files in `afspec/`.

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

The v1 schemas have no copy in `afspec/`: the `legacy` package's types were
generated from them once and are not regenerated.

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
| Migration from version 1.3 | `migrate.go`, `legacy/` |

The full API is listed in [afspec/README.md](../afspec/README.md).

## Consumers

The `spec` CLI and the `specgen` generation pipeline live in
[agent-fox](https://github.com/agent-fox-dev/agent-fox) and import `afspec` as
a library. This repository ships no binary. Lifecycle management and migration
have no CLI surface anywhere; they are library API for an embedder to call.
