# The shape of a PRD

The sections a PRD holds, in the order `spec`'s own PRD phase writes them, and
what each one holds. `{{invoke af-prd}}` writes a PRD to this shape and
`{{invoke af-spec}}` reads this same file when it writes one itself, so the two
cannot drift.

```markdown
# {Title, as an imperative verb phrase}

## Intent

## Goals

## Non-goals

## Background

## Requirements

### {Area}

## Design Decisions

## Dependencies

## Verified External API
```

Those are the sections `spec`'s PRD phase writes, in its order. What each
holds:

- **Title.** An imperative verb phrase: "Report a spec package's state from
  the shell", not "speca".
- **`## Intent`.** Required, and spelled exactly so, on a line of its own:
  `spec`'s PRD phase may not submit a PRD without it, the package's intent
  hash is computed from it, and a package without one cannot be activated.
  Written here, it is the intent `spec` keeps rather than one it invents.
  One short paragraph: what this is for and why it matters.
  Activation hashes it, and after that every write to the package is refused
  while the section differs from its hash, so write a stable statement of
  purpose, not a summary of the current plan.
- **`## Goals`.** Concrete, verifiable outcomes: "a bare `speca <spec>`
  reports every task with its state", "p99 under 200ms", not "faster" or
  "easier to use".
- **`## Non-goals`.** What is deliberately out, each item specific, and where
  it lands when it lands elsewhere (another package, a later PRD). It is the
  cheapest section to write and the most expensive to omit: it is what stops
  a generator inventing requirements for scope you excluded.
- **`## Background`.** What exists today and why this is worth doing, from
  the survey: the real files, commands and modules, and what each does now.
- **`## Requirements`.** The behaviour, in prose, grouped under `###`
  headings, one per area. Not EARS; `spec` writes the EARS criteria from
  this. For every behaviour:
  - who or what triggers it, and in what state;
  - what the system does, and what the caller observes: the output, the
    exit code, the file written, the message;
  - every way it can fail, each with what the caller observes (an exit code,
    an error and its wording or category, what is left on disk, that nothing
    was written). This is the `contract` of an `unwanted` criterion, which
    the format requires and which the tests assert. "It reports an error" is
    not one;
  - the boundaries, with numbers: empty input, the largest input, a limit
    and what happens one past it;
  - what happens when a dependency is unavailable: a missing file, a network
    call that fails, a tool that is not installed.

  Describe the end-to-end flows too, each from the actor's first action to
  its last observable effect, through the real components it passes on the
  way ("the operator runs `speca 16 --task 4 --state done`; the package is
  written through the library; the result lists the change"). `spec` turns
  them into execution paths, and each path into a smoke test that runs those
  components unmocked. Mark a constraint as one: "Constraint: Linux and
  macOS; Windows is best effort."
- **`## Design Decisions`.** Every question the input left open, numbered:
  the answer chosen and one line of why. A decision the person made is
  written as theirs; one you made at their "you decide" is written the same,
  with its reason. Omit the section when there were none.
- **`## Dependencies`.** Each existing package, ADR or external system this
  depends on, changes or supersedes, with its spec id or path, its status
  where it is a package, and the reason. Omit the section when there are
  none.
- **`## Verified External API`.** For each external package the work relies
  on: the symbols, their real signatures, and where you read them. A
  signature you could not read is marked **unverified**, and one that should
  exist and does not is marked **NOT FOUND**, with what was assumed. Omit
  the section when the work uses only the standard library and the project's
  own code.

No other sections. In particular there is no `## Open Questions`: `spec`
would answer each one itself, unattended, and report the ones it is least
sure of. No YAML frontmatter either: `spec` writes the frontmatter of the
package's `prd.md` itself (identity, status, `source`), and its PRD phase may
not submit a body that has one.

## No vague words

The format's validator flags `appropriate`,
`properly`, `correctly`, `reasonable`, `relevant`, `adequate`, `suitable`,
`as needed`, `if necessary` and `etc` in the criteria `spec` writes, and
the criteria take their words from the PRD. Say what each one stands for.

## The size of one package

One package holds at most 10 requirements and 12 tasks, and
`spec` splits a larger PRD into several packages, one per scope, the first
one foundational. Count the areas of `## Requirements`: past what one
package holds, tell the person a split is coming, and make each area one
that can be built and tested on its own, the foundational one first. A
PRD of several scopes is written whole; `spec` does the split.
