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
phase (it has no environment variable). The phases are `prd`, `prd_review`,
`prd_revision`, `requirements`, `tasks`, `plan_review`, `plan_revision` and
`architecture`. The context window, output cap and
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

### The steering

`steering.md` may open with YAML frontmatter that gives it a version and a
dated record of its amendments, newest first:

```yaml
---
version: 2.0.1
amendments:
  - version: 2.0.1
    date: 2026-11-06
    change: The file carries a version and this record.
  - version: 2.0.0
    date: 2026-10-25
    change: Tests are organised by behaviour.
---
```

The frontmatter is well formed when:

- it holds only the keys `version` and `amendments`;
- `version` is `MAJOR.MINOR.PATCH`: three non-negative integers with no leading
  zeros, compared as numbers (`2.10.0` is newer than `2.9.0`);
- `amendments` lists at least one amendment, each with a `version` in that
  form, a `date` in `YYYY-MM-DD` and a non-empty `change`;
- the amendments' versions strictly decrease and their dates do not increase;
- the first amendment's version is `version`.

A file that meets them all, and whose body says something, is **versioned**:
every phase is given the body (never the frontmatter), with the line
`Steering version <version>` before it, and every package `spec` writes records
the version in its PRD frontmatter as `steering_version`. The amendment record
is never sent to a model; `speca --validate` reads it, and names the amendments
a draft or active package was written before ([speca.md](speca.md)). A file
with no frontmatter is unversioned and behaves as it always has. A frontmatter
that breaks a rule, or never closes, raises one high `steering_malformed`
warning on `spec` and on `speca --validate`; the body is used all the same and
the version is treated as absent. The size bound, the placeholder and the bare
`# Steering` heading are checked on the body alone, and a body that says
nothing makes the file nothing, whatever its frontmatter holds.

The numbers say what an amendment did, for the person who writes it; no machine
checks that the right one was bumped:

| Bump | When |
|---|---|
| MAJOR | a directive was removed, or changed so that a package or code that followed it may now break it |
| MINOR | a directive was added |
| PATCH | a wording that changes no directive |

## The gate's state directory

`speca --gate` keeps what it records outside every repository: each command's
log, the passes it reuses, and the run log `speca --gate-report` reads
([speca.md](speca.md#the-state-directory)).

| Variable | Effect |
|---|---|
| `XDG_STATE_HOME` | when set to an absolute path, the state directory is `$XDG_STATE_HOME/agent-fox/speca`; a relative value is ignored |
| `HOME` | otherwise, the state directory is `~/.local/state/agent-fox/speca`, on Linux and macOS alike |

Each repository has a directory of its own under it, `<name>-<hash>`, the base
name and a hash of the path of its git top level. Nothing there is pruned:
deleting the directory, or any file in it, loses only the logs, the reuse of a
pass and the runs the gate report counts. `--clean-env` sets `HOME`,
`GIT_CONFIG_GLOBAL`, `GIT_CONFIG_SYSTEM`, `GIT_CONFIG_NOSYSTEM` and
`GIT_TERMINAL_PROMPT` for the gate's commands, and only for them.

## specrun

`specrun` takes every setting from a flag, whose default an environment
variable sets ([specrun.md](specrun.md#configuration)):

| Variable | Flag | Default |
|---|---|---|
| `SPECRUN_MAX` | `--max` | no limit |
| `SPECRUN_STALL` | `--stall` | `30m` |
| `SPECRUN_LOG_DIR` | `--log-dir` | `agent-fox/specrun/<repository name>` under the cache directory |
| `SPECRUN_RECONCILE_EVERY` | `--reconcile-every` | `5` |
| `SPECRUN_STOP_ON` | `--stop-on` | `security,data-loss` |
| `SPECRUN_STRICT_SKILLS` | `--strict-skills` | off |

It also reads:

| Variable | Effect |
|---|---|
| `XDG_STATE_HOME` | the state file is `$XDG_STATE_HOME/agent-fox/specrun/<repository name>.json`, else under `~/.local/state` |
| `XDG_CACHE_HOME` | the default log directory is under it, else under the platform's cache directory (`~/Library/Caches`, `~/.cache`) |
| `HOME` | the user's af-impl skill, `~/.claude/skills/af-impl/SKILL.md`, unless the repository has its own `.claude/skills/af-impl/SKILL.md` |
| `PATH` | where `claude`, `git` and `issue` are found |
| `GITHUB_TOKEN`, `GH_TOKEN` | the token of a GitHub origin, which `specrun` and the sessions' `issue` calls use; without one for the origin's forge a run refuses to start |
| `GITLAB_TOKEN` | the same for a GitLab origin |
| `GITHUB_API_URL`, `GITLAB_API_URL` | the API of a self-hosted forge, as `aflib/issuex` reads them |

It sets `CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS` for each session to the stall
time in milliseconds.

## The build

The build reads no configuration. `go.mod` pins a released agentkit-go and
carries no `replace`, so every target builds the committed pin and no target
needs the network ([ADR 13](adr/13-pin-a-released-agentkit-go.md)).

`make update-agentkit` moves the pin to agentkit-go's latest release. It is
explicit: nothing runs it for you, and the move is committed as its own
change. `github.com/agent-fox-dev/agentkit-go/codesearch` has no tags, so it
stays on the pseudo-version in `go.mod` and is moved by hand.

`AGENTKIT_UPDATE` is gone, with the per-target dependency it controlled.

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
| One command of `speca --gate` | 20 minutes (`--timeout`), then it is ended with every process it started |
| A failing gate command's output in the verdict | its last 40 lines (`tail`); the whole output is in its log |
| A full gate's slowest units | 5 |
