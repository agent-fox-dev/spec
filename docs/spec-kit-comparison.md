# The agent-fox spec format compared with spec-kit, OpenSpec and EGM

A comparative analysis of this repository's spec kit (the format in
`specification/`, the `afspec` library, the `spec`, `specr` and `speca`
commands and the `af-prd`, `af-spec` and `af-impl` skills) against three
other kits for spec-driven development with coding agents:

- **GitHub Spec Kit** — <https://github.com/github/spec-kit>
- **OpenSpec** (Fission AI) — <https://openspec.dev>, <https://github.com/Fission-AI/OpenSpec>
- **Elephant-Goldfish Model, EGM** (Google) — <https://github.com/google/elephant-goldfish-model>

Its purpose is a decision: improve this kit, borrow from the others, or
drop it. The last section gives the recommendation.

The three repositories were cloned on 2026-10-28 and their templates,
command and skill files, validators and docs were read directly. Star
counts and release dates are from their GitHub pages on that day. Several
third-party reviews (martinfowler.com, Scott Logic, Help Net Security, a
few dev.to and blog posts) were not reachable from the research
environment and are cited from search snippets; they are marked as such.

## 1. What each kit is

The four kits are not four answers to one question. They are four
different kinds of thing that happen to share the words "spec-driven".

| | agent-fox spec (this repo) | GitHub Spec Kit | OpenSpec | EGM |
|---|---|---|---|---|
| Kind of thing | A **machine-validated contract format** plus a generator and admin commands | A **process harness**: prompt templates and a scaffolding CLI | A **change workflow** over a living spec, with a structural validator | A **design-doc and adversarial-review protocol** |
| Maker, licence | agent-fox-dev, Apache-2.0 | GitHub, MIT | Fission AI, MIT | Google ("not an officially supported Google product"), Apache-2.0 |
| Age, activity | First commit 2026-09; `afspec` moved here 2026-10-13; no release tags | First commit 2025-08; 1.0.0 2026-08-21; v1.2.4 on 2026-10-27, releases every 3–5 days | v1.15.0 on 2026-10-24; weekly releases | Announced 2026-10-22; v0.1.0 prerelease 2026-10-27 |
| Stars (2026-10-28) | — | ~143k | ~73k | ~3.9k |
| Implementation | Go, ~24k lines plus ~24k lines of tests | Python CLI (`specify`); the method is Markdown prompts | TypeScript CLI (`openspec`), also standalone binaries | Markdown skills and personas; Python only for the kit's own tests and packaging |
| Artifact formats | `prd.md` (YAML frontmatter), `requirements.json`, `test_spec.json`, `tasks.json`, optional `architecture.md` | Markdown only: `constitution.md`, `spec.md`, `plan.md`, `research.md`, `data-model.md`, `contracts/`, `quickstart.md`, `tasks.md`, `checklists/*.md` | Markdown only: living `specs/<capability>/spec.md`, per change `proposal.md`, delta `specs/`, `design.md`, `tasks.md`, plus `.openspec.yaml` and `config.yaml` | Markdown only: `concepts_brief.md`, `docs/designs/<feature>.md`, review reports, drift reports, bootstrap READMEs |
| Where the model runs | Inside the kit: `spec` drives Claude itself (Anthropic API, Vertex, Bedrock) | In the user's agent; the CLI never calls a model | In the user's agent; the CLI never calls a model | In the user's agent; the kit needs a harness with subagents |
| Agents supported | Claude Code skills; the generator is Claude-only | 43 integrations rendered from one command source | 40 tool targets, skills and commands | Antigravity, Claude Code, Codex; Cowork, ChatGPT Work and omp partially |

### 1.1 This repository

One feature is one *package* under `.specs/NN_name/`. The PRD is prose
with a hashed `## Intent`. Requirements are EARS criteria in six patterns,
decomposed into fields, plus end-to-end *execution paths*. Tests are one
flat list with a `kind` and a `verifies` link; tasks are one flat list
owning criteria and tests, with steps, touched files, dependencies and a
state. JSON Schemas validate each file; eleven cross-file rules (C1–C11)
prove that every criterion is tested, every test is owned by a task, every
path has a smoke test and the last task is the integration task; cross-spec
rules check dependencies, cycles and shared actors. The lifecycle is
`draft → active → sealed` with `superseded` and `archived`, and the task
state machine is `pending → in_progress → done`, with `dropped`.

Three commands share one JSON envelope: `spec` writes a package
unattended from a file, an issue URL or text, in phases on a Claude model
with per-phase model choice, budgets, a preflight estimate, a split into
several packages and an optional adversarial review of the PRD; `specr`
renders a package whole, scoped to one task, or cut to a token budget;
`speca` reports a package's state and verdict, lists packages, moves tasks
through the state machine and moves a package through its lifecycle.
Three Claude Code skills wrap the steps for a person in a session.

### 1.2 GitHub Spec Kit

`specify init` installs slash commands (`/speckit.constitution`,
`specify`, `clarify`, `plan`, `checklist`, `tasks`, `analyze`,
`implement`, `converge`) into the agent's own format, with templates and
helper scripts under `.specify/`. A feature lives in
`specs/NNN-short-name/`. The spec template has prioritised user stories
(P1, P2, P3, each "INDEPENDENTLY TESTABLE"), Given/When/Then acceptance
scenarios, `FR-001`-style functional requirements with a budget of three
`[NEEDS CLARIFICATION]` markers, and technology-agnostic success criteria.
The plan has a **Constitution Check** gate against a versioned,
project-level `constitution.md`. Tasks are checkboxes `- [ ] T012 [P]
[US1] …` grouped by user story, with `[P]` for parallel. `/speckit.analyze`
is a read-only, LLM-run consistency and coverage report; `converge`
compares code with the artifacts and appends tasks. The CLI never parses
`spec.md`, `plan.md` or `tasks.md`; there is no schema for them.

### 1.3 OpenSpec

`openspec/specs/<capability>/spec.md` is the living description of what
the system does now: `## Purpose`, `## Requirements`, each
`### Requirement:` written with SHALL or MUST and at least one
`#### Scenario:` in Given/When/Then. A change under `openspec/changes/<name>/`
holds a `proposal.md`, a `design.md` when needed, a `tasks.md` of checkboxes
and **delta specs** with `## ADDED`, `## MODIFIED`, `## REMOVED` and
`## RENAMED Requirements`. `/opsx:propose` writes the change, `/opsx:apply`
implements it, `/opsx:archive` validates, merges the deltas into the
living specs and moves the change under `changes/archive/YYYY-MM-DD-<name>/`.
`openspec validate` is a structural check of the Markdown (sections
present, scenario per requirement, SHALL/MUST, delta headers that match the
living spec, no scenario lost in a MODIFIED block). The artifact graph is a
customisable *schema* (`schema.yaml` with templates, instructions and a
dependency DAG); only `spec-driven` ships, community schemas add ADRs or
TDD.

### 1.4 EGM

EGM is a plugin of seven skills and nine reviewer personas. The
*Elephant* is the context-rich author; the *Goldfish* are reviewers spawned
as fresh subagents with no access to the author's context, read-only tools
and no `CLAUDE.md`, so that a review is not a confirmation. `/egm-brainstorm`
writes a concepts brief through one-question-at-a-time dialogue;
`/egm-draft` writes a design doc section by section, each behind an
approval gate, with the rule that the owner, never the model, picks the
architecture; `/egm-review` runs five sequential stages (zoom out,
comprehension test, critic review, intermediate zoom out, implementation
readiness) against five written rubrics, and `/egm-resolve` triages every
finding as CONTEST, ESCALATE, PROPOSE FIX or DECLINE. A review loop
converges on two consecutive rounds with no unsettled Blocker or Major,
capped at five rounds. `/egm-drift-report` lists where code and design
docs disagree; `/egm-bootstrap` writes README contracts for an existing
tree. There is no implementation skill, no state, no schema for the design
doc, and tests are a prose "Verification Plan".

## 2. Feature comparison

| Dimension | agent-fox spec | Spec Kit | OpenSpec | EGM |
|---|---|---|---|---|
| Requirement syntax | EARS, six patterns, fields not prose; `contract` required on `unwanted` | `FR-###: System MUST …`, free prose | `### Requirement:` with SHALL/MUST and `#### Scenario:` Given/When/Then | Goals, Non-goals, CUJs in Given/When/Then, prose |
| Machine validation of the spec | JSON Schema per file; C1–C11 cross-file rules; cross-spec rules; warnings for scope and vague words | None. The CLI never reads the artifacts; `/speckit.analyze` is an LLM report | Structural, over a Markdown parse (zod): sections, scenario presence, SHALL/MUST, delta/live consistency; `--strict` turns warnings into errors | None for the user's documents; the kit tests its own skills |
| Requirement → test → task traceability | Enforced and derived: every criterion `verifies`-linked, every test owned by a task, matrix computed, never stored | Inferred by the model from `FR-###`, `[US1]` labels and key phrases in `analyze`; "no tooling, no metadata schema" | None; `/opsx:verify` searches for tests per scenario heuristically and "won't block archive" | None; findings are traced to action items inside a review report |
| Tests | First-class artifact, four kinds, smoke test per execution path, integration task required, TDD mandated by `af-impl` | "Tests are OPTIONAL - only include them if explicitly requested"; TDD when the constitution says so | Not mandated; each task "MUST state how to verify completion" | Not mandated; a Verification Plan section, flagged by one reviewer if missing |
| Lifecycle of a spec | `draft → active → sealed`, `superseded`, `archived`; intent hashed at activation; writes refused on drift | `**Status**: Draft` that nothing updates; three "persistence models" left to the team | Filesystem: in `changes/` or `changes/archive/`; the living spec accumulates | `Status: Draft/Proposed/In Review/Approved` by hand; no archive step |
| Task state | State machine with compare-and-set claims through `speca`; readiness from `depends_on` | Checkboxes `[ ]` → `[X]` | Checkboxes; only `[x]` counts | None |
| What a coder is given | `specr --task N`: the task's criteria, tests and paths in full, the rest by id; `--max-tokens` drops detail | The whole `tasks.md`, `plan.md`, `spec.md` and design files | `openspec instructions apply --json`: tasks, context files, progress | The design doc; "no Skill.md is required here because the design document is complete" |
| Human clarification | `af-prd` asks gap questions in batches of three or four; `spec` itself asks nothing and records decisions | `/speckit.clarify`: taxonomy-driven, at most five questions, one at a time, with a recommended option | `/opsx:explore` as a thinking partner; proposals written by the agent | Brainstorm with open-ended questions one at a time; section approval gates; "Owner Decides Architecture" |
| Review of the spec | `--adversarial`: a fresh reviewer and a fresh reviser on the PRD, 1–5 rounds, stop when nothing critical or major | `/speckit.analyze` (read-only report), `checklist` ("unit tests for English") | `openspec validate`; `/opsx:verify` | The core of the kit: five stages, seven personas, five rubrics, CONTEST/ESCALATE/PROPOSE FIX/DECLINE, convergence on two clean rounds |
| Project-level rules | `.specs/steering.md`, fed to every phase and skill, not versioned, not checked | Versioned `constitution.md` with a Constitution Check gate and semver amendments | `config.yaml` `context:` and per-artifact `rules:` injected into every planning call | Project "elephant" skill with context; rubrics are the kit's, not the project's |
| Living description of the system | None by design: after implementation "the code, its tests and the ADRs are the record; the spec is archived" | None; "flow-forward", "living spec" or "flow-back" are the team's choice | Yes: `openspec/specs/` is the source of truth, built up by merged deltas | Design docs stay as "checked-in architectural memory"; drift report keeps them honest |
| Drift between code and spec | Intent drift (PRD edited after activation) is detected; code drift is not, except in `af-impl`'s conformance pass at PR time | `/speckit.converge` compares code to the artifacts and appends tasks | Process only: "update the delta spec to match what you shipped" | `/egm-drift-report`, meant as a weekly ritual |
| Scaling a feature | A PRD past 10 requirements is split into several packages, generated as a pipeline | One feature directory; stories give MVP slices | At most 10 deltas per change (warning); one change per branch | One design doc per feature |
| Unattended operation | Yes: `spec <issue-url>` from idea to validated, activated package, with budgets and a preflight estimate; `--comment` posts the PRD back | No; every command runs in a chat | No; the agent drives the CLI | No; the skills are gated by human approval at every section |
| Generating from an issue | GitHub and GitLab issue URLs read with comments | `taskstoissues` writes issues from tasks (now an extension) | `x-*` metadata such as `x-jiraKey` | No |
| Customising the method | Not possible: one format, prompts embedded in the binary | Template overrides, presets, extensions with before/after hooks, workflows, bundles | Fork or write a `schema.yaml` with its own artifacts and DAG | Reference files per skill; personas and rubrics are the kit's |
| Install | `git clone && make build`; `go install` does not work while `go.mod` carries a `replace` for untagged agentkit-go; every `make` fetches agentkit-go `main` | `uv tool install specify-cli`; assets bundled, offline capable | `npm i -g`, `brew install`, curl installer, Nix, binaries | `claude plugin marketplace add …`; `agy plugin install …`; Codex plugin |
| Library API | `afspec`: load, validate, mutate, save, render, lifecycle, task states; `envelope`: the commands' output as Go types | No | No public API; `--json` on CLI commands is the contract | No |

## 3. Where this kit is unique or stronger

1. **It is the only one that can say a plan is complete.** The C-rules
   make "every criterion is tested, every test is owned by a task, every
   path is smoke-tested" a property the validator proves, and a package
   that breaks one is invalid, not a warning. Spec Kit's coverage table is
   what a model inferred from key phrases; OpenSpec checks that sections
   exist; EGM checks nothing. The v2 rationale (§0 of the format) is exactly
   the failure the others cannot see: in every archived v1 spec, the
   edge-case and property tests were owned by no task, "so no coder ever
   implemented them".
2. **Tests are a contract, not an option.** A test spec with kinds,
   Given/When/Then and pseudocode, smoke tests that name the
   `real_components` that may not be mocked, an integration task that must
   own every smoke test, and a skill that refuses to implement a task
   whose tests it has not seen fail. Spec Kit makes tests opt-in; OpenSpec
   and EGM leave them to prose.
3. **State is a state machine, not a checkbox.** A `pending →
   in_progress` move through `speca` is a compare-and-set, so two agents
   cannot both claim a task; readiness follows `depends_on`; activation
   hashes the intent and every later write is refused while the intent
   differs. The others track progress in `[x]` marks and a `Status:` line
   nobody updates.
4. **Context is engineered for the coder.** `specr --task N` gives one
   task in full and the rest by id, under a token budget if asked. The
   other kits hand the agent whole files, and their own criticism is that
   "LLMs don't degrade gracefully with blob specs" (Spec Kit discussion
   #1784).
5. **It runs without a person.** `spec` goes from an issue URL to an
   activated package with budgets, a preflight cost estimate, per-phase
   models, a split into several packages run as a pipeline, retries of a
   failed stream and a resumable plan; the result is one JSON object with
   a `next` call. None of the others has an unattended mode at all.
6. **Cross-spec rules.** Dependencies that must exist, a DAG, shared
   actors between a spec and the specs it depends on. The others have no
   notion of a relation between specs beyond a directory listing.
7. **A library and a documented envelope.** An embedder gets `afspec` and
   `envelope`; a framework gets `--schema`. The other kits are CLIs and
   prompts with no API surface.
8. **Engineering discipline around it.** Eleven ADRs, errata for every
   divergence between a spec and its implementation, a steering file that
   fixes how tests are organised and how traceability is recorded in
   commits. The others document their workflow; this repository documents
   its decisions.

## 4. Where this kit is weaker

1. **Reach.** The generator is Claude-only and the skills are Claude Code
   only. Spec Kit renders one command source into 43 agent formats,
   OpenSpec into 40, EGM into three with more under way. Anyone on
   Cursor, Copilot, Codex or Gemini cannot use this kit without rewriting
   its skills, and `spec` cannot run on another vendor's model at all.
2. **Installation and distribution.** A Go toolchain, a clone, `make build`
   that fetches agentkit-go's `main` on every target, no release tags, and
   `go install` refused because of the `replace` (ADR 01, ADR 06). `specr`
   and `speca` link the whole model stack they never use (ADR 05, ADR 10).
   The others install with one package-manager command. The format itself
   is still labelled "Version 2.0 — Draft".
3. **Weight per feature.** The four live packages under `.specs/` are
   88–140 KB each. Spec 13, whose job is to make three commands name one
   another, has a 17 KB PRD, 43 requirement and criterion ids, 34 tests
   and 6 tasks. A PRD phase alone has cost $3.54 and ten minutes (ADR 08),
   before three generation phases and any adversarial rounds. The v2
   format was written to cure v1's 65–100 KB artifacts; the cure is
   partial. Spec Kit's loudest criticism, a "sea of markdown documents"
   that is "overkill for small changes" (Scott Logic, discussion #1784,
   via snippets), applies here in JSON.
4. **Humans cannot review the artifacts directly.** Three of the four
   files are JSON that only `specr` turns into something readable, so a
   pull request that adds a package is reviewed either through a rendered
   copy nobody commits or not at all. All three other kits are Markdown
   for the stated reason that a reviewer reads it in the diff.
5. **No description of the system as it is.** A package describes one
   change and is then sealed and archived; the steering says the code,
   tests and ADRs are the record. OpenSpec's whole premise is the other
   half: `openspec/specs/` says what the system does now, and every change
   is a delta to it. A new agent joining this repository can read the
   ADRs and `docs/generated/`, but no requirement-level statement of
   current behaviour exists, and the sealed packages contradict one
   another where later ones changed what earlier ones specified.
6. **Code drift is not detected.** Intent drift (someone edited the PRD)
   is; code that no longer does what a sealed package says is not, except
   during `af-impl`'s conformance pass on its own branch. EGM runs a drift
   report as a ritual; Spec Kit's `converge` turns the gap into tasks.
7. **Validation is structural, and partly lexical after all.** The C-rules
   prove that the plan covers the spec; they cannot say the spec is right.
   The one semantic rule across specs, the shared actor, "is a match of
   free-text names, which the model must now reuse exactly" (ADR 11) and
   has already rejected six packages in agent-fox for coining `agent-fox
   service` where another spec said `agent-fox`. The vague-word warning
   greps for words, which design rule 3 of the format says validation
   never does. EGM's rubrics ask the questions a structural check cannot:
   is the root problem stated, is one path proposed where alternatives
   exist, does the implementer have to make design decisions.
8. **Only the PRD is reviewed adversarially.** `requirements.json`,
   `test_spec.json` and `tasks.json` are checked by the validator alone,
   and a plan that validates can still name a module that does not exist
   or a test that proves nothing. EGM's implementation-readiness stage,
   with its Zero-Decision Rule and a codebase-details reviewer, is aimed
   at exactly that artifact.
9. **Governance is weaker than a constitution.** `steering.md` is the
   equivalent of Spec Kit's constitution and is already fed to every phase
   and skill, but it has no version, no amendment record and no gate that
   checks a package against it. A rule that is read is not a rule that is
   enforced.
10. **No customisation.** The prompts are embedded in the binary; the
    format is fixed; there is no override, preset, hook or schema a team
    can add without a fork. Spec Kit has five extension primitives;
    OpenSpec lets a team describe its own artifact graph.
11. **The skills are the largest of the four.** `af-spec` is 40 KB,
    `af-impl` 35 KB, `af-prd` 18 KB, each loaded into the session that
    runs it. OpenSpec's 10 KB `propose` skill drew a 53 % score for
    verbosity in a Tessl review; EGM's harnesses cap a skill body at 24 KB.
12. **A one-person surface.** About 48 k lines of Go, a dependency pinned
    to another untagged module of the same author, no external users, no
    community catalog. The others are maintained by teams and release
    weekly; their defects are found by thousands of users, this kit's by
    its author.

## 5. What is unique to each of the others

**Spec Kit.** The constitution as a versioned governing document with a
Sync Impact Report and a gate in every plan. Budgets that shape the
model's behaviour: at most three `[NEEDS CLARIFICATION]` markers, at most
five clarify questions, one at a time, each with a recommended answer.
Checklists as "unit tests for English" with `CHK###` items that cite a
spec section. Independently testable P1/P2/P3 stories that give an MVP
slice, with `[P]` for parallelisable tasks. A read-only `analyze` and an
append-only `converge`. An extension system with before/after hooks,
presets and bundles, and a feature pointer file that makes git optional.

**OpenSpec.** Delta specs (`ADDED`, `MODIFIED` with the full new text,
`REMOVED` with a reason and a migration, `RENAMED` with FROM/TO) merged
deterministically into living capability specs when a change is archived.
Specs that "start nearly empty and accumulate", with the explicit advice
not to back-fill a brownfield codebase because those specs "go stale".
A forkable artifact DAG (`schema.yaml`) where dependencies are "enablers,
not gates". `openspec instructions <artifact> --json`, a documented
contract that injects project context and rules into every planning
call. `validate --archived` as a pre-commit hook; a separate planning
*store* repository.

**EGM.** Reviewers that provably have no access to the author's context,
with canary tests to prove it; a persona × rubric matrix; a triage
vocabulary where a CONTEST must cite evidence the reviewer could have
read, a comprehension gap cannot be contested and a severity cannot be
changed; convergence defined as two consecutive clean rounds because a
single clean round is sampling noise. "Owner Decides Architecture": the
model presents up to three options and refuses to pick even when told to.
The Zero-Decision Rule for implementation plans. A drift report in both
directions, and bootstrap READMEs whose every claim must carry a
`[file:symbol]` citation.

## 6. What the others are criticised for, and whether it applies here

| Criticism of the others | Source | Applies to this kit? |
|---|---|---|
| Volume of documents, overkill for small changes, "a tool for a product owner" | Spec Kit #1784, Scott Logic (snippets) | Yes, more so: JSON of 90–140 KB per feature and a paid model run |
| No enforcement; everything depends on the model following the prompt | Comparisons of Spec Kit; OpenSpec's `anvil` README ("OpenSpec only checks that artifacts exist") | No. This is the one thing the kit does that the others do not |
| Validation structural only; verify step advisory | OpenSpec | Partly: structural rules are strong, semantic review covers only the PRD |
| Specs go stale; drift unsolved | Spec Kit docs, OpenSpec docs, dev.to "renamed an old problem" | Yes for code drift; the kit sidesteps staleness by archiving, at the price of no living spec |
| Brittle Markdown parsing ("fails silently" on a wrong heading level) | OpenSpec `schema.yaml` | No; JSON Schema reports the field |
| Skills too long for the context window | Tessl on OpenSpec; EGM's 24 KB cap | Yes, worse |
| No evidence the method improves outcomes | EGM issue #61; Help Net Security on EGM (snippet) | Yes. No measurement exists for this kit either |
| Fast-moving, renamed commands, breaking CLI changes | OpenSpec `/openspec:*` → `/opsx:*`; Spec Kit `--ai` → `--integration` | Less: three commands, one envelope, but a Draft format and no tags |
| Interactive by design; cannot run unattended | nateinaction/factory on EGM | No; unattended is the kit's native mode |
| Tied to one vendor or IDE | OpenSpec's own criticism of Kiro | Yes: Claude for the generator, Claude Code for the skills |

## 7. Assessment

The kit's thesis, that a spec should be a contract a validator can prove
complete, is not shared by any of the three and is the reason to keep it.
Nothing else in this field can tell an agent that a test is owned by no
task, that a path has no smoke test, or that two agents are about to work
on the same task. Everything the others do that this kit does not is
either prompt engineering that can be adopted (clarify budgets, rubrics,
triage vocabulary) or a product choice that can be made (Markdown
rendering, agent-neutral skills, a living view).

The costs are also real, and the author has already felt them once: v2
exists because v1 was "at the same time over-complicated and
under-specified". The current packages are smaller than v1's but still
heavy for the size of the changes they describe, and the whole thing is
reachable only from Claude Code with a Go toolchain.

Three options:

**A. Drop it and adopt OpenSpec**, with a community TDD schema such as
`anvil` for the test gate. Right if the work is a person driving one agent
through small and medium changes and a living spec of the system matters
more than a proven plan. Lost: validated coverage, the task state machine,
scoped rendering, unattended generation from issues, cross-spec rules, the
library. Gained: zero maintenance, forty agents, a living spec, a
community.

**B. Keep it as it is.** Not recommended. A Claude-only generator, a
`make build` install, a Draft format and 40 KB skills keep it a personal
tool, and personal tools die with the next shift of attention.

**C. Keep the format and the library, shrink the surface, borrow.**
Recommended if the agent-fox vision of unattended, multi-agent work from
an issue to a pull request is still the goal. The format and `afspec` are
the moat; the rest is replaceable.

### 7.1 What to borrow, in order of return on effort

1. **Review the plan, not only the PRD** (from EGM). Add an adversarial
   stage over the generated `tasks.json` and `test_spec.json` with two
   written rubrics: the Zero-Decision Rule (an implementer who must make a
   design choice means the plan fails) and codebase grounding (every path
   in `touches` and every symbol in `pseudocode` exists or is said to be
   new). The reviewer is already a fresh agent per phase; what is missing
   is the stage and the rubric. Adopt the triage vocabulary and let the
   reviser answer each finding as CONTEST, ESCALATE, PROPOSE FIX or
   DECLINE; replace "stop when nothing critical or major" with "stop after
   two consecutive clean rounds".
2. **Commit a rendered Markdown beside the JSON** (from all three). A
   `spec.md` written by `specr` into the package on every save, marked
   generated like `docs/generated/`, makes a package reviewable in a pull
   request diff without changing the canonical format. It costs one rule
   in the completeness check and one line in `Spec.Save`.
3. **Make the skills agent-neutral and smaller** (from Spec Kit and
   OpenSpec). The skills are Markdown already; render one source into the
   Agent Skills layout, Codex, Cursor and Copilot formats, as both kits
   do from one template set, and move the reference material (the JSON
   shapes, the field tables) into files the skill reads on demand so the
   body fits under 24 KB. The generator stays Claude-only; the skills need
   not.
4. **Version and check the steering** (from Spec Kit). Give `steering.md`
   a version and an amendment record, and add a "steering alignment"
   finding to the adversarial review so a package that contradicts a
   directive is reported, not merely generated beside it.
5. **A drift check for sealed packages** (from EGM and Spec Kit). A
   `speca <spec> --drift` or an `af-drift` skill that reads a sealed
   package's criteria and paths against the code and reports
   missing, partial, contradicted and unrequested behaviour, as
   `af-impl`'s conformance pass already does once, at PR time.
6. **Ship it** (from all three). Tag a release; get agentkit-go tagged
   so `go install` works; split `envelope`, `specr` and `speca` from the
   model stack, which ADR 05 and ADR 10 already name as the next step;
   publish binaries. Promote the format from Draft.
7. **A living view, cheaply** (from OpenSpec). Before building delta
   semantics, try the derived version: `speca --list` of sealed packages
   joined with `specr --section requirements` gives every criterion the
   system has committed to, grouped by actor. If that is read, a
   `supersedes` at criterion granularity is the next step; if it is not,
   the living spec was not the gap.

### 7.2 What not to borrow

- OpenSpec's Markdown-as-data. Its validator's own warning that a wrong
  heading level "will fail silently" is the argument for JSON Schema.
- Spec Kit's optional tests. The format's one unarguable advantage is
  that a test is not optional.
- EGM's absence of an implementation step and of state. The kit's
  `af-impl` and `speca` are ahead here.

### 7.3 What should decide it

None of the four kits, this one included, has measured whether its
method produces better code than a plain prompt. Before investing in the
list above, run the same three features through this kit and through
OpenSpec with a TDD schema, in the same repository with the same model,
and count defects found in review, iterations to green and the cost in
tokens and time. If the proven-complete plan does not show up in those
numbers, option A is the honest choice; if it does, option C is paid for.
