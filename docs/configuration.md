# Configuration

The `spec` command has no configuration file. It is configured by its flags
([cli.md](cli.md)) and by the environment variables below. A flag always wins
over the variable that supplies its default.

## The model

| Variable | Effect |
|---|---|
| `AF_MODEL` | The model when `--model` is not given: a tier (`SIMPLE`, `STANDARD`, `ADVANCED`) or a catalog model id. |
| `AGENTKIT_MODEL` | Read when `AF_MODEL` is unset. |
| `AF_MODEL_EFFORT` | The effort of every phase when `--effort` is not given: `off`, `low`, `medium`, `high`, `xhigh` or `max`. An invalid value is a usage error. |

With neither set the model is the `STANDARD` tier. A tier chooses a model and
an effort per phase, and `--phase-model PHASE=MODEL[:EFFORT]` changes one
phase (it has no environment variable). The context window, output cap and
price of every model id come from AgentKit's embedded catalog; see
[cli.md](cli.md#the-model-and-the-ceilings).

## The model credential

`spec` reaches Claude through AgentKit's `anthropic.Resolve`, which reads the
environment once, before any phase runs, and makes no network request. A run
with no credential stops at stage `preflight` with category `auth`.

| Variable | Effect |
|---|---|
| `ANTHROPIC_API_KEY` | An Anthropic API key, for the direct deployment. Checked first. |
| `ANTHROPIC_AUTH_TOKEN` | A bearer token for the direct deployment; on Vertex AI, a Google access token. |
| `ANTHROPIC_OAUTH_TOKEN` | An OAuth bearer for the direct deployment. |
| `ANTHROPIC_BASE_URL` | A proxy or gateway in front of the Anthropic API. |
| `CLAUDE_CODE_USE_VERTEX` | `1` or `true` selects Claude on Vertex AI. |
| `ANTHROPIC_VERTEX_PROJECT_ID`, then `GOOGLE_CLOUD_PROJECT` | The Vertex project; required once Vertex is selected. |
| `CLOUD_ML_REGION`, then `GOOGLE_CLOUD_LOCATION`, `CLOUDSDK_COMPUTE_REGION` | The Vertex location; default `global`. |
| `ANTHROPIC_VERTEX_BASE_URL` | A proxy in front of Vertex. |
| `CLAUDE_CODE_USE_BEDROCK` | `1` or `true` selects Claude on Amazon Bedrock, when Vertex is not selected. |
| `AWS_REGION`, then `AWS_DEFAULT_REGION` | The Bedrock region; default `us-east-1`. |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, `AWS_BEARER_TOKEN_BEDROCK` | Bedrock credentials; without them, the AWS SDK's own chain. |

A request that fails before its response begins is retried by the Anthropic
SDK (two retries). A call that fails once its response is streaming — a
dropped connection, a truncated body, an overloaded or rate-limited API — is
sent again by `spec` itself, after 2, 8 and 30 seconds; the failed attempts'
usage is counted. A credential, a refusal or an invalid request is not
repeated.

## The forge

Read when the input is an issue URL, and for `--comment`.

| Variable | Effect |
|---|---|
| `GITHUB_TOKEN`, then `GH_TOKEN` | The GitHub token. A public issue is read without one; a private one, and `--comment`, need it. |
| `GITHUB_API_URL` | The API of a GitHub Enterprise Server. |
| `GITLAB_TOKEN` | The GitLab token. |
| `GITLAB_API_URL` | The API of a self-hosted GitLab, which also makes its host recognised as a GitLab one. |

The forge is decided from the issue URL's host (`--comment` needs an issue
input, so it posts to the same forge). A host that names neither GitHub nor
GitLab is refused before any request is sent, so a token never leaves for a
host it does not belong to.

## The spec root

| Variable | Effect |
|---|---|
| `AF_SPEC_DIR` | Where `NN_name` packages live when `--specs-dir` is not given; relative to `--dir`. Default `.specs`. |

The spec root's `steering.md`, when it exists and says something, is given to
every phase as the project's directives.

## The build

| Make variable | Default | Effect |
|---|---|---|
| `AGENTKIT_UPDATE` | `1` | the targets that compile (`build`, `test`, `test-fast`, `coverage`, `vet`, `lint`, `doc`) first run `make update-agentkit`, which moves the agentkit-go pin in `go.mod` to the latest `main` ([ADR 06](adr/06-build-against-the-latest-agentkit-go.md)); `0` builds the committed pin, e.g. offline |

## The skills

`make skills`, `make skills-check`, `make install-skills` and
`make uninstall-skills` read these. None of them makes a network call or reads
a credential.

| Make variable | Default | Effect |
|---|---|---|
| `AGENT` | `claude` | the agent to render and install for: `claude`, `codex`, `cursor` or `copilot`. Any other value is refused, and nothing is written |
| `PROJECT` | *(unset)* | install into this repository's own skill directory instead of the user's. A path that is not an existing directory is refused, and nothing is written |
| `SKILLS_DIR` | *(unset)* | install into this directory, whatever the agent. It overrides `AGENT`'s directory and `PROJECT` |
| `SKILLS_SRC` | `skills` | the source tree to read |
| `SKILLS_OUT` | `build/skills` | where `make skills` writes. It is replaced whole on every run, so `.`, `/` and `SKILLS_SRC` itself are refused |

The directory each agent loads from, which `AGENT` and `PROJECT` select
together:

| Agent | User directory | Project directory |
|---|---|---|
| `claude` | `~/.claude/skills` | `<PROJECT>/.claude/skills` |
| `codex` | `$CODEX_HOME/skills`, else `~/.codex/skills` | `<PROJECT>/.agents/skills` |
| `cursor` | `~/.cursor/skills` | `<PROJECT>/.cursor/skills` |
| `copilot` | `~/.copilot/skills` | `<PROJECT>/.github/skills` |

| Environment variable | Effect |
|---|---|
| `CODEX_HOME` | with `AGENT=codex` and no `PROJECT`, the user install goes to `$CODEX_HOME/skills` instead of `~/.codex/skills`. It does not reach a project install, which is the repository's directory and not the user's |

Each row of both tables is recorded, with the date it was confirmed against
that vendor's documentation, in
[ADR 12](adr/12-render-the-skills-for-every-agent-from-one-source.md).

## Limits

| Limit | Value |
|---|---|
| Input (with `--context`) | 256 KB, cut at a line boundary |
| Issue comments read | 5 pages of 100 |
| Per-phase turns | 60 (`--max-turns`) |
| Per-phase spend | $5.00 (`--budget`) |
| Output tokens of one model response, thinking included | 65,536 (`--max-tokens`), and never more than the model's own output ceiling |
| Repository map | 6000 tokens (`--repo-map-tokens`) |
| Codebase notes a PRD phase hands on | 12000 bytes, cut at a line boundary |
| Earlier scopes' notes shown to a later scope's PRD phase | 16000 bytes, nearest scope first |
| Warnings sent back for review | 25 listed, once per phase |
| Old tool results pruned from the request | once it reaches 35% of the model's context window; the last 4 turns are kept whole |
| `steering.md`, `AGENTS.md`, `CLAUDE.md` in a prompt | 24 KB each; larger files are left out |
