---
spec_id: "14"
spec_name: "render_skills_from_one_source"
title: "Render the skills for every agent from one source"
status: "active"
created_at: "2026-11-06T08:14:00Z"
updated_at: "2026-11-06T08:54:33Z"
intent_hash: "0fa8296fc069cac263cae0437e0a2188564e76c83633c22bd3f7c4b8b5602fcd"
schema_version: 2
source: "docs/prd/03-render-the-skills-for-every-agent-from-one-source.md"
---

# Render the skills for every agent from one source

## Intent

`af-prd`, `af-spec` and `af-impl` put `spec`, `specr` and `speca` to work in
a person's own session, but they reach one agent and load more text than a
step needs. They are Claude Code files, installed into Claude Code's
directory, and their text uses Claude Code's argument placeholder and slash
syntax. `af-spec` is 40 KB and `af-impl` 35 KB, all of it in context for
every step of a run. Codex, Cursor and GitHub Copilot load skills in the same
directory layout, so the method can reach them from one source with a render
per agent. Each skill becomes a body that carries the order of the run, its
gates and its refusals, plus reference files the agent reads at the step they
serve. One source tree renders and installs the three skills for each agent,
and refuses a skill whose body would load more than 24 KB at once. What the
skills do does not change; where it is written does.

## Goals

- `make install-skills AGENT=<agent>` installs the three skills for `claude`,
  `codex`, `cursor` or `copilot`, each into that agent's user skill
  directory. `make install-skills` with no `AGENT` does what it does today:
  installs for Claude Code into `~/.claude/skills`.
- An installed skill works in its agent with no edit. Its frontmatter carries
  only the keys that agent reads. Every invocation of another skill in its
  text is written in that agent's syntax. Every reference file it links is in
  its own directory.
- The `SKILL.md` body of every rendered skill, for every agent, is at most
  24,576 bytes.
- `make check` fails on a skill that breaks a render rule, and names the
  file and the rule. The rules: a body over the limit, a link to a reference
  file that does not exist, a name that does not match its directory, an
  invocation of a skill that does not exist.
- A run of each skill follows the same steps, takes the same gates and stops
  for the same reasons as before the change. The one new action is reading a
  named reference file at the step it serves.
- `af-spec`, when it writes the PRD itself, writes it to the same shape as
  `af-prd`, read from the same file, without `af-prd` being loaded.
- `make install-skills AGENT=<agent> PROJECT=<path>` installs into that
  repository's project skill directory, so a team can commit the skills for
  an agent that reads only repository skills, such as Copilot's cloud agent.

## Non-goals

- Running `spec` on another vendor's model. The generator stays Claude-only,
  as `docs/spec-kit-comparison.md` §7.1 says.
- Changing what any skill does: its method, its gates, its refusals, the
  commands it calls, its report. A change the split shows to be needed is a
  change of its own.
- Agents other than these four, Gemini CLI among them. Adding one is a row in
  the agent table and a later change.
- Command and prompt files: Cursor commands, Copilot prompt files, Codex
  custom prompts. All four agents load skills directly, and a wrapper would
  be a second copy to keep in step.
- Installing without a checkout: a release archive per agent, a plugin or
  marketplace entry, the skills embedded in `speca`. These belong with
  tagging a release (§7.1 item 6 of the comparison).
- Committing the rendered skills to the repository.
- A template language. The render substitutes one token, filters the
  frontmatter and copies the linked files; no other text is rewritten.
- Measuring whether the smaller skills produce better packages or better
  code (§7.3 of the comparison). This change shows that they behave the same.
- The `af-impl` block quoted in `docs/specr.md` and `docs/speca.md`. It is an
  example of a skill's step, not a copy of `af-impl`, and stays as it is.
- `docs/spec-kit-comparison.md` itself, which is an analysis dated
  2026-10-28 and stays as written.
- Install paths on Windows, which are best effort as the steering file says.

## Background

`skills/` holds three files with no extension: `af-prd` (17,504 bytes, 330
lines), `af-spec` (40,317 bytes, 746 lines) and `af-impl` (34,702 bytes, 594
lines). Each opens with frontmatter carrying `name`, `description` and
`argument-hint`. `make install-skills` copies each one to
`~/.claude/skills/<name>/SKILL.md`, and `make uninstall-skills` removes
`~/.claude/skills/<name>` for each name. No other directory and no other
agent is served. `make clean` removes `build/` and `coverage.txt`.

The text the skills hold for Claude Code alone is small:

- `$ARGUMENTS`, five times: three in `af-impl`, one in `af-prd`, one in
  `af-spec`;
- three slash invocations of another skill: `/af-prd` in `af-spec` §1, and
  `/af-impl` in the next steps of `af-spec` §12 and `af-prd` §8;
- the `argument-hint` key in each frontmatter.

The rest is already neutral. The skills name `AGENTS.md` beside `CLAUDE.md`
and say "if you have a task-list tool".

The comparison recommends moving "the JSON shapes, the field tables" into
files read on demand so that each body fits under 24 KB. Measured, that is
not enough. In `af-spec` the code blocks and tables are 7.3 KB, and moving
them alone leaves about 33 KB. In `af-impl` they are 0.5 KB. Its bulk is
procedure for one phase at a time:

- reading `speca`'s and `specr`'s output (§1);
- writing tests first (§5, step 4);
- the conformance pass (§6);
- the pull request and the issues for its open items (§7).

The split therefore goes by phase.

Several things are written more than once across the skills:

- how to read `speca`'s and `specr`'s JSON;
- their exit codes;
- sending `--markdown-file` and stdout to files outside the repository;
- what to do when the commands are not installed;
- the `make build` line;
- the vague words the validator flags;
- the size limits of one package.

`af-spec` §1 tells the agent, when no PRD exists and the person chooses this
run to write one, to write it "to `af-prd`'s shape". That shape is defined
only in `af-prd` §4. Installed or loaded alone, `af-spec` carries no
definition of it.

The other three agents read the same layout, a directory per skill holding a
`SKILL.md`, from their own directories, and two of them also read Claude
Code's:

- **Cursor** reads `.cursor/skills/` and `~/.cursor/skills/`. For
  compatibility it also reads `.agents/skills/`, `.claude/skills/` and
  `.codex/skills/`.
- **Copilot** reads `.github/skills/`, `.claude/skills/` and
  `.agents/skills/` in a repository, and `~/.copilot/skills/`,
  `~/.claude/skills/` and `~/.agents/skills/` for a user.
- **Codex** reads `$CODEX_HOME/skills/`, by default `~/.codex/skills/`, and a
  skill is invoked as `$name`.

None of this could be read at its source from where this PRD was written
(see *Verified External API*). One consequence holds whatever the final
paths are: a Claude Code install in `~/.claude/skills` already reaches
Copilot, and installing for Copilot as well loads each skill twice there.

The 24 KB figure is the cap EGM's harnesses put on a skill body (§4.11 of the
comparison). Claude Code's own guidance is to keep `SKILL.md` under 500 lines
and to move detailed reference material into separate files.

## Requirements

### The source

- Each skill is a directory `skills/<name>/` holding `SKILL.md`, which is the
  frontmatter and the body. Its own reference files are in
  `skills/<name>/references/`. Reference files linked by more than one skill
  are in `skills/_shared/references/`. A directory under `skills/` whose name
  starts with `_` is not a skill.
- A skill links a reference file with a Markdown link whose target is
  `references/<file>.md`. The link is the inclusion. A file is rendered into
  a skill when that skill's `SKILL.md` links it, or when a reference file
  already included links it. A link resolves in the skill's own
  `references/` first, then in `skills/_shared/references/`.
- `{{invoke <name>}}`, in a body or a reference file, stands for an
  invocation of the skill `<name>`. It is the only token the render replaces.
- The skill text names its argument in words ("the argument the skill was
  given"), not with a placeholder.

### Rendering

- `make skills` renders every skill for every agent into
  `build/skills/<agent>/<name>/`: its `SKILL.md`, and a `references/`
  directory holding each file the skill links, directly or through another
  reference file. A file nothing links is not rendered. `make clean` removes
  the output with the rest of `build/`.
- Rendering makes no model call and no network call, and needs no
  credential. It does not run `update-agentkit`.
- For each agent, the frontmatter keeps only the keys in this table, in the
  source's order. The body and the reference files are unchanged except for
  the token.

  | Agent | Frontmatter keys kept | `{{invoke af-impl}}` becomes |
  |---|---|---|
  | `claude` | `name`, `description`, `argument-hint`, `disable-model-invocation` | `/af-impl` |
  | `codex` | `name`, `description` | `$af-impl` |
  | `cursor` | `name`, `description`, `disable-model-invocation` | `/af-impl` |
  | `copilot` | `name`, `description` | `/af-impl` |

- The same source renders to byte-identical output on every run.
- A render replaces the previous output of `make skills` whole, so a file
  removed from the source is not left in `build/skills/`.

### What the render refuses

The render reports every problem it finds in one run, not the first only.
Each problem is one line on stderr, `<source path>: <problem>`. On any
problem it exits non-zero and writes nothing, for any agent. These are the
problems:

- a skill directory with no `SKILL.md`, or a `SKILL.md` with no frontmatter
  or with frontmatter that is not YAML;
- a `name` that is missing, that differs from the directory name, or that
  breaks the Agent Skills rule: 1 to 64 characters, lowercase letters,
  digits and single hyphens, neither first nor last character a hyphen;
- a `description` that is missing, empty, or longer than 1,024 characters;
- a link to `references/<file>.md` that resolves in neither directory, for
  example
  `skills/af-spec/SKILL.md: links references/tasks.md, which is in neither skills/af-spec/references/ nor skills/_shared/references/`;
- a reference file name present in both the skill's own `references/` and
  `skills/_shared/references/`;
- `{{invoke <name>}}` naming no skill under `skills/`, and any other `{{…}}`
  token;
- a rendered body longer than 24,576 bytes for any agent, measured in bytes
  of UTF-8 after the line that closes the frontmatter and after the token is
  replaced, for example
  `skills/af-spec/SKILL.md: body is 31,204 bytes for codex, 6,628 over the 24,576-byte limit`.
  A body of exactly 24,576 bytes passes. Reference files have no limit.

Two findings are warnings, which go to stderr and leave the exit code 0:

- a file in a skill's own `references/` that nothing links:
  `skills/af-impl/references/old.md: linked by nothing; not rendered`;
- a file in `skills/_shared/references/` that no skill links.

### Installing and uninstalling

- `make install-skills [AGENT=<agent>] [PROJECT=<path>] [SKILLS_DIR=<dir>]`
  renders the skills for the agent and copies each one to `<dir>/<name>/`.
  `AGENT` defaults to `claude`. The directory is the agent's user directory,
  or its project directory under `PROJECT` when `PROJECT` is given.
  `SKILLS_DIR` overrides both.

  | Agent | User directory | Project directory |
  |---|---|---|
  | `claude` | `~/.claude/skills` | `<PROJECT>/.claude/skills` |
  | `codex` | `$CODEX_HOME/skills`, else `~/.codex/skills` | `<PROJECT>/.agents/skills` |
  | `cursor` | `~/.cursor/skills` | `<PROJECT>/.cursor/skills` |
  | `copilot` | `~/.copilot/skills` | `<PROJECT>/.github/skills` |

- It prints `installed: <name> -> <dir>/<name>` for each skill, and exits 0.
- It replaces `<dir>/<name>/` whole, so a reference file removed from the
  source is not left installed. It changes no other directory under `<dir>`.
- On a render problem it installs nothing, prints the problems as `make
  skills` does, and exits non-zero.
- An `AGENT` outside the four exits non-zero with
  `unknown agent "<value>"; one of: claude, codex, cursor, copilot`, and
  writes nothing.
- A `PROJECT` that is not an existing directory exits non-zero with
  `PROJECT <path> is not a directory`, and writes nothing.
- When the agent also reads a directory of another agent (see the table in
  *Background*) and that directory already holds a skill of the same name,
  the install prints
  `warning: <agent> also loads <path>/<name>; it will see <name> twice` and
  installs all the same.
- `make uninstall-skills [AGENT] [PROJECT] [SKILLS_DIR]` resolves the same
  directory and removes `<dir>/<name>` for each of the three skill names,
  printing `removed: <name>`. A name that is not installed is skipped without
  a message. Nothing else under `<dir>` is touched.

### Agent-neutral text

- No rendered skill, for any agent, contains `$ARGUMENTS`, and none names
  another skill in another agent's invocation syntax.
- In Claude Code the argument still reaches the skill. When no placeholder
  is present, Claude Code appends `ARGUMENTS: <input>` to the skill's
  content.
- `af-impl` carries `disable-model-invocation: true`. In Claude Code and
  Cursor it then runs only when the person invokes it. `af-prd` and
  `af-spec` stay invocable by the model.

### The skills' content

- Each body keeps:
  - the paragraph of rules that opens it;
  - the run's order and its list of reasons to stop;
  - every numbered step, with the commands that gate it;
  - *Never*;
  - the report.

  A rule that stops the run, a gate or an item of *Never* is never only in a
  reference file.
- Each step whose detail moved opens with an instruction to read the file
  before acting: "Read [references/<file>.md](references/<file>.md) in full
  before …".
- The detail moves to these files:

  | File | Holds | Linked by |
  |---|---|---|
  | `_shared/references/prd-shape.md` | the PRD's sections in order and what each holds (`af-prd` §4); the vague words and the size of one package (`af-prd` §6) | `af-prd` §4 and §6; `af-spec` §1 and §5 |
  | `_shared/references/speca-specr.md` | the result fields of `speca` and `specr`, their exit codes 0, 1 and 2, writing `--markdown-file` and stdout to files outside the repository, the install line, what to do when the commands are not installed | all three |
  | `af-spec/references/prd-frontmatter.md` | §5: the frontmatter and its field table, the three scaffolds, the encoding | `af-spec` §5 |
  | `af-spec/references/requirements.md` | §6: the example, the ids, the EARS patterns, contracts, the glossary, execution paths, `external_apis` | `af-spec` §6 |
  | `af-spec/references/test-spec.md` | §7: the example, the fields and kinds, the coverage rules, one test per behaviour | `af-spec` §7 |
  | `af-spec/references/tasks.md` | §8: the example, the fields, `test_commands`, `dependencies`, the plan's rules, the integration task | `af-spec` §8 |
  | `af-spec/references/architecture.md` | §9: when to write it and its sections | `af-spec` §9 |
  | `af-spec/references/verdict.md` | §10: the stages of the verdict and their order, the warnings worth acting on, validating without `speca` | `af-spec` §10 |
  | `af-impl/references/test-first.md` | §5, step 4, and the integration task | `af-impl` §5 |
  | `af-impl/references/conformance.md` | §6: the checklist and the `unmet` list | `af-impl` §6 |
  | `af-impl/references/landing.md` | §7: the pull request body, tracking the open items with `issue`, the fallbacks | `af-impl` §7 |

- Nothing is lost. Every instruction of the three current skills is, after
  the change, in a body or a reference file, or it is in a list of deliberate
  cuts, each with its reason, in the description of the pull request that
  makes the change. Text written more than once and collapsed into one shared
  file counts as kept.

### Showing that the skills behave the same

- Before the change merges, each skill is run once in Claude Code and once
  in Codex, in a scratch repository, before the change and after it, on the
  same input:
  - `af-prd` on an idea of two sentences;
  - `af-spec` on a PRD of one or two requirement areas;
  - `af-impl` on a package of two tasks.
- Each run after the change:
  - reads each linked reference file at the step that links it;
  - takes the same gates;
  - ends at the same stop, or the same end;
  - for `af-spec`, ends with a package that `speca --validate` reports
    `valid`.
- What each pair of runs showed, and any difference, is written in the pull
  request.

### Documentation and build

- `make check` runs the render's checks after `test`, with no network and no
  API key.
- `make help` lists `skills`, `skills-check`, `install-skills` and
  `uninstall-skills`.
- `README.md`, *Agent Skills*: the four agents, the install line for each,
  and the note that a Claude Code install already reaches Copilot. Its table
  of make targets gains the new ones.
- `docs/configuration.md`: `AGENT`, `PROJECT` and `SKILLS_DIR`.
- `docs/architecture.md`: the source layout, the render and the install.
- An ADR records the decision and the two agent tables. For each row it
  gives the date the row was confirmed against that vendor's documentation.
- `CLAUDE.md` and `AGENTS.md`, *Repository Layout*: a line for `skills/`, and
  one for wherever the render lives.

## Design Decisions

1. **One source in the Agent Skills layout, and no format per agent.** All
   four agents load `SKILL.md` directories. Command or prompt files would be
   a second copy of each skill.
2. **The split goes by phase, not by data shape.** The shapes and tables are
   7.3 KB of `af-spec` and 0.5 KB of `af-impl`. Moving only them leaves both
   over the limit.
3. **The link is the inclusion.** A manifest of the files each skill uses
   would be one more thing to keep in step. With the link as the record, a
   broken link is found by the same check.
4. **One name in both directories is an error, not an override.** A skill's
   own file shadowing a shared one would let one skill drift from its
   siblings without a word.
5. **One token, no template language.** The invocation of another skill is
   the only text that differs by agent. The argument is named in words,
   which every agent reads the same.
6. **The 24,576-byte ceiling is on the body only, per agent.** The body is
   loaded with the skill, and the reference files only at their step. The
   ceiling is measured in bytes, not lines, because bytes are what load.
7. **The ceiling is a check in `make check`, not a Go test of the skills.**
   The steering file forbids tests of a document's content. The render's own
   behaviour is what the tests cover.
8. **The rendered skills are not committed.** Users of the other agents
   still need `speca` and `specr`, which need Go and a checkout until a
   release exists. Archives per agent come with the release.
9. **No new command.** The render runs through `make`, and the public
   surface of the repository does not grow (§4.2 of the comparison).
10. **`AGENT` defaults to `claude`**, so `make install-skills` does what it
    did.
11. **An install replaces a skill's directory whole, and an uninstall
    touches only the three names**, so neither leaves stale files nor
    removes a skill it did not install.
12. **A skill found twice is a warning, not a refusal.** The person may use
    both agents on one machine.
13. **`af-prd` and `af-spec` stay invocable by the model.** They write files
    in the working tree and nothing beyond it.
14. **Provisional: `af-impl` runs only when invoked**
    (`disable-model-invocation: true`) in Claude Code and Cursor. It pushes
    branches and opens pull requests and issues, which a person should start.
    It is provisional because it changes how `af-impl` starts today in
    Claude Code. Codex and Copilot have no verified equivalent, so there it
    stays invocable by the model. Codex's `agents/openai.yaml` may carry one,
    which the ADR settles.
15. **Provisional: the agent directories are the ones in the install
    table.** They come from secondary sources and could not be read at their
    vendor's documentation (see below). `SKILLS_DIR` overrides any of them,
    and the ADR records each row as confirmed, with its date, before the
    change merges.

16. **The render lives in `internal/skillgen`, as a `package main` that `make`
    runs with `go run ./internal/skillgen`.** The PRD forbids a new command,
    and `cmd/` is where `make build` installs commands from. `internal/` is
    excluded from `make doc`, so the render adds nothing to the public
    surface, and the repository already tests a `package main` with its tests
    beside it (`cmd/speca`). It imports only the standard library and
    `github.com/goccy/go-yaml`, which `go.mod` already carries, so no make
    target of the render depends on `update-agentkit` and none reaches the
    network.
17. **The same program renders, installs and uninstalls**, under `--check`,
    `--install` and `--uninstall`, and the `make` targets only pass `AGENT`,
    `PROJECT` and `SKILLS_DIR` through to it. Resolving the agent
    directories, replacing a skill's directory whole and warning about a
    skill loaded twice are behaviours with their own tests; in shell they
    would have none.
18. **The frontmatter is filtered line by line on its top-level keys**, each
    kept key keeping its own text and its continuation lines. It is parsed
    with `go-yaml` only to read and check `name` and `description`.
    Re-marshalling a parsed map would lose the source's key order, which the
    render is required to keep.
19. **`$ARGUMENTS`, and a skill named in an agent's own invocation syntax,
    are render refusals.** The PRD requires that no rendered skill carry
    either and that `make check` fail on a skill that breaks a render rule,
    but its list of refusals has no check for it; without one the
    requirement is enforced by nobody. A name is matched only where it
    stands alone — `/af-impl` or `$af-impl` preceded by the start of a line,
    whitespace or an opening delimiter and followed by the end of a line,
    whitespace or punctuation — so that the path `skills/af-impl` in prose is
    not a refusal.
20. **The editorial guarantees are task steps, not acceptance criteria.**
    Which sections each body keeps, that nothing is lost, and the before and
    after runs in two agents are work a reviewer confirms, not an outcome a
    program can observe, and Decision 7 keeps the skills' text out of the Go
    tests. They are steps and `done_when` entries of the tasks that carry
    them. What a program can observe — that the committed `skills/` tree
    renders for all four agents with no problem — is a criterion.
21. **The documentation obligations are task steps too.** `README.md`,
    `docs/configuration.md`, `docs/architecture.md`, the ADR and the
    *Repository Layout* line are required by the steering file in the same
    session, and it forbids tests of a document's content, so they are steps
    of the tasks that change the behaviour they describe.

## Dependencies

- **ADR 05** (`specr`) and **ADR 10** (`speca`): the skills call both
  commands as they are, and nothing in them changes.
- **`docs/spec-kit-comparison.md`** §4.11 and §7.1, item 3: the
  recommendation this PRD carries out, corrected where *Background* says.
- No package under `.specs/` is changed or depended on.
- The skill loaders of Claude Code, Codex, Cursor and GitHub Copilot, below.

## Verified External API

- **`github.com/goccy/go-yaml` v1.19.2**, in `go.mod`, parses the PRD
  frontmatter in `afspec/prd.go`. `func Unmarshal(data []byte, v
  interface{}) error`, read with `go doc`.
- **`io/fs`** and **`testing/fstest`**, the standard library:
  `type MapFS map[string]*MapFile`, read with `go doc`.
- **Claude Code skills**, read at <https://code.claude.com/docs/en/skills>
  on 2026-11-06:
  - when no placeholder in the content receives the arguments, "Claude Code
    appends `ARGUMENTS: <your input>` to the end of the skill content";
  - the frontmatter keys include `name`, `description`, `argument-hint`,
    `disable-model-invocation`, `metadata`, `license` and `compatibility`,
    all optional;
  - "Claude Code ignores a field it doesn't recognize without reporting an
    error";
  - `${CLAUDE_SKILL_DIR}` is the directory holding the skill's `SKILL.md`;
  - the personal location is `~/.claude/skills/<skill-name>/SKILL.md`, the
    project location `.claude/skills/<skill-name>/SKILL.md`;
  - "Keep `SKILL.md` under 500 lines. Move detailed reference material to
    separate files."
- **Agent Skills specification** (<https://agentskills.io/specification>):
  **unverified**. The host was not reachable from where this PRD was
  written. Assumed: `name` is 1 to 64 lowercase letters, digits and single
  hyphens, matches its directory, and does not start or end with a hyphen;
  `description` is at most 1,024 characters; `references/` beside
  `SKILL.md` holds files read on demand, resolved from the skill's
  directory.
- **Codex skills** (<https://developers.openai.com/codex/skills>):
  **unverified**, host not reachable. Assumed from third-party copies of the
  page:
  - user skills in `$CODEX_HOME/skills/`, by default `~/.codex/skills/`, and
    admin skills in `/etc/codex/skills`;
  - explicit invocation as `$name`;
  - optional metadata in `agents/openai.yaml`.

  Whether a repository's `.agents/skills/` is read is **NOT FOUND** in any
  source read.
- **Cursor skills** (<https://cursor.com/docs/skills>): **unverified**, host
  not reachable. Assumed from search results quoting the page:
  - `.cursor/skills/` and `~/.cursor/skills/`, plus `.agents/skills/`,
    `.claude/skills/` and `.codex/skills/` for compatibility;
  - a `/` invocation by name;
  - `disable-model-invocation: true` limiting a skill to explicit
    invocation.
- **GitHub Copilot skills**
  (<https://learn.microsoft.com/en-us/visualstudio/ide/copilot-agent-skills>,
  <https://docs.github.com/en/copilot/how-tos/use-copilot-agents/cloud-agent/create-skills>):
  **unverified**, hosts not reachable. Assumed from search results quoting
  the pages: `.github/skills/`, `.claude/skills/` and `.agents/skills/` in a
  repository, and `~/.copilot/skills/`, `~/.claude/skills/` and
  `~/.agents/skills/` for a user.
