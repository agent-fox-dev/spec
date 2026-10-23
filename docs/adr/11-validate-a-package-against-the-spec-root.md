# 11. Validate a package against the spec root

**Status:** accepted
**Date:** 2026-10-23
**Related:** [ADR 03](03-pipeline-the-scopes-of-a-split.md),
[ADR 10](10-report-and-change-the-state-of-a-package-with-its-own-command.md),
[ADR 04](04-hand-on-what-a-phase-learned.md)

## Context

`spec` wrote a package, read it back, validated it and activated it. The
validation was `(*Spec).Validate`: the schemas and the cross-file rules C1 to
C11, which look at one package. The rules across specs (format v2 §10.4) were
never run by `spec`. `speca <spec> --validate`, added later, runs them, so a
package `spec` had written and activated could be rejected by the next command
an agent ran on it.

That happened to `16_af_service_client` of agent-fox: four `cross_spec_actor`
errors on a package that was `active`. The five packages written beside it
(11 to 15) fail one of the two rules as well. Two things were behind it:

- **Nothing checked.** `ValidateCrossSpec` is called from `speca` only.
- **Nothing taught.** The generation prompts say nothing of the rule that a
  spec shares an actor with each spec it depends on, and the existing specs
  were shown to the model by number, name and status, not by the actors the rule
  is decided by. The model coined `agent-fox service` where the spec it depends
  on says `agent-fox`. Three of the packages named a dependency by directory
  (`05_af_cli_envelope`) where the rule looks for the number (`05`); per-package
  validation accepts both.

The rule cannot be repaired by the phase that could learn of it. `dependencies`
are written by the tasks phase, after the requirements it would have to change.

## Decision

- **The verdict is one function, used by both commands.** `specpkg.Verdict` is
  the package's own validation followed by the cross-spec findings that concern
  it. `speca --validate` called the code that makes it from inside `cmd/speca`;
  it moved to `internal/specpkg`, and `speca` and `specgen` call it. The graph is
  built from the dependencies the loaded packages hold, not from `tasks.json` on
  disk, so a package that is not on disk has its edges all the same.
- **`spec` applies it to every package it writes**, after the read-back and
  before the activation. Its findings are in `result.validation` beside the
  package's own, a package that has any is reported as `invalid_spec`, and it is
  left on disk in `draft`. It is not activated and its PRD is not posted under
  `--comment`. A spec root that cannot be read for it fails the run at stage
  `validate`: the package was just written there.
- **The generation prompts teach the rule.** The requirements phase, which
  writes the execution paths, is told what the rule asks and is shown the actors
  of the existing specs; the landscape and the tasks phase say a dependency is
  named by its number. The rule is not added to the submit tool's check: the
  phase that has the dependencies cannot change the paths.
- **A package of a split is checked after the packages before it.** Packages are
  built concurrently, and a package depends only on those before it, which may
  still be being built. Two waits, both on packages with a lower number so that
  none waits for a package that waits for it:
  - a package's *validation* waits for each package before it to settle, written
    and validated or failed. Without it a dependency still being built is not on
    disk, and is reported as a dependency on no spec;
  - a package's *requirements phase* waits for the requirements phase of each
    package before it, so that it can be shown their actors, which exist only in
    memory until the package is written. The phases after it overlap as before.
- **A dry run checks too.** It writes nothing, so the packages of a dry run are
  handed to the verdict in memory, and a spec root that does not exist holds
  none.

## Consequences

- A package that `spec` activates validates under `speca --validate`, as it
  stood in the spec root when it was written. Specs added or changed later are
  that command's to report.
- A run on an input that breaks the rule exits 1 with `invalid_spec` where it
  exited 0 before. That is the point; the cost is a package to repair by hand,
  or a run again, where the finding used to come later.
- **ADR 03 is narrowed.** The scopes still overlap, but a package can no longer
  finish before the package before it: it is validated after it. A third scope
  whose phases end first waits for the first. The requirements phases of a
  split's packages now run in plan order, the phases after them still overlap,
  and the packages are validated in plan order. What that costs a split in time
  depends on how long those phases take beside the PRD phases that already ran
  in order; it was not measured. `--max-parallel 1` is unchanged.
- The model is shown the actors of every existing spec in the requirements
  prompt. By estimate, a repository with a hundred specs adds a few thousand
  tokens to that one phase, a line per spec; a repository that outgrows it can
  show only the specs the PRD lists as dependencies.
- The rule is a match of free-text names, which the model must now reuse
  exactly. A package whose dependency's paths were never read still fails it.
  Whether the rule asks the right thing is a question about the format, not
  about this decision; it is applied as the format defines it.
