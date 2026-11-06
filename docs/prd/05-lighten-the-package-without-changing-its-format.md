# Lighten the package without changing its format

## Intent

A package says one behaviour up to five times, and every copy is something a
coder reads and the validator checks. `docs/spec-format-simplification.md`
measured the cost and proposed a v3 format. Before that migration, five
changes take weight and noise out of a package and its tooling without
breaking a package that exists. A coder's scoped render stops carrying the
PRD's prose requirements. The generator stops writing what the format
already defaults, and stops pinning steps to line numbers that are wrong
after one commit. The validator stops matching words, and the review rubrics
take that job. A team can carry its own metadata in a package. A test proves
that a reviewer never sees what the author saw. These are items S2 (first
option), S5, S7, A3 and A4 of that document, §6, "First, without a format
change".

## Goals

- `specr <package> --task N` no longer carries the PRD's `## Background`
  and `## Requirements` sections. It says once that they were left out and
  where they are. The unscoped render and `spec.md` are unchanged.
- The tasks a `spec` run writes carry no `depends_on` that only restates the
  default, the task before in the array.
- A step or a `touches` entry that names a line of a file (`afspec/afspec.go:88`,
  "line 88") is sent back to the tasks phase once, as wording is today, and the
  prompts ask for none.
- `afspec` validation no longer warns on vague words. The generation phase
  still nudges the model once about them, and the PRD review and the plan
  review rubrics both list them.
- Every object of the four v2 schemas accepts keys that begin with `x-`.
  `afspec` keeps them through a load and a save, so that a task move or a
  status move never deletes one.
- A test runs a whole adversarial run, PRD review and plan review, on a
  scripted model with canaries placed where only the author can see them,
  and fails if any reviewer or reviser request carries one.
- No package under `.specs/archive/` becomes invalid, and no committed
  `spec.md` changes.

## Non-goals

- Any part of the v3 format: checks on criteria, one requirements file,
  progress out of `tasks.json`, the derived integration task, a lite profile.
  Those are §6 "Then, as v3", one later migration.
- Removing `## Background` and `## Requirements` from the package PRD
  itself (S2's second option). The PRD on disk is unchanged; only the
  scoped render leaves the two sections out.
- Rendering `x-` keys. They are a team's data for its own tools, not the
  format's, and the render does not show them.
- A canary at run time. Putting a false fact into a real author's context
  costs tokens on every run and risks the author writing it into the PRD.
  The canary is a test of the code that builds the reviewer's request,
  which is where isolation is decided.
- Changing which steps of a run are taken, their models, or their cost.

## Background

### What a coder reads

`specr --task N` calls `(*Spec).RenderIndividualScoped` in
`afspec/render.go`. Its `prd` part is the frontmatter and the whole PRD body.
In the ten packages written under v2, the scoped render of one task is 31 to
35 KB, a little over half of it the PRD. The PRD's `## Requirements` is the
prose the criteria were generated from, and `## Background` describes the code
before the change. A coder working on one task gets both before the first
criterion it owns. The criteria it owns are rendered in full in the same
document.

### What the generator writes

`depends_on` absent means "the task before in the array"
(`afspec/task.go`, `ready`; `cmd/speca/plan.go`, `openDependencies`).
Of 85 tasks in the ten packages, 42 carry a `depends_on` that is exactly the
task before. A step averages 206 characters. The errata of specs 10 to 13
are about the most specific layer of a plan: file names, line numbers
(`afspec/afspec_test.go:88` in spec 15's task 1), how to copy a struct.
None is about a criterion.

### What the validator greps

`afspec/validate.go` matches criterion fields against a list of words
(`vagueLanguageRe`) and warns `vague_language`. Design rule 3 of the format
says validation never greps for words. `specgen/artifacts.go` sends the
warnings `vague_language`, `missing_contract` and `scope_limit` back to the
generating phase once, so the model can fix the wording (ADR 04). The plan
review of spec 18 now exists, with a `zero_decision` rubric whose question 4
already names open words in a step.

### What a team cannot add

Every object in the v2 schemas sets `additionalProperties: false`. The Go
types in `afspec/*.v2.go` are generated from the schemas, and `encoding/json`
drops a key they have no field for. `LoadSpec` records such a key so that
`ValidateSchema` reports it as `unknown_field`, and a save writes the package
without it. `speca --task` warns before a save that would delete one
(`cmd/speca/task.go`, `droppedKeys`). A key in the PRD frontmatter is dropped
the same way. OpenSpec allows `x-*` keys for a team's own metadata, such as
an issue key.

### What the reviewer sees

A review phase is a fresh conversation (`specgen/phases.go`, `Review`,
`ReviewPlan`, `Revise`, `RevisePlan`). The PRD reviewer is sent the input,
the PRD and the author's codebase notes. The plan phases are never sent the
input or `--context` (ADR 17). Nothing checks either rule: a change that
passes the author's transcript, or the input to the plan reviewer, would go
unnoticed.

## Requirements

### 1. The scoped render leaves out the PRD's prose requirements

- In the render of one task, the PRD body has no `## Background` section
  and no `## Requirements` section. A section runs from its heading line to
  the next level-two heading, or the end of the body. A `## ` line inside a
  fenced code block is not a heading.
- In the place of the first section left out, the render has one line:
  `_The PRD's Background and Requirements are not in a task's render: the
  criteria this task owns are below. Read them in prd.md or spec.md._`
  The line names only the sections that were there.
- A PRD with neither section renders as before, with no line added.
- The frontmatter, `## Intent`, `## Goals`, `## Non-goals`,
  `## Design Decisions`, `## Dependencies`, `## Verified External API`, and
  any other section are rendered as before.
- The unscoped render, `spec.md`, and `specr` without `--task` are byte for
  byte as before. No committed `spec.md` changes.
- A task id that names no task falls back to the unscoped render, as before.

### 2. The generator omits the default dependency

- When the tasks phase or a plan revision submits a task whose `depends_on`
  is exactly one id, and that id is the id of the task before it in the
  array, the run removes the field before it validates the artifact.
- A `depends_on` that lists several ids, an id other than the previous
  task's, or an empty list, is left as it was. The first task is left as
  it was.
- The tasks prompt says to omit `depends_on` when it is the task before.
- A package on disk is never rewritten for this; a save writes what it
  loaded.

### 3. Steps without line numbers

- The tasks prompt and the plan revision prompt say that a step names a
  behaviour or a decision, not an edit, that files are named in `touches`,
  and that no step or `touches` entry names a line of a file.
- When the tasks phase submits an artifact whose steps or `touches` name a
  line of a file, the submission is sent back once, beside the warnings it
  is already sent back for, as `line_reference`, naming the task, the step
  and the reference. The next submission is accepted whatever it still
  holds, as for every warning sent back.
- A line reference is a path with an extension followed by `:` and digits
  (`afspec/afspec.go:88`, `render.go:12-30`), or the word `line` or `lines`
  followed by a number.
- The plan review's `zero_decision` rubric says that a line number or a
  quoted block of code in a step is not a decision the plan made, and that
  the finding is `minor` unless a test depends on it.
- This check is the generator's, not the format's: `afspec` validation does
  not report it, and `speca --validate` does not show it.

### 4. The vague-word list moves to the review rubrics

- `afspec` validation no longer emits `vague_language`. `speca --validate`,
  `speca --list` and the result of a `spec` run no longer carry it.
- The generation phase still sends the requirements artifact back once when
  a criterion's `condition`, `guard` or `action` holds one of the words, as
  `vague_language`, with the same message as today. The check lives in the
  generator beside `line_reference`.
- The PRD review's rubric and the plan review's `zero_decision` rubric list
  the words. A vague word is a finding when two readers would build
  different things from it; the word alone is not.
- The words are those of today's list: appropriate, properly, correctly,
  reasonable, relevant, adequate, suitable, as needed, if necessary, etc.

### 5. Extension keys

- Every object of `requirements.v2.json`, `test_spec.v2.json`,
  `tasks.v2.json` and `prd-frontmatter.v2.json` accepts any key that begins
  with `x-`, with any JSON value. The glossary is a map of terms, not an
  object with fields, and is not changed.
- `LoadSpec` keeps every `x-` key of the three JSON artifacts and of the
  PRD frontmatter. `ValidateSchema` does not report one as `unknown_field`.
- Every save writes each kept key back on the object it was on, after that
  object's own fields, in key order. An object in an array is found by its
  `id` when it has one (a requirement, a criterion, a path, a test, a task)
  and by its position otherwise. A key on an object that a mutation removed
  is not written.
- A package loaded and saved with no change is byte for byte what it was,
  its `x-` keys included, when its files are in the library's encoding.
- In the PRD frontmatter, a kept key is written after the format's fields,
  in key order, with its value as compact JSON, which is valid YAML.
- `speca --task` and `speca --status` no longer warn that a save would drop
  an `x-` key, since it does not.
- A key that begins with `x-` in an artifact a model submits during
  generation is refused, by its path, as an unknown key is today: the
  generator writes no extension keys.
- `afspec.UnknownFields` does not list an `x-` key.
- The render does not show `x-` keys.

### 6. A canary proves the reviewers are fresh

- A test runs `Run` with `--adversarial` and `--adversarial-plan` on the
  scripted provider, through the real phases, with one finding in each
  review so that both revision phases run.
- The author's PRD phase writes a canary in its own reply text before it
  submits. The canary is in no file, in no prompt and in nothing the PRD
  phase submits.
- The input carries a second canary, and `--context` a third.
- The test fails if any request of `prd_review` or `prd_revision` carries
  the first canary, or any request of `plan_review` or `plan_revision`
  carries any of the three. It also fails if the canaries never reached
  the requests where they belong, so that the test cannot pass by placing
  them nowhere.

### 7. Documentation

- `specification/spec-format-v2.md`: the scoped render in §11.1 and its
  exception in §11.2; the extension keys; the vague-word rule moved out of
  the validator in §6.5 and §10.2; the generation instructions on
  `depends_on` and on steps in §8.
- `docs/specr.md` and `docs/cli.md` where they describe the scoped render
  and the warnings.
- `docs/api.md`: `UnknownFields` and the kept extension keys.
- `docs/architecture.md` where it describes the render and the save.
- The skills: `af-spec`'s verdict reference no longer names
  `vague_language`, and the shared PRD shape says the review, not the
  validator, flags the words. Every skill body stays within the 24,576-byte
  ceiling.
- An ADR for the extension keys and the vague-word move.
- `make doc` for the changed doc comments of exported identifiers.

## Design Decisions

1. **Only the scoped render changes.** `spec.md` is what a reviewer reads in
   a diff, and the reviewer needs the reason behind each requirement. A coder
   on one task has the reason in `## Intent`, `## Goals` and
   `## Design Decisions`, and the requirements as criteria. Leaving the two
   sections out of the scoped render alone means no committed `spec.md`
   changes, and the PRD on disk keeps what a person wrote.
2. **The sections are matched by their exact headings.** `spec`'s PRD phase
   and the shared PRD shape write `## Background` and `## Requirements`
   exactly. A PRD written another way keeps its sections in the render,
   which costs tokens and loses nothing.
3. **The left-out sections are named in the render.** A scoped render names
   by id what a task does not own (§11.2); the same rule holds for the PRD.
   A coder who needs the prose knows where it is.
4. **The default dependency is dropped by the run, not by the model alone.**
   The prompt asks for it, but the rule is mechanical, the meaning is
   identical, and 42 of 85 tasks show the model writes it anyway. Removing it
   is not a judgement, so the run does it.
5. **Line references are a generator hint, not a validation rule.** A step
   that names a line is a plan that will be wrong after one commit, but it is
   not an invalid package, and design rule 3 keeps word matching out of the
   validator. The hint costs one turn, which is what the warnings sent back
   today cost.
6. **The vague-word nudge stays in the generator.** Removing the validator
   warning removes the only signal a run without `--adversarial-plan` had.
   The generator keeps the same one-turn nudge from its own check, so a run
   behaves as before, and the validator matches no words. The review rubrics
   carry the list for the runs that ask for a review.
7. **Extension keys are kept beside the typed artifacts, not in them.** The
   artifact types are generated from the schemas and must stay so. The
   library records each `x-` key with the address of its object and the
   writer puts it back. An id addresses an object better than a position,
   because the library's mutations add and remove objects that have ids.
   Objects without one (a path step, an external API, a dependency) are not
   added or removed by any library call.
8. **A model may not write an extension key.** The generator is never
   shown them, so one in its output is a mistake, and refusing it by path
   is what the generator does with every other key the schema does not
   name.
9. **Extension values in the frontmatter are written as compact JSON.** The
   frontmatter renderer is hand-written so that a save is byte-stable.
   JSON is YAML, so one encoding writes every value deterministically
   without a YAML marshaller.
10. **The canary is a test of the request, not of the model.** Whether a
    reviewer is fresh is decided by the code that builds its request. A
    scripted run that checks every request the provider received proves
    that for every path the run takes, including both revision phases.

## Dependencies

| Spec or document | Reason |
|---|---|
| `docs/spec-format-simplification.md` | The source of the five items, §3 S2, S5, S7 and §4 A3, A4. |
| Archived spec 15 | `spec.md` is the unscoped render, which this change leaves as it is. |
| Archived spec 17 and 18 | The PRD review and plan review prompts gain the vague-word list; the canary test drives both stages. |
| ADR 04 | The warnings sent back once to a generating phase, which gain `line_reference` and keep `vague_language`. |
| ADR 11 | `UnknownFields` and the unknown-key report, which skip `x-` keys. |

## Verified External API

None. The change uses the standard library, `goccy/go-yaml` as `afspec`
already does, and the agentkit-go faux provider as the tests already do.
