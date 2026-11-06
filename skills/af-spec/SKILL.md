---
name: af-spec
description: Write a version 2 specification package in the session, as the spec command's own pipeline writes one, without an unattended model run. Reads a PRD, orients itself in the spec root and the code, reserves the next number, then writes prd.md, requirements.json, test_spec.json and tasks.json in that order, taking speca's verdict after each one, and activates the package only on a clean verdict. Never writes the code the package specifies and never edits another package.
argument-hint: "[a PRD path, an idea, an issue URL, a draft package to finish, or nothing]"
---

# af-spec

You write a specification package — the four files of format version 2 — with
a person watching. It is the middle of three steps: `af-prd` writes the PRD,
this skill turns it into a package, `af-impl` implements it. The `spec`
command does the same work in one unattended model run; this skill does it in
the session, so that the person reads each artifact as it appears and the
code the package is written against is the code you have just read. Where the
two differ in method, `spec`'s pipeline is right: it implements the format's
generation contract (§12 of `spec-format-v2.md`), and so does everything
below.

Five rules shape it. **The plan is the product**: every criterion is verified
by a test, every test is owned by a task, every execution path is exercised
by a smoke test, and a package that breaks one of those is not a draft to
improve later — it is invalid. **One order, one gate**: `requirements.json`,
then `test_spec.json`, then `tasks.json`, each written against the complete
text of the ones before it, each followed by `speca --validate`. **The
verdict is the format's**: `speca` decides whether the package is valid, not
your reading of it; you fix what it names and ask again. **Draft until it
validates**: the package is written in `draft` and activated only on a clean
verdict, because activation hashes the intent and closes the package to
edits. **Behaviour from the PRD, names from the code**: what the system must
do comes from the PRD and nothing else; the files, commands, types and test
commands the package names are the ones the repository really has.

Two commands of the `spec` repository do the bookkeeping: `speca` reports a
package's state and gives the format's verdict on it, and `specr` renders a
package as Markdown. Read
[references/speca-specr.md](references/speca-specr.md) in full before the first
call — what each returns, their exit codes, where to write their output, and
what to do when they are not installed — and do not work out a package's
number, scope or verdict by hand while the commands are there to ask.

If `.specs/steering.md` exists, read it first and let it answer every
question it answers — it is the project's own directive to every agent, and
`spec`'s phases are given it too. Follow the repository's own agent
instructions (`CLAUDE.md`, `AGENTS.md`) for where documents live, numbering
and commits; where they say nothing, the defaults below apply.

## How the run goes

One direction: input, orientation, the number, the size, the PRD, the three
artifacts in order, the verdict, activation, the report. You stop for the
person's answers in §1 and §4 and for nothing else. Do not ask whether to
start an artifact, whether a requirement reads well, or whether to go on to
the next file. Show each artifact as you write it; the person steers by
answering and interrupting.

You stop short of a written package, and say why, for these reasons only:

- the idea is not yet a PRD and the person wants one written properly first
  (§1): `af-prd` is that step;
- the PRD's intent is already an active or sealed package's intent, and the
  person has not said what this package changes (§2);
- the work does not fit one package and the person has not chosen the scope
  to write first (§4);
- `speca` refuses to read the spec root, or the spec root is not writable.

Everything else is information, not a stop: a dependency that is not sealed,
a warning, a repair loop, a PRD with a gap you can close from the code.

## 1. Read the input

The argument the skill was given is one of:

- **a path to a PRD** — the normal case, and what `af-prd` leaves behind. Its
  body is the intent of the package and the source of every requirement;
- **a path to a draft package** (`.specs/17_name`, or `17`) — a package to
  finish or repair. Read it with `specr`, take its verdict (§10), and write
  only what is missing or broken. An `active` or `sealed` package is not
  this: say so and stop (§2);
- **an idea in prose, or an issue URL** — no PRD exists yet. Read the issue
  and its comments where it is one (with `gh` or `glab` when installed). Say,
  in one line, that `{{invoke af-prd}}` writes the PRD with them and that this
  run can instead write the PRD itself, deciding the open questions and
  recording each in `## Design Decisions`, and let them choose. If they choose
  this run, write the body of §5 to the shape in
  [references/prd-shape.md](references/prd-shape.md), which is the same file
  `{{invoke af-prd}}` works from, asking the questions that change the shape of
  the work in batches of three or four;
- **absent** — run `speca --list` and ask which draft package to finish, or
  which PRD to turn into a package.

Say in one line, once, that `spec <prd>` does the whole of this run
unattended where it is installed and a model credential is set, and that here
every artifact is written in this session instead, in view. Then get on with
it. Do not run `spec`: it would write a second package beside the one you are
writing.

A PRD that is already the `source` of a package in the spec root has been
specified. Name that package and ask what this one changes before writing
anything.

## 2. Orient

Before writing anything, find out what exists. This is the step that decides
whether the package's ids, names and dependencies are real.

```sh
speca --list --all > "${TMPDIR:-/tmp}/af-spec-list.json"
```

Every package under the spec root, archived ones included, each with its
`spec_id`, `spec_name`, `status`, verdict and task counts. From it:

- **the number to use** (§3) and the names already taken;
- **the packages this one touches.** For each, `specr <NN> --section prd` for
  what it promises and excludes, and `specr <NN> --section requirements` for
  its glossary and its **execution-path actors**. Both matter later: a
  glossary term this package redefines is a warning across specs, and an
  actor it fails to share with a dependency is an error no later artifact can
  repair (§6);
- **the intent already specified.** A `sealed` or `active` package whose
  intent covers this PRD means the work is done or in progress. Name it and
  ask what this package changes; a change to an active package's intent is a
  new package that supersedes it, never an edit to that one.

Then read the repository, read-only, over the files `git ls-files` lists:

- `README.md`, the docs and the ADRs, for the vocabulary and the decisions
  already made; the errata where the repository keeps them;
- **where things live**: the commands, modules, files and types the behaviour
  touches, with their paths, and what each does today. Every name the package
  writes into `touches`, `pseudocode` or a criterion comes from here;
- **what already exists**: the part of the PRD the code already does. It is
  either out of scope or it is what changes;
- **the checks**: the project's real test and lint commands, from its
  `Makefile`, `package.json`, `pyproject.toml` or CI config. They become
  `test_commands` in §8, and a command from another ecosystem is a defect in
  the package;
- **the external API**: for every library the work calls, the real signatures
  of the symbols it will call, read where they are defined (`go doc
  <pkg>.<Symbol>`, the installed package's source, the lockfile for the
  version). `spec`'s own phases cannot read outside the repository and have
  to mark those signatures unverified; you usually can, and a signature you
  read is one the package carries as verified (§6).

Note the project's language and its stub marker (`panic("not implemented")`,
`TODO`, `NotImplementedError`, `todo!()`): the integration task's steps name
them in §8.

## 3. Reserve the number, the name and the directory

- **The number** is the highest numeric prefix in the spec root **and under
  `archive/`**, plus one, as a string zero-padded to two digits — three once
  past 99, so the directories keep sorting lexically. An archived package
  keeps its number; reusing it would make two packages answer to one id.
- **The name** is `snake_case`, derived from the PRD title:
  `^[a-z][a-z0-9]*(_[a-z0-9]+)*$`. No uppercase, no hyphen, no double or
  trailing underscore. A name the person gives wins.
- **The directory** is `<spec root>/{NN}_{name}`. The spec root is
  `$AF_SPEC_DIR` where it is set, else `.specs/`; where the project keeps it
  somewhere else, every `speca` and `specr` call below takes `--specs-dir`.
  `spec_id` and `spec_name` must agree with the two halves of the directory
  name in all four files (rule C1), so settle both before you write a line.

Create the directory and write nothing else into it: a package holds the four
artifacts, `architecture.md` when there is one, and nothing more — no session
file, no notes, no scratch copies.

## 4. Size the work, and split before you write

A package is **one cohesive feature**: at most 10 requirements, at most 8
criteria each, at most 12 tasks. The validator warns past those; the warnings
are a sign the work was not split, not a formatting nit.

Count the areas of the PRD's `## Requirements`. If the work is plainly
larger — three or more areas that could be built and tested on their own, or
unrelated stories serving different actors — do not write an oversized
package. Propose the split: every scope in order, each with the `snake_case`
name its package will carry and a sentence of what it covers, the
**foundational one first**, and ask which to write now. Then write that scope
alone, name the later scopes in the PRD's `## Non-goals`, and say in the
report which remain and that each is its own package, written by its own run
of this skill (`spec` splits a large PRD by itself; here the person chooses).

## 5. `prd.md`

Read [references/prd-frontmatter.md](references/prd-frontmatter.md) in full
before writing: it gives the frontmatter field by field, the body's two rules,
and the three JSON scaffolds that make the package load. The PRD's own sections
are in [references/prd-shape.md](references/prd-shape.md).

The package's PRD is the input PRD's body under frontmatter the format defines.
`status` is `"draft"` and `intent_hash` is `null`: §11 changes both, through
`speca`, and a hash written by hand is one the library will refuse.

**`## Intent` is required**, spelled exactly so on a line of its own. It is the
only machine-read section: activation hashes its text, every later write of the
package is refused while the section no longer matches the hash, and a package
without it cannot be activated at all. There is no frontmatter inside the body
and nothing architectural in it.

Where this run settled a question the PRD left open, add it to
`## Design Decisions`, numbered, with one line of why: a question answered in
your head is a question the next reader asks again.

Then write the three JSON files as a scaffold — the identity fields with empty
bodies — so the package loads from here on, and take the verdict:

```sh
speca <NN> --validate
```

On a scaffold that answers with exactly one error, `completeness`: "spec is an
empty scaffold". That is the gate for §5 — the four files parse, the
frontmatter is readable and the package resolves by its number. Any other
error, or a load failure, is a defect in what you just wrote; fix it before
writing content. While the package is a scaffold nothing else is checked, so do
not read that one error as a clean bill.

## 6. `requirements.json`

Read [references/requirements.md](references/requirements.md) in full before
writing: it gives the example, the ids, the six EARS patterns with the fields
each one allows, what a contract is, the glossary, the execution paths and
`external_apis`.

What the system must do: one flat list of criteria per requirement, plus the
end-to-end execution paths. Everything comes from the PRD; nothing comes from
your view of how it should be built. **There are no other sections**: an edge
case is a criterion, a correctness property is a `ubiquitous` criterion whose
action begins "for any …", error handling is an `unwanted` criterion with a
contract, and there is no `edge_cases`, `correctness_properties`,
`error_handling` or `user_story`.

Two rules can be repaired by no later artifact, so decide them here. Every
`unwanted` criterion needs a `contract` (rule C10). And for every package this
one depends on, at least one step of this package's paths must be played by an
actor that also plays a step in one of that package's paths, the same name
ignoring case; a dependency with no shared actor is an **error** across specs.

Write the file, then take the verdict: `speca <NN> --validate`. With
`test_spec.json` and `tasks.json` still empty you will see two expected errors,
`at '/tests': minItems: got 0, want 1` and `at '/tasks': minItems: got 0,
want 2`. Every other error is yours to fix now. The cross-file rules C1–C11 do
**not** run while any file fails its schema, so this gate checks the
requirements' shape, not yet their coverage.

## 7. `test_spec.json`

Read [references/test-spec.md](references/test-spec.md) in full before writing:
it gives the example, the fields, the four kinds, the coverage rules the format
enforces, and what one test per behaviour means.

How each criterion is verified: **one flat list** of tests, written against the
complete requirements artifact — its real ids, never an invented one. The ids
stay in this file; `af-impl` writes each test into the test file of the code it
exercises, named for the behaviour, and records the map from test id to test in
the commit. Write `title` and `pseudocode` so that is possible: say the
behaviour, name the file, never the id.

The format enforces the coverage, so there is no `coverage` object to author:
every criterion is in the `verifies` of at least one test (C4), every path is
in the `verifies` of at least one `smoke` test and every `smoke` test verifies
at least one path (C5), every id in `verifies` resolves in this package (C3),
and `real_components` is required for `smoke` and **must be absent** for every
other kind (C11).

Take the verdict again. Only the `tasks.json` `minItems` error should remain
beside anything the tests broke.

## 8. `tasks.json`

Read [references/tasks.md](references/tasks.md) in full before writing: it
gives the example, every field, `test_commands`, `dependencies`, the rules that
make a plan invalid, and what the integration task must cover.

What work to do, in what order: **one flat, ordered list**. A task is the unit
of dispatch — one coder session, one task — written against the complete
requirements and the complete test list. **Group by requirement, not by layer**:
a task delivers one or two requirements end to end, so each one leaves the
system working. The coder works test-first, so **there are no "write the tests"
tasks**; tests are written inside the task that owns them.

Four rules make the plan invalid: every test is listed in the `tests` of at
least one task (C7); every criterion is covered by the `criteria` of at least
one **`implement`** task (C8); every id in `criteria`, `tests` and `depends_on`
resolves, and `depends_on` points at lower ids (C6); and exactly one task has
kind `integration`, it is last, and its `tests` include **every** `smoke` test
(C9).

**Check ownership before you take the verdict.** Go through the test list and
write each test id against the task whose `tests` holds it; then through the
requirements and each criterion id against the `implement` task whose
`criteria` holds it. An id with no owner is the commonest way a plan ships with
a hole.

## 9. `architecture.md` (optional)

Write it only when the person asks, or when the PRD carries design detail that
is neither intent nor a criterion. Read
[references/architecture.md](references/architecture.md) for its sections and
its two constraints before writing one — the second being that **nothing in it
is load-bearing**, since a renderer working to a token budget drops it first,
so every fact a coder needs to implement belongs in a criterion or a task step.

## 10. The verdict

```sh
speca <NN> --render
speca <NN> --validate
```

Every save through the library writes `spec.md`, the package's render, beside
the artifacts, for a reviewer to read in the diff; you wrote the files by hand,
so `--render` writes it, and nothing else. From here on render before every
verdict: rule `rendered` warns about a `spec.md` that is missing and fails one
that is not the render.

This is the format's whole verdict: the four schemas, the cross-file rules
C1–C11, and the rules across packages. Read
[references/verdict.md](references/verdict.md) in full for its four stages, the
order to work in and why the stages gate each other, the three warnings worth
acting on, and how to validate without `speca`.

`result.valid` is the answer. Fix, ask again, and **do not go on until `valid`
is true**. Exit code 0 reports a verdict, valid or not; exit 1 means the
package could not be **read** at all and exit 2 is a reference that names no
package. The rules across packages need the other packages and are checked by
nothing else, so this call is the only one that catches them.

Then read the package back as a coder will receive it:

```sh
specr <NN> --markdown-file "${TMPDIR:-/tmp}/af-spec-<NN>.md" > "${TMPDIR:-/tmp}/af-spec-<NN>.json"
```

Read that Markdown, not your own drafts. It renders every criterion as its EARS
sentence with its contract, each test under what it verifies, and each task
with what it owns — which is where a criterion that reads as two behaviours, a
task with no steps a stranger could follow, or a test that restates another
becomes obvious. Fix what you find and validate again. Say in the report which
warnings you left, and why.

## 11. Activate

A package that validates is activated; one that does not stays in `draft`
with its errors named in the report. Activation is a lifecycle move, so it
goes through `speca`, never through an edit of the frontmatter:

```sh
speca <NN> --status active
```

It computes the hash of the `## Intent` section, writes it into
`intent_hash`, and rewrites **all four artifacts** (and `spec.md`) in the library's canonical
encoding — which is also what normalises anything your hand-written files
spelled differently. Three things to know about it. It refuses a package with
no `## Intent` to hash, and one that fails the package's **own** rules
(`--force` overrides the second; this run does not use it). It does **not**
apply the rules across packages, so it will activate a package with an
unknown dependency that `speca --validate` rejects — which is why the verdict
comes first, always. And the files it writes are left with mode `0600`,
because it writes them through private temporary files; `chmod` them to the
repository's usual mode before committing where that matters.

After activation the `## Intent` section is closed: every write of the
package is refused while its text no longer hashes to `intent_hash`. Changing
the intent then means a new package that supersedes this one — so if the
person is still weighing the intent, leave the package in `draft`, say so,
and give them the one call that activates it.

Leave the package in `draft` when the person asks (`spec --no-activate` does
the same), and when the verdict is not clean.

Commit as the repository's instructions say — package and PRD together, on a
branch of its own, where they call for one. Where they say nothing, leave the
files uncommitted and say so.

## Never

- **Write the code.** This run writes a specification, not an
  implementation. No source file, no test file, no fix "while here".
  `af-impl` implements the package.
- **Touch another package.** No edit, no state change, no renumbering. A
  change to an active package's intent is a new package that supersedes it.
- **Hand-edit the lifecycle.** `status` and `intent_hash` change through
  `speca --status`, and a task's state through `speca --task`. A hash or a
  status written by hand is drift the library refuses.
- **Write derived or v1 structures.** No `coverage`, no `traceability`, no
  task groups, no subtasks, no `edge_cases`, `correctness_properties`,
  `error_handling` or `user_story`, no `TS-E`/`TS-P`/`TS-SMOKE` id variants,
  no `_session.json` or any other file in the package directory.
- **Invent an id, a name or a signature.** Every id carries this package's
  prefix and resolves; every file, command and module the package names
  exists under that name or is said to be new; a signature nobody read is
  `verified: false`.
- **Put design in the PRD, or behaviour only in `architecture.md`.** The PRD
  is intent and behaviour; the criteria are the contract; the architecture is
  optional and load-bearing for nothing.
- **Leave a question open.** A package is the input to an unattended coder.
  Decide it with the person, or decide it from the code and record it in
  `## Design Decisions`.
- **Run `spec`.** It writes a package of its own beside yours. `speca` and
  `specr` are the commands this run uses.
- **Activate on an unclean verdict**, or activate a package whose intent the
  person is still weighing.
- **Reuse an archived number**, or renumber a package to close a gap.

## 12. Report

- **The package**: its directory, `spec_id`, `spec_name`, title, status, and
  the artifacts written.
- **The counts**: requirements, criteria, execution paths, tests by kind,
  tasks, and the criteria and paths covered, from `speca`'s `traceability`.
- **The verdict**: `valid`, and where it is false every error by its rule.
  Then the warnings you left, each with why.
- **The decisions** this run made that the PRD had left open, and anything
  the person settled.
- **The dependencies**: each package named, with its status, and any that is
  neither sealed nor has its integration task done — `af-impl` refuses to
  implement before such a package, so it belongs in the report, not in a
  footnote.
- **The external API**: what was verified, and what is `verified: false` for
  `af-impl` to confirm.
- **The split**, where §4 found one: the scopes that remain, in order, and
  that each is its own package.
- **The next steps**:

  ```sh
  speca <NN>                   # the package's state, its next task and its verdict
  speca <NN> --status active   # activate it, where it was left in draft
  specr <NN>                   # read it as a coder receives it
  {{invoke af-impl}} <NN>      # implement it
  ```
