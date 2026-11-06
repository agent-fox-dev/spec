# Making the spec format lighter: an analysis of v2 and ideas for v3

An analysis of the format in `specification/spec-format-v2.md`, measured
against the ten packages under `.specs/` written with it, with three
questions in mind: how the format can be simpler without giving up what the
validator proves; what the other kits in `docs/spec-kit-comparison.md` have
that is worth taking; and what else could make a spec better.

It is a companion to the comparison. The comparison decided *whether* to keep
the kit, and recommended keeping the format and the library because they are
the only part of the field that can prove a plan complete. Four of its seven
recommendations are implemented or in progress (the committed render, spec
15; the versioned steering, spec 16; the review triage, spec 17; the plan
review, spec 18). This document is about the format itself, which none of
them touches.

Nothing here is decided. The last section orders the ideas by what they
return for what they cost, and says what should not change.

## 1. What the ten packages say

Everything below was measured on packages 09 to 18, all written by `spec`
under the v2 format, with the renders they now carry.

| Measure | Value |
|---|---|
| Size of a package on disk, `spec.md` included | 142 to 278 KB |
| Of which the three JSON artifacts | 59 to 125 KB |
| Of which `prd.md` | 16 to 27 KB; its hashed `## Intent` is 0.5 to 1.2 KB, 2 to 5 % of the file |
| Criteria in all | 442; tests 404 |
| Criteria verified by more than one test | 30 of 442 (7 %) |
| Tests that carry `pseudocode` | 396 of 404 (98 %) |
| Smoke tests that also list criteria, against the generation rule that they list the path alone | 18 of 35 |
| Criteria with a `guard` | 14 of 442 (3 %) |
| Distinct `system` values per package | 2 to 13; in five packages one value covers more than half the criteria (`speca` is on 159 of 442 overall) |
| Tasks with `optional: true` | 0 of 85; tasks in state `dropped`: 0 |
| Tasks whose `depends_on` is absent or equals the previous task, which is the default | 55 of 85 |
| Tasks with `done_when` | 19 of 85; outside two packages, only the integration task has one |
| Frontmatter `owner`, `tags`, `supersedes` | set on 0 of 10 packages |
| Average size of one test object | 772 bytes; of one criterion 380 bytes; of one task 1,942 bytes; of one step 206 characters |
| Scoped render a coder gets for one task (`specr --task N`) | 31 to 35 KB, about 8,000 tokens, of which the PRD is a little over half |
| Commits that touch `.specs/` | 87 of the repository's 183 |

Two numbers carry most of the argument. **Tests are criteria restated**: 404
tests for 442 criteria, one test per criterion in 93 % of cases, each test
twice the size of the criterion it verifies. **The PRD is most of what a
coder reads**: the scoped render carries the whole PRD unfiltered, so a coder
working on one task of five gets 16 KB of prose before the first criterion
it owns, and that prose already holds a `## Requirements` section saying in
sentences what the criteria say in fields.

## 2. Where the weight comes from

The format's design rule 1 is "one concept, one place". The packages break it
along one chain, and every link of the chain was put there deliberately.

One behaviour of spec 15, "a save that cannot write leaves the package as it
was", exists in the repository as:

1. a paragraph of the input PRD under `docs/prd/`;
2. a bullet in the package's `prd.md` under `## Requirements`;
3. criterion `15-REQ-1.4`, with a `condition`, an `action` and a 300-byte
   `contract`;
4. test `TS-15-4`, whose `given` restates the condition, whose `then`
   restates the contract item by item, and whose `pseudocode` says it a third
   time in code;
5. a step of task 1, which names the test and the behaviour again;
6. all of the above, rendered, in `spec.md`.

The chain is not an accident of generation. The PRD phase is told to write
`## Requirements` in prose "not EARS; that is generated later from this". The
test phase is told every criterion needs a test and every test needs
`given`, `when`, `then`, and is recommended `pseudocode`. The tasks phase is
told steps must be "concrete enough that a coder who sees only this task
needs nothing else". Each instruction is sound alone. Together they say the
same thing five times, and the C-rules then prove that the five copies agree.

The errata say what the extra copies buy. Every erratum of packages 10 to 13
is a divergence in the *most* specific layer: a file name in `touches`
(`envelope.go` where `Change` lived in `admin_result.go`), a line number in
a step, a test that was to call three `package main` entry points from one
process, a remark that a copy should be made "as `GetTask` does" when
`GetTask` did not. None is a divergence in a criterion or a contract. The
precision that drifts is the precision that was never a requirement.

## 3. Simplifications that keep the proof

Each item says what changes, why the validator loses nothing, and what it
costs. The validator proves four things today: every criterion is verified
(C4), every path is smoke-tested (C5), every test is owned (C7) and every
criterion is owned by an implement task (C8). The test of a simplification is
that these four stay provable.

### S1. A criterion carries its own check; the test list goes

The 1:1 ratio says the test is the criterion's verification clause, not a
separate thing. Make it one:

```json
{
  "id": "REQ-1.4",
  "pattern": "unwanted",
  "condition": "a save cannot write its temporary files",
  "action": "return a save error, leave every artifact as it was, write no spec.md",
  "contract": "returns *SaveError; every file byte-identical to before; no *.tmp.* left",
  "check": {
    "kind": "unit",
    "setup": ["a complete package on disk; the directory made unwritable"],
    "pseudocode": "in afspec/afspec_test.go beside TestSave_LeavesNoTempFiles: before = hashEveryFile(dir); err = Save(dir); assert errors.As(err, &SaveError); assert hashEveryFile(dir) == before"
  }
}
```

`given` is the criterion's `condition` plus setup; `when` is the `action`'s
trigger; `then` is the `contract`. The only fields a test adds that the
criterion does not have are `kind`, the setup that is not in the condition,
and `pseudocode`. Those three become `check`. The 7 % of criteria that share
a test are written as one criterion that lists `also_verifies` of its
neighbours, or, simpler, the shared check is written once and the others say
`check: {same_as: "REQ-1.2"}`.

Smoke tests belong to paths the same way: a path carries `smoke:
{real_components, end_state, pseudocode}`. The rule that a smoke test
asserts only the end of the path, which 18 of 35 smoke tests already break,
becomes the shape of the field.

What the validator proves afterwards:

| v2 rule | v3 |
|---|---|
| C3 every `verifies` resolves | gone; there is nothing to resolve |
| C4 every criterion has a test | schema: `check` is required |
| C5 every path has a smoke test, every smoke test a path | schema: `smoke` is required on a path |
| C7 every test is owned by a task | follows from C8, since the check is part of the criterion |
| C8 every criterion is owned by an implement task | unchanged |
| C9 the integration task owns every smoke test | the integration task owns every path (or see S6) |
| C11 `real_components` exactly on smoke | schema: it is a field of `smoke` |

Five cross-file rules become schema constraints or disappear. `test_spec.json`
disappears. One of three generation phases disappears, and with it the phase
that is given "the complete requirements artifact" and gives back a copy of
it. On the packages measured, the test artifacts are 23 to 60 KB each; the
checks would add perhaps a third of that to the requirements artifact.

What is lost: the test ids `TS-NN-N`. The steering already says ids stay out
of test names and comments and the traceability map lives in the commit; the
commit line would name the criterion (`REQ-1.4 → afspec_test.go:TestSave_…`)
instead of a test id that pointed at the criterion. The `af-impl` test-first
gate is unchanged: the coder writes the check, sees it fail, implements.

### S2. The package PRD stops restating the requirements

The package's `prd.md` is a copy of the input PRD with frontmatter. Its
`## Requirements` prose is the source the criteria were generated from, and
once they exist it is the one place a reader can find a sentence that the
validator does not see and the coder is not bound by. In the scoped render it
is read by every coder for every task.

Two options, in increasing strength:

- **Scoped renders drop the PRD's `## Requirements` and `## Background`.**
  No format change; the coder keeps Intent, Goals, Non-goals and Design
  Decisions, and the criteria it owns. The scoped render falls from about 33
  KB to about 26 KB for a spec-15 task.
- **The package PRD is written by the generator, not copied.** It keeps
  `## Intent`, `## Goals`, `## Non-goals`, `## Design Decisions` and
  `## Dependencies`; the prose requirements, the background and the verified
  API table stay in the input document, which `source:` in the frontmatter
  already points at. The hashed Intent stays what it is. A package loses 10
  to 15 KB, and the render shows a reviewer the decisions and the criteria
  without the draft they came from.

The second option changes what `af-prd` and the PRD phase write for a package
and is the better one; the first is a renderer change and could ship this
week.

### S3. Retire the fields nothing uses

| Field | Evidence | Proposal |
|---|---|---|
| `guard` on criteria | 14 of 442 | Fold into `condition` ("X and Y"). The `complex_event` pattern goes with it; the schema's per-pattern conditional shrinks to two cases: `condition` absent for `ubiquitous`, present otherwise. |
| `system` on criteria | 2 to 13 values per package; the criteria of one requirement nearly always share one | A `system` default on the requirement, overridable per criterion. Same sentences rendered, one value written per requirement instead of per criterion. |
| `optional` on tasks | 0 of 85 | Drop. A task that may be skipped is a task that should not be in the plan. |
| `dropped` task state | 0 of 85 | Keep: it is the state machine's only terminal exit, and the cost is one enum value. |
| `owner`, `tags` in frontmatter | 0 of 10 | Drop. `supersedes` stays, the lifecycle uses it. |
| `introduction` in requirements | duplicates `## Intent` | Drop. |
| `glossary` | documentation by the format's own account, not validated | Move to the PRD as a section, where documentation lives. The cross-spec rule that compares shared definitions (a warning) goes with it. |
| `depends_on` | 55 of 85 are the default | Keep, but have the generator omit the default, which the format already allows. |
| `spec_tests` | used | Keep. |

### S4. Shorter ids

Every id inside a package carries the package's prefix (`15-REQ-1.4`,
`TS-15-21`) and rule C2 checks that the prefix is the package's own. Nothing
references a criterion across packages; `dependencies` are at package
granularity by design (§8.2). Inside the package, `REQ-1.4`, `PATH-1` and
task `3` identify everything, and a renderer that needs a global name can
prefix it. C2 becomes a format check alone. This is small on bytes and large
on the number of places an id can be wrong by one character, which the
inline-repair loop of the generation contract pays for in regenerations.

### S5. Steps say what, `touches` says where

The task object is the largest item in the format at 1.9 KB on average, and
the errata show its steps are where the plan is most often wrong: line
numbers (`afspec/afspec_test.go:88`), file names the author guessed, and
instructions about how to copy a struct. Spec 18 is adding a review that
checks whether what `touches` names exists; the lighter measure is for the
format to ask for less:

- a step names a behaviour to add or a decision already taken, not an edit;
- `touches` is where the files are named, once per task, and the grounding
  check of spec 18 reads it;
- a step has no line numbers and no quoted code, which cannot be true for
  longer than one commit.

The Zero-Decision Rule that spec 18 adopts from EGM is sometimes read as
"maximum detail". It is the opposite constraint: no *design* decision left to
the coder. A step that says "add `RenderedBytes()` returning the header, a
blank line and the combined render" leaves no decision; the line number the
assertion lives at is not a decision, it is noise that will be wrong.

### S6. The integration task is derived

The last task is mandated in shape (kind `integration`, last, owns every
smoke test) and in content (four steps the template dictates: trace every
path, confirm every contract is consumed, grep `touches` for stub markers,
confirm foreign entry points are called). Rule 2 of the format says derived
data is not stored. The integration task is derived: the validator knows
every path, every contract, every `touches` list and every dependency, and
can render the checklist. `tasks.json` would then hold implement tasks only;
`specr --task last` or `specr --integration` renders the checklist; `speca`
keeps a state for it. C9 goes away as a rule on authored data, and the plan
loses its one task that the model writes the same way every time.

### S7. The validator stops reading words

Design rule 3 says validation never greps for words; the vague-word warning
does. Spec 18 gives the plan a reviewer with written rubrics. Move the
vague-word list into the reviewer's `prd` rubric and out of the validator.
The same goes for the glossary hint (backticked terms in three criteria),
which S3 removes with the glossary.

### S8. Contract and progress are separate files

A task's `state` is the only thing that changes in a package after
activation, and it lives in `tasks.json`, so every state move rewrites a
contract artifact, bumps `updated_at` in the PRD frontmatter, and re-renders
`spec.md`. The two packages implemented since the render landed show 7
commits each touching `tasks.json` and `spec.md`, each commit a two-line
diff of an 80 KB file.

Put progress in its own small file, `progress.json`: task states, and, if
the project wants it there, the map from criterion id to the test that
covers it, which the steering today puts in commit bodies. Then:

- the three contract artifacts are immutable from activation, and the
  intent hash can cover all of them instead of one PRD section, which makes
  "sealed" a property the files have rather than one the status says;
- `updated_at` moves to the progress file, and the PRD is never rewritten
  after activation;
- the compare-and-set claim in `speca` writes one 1 KB file;
- `spec.md` re-renders only when the contract changes, and the render can
  show progress from the progress file.

This is a format change with a migration, not a rule change, and it is the
one item here that improves correctness rather than only weight.

### S9. The actor rule gets something to match against

ADR 11 records that the one semantic cross-spec rule, "a downstream spec
shares a path actor with each spec it depends on", matches free-text names
and has rejected six packages for coining `agent-fox service` where another
package said `agent-fox`. Either drop the rule (the `reason` on a dependency
is the human-readable version of it) or give it a thing to match: a short
`actors` list in the spec root (or the steering's frontmatter), which paths
reference by name and the generator is shown. A registry of ten names is
cheaper than a rule that is right only when the model guesses a string.

## 4. What to take from the other kits

The comparison's list is being worked through. These are the items it did
not name, or named only in passing, that bear on the format rather than on
the process around it.

### A1. Priorities on requirements (Spec Kit)

Spec Kit's stories carry P1, P2, P3, and the P1 set is "independently
testable" as an MVP. The format has no notion of priority; every criterion
is equal and every task must be done. A `priority` on a requirement, with
the rule that a P1 requirement's tasks come before a P2's in `depends_on`
order, gives three things at once: `spec --budget` can stop after P1 and
still have written a complete smaller plan, `speca` can report "P1 done",
and a reviewer can see which third of the package the author would cut.

### A2. Open questions live in the package (Spec Kit, EGM)

Spec 17 writes an escalated finding into `## Design Decisions` as a
paragraph opening with "Provisional:", and `spec` reports `open_questions`
in its result envelope, which nobody stores. Spec Kit's `[NEEDS
CLARIFICATION]` markers are in the artifact and counted. Put them in the
package: an `open_questions` list in the PRD frontmatter, each with the
question, the provisional answer and whether a person has closed it, so
that `speca` can report a package with open questions, a reviewer can see
them in the render, and activation can warn on them. A convention in prose
is not reportable; a list is.

### A3. Extension fields (OpenSpec)

OpenSpec allows `x-*` keys anywhere for a team's own metadata (`x-jiraKey`).
Every v2 schema sets `additionalProperties: false`, so a team cannot add an
estimate, an issue key or an owner to a task without forking the format.
Allowing `x-` prefixed keys on every object costs one `patternProperties`
entry per schema and answers the comparison's "no customisation" finding at
the data layer, where it is cheapest.

### A4. A reviewer canary (EGM)

EGM proves its reviewers have no access to the author's context with canary
tests. The adversarial review here uses a fresh agent per phase, and the plan
review will too. A canary is cheap: put one fact in the author's context that
is nowhere in the PRD or the repository (a named decision, a number), and
fail the review run if a finding cites it. It turns "the reviewer is fresh"
from a design claim into something the test suite checks.

### A5. Grounded symbols, not only grounded paths (EGM)

EGM's bootstrap READMEs require a `[file:symbol]` citation on every claim.
Spec 18 computes whether every `touches` path exists. Extend the same
computation to `pseudocode`: a symbol it names (`TestSave_LeavesNoTempFiles`,
`RenderCombined`) is either found in the repository, or the step that
creates it is named, or the reviewer sees it as unfounded. With S1, the
check is on the criterion's own `check`, where the coder reads it.

### A6. Do not back-fill (OpenSpec)

OpenSpec's advice that specs "start nearly empty and accumulate" and that a
brownfield codebase should not be back-filled is worth writing into the
steering. A living view (O4 below) built only from packages written from now
on is honest; one that pretends to describe code written before the format
is the staleness every kit is criticised for.

### A7. A pre-commit validation (OpenSpec)

`openspec validate --archived` runs as a pre-commit hook. `speca --validate`
over every draft and active package is one line in a hook, and it is where
the `rendered` rule of spec 15 should run so that a stale `spec.md` never
reaches a pull request.

## 5. Further out

### O1. Two files

With S1 the JSON is requirements and tasks. Put them in one file, `spec.json`,
beside `prd.md`:

```
NN_name/
  prd.md          intent, goals, non-goals, decisions, dependencies; hashed
  spec.json       requirements with checks, paths with smoke checks, tasks
  progress.json   task states; written by speca, never by a person
  spec.md         the render, generated
```

Three identity headers (`$schema`, `spec_id`, `spec_name`, `schema_version`
in each JSON file) become one. Rule C1, that the identity agrees across
files, becomes a check against the folder name. The bootstrap mode of §3,
in which cross-file rules are deferred until all files exist, disappears
because there is one file. The all-or-nothing write of §3 is one rename.
Generation becomes two phases, requirements-with-checks and tasks, each
validated inline as today.

### O2. A lite profile

The comparison's loudest criticism of every kit is volume for small changes,
and it applies here most. Spec 13, which makes three commands name one
another, has 33 criteria, 34 tests, 65 KB of render. A `lite` profile for a
package of at most three requirements: no execution paths and so no smoke
checks and no integration task; checks required as in S1; tasks as today.
The validator's rules are the same rules minus the path rules; the profile
is a field in the frontmatter, chosen by the generator from the size the PRD
phase already estimates, and a package can be promoted to `full` by adding
paths. A one-requirement change costs one requirement.

### O3. Contracts a machine can run

98 % of tests carry pseudocode and 364 of 442 criteria carry a `contract`,
written in prose: "exit code 1; stdout is `{ok: false, error: string}`". For
the kinds of contract that recur in every package, the prose has a shape:

```json
"contract": {"exit": 1, "stdout": {"json": {"ok": false, "error": "string"}}}
"contract": {"error": "*SaveError", "files_unchanged": ["prd.md", "requirements.json"]}
"contract": {"status": 400, "body": {"json": {"error": "string"}}}
```

A structured contract for the three or four common shapes (exit code and
stdout for a command; status and body for an endpoint; an error type for a
library call), with prose as the fallback, gives two things the format does
not have. The coder's test asserts a thing the spec states, not a reading of
it. And `speca --drift` on a sealed package can run every command-shaped
contract against the built binary and report which criteria the code no
longer meets, which is the code-drift check the comparison lists as weakness
6, built from data the package already holds.

### O4. A living view from `system` and supersession

The `system` field on a criterion names the component that acts and is used
for nothing but the rendered sentence. Give it a job: `speca --living`
prints every criterion of every sealed package, grouped by `system`, minus
those a later package superseded. Supersession at criterion granularity is
one optional field, `supersedes: ["12-REQ-3.2"]`, on a criterion. That is
OpenSpec's delta spec with no merge step: the deltas are the packages, the
living spec is a query. If nobody reads it, nothing was built; if they do,
the merge can come later.

### O5. The render is the review surface, so render for review

`spec.md` is committed so that a reviewer reads one file. A reviewer does not
need the file in the order the JSON has it. A review render can lead with
what a reviewer decides on: the Intent, the decisions, the open questions
(A2), then each requirement with its criteria and their checks inline, then
the paths with their smoke checks, then the plan as a table (task, criteria,
files, depends on). That is the order S1 and O1 give the data anyway. With
S8, the render shows progress from `progress.json` without re-rendering the
contract.

### O6. A smaller generation, by construction

Putting the numbers together for a spec-15-sized package: S1 removes the
test artifact (26 KB) and adds checks to the criteria (perhaps 9 KB); S2
removes the prose requirements, background and API table from the package
PRD (about 9 KB); S3 and S4 trim fields and ids (2 to 3 KB); S6 removes the
integration task (3 KB); S8 moves state out. The package goes from 80 KB of
artifacts to about 45 to 50 KB, the render shrinks by about the same share,
and the generation emits one artifact fewer and restates nothing, which is
where the model spends its output tokens. The coder's scoped render goes
from 33 KB to something near 18 KB. These are estimates from the measured
sizes, not measurements.

### O7. What a v3 rule table would be

| | v2 | v3 with S1, S3, S4, S6, O1 |
|---|---|---|
| Files written by a person or the generator | 4 (+ render) | 2 (+ progress, + render) |
| ID families | 5 | 3: `REQ-N.C`, `PATH-N`, task `N` |
| EARS patterns | 6 | 5 (no `complex_event`) |
| Test kinds | 4 | 4, as `check.kind` and `smoke` |
| Cross-file rules | 11 | about 4: identity against the folder; `task.criteria` and `depends_on` resolve and form a DAG; every criterion owned by a task; every path's smoke check owned by the integration checklist |
| Generation phases | PRD, requirements, tests, tasks (+ architecture, reviews) | PRD, requirements, tasks (+ architecture, reviews) |

What the validator proves is unchanged: every criterion has a check, every
path has a smoke check, every check is owned by a task. Four of the proofs
move from cross-file rules into the schema, where a violation cannot be
written at all.

## 6. Order of work, and what to leave alone

**Do not change** the things the comparison names as the moat: that a check
is mandatory, that ownership is proven, that the coder is given a scoped
render, that task state is a state machine with a compare-and-set claim, and
that a package is sealed and archived rather than kept alive by hand. Every
item above keeps those; S1 and S8 strengthen them.

**First, without a format change:** scoped renders drop the PRD's prose
requirements (S2, first option); the generator omits default `depends_on`
and writes steps without line numbers (S5); the vague-word list moves to
the review rubric when spec 18 lands (S7); `x-` keys are allowed (A3); a
reviewer canary (A4). Each is a day or less and none breaks a package.

**Then, as v3 of the format:** one requirements file with checks on criteria
and smoke checks on paths (S1), progress in its own file (S8), the fields
of S3 and the ids of S4, the derived integration checklist (S6), and the
lite profile (O2). This is one migration, deterministic from v2 as the v1
migration was, and it should be done once rather than as four minor
versions that each re-render every committed `spec.md`.

**Measure before the rest.** Priorities (A1), stored open questions (A2),
structured contracts (O3) and the living view (O4) each add a field, and
the comparison's closing point stands: no kit has shown its method produces
better code. The cheapest experiment is already available in this
repository: regenerate one of the ten packages under the v3 rules by hand,
hand both renders to the same coder session for the same task, and count
what each asked, what each got wrong and what each cost. If the lighter
package does as well, the rest of the list has its case; if it does not,
the weight was buying something, and this document should say what.
