# Development

## Prerequisites

- Go 1.26.5 or later

## Repository layout

```
golang/                   # Go library (afspec package) and CLI
  agentspec/              # AI-powered spec creation (session state machine, Claude API integration)
  cmd/
    spec-cli/             # CLI binary entry point (main.go, cross-compiled)
    spec/                 # CLI command package (cobra commands, flags, output)
  schemas/                # Bundled JSON Schema copies (synced from specification/)
specification/            # Canonical spec format specification
  schemas/                # JSON Schema files (source of truth for validation)
skills/                   # Agent skills (Claude Code SKILL.md files: af-spec, af-prd)
docs/                     # Documentation and ADRs
dist/                     # Pre-built binaries
testdata/                 # Shared test fixtures
```

The Go module lives in `golang/` under module path
`github.com/agent-fox-dev/spec`.

## Setup

```bash
# Clone the repo
git clone <repo-url> && cd spec-format

# Fetch Go dependencies
cd golang && go mod download
```

## Go package structure

The codebase is organized into three packages:

- **afspec** (`golang/`) -- core library. Provides spec loading, validation
  (schema and cross-file), lifecycle state machines, EARS criterion builders,
  dependency graphs, coverage analysis, rendering, and discovery. Types are
  value-oriented -- mutation methods return new copies. No goroutine-safety
  guarantees; callers must synchronize externally.

- **agentspec** (`golang/agentspec/`) -- AI session layer. SpecAgent pipeline
  (AssessPRD, RefinePRD, GenerateArtifacts), TOML configuration and model
  registry, campaign directory lifecycle, session state machine with atomic
  persistence, prompt templates, and tool schema definitions for Claude API
  integration.

- **cmd/spec-cli** (`golang/cmd/spec-cli/`) -- CLI binary. Built with cobra.
  Wires afspec and agentspec together behind user-facing commands: `new`,
  `list`, `refine`, `generate`, `validate`, `lint`, `render`, `status`,
  `campaign`.

## Common tasks

All tasks are driven through `make`. Run from the repository root.

| Command                  | Description                                                    |
|--------------------------|----------------------------------------------------------------|
| `make check`             | Run lint and all tests (run this before committing)            |
| `make test`              | Run all Go tests: `go test ./... -count=1`                     |
| `make lint`              | Lint Go source: `gofmt` + `go vet`                            |
| `make format`            | Auto-format Go source: `gofmt -w`                             |
| `make build`             | Cross-compile binaries (darwin-arm64, linux-arm64, linux-amd64)|
| `make clean`             | Remove build artifacts                                         |
| `make json-gen`          | Copy schemas and regenerate Go types (see below)               |
| `make install-skills`    | Install agent skills from `skills/` to `~/.claude/skills/`     |
| `make uninstall-skills`  | Remove installed agent skills                                  |

## Running tests

```bash
cd golang && go test ./... -count=1
```

## Linting

```bash
cd golang && gofmt -l .       # List unformatted files
cd golang && go vet ./...     # Static analysis
cd golang && gofmt -w .       # Auto-format
```

## Schema workflow

The four JSON schemas in `specification/schemas/` are the single source of truth:

- `prd-frontmatter.v1.json`
- `requirements.v1.json`
- `tasks.v1.json`
- `test_spec.v1.json`

After modifying any schema, run:

```bash
make json-gen
```

This copies the schemas into `golang/schemas/` and regenerates Go struct types
from the schemas using
[go-jsonschema](https://github.com/atombender/go-jsonschema). The generated
files are `golang/*.v1.go`.

Always run `make check` after regenerating to verify nothing broke.

## Git workflow

- Branch from `main` using `feature/<descriptive-name>`.
- Never commit directly to `main`.
- Use conventional commit messages: `feat:`, `fix:`, `refactor:`, `docs:`,
  `test:`, `chore:`.
- Feature branches are local-only. Only `main` is pushed to the remote.
- Run `make check` before every commit.
