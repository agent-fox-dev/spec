# tasks.json

What `af-spec` §7 writes: the example, the fields, `test_commands`,
`dependencies`, the rules that make a plan invalid, and the integration task
the tools derive.

What work to do, in what order: **one flat, ordered list** of implement
tasks. A task is the unit of dispatch — one coder session, one task. Write it
against the complete requirements, checks included.

```json
{
  "$schema": "https://agent-fox.dev/schemas/tasks.v3.json",
  "spec_id": "07",
  "spec_name": "recipe_catalog",
  "schema_version": 3,
  "test_commands": { "all_tests": "go test ./... -count=1", "linter": "go vet ./...", "spec_tests": "go test ./catalog/... -count=1" },
  "dependencies": [ { "spec": "06", "reason": "uses the HTTP router and the database handle it sets up" } ],
  "tasks": [
    { "id": 1, "title": "Persist and read back recipes",
      "criteria": ["REQ-1"],
      "steps": [
        "Add the task's behaviours to catalog/store_test.go and http/handlers_test.go and confirm they fail",
        "Add the recipe row type and the catalog Insert and Get methods",
        "Wire POST /recipes to Insert; return 201 with the assigned id",
        "Reject a body with no name with 400 and an error string, writing nothing"
      ],
      "touches": ["catalog/store.go", "catalog/store_test.go", "http/handlers.go", "http/handlers_test.go", "http/server_test.go"],
      "done_when": ["`go test ./catalog/... -count=1` passes against a real database"] }
  ]
}
```

The example shows the one task a one-requirement package needs; the tools
derive its task 2, the integration task, from `PATH-1`'s smoke check, the
contracts and this task's `touches`.

**Fields.** `id` is an integer from 1, and array order is the default
execution order. `title` is imperative and specific. `criteria` is non-empty
and holds requirement or criterion ids, where a requirement id means all its
criteria; the checks of those criteria are the tests the task writes. `steps`
is non-empty, in order, and concrete enough that a coder who sees only this
task and its criteria needs nothing else. A step says a behaviour or a
decision, not an edit: no line of a file (`store.go:88`) and no quoted block of
code, which are wrong after one commit. `touches` is optional: the files,
packages and modules the task creates or changes, the test files it extends
among them, each named once. `depends_on` is optional, references lower ids,
and absent means "the previous task": leave it out when it would name only the
task before, and use it to let independent tasks run in parallel.
`done_when` is optional, for checks beyond the implicit ones. There is no
`kind`, no `tests`, no `state` and no `optional`: every stored task is an
implement task, its tests are its criteria's checks, and its state lives in
`progress.json`, which the tools write.

Every task carries an implicit definition of done you do not write out: the
checks of the criteria it owns exist as tests, are executable and pass;
`all_tests` passes; `linter` passes. The coder works test-first, so **there
are no "write the tests" tasks** — the checks are written inside the task
that owns their criteria, into the test files of the code in `touches`. Write
steps that say the behaviours to add, not the criterion ids to test.

**`test_commands`** are the project's real commands, from §2: `all_tests`
and `linter` are required, `spec_tests` is optional and narrows the run to
this package's area. A default from another ecosystem makes the plan wrong
for the repository it will be run in.

**`dependencies`** lists the packages the PRD's `## Dependencies` names, each
`{spec, reason}`, where `spec` is the other package's **number alone**
(`"06"`, never `06_router` or its name). A number that names no package in
the spec root makes the package invalid, and so does a dependency whose paths
share no actor with this one (§6).

**Rules that make the plan invalid.** Every criterion is owned by the
`criteria` of at least one task, directly or through its requirement id (C5).
Every id in `criteria` and `depends_on` resolves, and `depends_on` points at
lower ids (C4). Every task id is a positive integer, unique in the plan (C2).

**Check ownership before you take the verdict.** Go through the requirements
and write each criterion id against the task whose `criteria` holds it, by
itself or through its requirement. An id with no owner is the commonest way a
plan ships with a hole.

Keep a task to at most 12 steps, and the plan to at most 12 tasks. **Group by
requirement, not by layer**: a task delivers one or two requirements end to
end — data, logic, interface and tests — so each one leaves the system
working.

**The integration task is not written.** In a `full` package the tools derive
it from the package as task N+1, where N is the highest task id: titled
"Verify every execution path end to end", depending on every task of the plan,
touching what every task touches, with steps that run each path's smoke check
with its real components and confirm its end state, trace the path through
the real code with no stub left, confirm that the result of every criterion
with a `contract` is consumed by production code and not only by a test,
search every `touches` path for stub markers, and, where the package depends
on other packages, confirm their entry points are called from production
code. `speca` lists it with its state, `specr <NN> --task N+1` renders it,
and `af-impl` works it last. What the plan has to give it is in the
requirements already — the paths with their smoke checks and the contracts —
and in each task's `touches`, so name the test files and the areas each task
changes there. A `lite` package has no paths and no integration task.
