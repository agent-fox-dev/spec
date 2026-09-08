# spec

CLI for AI-powered spec creation. Drives a multi-step workflow that turns a
plain-language PRD into a complete, validated specification (requirements,
test contracts, tasks, and architecture). Output is JSON on stdout; progress and
errors go to stderr, making the tool suitable for both human and agent use.

## Installation

```bash
pip install "spec @ git+https://github.com/agent-fox-dev/spec.git@v1.4.1#subdirectory=packages/spec"
```

## Quick Start

```bash
# 1. Create a new spec from a PRD file (auto-initialises .specs/ if needed)
spec new docs/my-feature.md

# 2. Refine the PRD through iterative AI review
spec refine 01_my_feature

# 3. Generate JSON artifacts (requirements, tests, tasks)
spec generate 01_my_feature

# List all specs and their states
spec list
```

## Commands

| Command | Description |
|---------|-------------|
| `spec new SPEC_PATH [--name TEXT]` | Create a new spec from a PRD file (auto-initialises campaign) |
| `spec list` | List all specs with session states (JSON output) |
| `spec refine SPEC [--answers TEXT] [--force]` | Assess PRD, submit answers, refine iteratively |
| `spec generate SPEC [--force]` | Generate JSON artifacts from accepted PRD |
| `spec validate [SPEC] [--cross]` | Run schema and cross-file validation checks |
| `spec lint [--all]` | Lint all specs for validation errors |
| `spec render SPEC [--combined] [--json]` | Render spec as markdown (or JSON with `--json`) |
| `spec status SPEC` | Query session state (read-only) |
| `spec campaign --path P --name N [--description D]` | Create a campaign at a non-default location |

## Global Options

| Option | Description |
|--------|-------------|
| `-d, --spec-dir PATH` | Override spec directory (default: `.specs`) |
| `-s, --source PATH` | Source code directory for AI context during spec creation (default: `.`) |
| `-q, --quiet` | Suppress progress output on stderr |
| `--version` | Show version and exit |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `ANTHROPIC_API_KEY` | API key for Anthropic (required for `new`, `refine`, `generate`) |
| `AF_SPEC_MODEL` | Override the default model used for AI calls |
| `AF_AGENT` | Set to `1` to force agent mode: suppresses the banner, forces quiet output, and wraps errors as JSON |
| `SPEC_DIR` | Override the default spec root directory (`.specs`). `--spec-dir` flag takes precedence. |

## Requirements

- Python 3.12+
- [afspec](../afspec) -- spec format library
- [agentspec](../agentspec) -- AI-powered spec creation library
- click 8.1+, rich 13.0+, pydantic 2.13+
