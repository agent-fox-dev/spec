Generate the implementation plan: **one flat, ordered list of implement tasks**. A task is the unit of dispatch — one coder session, one task. There are no task groups, no subtasks, no verification subtasks, no `traceability` array and no integration task: the tool derives the last task from the paths, the contracts and the touches of the plan.

The complete requirements artifact is above, with the check of every criterion and the smoke check of every path. Use its real IDs. Do not invent one.

## Task fields

- `id` — integer, sequential from 1. Array order is the default execution order.
- `title` — imperative and specific.
- `criteria` — non-empty. The requirement or criterion IDs this task satisfies; a requirement ID means all of its criteria. The checks of those criteria are the tests the task writes.
- `steps` — non-empty, in order, concrete enough that a coder who sees only this task, its criteria and their checks needs nothing else. A step says a behaviour to add or a decision already taken, not an edit: it names no line of a file (`store.go:88`, "line 88") and quotes no block of code, because both are wrong after the first commit that moves them, and a line number is not a decision.
- `touches` — optional. The files, packages or modules the task creates or modifies, the test files it extends among them. This is where the files are named, each once, without a line number.
- `depends_on` — optional. Task IDs that must be done first; must reference lower IDs. Absent means "the previous task": **omit it** when it would name only the task before, and write it only to let independent tasks run in parallel or to name more than one task.
- `done_when` — optional. Extra completion checks beyond the implicit ones.
- `takes_handoffs` — optional. The handoffs another package addresses to this one that the task wires, each `"<spec id>:H-N"`. Every handoff the packages this one depends on address to it is taken by some task.

There is no `kind`, no `tests`, no `state` and no `optional`: every stored task is an implement task, a task's tests are the checks of its criteria, and the state of every task lives in progress.json, which the tools write.

Every task carries an implicit definition of done that you do not need to write out: the checks of its criteria exist as tests, are executable and pass; `test_commands.all_tests` passes; `test_commands.linter` passes. The coder works test-first, so **there are no separate "write the tests" tasks** — the checks are written inside the task that owns their criteria.

A task's tests extend the test files of the code in its `touches`: the test file of each handler, store, command or function it changes, created only when there is none. A task does not create a test file of its own, or one named after itself, the spec or a check kind. Write steps that say the behaviours to add, not the criterion ids to test: the ids stay in the spec, and the coder records which test covers each one outside the code.

## Dependencies on other specs

`dependencies` lists the specs the PRD's `## Dependencies` table names, each as `{spec, reason}`. `spec` is the other spec's number alone — `"06"`, as in the example below — never its directory name (`06_router`) or its name. A number that names no existing spec makes the plan invalid. Leave the array empty when the PRD names none.

## Rules that make the plan invalid

- **Every criterion is covered by the `criteria` of at least one task**, directly or through its requirement ID. Nothing may be left unowned — that is the single most common way a plan ships with holes.
- **Before you submit, check ownership explicitly.** Go through the requirements and write each criterion id next to the task whose `criteria` lists it or its requirement. An id with no owner is rejected, and each rejection costs a full regeneration of this file.
- Every ID in `criteria` and `depends_on` resolves, and `depends_on` names lower ids.

Keep a task to at most 12 steps, and the spec to at most 12 tasks. Group by requirement, not by layer: a task should deliver one or two requirements end to end — model, logic, interface and tests — so that each task leaves the system in a working state.

## The integration task

Do not write one. A full package's last task is derived by the tool: it depends on every task of the plan, touches what every task touches, runs every path's smoke check with its real components and traces the path through the real code, confirms every contract is consumed in production code, searches the touches for stub markers (the project's own, named in the language block above, then the common ones), and, where the package depends on other specs, confirms their entry points are called from production code. What the plan has to give it is in the requirements already: the paths with their smoke checks, the contracts, and each task's `touches`.

## Test commands

`test_commands.all_tests` and `test_commands.linter` are required and must be the project's real commands, taken from the language and tooling stated above — never a default from another ecosystem. `spec_tests` is optional and narrows the run to this spec's tests. `affected_tests` is optional and written only when the project already has a command that runs the tests a change can affect from a base commit; it holds the placeholder `{base}` exactly once (for example `make test-affected BASE={base}`). Never invent one.

## Example

The example is a Go project's. In this project, use this project's own commands, file paths, layout and stub marker — the ones the language block above names — not Go's.

```json
{
  "test_commands": {
    "all_tests": "go test ./... -count=1",
    "linter": "go vet ./...",
    "spec_tests": "go test ./catalog/... -count=1"
  },
  "dependencies": [
    { "spec": "06", "reason": "uses the HTTP router and the database handle it sets up" }
  ],
  "tasks": [
    {
      "id": 1,
      "title": "Persist and read back recipes",
      "criteria": ["REQ-1"],
      "steps": [
        "Add the task's behaviours to catalog/store_test.go and http/handlers_test.go and confirm they fail",
        "Add the recipe row type and the catalog Insert and Get methods",
        "Wire POST /recipes to Insert; return 201 with the assigned id",
        "Reject a body with no name or no ingredient with 400 and an error string, writing nothing"
      ],
      "touches": ["catalog/recipe.go", "catalog/store.go", "catalog/store_test.go", "http/handlers.go", "http/handlers_test.go"]
    },
    {
      "id": 2,
      "title": "List recipes by ingredient",
      "criteria": ["REQ-2"],
      "steps": [
        "Add the task's behaviours to catalog/store_test.go and http/handlers_test.go and confirm they fail",
        "Add the catalog ByIngredient query, ordered by name",
        "Wire GET /recipes?ingredient= to it; return 200 with the list, 400 for an empty ingredient"
      ],
      "touches": ["catalog/store.go", "catalog/store_test.go", "http/handlers.go", "http/handlers_test.go"],
      "depends_on": [1]
    }
  ]
}
```
