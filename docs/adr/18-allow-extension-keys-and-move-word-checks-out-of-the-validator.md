# 18. Allow extension keys, and move word checks out of the validator

**Status:** accepted
**Date:** 2026-11-06
**Related:** [ADR 04](04-hand-on-what-a-phase-learned.md), [ADR 11](11-validate-a-package-against-the-spec-root.md), [ADR 14](14-commit-the-rendered-package-beside-its-json.md), [ADR 17](17-review-the-plan-adversarially.md)

## Context

`docs/spec-format-simplification.md` §6 lists five changes that make a
package lighter without a format migration; `docs/prd/05` specifies them.
Two of them change what the format and the library promise, and need a
record.

- **A team cannot add its own metadata.** Every object in the v2 schemas sets
  `additionalProperties: false`. The artifact types are generated from the
  schemas, `encoding/json` drops a key they have no field for, and a save
  writes the package without it. An issue key or an estimate on a task is
  deleted by the next `speca --task` move.
- **The validator greps for words.** Design rule 3 of the format says
  validation never matches words, and `vague_language` did. Since ADR 17 the
  plan review has a written `zero_decision` rubric whose question about open
  wording is the same check, made by something that can tell a vague word
  from a precise one.

## Decision

- **Every object accepts `x-` keys.** The four v2 schemas gain
  `patternProperties: {"^x-": {}}` on every closed object, the glossary aside,
  which is a map of terms. The format gives such a key no meaning.
- **The library keeps them beside the typed artifacts, not in them.** The
  types stay generated. `LoadSpec` records each `x-` key with the address of
  its object and every save writes it back after the object's own fields, in
  key order. An address uses an object's `id` where it has one, because the
  library's mutations add and remove objects with ids; objects without one (a
  path step, an external API, a dependency) are never added or removed by the
  library. In the PRD frontmatter a kept value is written as compact JSON, so
  the hand-written frontmatter renderer stays byte-stable without a YAML
  marshaller. `UnknownFields` skips `x-` keys, so `ValidateSchema` and the
  dropped-key guard of `speca --task` (ADR 11) no longer report them.
- **The generator writes none.** `DecodeArtifact` refuses an `x-` key in a
  model's artifact by its path, and the tool schema converter drops
  `patternProperties`, so a phase is never offered one. The strict tool
  schema subset forbids the keyword in any case.
- **The render does not show extension keys.** They are a team's data for its
  own tools; a coder implementing a task is not their reader.
- **`vague_language` leaves the validator.** `afspec` validation matches no
  words. The generator keeps the one-turn nudge of ADR 04 from its own check,
  beside a new `line_reference` hint for a step or `touches` entry that names
  a line of a file. Neither is a rule of the format, and `speca --validate`
  reports neither. The PRD review and the plan review rubrics list the vague
  words, and the plan review treats a line number or quoted code in a step as
  a `minor` `zero_decision` finding.

## Consequences

- A package can carry a team's metadata through every library write.
- A JSON number in an extension value is read as a float, so an integer past
  2^53 loses precision; a whole number is written without a decimal point.
- `speca --validate` on an existing package no longer lists `vague_language`
  warnings; no package changes validity, since warnings never block.
- A run that generates a package behaves as before on wording: the nudge
  moved, it did not go.
- Regenerating the artifact types with `make json-gen` must keep ignoring
  `patternProperties`; the generated structs have no field for extension keys
  and need none.
