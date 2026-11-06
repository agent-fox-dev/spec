# The package's prd.md: its frontmatter and the three scaffolds

What `af-spec` §5 writes: the frontmatter the format defines, field by field, the
body's two rules, and the three JSON scaffolds that make the package load.

The package's PRD is the input PRD's body with frontmatter the format
defines. Write the frontmatter exactly in this order and spelling:

```yaml
---
spec_id: "17"
spec_name: "indexed_code_search"
title: "Search the repository by symbol from the shell"
status: "draft"
created_at: "2026-10-27T09:12:44Z"
updated_at: "2026-10-27T09:12:44Z"
intent_hash: null
schema_version: 2
source: "docs/prd/03-search-the-repository-by-symbol.md"
---
```

| Field | Value |
|---|---|
| `spec_id`, `spec_name` | the two halves of the directory name, quoted strings; `spec_id` keeps its leading zero |
| `title` | the PRD's title, an imperative verb phrase |
| `status` | `"draft"`. §11 changes it, through `speca` |
| `created_at`, `updated_at` | the same ISO 8601 UTC instant, now |
| `intent_hash` | `null`. Activation computes it; a hash you write by hand is one the library will refuse |
| `schema_version` | `2` |
| `source` | where the idea came from: the PRD's path, the issue URL, or `"interactive"` when the person typed it. It is the first question anyone asks of a package |
| `owner`, `supersedes`, `tags` | optional; omit them rather than writing empty values. `supersedes` carries the spec ids this package replaces |

Then the body, which is the PRD's own text: `# {title}`, then `## Intent`,
then the rest. Two rules about it:

- **`## Intent` is required**, spelled exactly so, on a line of its own. It
  is the only machine-read section: activation hashes its text, every later
  write of the package is refused while the section no longer matches the
  hash, and a package without it cannot be activated at all. One short
  paragraph of stable purpose, not a summary of the current plan.
- **No frontmatter inside the body**, and nothing architectural. Design
  belongs in `architecture.md` (§9); behaviour belongs in the requirements.

Keep the PRD's sections as `af-prd` writes them — `## Intent`, `## Goals`,
`## Non-goals`, `## Background`, `## Requirements`, `## Design Decisions`,
`## Dependencies`, `## Verified External API`. Where this run settled a
question the PRD left open, add it to `## Design Decisions`, numbered, with
one line of why: the PRD in the package is the record of what was decided,
and a question answered in your head is a question the next reader asks
again.

Now write the three JSON files as a scaffold — the identity fields with empty
bodies — so the package loads from here on:

```json
{ "$schema": "https://agent-fox.dev/schemas/requirements.v2.json", "spec_id": "17", "spec_name": "indexed_code_search", "schema_version": 2,
  "introduction": "…", "glossary": {}, "requirements": [], "execution_paths": [] }
```

and the same identity in `test_spec.json` (`"tests": []`) and `tasks.json`
(`test_commands`, `"dependencies": []`, `"tasks": []`), each with its own
`$schema` URI — `https://agent-fox.dev/schemas/test_spec.v2.json` and
`https://agent-fox.dev/schemas/tasks.v2.json`. `spec_id`, `spec_name` and
`schema_version` are identical in all three files and agree with `prd.md` and
the directory name; `$schema` is the one of the file it heads.

`speca <NN> --validate` on a scaffold answers with exactly one error,
`completeness`: "spec is an empty scaffold". That is the gate for §5 — the
four files parse, the frontmatter is readable and the package resolves by its
number. Any other error, or a load failure (exit 1), is a defect in what you
just wrote; fix it before writing content. While the package is a scaffold
nothing else is checked, so do not read that one error as a clean bill.

Encode every JSON file with two-space indentation, the fields in the order
the shapes below give them, and a trailing newline. That is the library's own
encoding, and §11 rewrites all four files in it anyway.
