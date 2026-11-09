# The review

What `af-spec` §9 runs when the person asks for a review: an adversarial
review of the plan by a fresh subagent, then your answer to every finding.
It is the session's form of `spec --adversarial-plan`, held to the same
rubrics and the same four answers.

## Why a fresh subagent

You wrote the requirements and the tasks, and everything you decided on the
way is still in your context. Read back by you, a gap you filled in your head
does not look like a gap. The reviewer must start from what a coder will be
handed — the package, the PRD and the code — and nothing else. So the review
runs in a **subagent your agent starts with a fresh context**, never in your
own: if your agent cannot start one, do not run the review in your own
context instead; skip it, and say in the report that no review ran and why.

The reviewer **reads and does not write**. Tell it so. Every change to the
package is yours, made after the review, in the order below.

## When

After §9's verdict is clean and before §10's activation, once the package is
the one you would activate. A package reviewed and then changed by hand is
validated again (§9) before it is activated.

## 1. Gather what the reviewer is given

Render the package as a coder receives it, which you have just read in §9:

```sh
specr <NN> --markdown-file "${TMPDIR:-/tmp}/af-spec-<NN>.md" > /dev/null
```

Compute the **grounding facts**: for every distinct path in a task's
`touches`, whether it exists in the repository now. They are the file system's
answer, not the reviewer's guess, and the reviewer is given them as facts:

```sh
jq -r '.tasks[].touches[]?' .specs/<NN_name>/tasks.json | awk '!seen[$0]++' |
  while IFS= read -r p; do
    if [ -e "$p" ]; then echo "- \`$p\` — exists"; else echo "- \`$p\` — does not exist"; fi
  done > "${TMPDIR:-/tmp}/af-spec-<NN>-facts.md"
```

Without `jq`, read the paths from `tasks.json` and test each one yourself; the
facts are the same list either way. With no path in any task, the fact is
"the plan's tasks name no path".

## 2. Brief the reviewer

The subagent knows only what the brief says. Give it, in this order:

1. **Its role**, in these words: *You are an adversarial reviewer of a plan —
   the requirements, each criterion with its check and each path with its
   smoke check, and the tasks — written from a PRD. A coder will be handed
   this plan and the code and will carry it out. Every decision the plan
   leaves open is one the coder makes alone, and every file or symbol it names
   that is not there is one the coder invents. Find what is wrong with it. Do
   not approve it, praise it or summarise it. An empty list is a legitimate
   outcome, but only after you have looked. Read; do not change any file.*
2. **The files to read**, by absolute path: the rendered package, the PRD
   (`.specs/<NN_name>/prd.md`), `.specs/steering.md` when it exists, and the
   repository root it may read the code under.
3. **The grounding facts**, inline, from the file of step 1.
4. **The three rubrics, the severities and the rules** below, verbatim.
5. **The output**: a JSON array, and nothing else, of findings
   `{"id": "F1", "rubric": "...", "severity": "...", "section": "...",
   "flaw": "...", "evidence": "..."}`, the most serious first.

In a second round, add the findings of the earlier rounds with your answer
to each (step 4).

### The rubrics

**`zero_decision`: the Zero-Decision Rule.** The plan fails if a coder who
holds the package and the code would have to make a design choice that the
plan does not make. Of every task and the checks of the criteria it owns, ask:

1. Where does the code go? The file, package or type is named in the steps
   or in `touches`, or there is only one place it can go.
2. Is every name, signature, error, message, format and order that a check
   asserts, or that another task relies on, stated?
3. Is a library, algorithm or data structure left open where the choice
   shows in a test or to a caller?
4. Does a step or a criterion leave the work open, with words like "as
   appropriate", "if needed", "decide" or "for example", or one of the vague
   words *appropriate*, *properly*, *correctly*, *reasonable*, *relevant*,
   *adequate*, *suitable*, *as needed*, *if necessary* or *etc*? The finding is
   the open choice, not the word: a vague word is a finding when two coders
   would build different things from it.
5. Could two coders who both follow the task write code that one of the
   package's checks passes for one and fails for the other?
6. Does a step pin the work to a line of a file (`store.go:88`, "line 88") or
   quote a block of code? It is wrong after the first commit that moves the
   line. Raise it as `minor`, unless a check depends on what the line says.

**`grounding`: the plan names only what is there.** Ask:

1. Does each path in `touches` exist? If not, does a step of the same task,
   or of a task it depends on directly or transitively, create it? A path that
   does not exist and that nothing creates is a finding, and its evidence is
   the grounding fact.
2. Does each symbol a step or a check's pseudocode names (a function, type,
   method, flag or constant) exist in the code, or does a task of this
   package create it? Read the code; do not guess.
3. Does each claim the plan makes about the existing code hold?
4. Does a command in `test_commands` run each check, given the file the
   check's test will live in? Does the derived integration task, rendered
   last, have the paths, contracts and touches it needs?

**`steering`: the project's directives.** When `.specs/steering.md` exists,
check the plan against every directive in it. A plan that contradicts one is
a finding whose evidence quotes the directive verbatim and the text of the
plan that contradicts it, and whose severity is `critical` or `major`. With no
steering there is nothing to check.

### Severity

- `critical`: the plan builds the wrong thing, or its checks contradict its
  criteria.
- `major`: a choice is left to the coder that a check or another task depends
  on; a path or symbol does not exist and nothing creates it; or a directive
  is contradicted.
- `minor`: a choice no check or task sees, or the wording.

Severity decides whether the plan is reviewed again, so do not inflate it.

### Rules

- Every finding names its `rubric` and its place in the plan by id in
  `section`: a task by its number (the derived integration task has the
  number after the last), a criterion id or a path id.
- Every finding needs `evidence`: a quote from the plan, the PRD or the code,
  a file path and what it shows, or a grounding fact. A finding that cannot
  be supported is withdrawn.
- At most 30 findings, the most serious first. One flaw per finding.
- In a later round: a finding you answered `contested` may be raised again
  only with evidence the contest did not answer; one `declined` or
  `escalated` may not be raised again; a flaw a `fixed` finding named that
  the plan still has is a new finding.

## 3. Check what comes back

Weigh each finding on its evidence, against the PRD, the grounding facts and
the code — not on who raised it, and not on your memory of why you wrote what
you wrote. A finding about the code is settled by reading the code. A finding
with no evidence, or with a quote that is not in the file it names, is
`contested` on that ground.

## 4. Answer every finding

Give every finding exactly one answer:

- **`fixed`**: it holds. Change the plan to remedy it — a criterion's contract,
  a check's pseudocode, a path's smoke check, a task's steps or `touches`.
  Never the PRD's `## Intent`, the spec id or name, or the actors of the
  paths. A step you write says a behaviour or a decision, not an edit.
- **`contested`**: it does not hold. Say why, with counter-evidence anyone
  can check: a quote that is in the PRD or the plan verbatim, or a path that
  exists and what it shows.
- **`escalated`**: it holds, and remedying it takes a decision the PRD, the
  steering and the code do not make. Ask the person, with two or three
  options in one sentence each and the one you would take. Their answer is
  the remedy; record it in `## Design Decisions`. This is the review's only
  stop.
- **`declined`**: it holds and is not remedied in this package. Say that it
  is out of scope, or name the package that carries it. A `critical` finding
  and a `steering` finding are never declined.

You do not change a finding's severity or its rubric.

Then take the verdict again (§9: `--render`, then `--validate`) and repair
until it is clean: a fix that breaks a rule is not a fix.

## 5. Another round, or stop

Run another round — a new fresh subagent, given the earlier rounds and your
answers — when this one raised a `critical` or `major` finding you fixed, at
most **three rounds** in all, or as many as the person asked for. Stop when a
round raises nothing `critical` or `major`, or the rounds are spent.

## 6. Report

In §11's report: the rounds run and why the review stopped, the findings by
severity and rubric, and the count of each answer. Name every `escalated`
finding with the decision taken, and every `declined` one with its reason.
When no review ran because the agent cannot start a fresh subagent, say so.
