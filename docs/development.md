# Development

How to build, install and work on this repository. What the commands do is
in [cli.md](cli.md), [specr.md](specr.md) and [speca.md](speca.md); how the
pieces fit is in [architecture.md](architecture.md); the environment
variables and make variables are in [configuration.md](configuration.md).

## Building the commands

Build the `spec`, `specr` and `speca` commands from a checkout:

```bash
git clone https://github.com/agent-fox-dev/spec && cd spec
make build          # installs spec, specr and speca into $GOBIN
```

`make build` stamps all three commands with `git describe`, the commit and the
build time (the variables in [version.go](../version.go)), which `--version`
prints, e.g. `spec v1.5.0 (commit fa6b562, built 2026-11-06T10:52:11Z)`.

Or install a command without a checkout, from the latest release:

```bash
go install github.com/agent-fox-dev/spec/cmd/spec@latest     # or cmd/specr, cmd/speca
```

`@v1.5.0` pins the release explicitly, and `@main` installs the current `main`.
Every form works because `go.mod` names released versions and carries no
`replace`, which `go install` would ignore
([ADR 13](adr/13-pin-a-released-agentkit-go.md)).

`go.mod` pins a released agentkit-go and carries no `replace`, so every target
builds the committed pin and no make target needs the network.
`make update-agentkit` moves the pin to agentkit-go's latest release when
somebody wants it moved.

## The Go library

```bash
go get github.com/agent-fox-dev/spec/afspec
```

The library's API is in [afspec/README.md](../afspec/README.md), its generated
reference in [generated/afspec.txt](generated/afspec.txt), and three programs
that use it in [examples/](../examples/README.md). A Go program that calls one
of the commands decodes their output with the [`envelope`](api.md#envelope)
package (`envelope.DecodeSpec`, `envelope.DecodeSpecr`, `envelope.DecodeSpeca`)
instead of writing structs of its own.

## The skills

Each skill under `skills/` is a directory holding a `SKILL.md` — the
frontmatter and the body — with the reference files it links in `references/`
beside it, and the files more than one skill links in
`skills/_shared/references/`. The body carries the order of the run, its gates
and its refusals; a reference file is read at the step that links it, so a step
loads only what it needs. No rendered body is over 24,576 bytes, which
`make check` enforces.

`make skills` renders every skill for every agent into `build/skills/<agent>/`.
`make install-skills` installs them, keeping only the frontmatter keys that
agent reads and writing an invocation of another skill in that agent's syntax:

```bash
make install-skills                      # Claude Code, into ~/.claude/skills
make install-skills AGENT=codex          # Codex, into $CODEX_HOME/skills
make install-skills AGENT=cursor         # Cursor, into ~/.cursor/skills
make install-skills AGENT=copilot        # Copilot, into ~/.copilot/skills
```

`PROJECT=<path>` installs into that repository instead, so a team can commit
the skills for an agent that reads only repository skills, such as Copilot's
cloud agent; `SKILLS_DIR=<dir>` overrides both. `make uninstall-skills` takes
the same variables and removes what it installed. The variables are documented
in [configuration.md](configuration.md#the-skills), and the directory of each
agent is recorded, with the date it was confirmed, in
[ADR 12](adr/12-render-the-skills-for-every-agent-from-one-source.md).

A Claude Code install already reaches Copilot, which reads `~/.claude/skills`
too, so installing for Copilot as well makes it load each skill twice; the
install says so and goes ahead.

## Make targets

The library and the commands need Go 1.27 or later. The tests need no network
and no API key: the command's tests run against a scripted model.

| Command | What it does |
| --- | --- |
| `make help` | List all targets |
| `make check` | `make fmt`, `vet`, `lint`, `test` and `skills-check` — run it before committing |
| `make build` | Install the `spec`, `specr` and `speca` commands, stamped with the version |
| `make test` | `go test ./... -count=1` |
| `make test-fast` | The tests with `-short` |
| `make fmt` | `gofmt -l -w .` |
| `make vet` | `go vet ./...` |
| `make lint` | `golangci-lint` if installed (config in `.golangci.yml`); it must be built with the Go version `go.mod` names or newer |
| `make coverage` | Test coverage per function |
| `make doc` | Write `go doc -all` for each package to `docs/generated/` (committed) |
| `make update-agentkit` | Move the agentkit-go pin to its latest release (explicit; no target runs it) |
| `make tidy` | `go mod tidy` |
| `make clean` | Remove `build/` and the coverage profile |
| `make skills` | Render the skills under `skills/` for every agent into `build/skills` |
| `make skills-check` | Check the skills against every render rule (part of `make check`) |
| `make json-gen` | Regenerate the Go artifact types from `afspec/schemas/` |
| `make install-skills`, `make uninstall-skills` | Install the skills for one agent, or remove them (`AGENT`, `PROJECT`, `SKILLS_DIR`) |

## Working on the repository

The rules every coding session follows — reading order, the spec-driven
workflow, conventional commits, what a finished session looks like — are in
[AGENTS.md](../AGENTS.md). The project-level directives the skills and the
`spec` command read are in `.specs/steering.md`. Decisions are recorded as
ADRs under [adr/](adr/), and every divergence between a spec and its
implementation as an erratum under [errata/](errata/).

Project-agnostic versions of `AGENTS.md` and `.specs/steering.md`, to paste
into a new project as a starting point, are in [templates/](templates/README.md).
