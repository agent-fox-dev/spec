# Starter agent instructions

Project-agnostic versions of this repository's [`AGENTS.md`](../../AGENTS.md)
and [`.specs/steering.md`](../../.specs/steering.md), to paste into a new
project as a starting point. Everything specific to `spec` (Go, `afspec`, the
commands, the skills, the test fixtures) has been taken out or turned into a
placeholder.

| Template | Copy to | What to fill in |
|---|---|---|
| [`AGENTS.md`](AGENTS.md) | `AGENTS.md` at the repository root | **Repository Layout** and **Quality Commands**, both marked with an HTML comment |
| [`steering.md`](steering.md) | `.specs/steering.md` | **Repository Specifics**, marked with an HTML comment, and the date of the first amendment |

## Adopting them

1. Copy both files into place:

   ```bash
   cp docs/templates/AGENTS.md /path/to/project/AGENTS.md
   ```

   ```bash
   mkdir -p /path/to/project/.specs && cp docs/templates/steering.md /path/to/project/.specs/steering.md
   ```

2. Agents that read `CLAUDE.md` instead of `AGENTS.md` get the same file
   through a symlink, as in this repository:

   ```bash
   cd /path/to/project && ln -s AGENTS.md CLAUDE.md
   ```

3. Replace every HTML comment (`<!-- … -->`) and every `<placeholder>` with the
   project's own content, and delete the comments. If the project has no
   `make doc` (or no doc generator), drop the **Generated Docs** section of the
   steering and the `make doc` lines in both files.
4. Set the `date` of the 1.0.0 amendment in the steering's frontmatter to the
   day you adopt it.
5. Create the directories the files refer to: `docs/adr/`, `docs/errata/`,
   `docs/prd/`, and the four reference docs `docs/api.md`, `docs/cli.md`,
   `docs/configuration.md` and `docs/architecture.md` (one line each is enough
   for a surface the project does not have).

## Amending the steering

The steering's frontmatter holds its `version` and an `amendments` record,
newest first ([ADR 15](../adr/15-version-the-steering.md)). When you change a
directive, add an amendment at the top and set `version` to it:

| Bump | When |
|---|---|
| MAJOR | a directive was removed, or changed so that a package or code that followed it may now break it |
| MINOR | a directive was added |
| PATCH | a wording that changes no directive |

The frontmatter may hold only `version` and `amendments`; each amendment needs
a `version` (`MAJOR.MINOR.PATCH`), a `date` (`YYYY-MM-DD`) and a `change`. A
file without frontmatter also works — it is simply unversioned.
