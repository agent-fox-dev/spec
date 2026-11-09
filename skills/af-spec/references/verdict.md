# The verdict

What `af-spec` §9 reads: the stages of the verdict and the order to work in,
the warnings worth acting on, and validating without `speca`.

```sh
speca <NN> --render
speca <NN> --validate
```

`--render` writes `spec.md`, the package's render, which every save through
the library writes beside the artifacts and a hand-written package lacks; it
changes no other file. Render before every verdict once the files exist.

This is the format's whole verdict: the three schemas (and `progress.json`'s,
when the file exists), the cross-file rules C1–C8, and the rules across
packages. Read `result.validation.errors` — each entry names its `check`, the
`artifact` and the `entity`, and most carry the repair in the message.
`result.traceability` gives the criteria and paths covered and the criteria
no task owns, and `result.valid` is the answer. Exit code 0 reports a
verdict, valid or not; exit 1 means the package could not be **read** at all
(a missing file, malformed JSON or YAML — `error.message` says which) and exit
2 is a reference that names no package.

Work in that order, because the stages gate each other:

1. **`completeness`** — "an empty scaffold" while both bodies are still
   empty (§5), or "incomplete: <files>" while one of the three is missing.
   A `schema_version` that is not `3` is refused here too, as
   `schema_version`, before anything else is read.
2. **`json_schema`** — a missing or misspelled field, a wrong type, an
   unknown key, a value outside an enum, `minItems` on a list you left empty,
   a criterion without a `check`, a path without `smoke`, a check with both
   `kind` and `same_as`. `'not' failed` on a criterion is the EARS
   combination of §6. The cross-file rules do not run until every file passes
   its schema, so a schema-clean verdict can still uncover a dozen C-rule
   errors.
3. **`C1`–`C8`** — the identity (C1), the ids (C2), every `same_as` (C3),
   every task reference (C4), every criterion owned (C5), every `unwanted`
   contract (C6), the profile (C7) and, when the file exists, the progress
   entries (C8).
4. **`cross_spec_*`** — an unknown dependency, a cycle, a dependency with no
   actor in common. These need the other packages and are **not** checked by
   anything else, so this is the only call that catches them.

Beside the stages, **`rendered`** compares `spec.md` with the render: a
missing one is a warning, one that differs is an error naming the first line
that differs. `speca <NN> --render` fixes either.

Fix, ask again, and do not go on until `valid` is true. Then read the
warnings: they never block, and both are worth acting on anyway.
`missing_contract` names a criterion that describes an error outcome and has
no contract; give it one. `scope_limit` means the work wanted a split (§4).
Say in the report which warnings you left, and why.

The verdict matches no words, so `speca` reports the checks `spec` makes as it
generates a package beside the verdict, in `result.hints`, never in it. Each
one names its `check`, `artifact`, `entity` and `message`:

- `vague_language`: a vague word in a criterion. Say what it stands for, or
  keep it where it is precise in context.
- `line_reference`: a step or `touches` entry naming a line of a file
  (`store.go:88`), which is wrong after the first commit that moves it. Say
  the behaviour, and name the file alone in `touches`.
- `foreign_test_command`: a `test_commands` entry from an ecosystem the
  repository does not have. Use the project's own command the message names.
  This one is always a mistake, and `spec` refuses it.

Act on each one, as `spec`'s phases are made to, or keep it and say in the
report why. A hint never blocks activation.

Then read the package back as a coder will receive it:

```sh
specr <NN> --markdown-file "${TMPDIR:-/tmp}/af-spec-<NN>.md" > "${TMPDIR:-/tmp}/af-spec-<NN>.json"
```

Both files go outside the repository, so the tree stays clean, and stdout
goes to a file because the envelope repeats the whole Markdown.

Read that Markdown, not your own drafts. It renders every criterion as its
EARS sentence with its contract and its check, each path with its smoke check,
and each task with what it owns, the derived integration task last — which is
where a criterion that reads as two behaviours, a task with no steps a
stranger could follow, or a check that restates another becomes obvious. Fix
what you find and validate again.

Without `speca` and `specr` installed, validate against the schemas in
`specification/schemas/*.v3.json` of a `spec` checkout with any JSON Schema
validator, check C1–C7 and the actor rule by hand from §6 and §7, leave the
package in `draft`, and say in the report that no verdict was taken.
