# Render the skills for every agent from one source

- **Status:** accepted
- **Date:** 2026-11-06
- **Spec:** `14_render_skills_from_one_source`

## Context

`skills/` held three files with no extension — `af-prd`, `af-spec` and
`af-impl` — installed into `~/.claude/skills/<name>/SKILL.md` by
`make install-skills`. They reached one agent, Claude Code, and their text used
Claude Code's argument placeholder (`$ARGUMENTS`) and its slash syntax for
invoking another skill. `af-spec` was 40,317 bytes and `af-impl` 34,702, all of
it loaded for every step of a run.

Codex, Cursor and GitHub Copilot load skills from the same layout — a directory
per skill holding a `SKILL.md` — so one source can reach all four with a render
per agent. `docs/spec-kit-comparison.md` §4.11 records the 24 KB cap EGM's
harnesses put on a skill body, and Claude Code's own guidance is to keep
`SKILL.md` short and move reference material into separate files.

## Decision

One source tree under `skills/`, rendered per agent by a program `make` runs.

1. **The Agent Skills layout is the source**, and there is no format per agent.
   A skill is `skills/<name>/SKILL.md` with its own `references/` beside it;
   files more than one skill links are in `skills/_shared/references/`; a
   directory whose name begins with `_` is not a skill. Command and prompt
   files — Cursor commands, Copilot prompt files, Codex custom prompts — would
   be a second copy of each skill to keep in step.
2. **A Markdown link is the inclusion.** A skill links `references/<file>.md`
   and that link is the record; a file is rendered when the skill links it or
   when an included file links it. A manifest would be one more thing to keep
   in step, and with the link as the record a broken link is caught by the same
   check. A name present in both a skill's own directory and the shared pool is
   an **error**, not an override, so one skill cannot drift from its siblings
   without a word.
3. **`{{invoke <name>}}` is the only token the render replaces.** The
   invocation of another skill is the one text that differs by agent. The
   argument is named in words, which every agent reads the same.
4. **The ceiling is 24,576 bytes, on the body only, per agent**, measured in
   bytes of UTF-8 after the line that closes the frontmatter and after the
   token is replaced. The body loads with the skill at every step; a reference
   file loads only at the step that links it, and has no limit. Bytes, not
   lines, because bytes are what load.
5. **The checks run in `make check`, not as Go tests of the skills.**
   `.specs/steering.md` forbids tests of a document's content. The render's own
   behaviour is what the Go tests cover; `make check` runs `skills-check` after
   `test`, and it reaches no network and reads no credential.
6. **The render lives in `internal/skillgen`**, a `package main` that `make`
   runs with `go run ./internal/skillgen`. It is not a command: `cmd/` is what
   `make build` installs from, and the repository's public surface does not
   grow. It imports only the standard library and `github.com/goccy/go-yaml`,
   so no render target depends on `update-agentkit`.
7. **The frontmatter is filtered line by line** on its top-level keys, each
   kept key keeping its own text, its quoting and its continuation lines.
   Re-marshalling a parsed mapping would lose the source's key order, which the
   render must keep.
8. **The rendered skills are not committed.** Users of the other agents still
   need `speca` and `specr`, which need Go and a checkout until a release
   exists. Archives per agent come with the release.
9. **An install replaces a skill's directory whole**, so nothing stale is left,
   and it touches no other entry under the directory. An uninstall removes only
   the source tree's own skill names.
10. **A skill an agent would see twice is a warning, not a refusal.** A person
    may use two agents on one machine.

## The agent tables

### Frontmatter and invocation

| Agent | Frontmatter keys kept | `{{invoke af-impl}}` becomes | Confirmed |
|---|---|---|---|
| `claude` | `name`, `description`, `argument-hint`, `disable-model-invocation` | `/af-impl` | 2026-11-06, <https://code.claude.com/docs/en/skills> |
| `codex` | `name`, `description` | `$af-impl` | **not confirmed at the vendor** — <https://developers.openai.com/codex/skills> was not reachable; taken from third-party copies of the page |
| `cursor` | `name`, `description`, `disable-model-invocation` | `/af-impl` | **not confirmed at the vendor** — <https://cursor.com/docs/skills> was not reachable; taken from search results quoting it |
| `copilot` | `name`, `description` | `/af-impl` | **not confirmed at the vendor** — the Microsoft Learn and GitHub Docs pages were not reachable; taken from search results quoting them |
| `pi` | `name`, `description`, `disable-model-invocation` | `/skill:af-impl` | 2026-11-13, <https://pi.dev/docs/latest/skills> |
| `opencode` | `name`, `description`, `disable-model-invocation` | `@af-impl` — a skill is named in the prompt, not run as a slash command | 2026-11-13, <https://opencode.ai/docs/skills/> |

Claude Code ignores a frontmatter key it does not recognise without reporting
an error, so a key kept for one agent would do no harm in another; the keys are
filtered anyway, so an installed skill carries nothing its agent would ignore.

### Install directories

| Agent | User directory | Project directory | Also loads | Confirmed |
|---|---|---|---|---|
| `claude` | `~/.claude/skills` | `<PROJECT>/.claude/skills` | — | 2026-11-06, vendor documentation |
| `codex` | `$CODEX_HOME/skills`, else `~/.codex/skills` | `<PROJECT>/.agents/skills` | — | **not confirmed**; whether a repository's `.agents/skills/` is read was **NOT FOUND** in any source read |
| `cursor` | `~/.cursor/skills` | `<PROJECT>/.cursor/skills` | `.agents/skills`, `.claude/skills`, `.codex/skills` in a repository | **not confirmed** |
| `copilot` | `~/.copilot/skills` | `<PROJECT>/.github/skills` | `~/.claude/skills`, `~/.agents/skills` for a user; `.claude/skills`, `.agents/skills` in a repository | **not confirmed** |
| `pi` | `$PI_CODING_AGENT_DIR/skills`, else `~/.pi/agent/skills` | `<PROJECT>/.pi/skills` | `~/.agents/skills` for a user; `.agents/skills` in a repository | 2026-11-13, vendor documentation; `PI_CODING_AGENT_DIR` from <https://pi.dev/docs/latest/configuration> |
| `opencode` | `~/.config/opencode/skills` | `<PROJECT>/.opencode/skills` | `~/.claude/skills`, `~/.agents/skills` for a user; `.claude/skills`, `.agents/skills` in a repository | 2026-11-13, vendor documentation; whether `XDG_CONFIG_HOME` moves the user directory was **NOT FOUND** |

`SKILLS_DIR` overrides every row, which is the way out when one of them turns
out to be wrong.

## Consequences

- One edit to a skill reaches four agents, and `make check` fails on a body
  over the ceiling, a link that resolves nowhere, a name that does not match
  its directory, an invocation of a skill that does not exist, `$ARGUMENTS`, or
  a skill named in one agent's own syntax.
- A step now reads a named reference file before acting. What the skills do —
  their method, gates, refusals, commands and reports — does not change.
- `af-impl` carries `disable-model-invocation: true`, so in Claude Code and
  Cursor it runs only when a person starts it: it pushes branches and opens
  pull requests and issues. Codex and Copilot have no verified equivalent, so
  there it stays invocable by the model. `af-prd` and `af-spec` stay invocable
  everywhere; they write files in the working tree and nothing beyond it.
  **This changes how `af-impl` starts in Claude Code.**
- Four of the rows above were not confirmed at their vendor, because the pages
  were not reachable from where this was written. Each is marked, and
  `SKILLS_DIR` and the frontmatter filter make a wrong row cheap to correct.
- Adding an agent is a row in both tables and nothing else. pi and opencode
  were added on 2026-11-13 that way. pi's prefix, `/skill:`, is the first
  longer than one byte, so a body with invocations in it can now be under the
  ceiling for one agent and over it for another, which the per-agent measure
  of decision 4 already reports. The check for a skill named in one agent's
  own syntax gained `@<name>` and `/skill:<name>`.
