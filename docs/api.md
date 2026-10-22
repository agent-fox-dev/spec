# API

This repository has no REST API. Its API is the Go library `afspec` and the
JSON object the `spec`, `specr` and `speca` commands write to stdout.

The authoritative reference is the package documentation: run `make doc` to
write `go doc -all` output for every public package to `docs/generated/` (committed), or read it with
`go doc -all github.com/agent-fox-dev/spec/afspec`. This file is the map.

| Package | Purpose |
|---|---|
| `afspec` | The reference library for the version 2 specification format: load (`LoadSpec`), create (`CreateSpec`), mutate, validate (`ValidateArtifactSchema`, `ValidateCrossSpec`), lint, render and compute the dependency graph of a spec package. A fuller tour is in [`afspec/README.md`](../afspec/README.md). |
| `envelope` | The JSON the `spec`, `specr` and `speca` commands write to stdout, as Go types a program decodes it into. Standard library only. See [below](#envelope). |
| `specgen` | The generation pipeline behind `spec`: phases, prompts and the split of a large spec. |
| `cmd/spec`, `cmd/specr`, `cmd/speca` | The commands; see [`cli.md`](cli.md), [`specr.md`](specr.md) and [`speca.md`](speca.md). |

The format itself, with its schemas and validation rules, is in
[`specification/spec-format-v2.md`](../specification/spec-format-v2.md); the
canonical JSON Schemas are in `specification/schemas/`.

How the packages fit together is in [`architecture.md`](architecture.md); the
configuration of the commands is in [`configuration.md`](configuration.md).

## envelope

`github.com/agent-fox-dev/spec/envelope` is the contract of the commands'
output. Every command writes exactly one JSON object to stdout, an
`envelope.Envelope[R]`, whose `result` is the command's own result. The
commands build their output from these types, so decoding with them cannot
drift from what is written.

| What | Where |
|---|---|
| `Envelope[R]`, and `SpecEnvelope` / `RenderEnvelope` / `AdminEnvelope` for the three commands | the object: `tool`, `version`, `ok`, `status`, `exit_code`, `summary`, `error`, `warnings`, `result`, `artifacts`, `side_effects`, `next`, `input`, `model`, `usage`, `duration_ms`, `started_at` |
| `DecodeSpec`, `DecodeSpecr`, `DecodeSpeca`, `Decode[R]` | read one envelope; unknown fields are ignored, anything but exactly one object is an error |
| `SpecResult`, `Package`, `ScopeReport`, `ValidationReport`, `TraceReport`, `OpenQuestion` | `spec`'s result, see [cli.md](cli.md#result) |
| `Review`, `ReviewRound`, `ReviewFinding`, the `Severity…`, `Disposition…` and `ReviewStop…` constants | `Package.Review`, the adversarial review of a package's PRD (`--adversarial`), see [cli.md](cli.md#the-adversarial-review---adversarial) |
| `RenderResult`, `TaskInfo` | `specr`'s result, see [specr.md](specr.md#result). `TaskInfo.Ready` is true for a pending task whose dependencies are all done, and absent otherwise; `speca` writes it too |
| `AdminResult`, `TaskCounts`, `PackageInfo` | `speca`'s result for every form: the report, the verdict (`--validate`), the list (`--list`) and the task form, which is the report after a change. A form fills the fields it produces and the rest are absent; `Valid` is a `*bool` so that false is written. See [speca.md](speca.md) |
| `Change` | one change a call made, or under `--dry-run` would have made: `kind` (`task`, an open set), `id`, `from`, `to` and `forced`, each left out when unset. `AdminResult.Changes` lists them in plan order, and `AdminResult.DryRun` is true under `--dry-run` |
| `ErrorInfo`, `FixHint`, the `Category…` constants | the failure; `*ErrorInfo` is an `error`. `CategoryLifecycle` (`lifecycle`, not retryable) is a call that was read and was well-formed and that the task state machine or the package's lifecycle does not allow: stages `task` and `status` of `speca`'s task form |
| `Warning`, `WarnCode` and the `Warn…` constants | what went differently without stopping the run. `WarnTaskNotReady` (`task_not_ready`) and `WarnTaskStateForced` (`task_state_forced`) are `speca`'s task form, at stage `task` |
| `Artifact`, `SideEffect`, `Next`, `InputInfo`, `ModelInfo`, `UsageInfo`, `PhaseInfo`, `PreflightCheck`, `Estimate` | the rest of the envelope |

```go
env, err := envelope.DecodeSpec(stdout) // or DecodeSpecr, DecodeSpeca
if err != nil {
    return err
}
if !env.OK {
    return env.Error
}
fmt.Println(env.Result.SpecDir, env.Result.Validation.Valid)
```

Fields are added over time and a decoder ignores the ones it does not know; a
field's name and meaning do not change. Warning codes, error categories and
stages are open sets: a value with no constant is not an error.
