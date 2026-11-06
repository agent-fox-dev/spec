# tasks.json

What `af-spec` §8 writes: the example, the fields, `test_commands`,
`dependencies`, the rules that make a plan invalid, and the integration task.

What work to do, in what order: **one flat, ordered list**. A task is the
unit of dispatch — one coder session, one task. Write it against the complete
requirements and the complete test list.

```json
{
  "$schema": "https://agent-fox.dev/schemas/tasks.v2.json",
  "spec_id": "07",
  "spec_name": "recipe_catalog",
  "schema_version": 2,
  "test_commands": { "all_tests": "go test ./... -count=1", "linter": "go vet ./...", "spec_tests": "go test ./catalog/... -count=1" },
  "dependencies": [ { "spec": "06", "reason": "uses the HTTP router and the database handle it sets up" } ],
  "tasks": [
    { "id": 1, "kind": "implement", "title": "Persist and read back recipes",
      "criteria": ["07-REQ-1"], "tests": ["TS-07-1", "TS-07-2", "TS-07-3"],
      "steps": [
        "Add the task's behaviours to catalog/store_test.go and http/handlers_test.go and confirm they fail",
        "Add the recipe row type and the catalog Insert and Get methods",
        "Wire POST /recipes to Insert; return 201 with the assigned id",
        "Reject a body with no name with 400 and an error string, writing nothing"
      ],
      "touches": ["catalog/store.go", "catalog/store_test.go", "http/handlers.go", "http/handlers_test.go"],
      "state": "pending" },
    { "id": 2, "kind": "integration", "title": "Verify the save-and-read path end to end",
      "criteria": [], "tests": ["TS-07-4"],
      "steps": [
        "Extend the end-to-end test in http/server_test.go to save a recipe and read it back against the running service and a real database, and confirm it passes",
        "Trace 07-PATH-1 from the HTTP handler through the catalog to the database; confirm each step calls the next",
        "Confirm the identifier Insert returns is consumed by the handler in production code, not only in tests",
        "Grep catalog/ and http/ for TODO and panic(\"not implemented\") and remove any that remain"
      ],
      "touches": ["http/server_test.go"], "depends_on": [1],
      "done_when": ["`go test ./... -count=1` passes against a real database"],
      "state": "pending" }
  ]
}
```

**Fields.** `id` is an integer from 1, and array order is the default
execution order. `kind` is `implement` or `integration`. `title` is
imperative and specific. `criteria` holds requirement or criterion ids —
non-empty for `implement`, where a requirement id means all its criteria; it
may be empty for `integration`. `tests` is non-empty. `steps` is non-empty,
in order, and concrete enough that a coder who sees only this task, its
criteria and its tests needs nothing else. `touches` is optional: the files,
packages and modules the task creates or changes, the test files it extends
among them. `depends_on` is optional, references lower ids, and absent means
"the previous task" — use it to let independent tasks run in parallel.
`done_when` is optional, for checks beyond the implicit ones. `state` is
`pending` for every task of a new plan. `optional` defaults to false.

Every task carries an implicit definition of done you do not write out: the
listed tests exist, are executable and pass; `all_tests` passes; `linter`
passes. The coder works test-first, so **there are no "write the tests"
tasks** — tests are written inside the task that owns them, into the test
files of the code in `touches`. Write steps that say the behaviours to add,
not the test ids to write.

**`test_commands`** are the project's real commands, from §2: `all_tests`
and `linter` are required, `spec_tests` is optional and narrows the run to
this package's area. A default from another ecosystem makes the plan wrong
for the repository it will be run in.

**`dependencies`** lists the packages the PRD's `## Dependencies` names, each
`{spec, reason}`, where `spec` is the other package's **number alone**
(`"06"`, never `06_router` or its name). A number that names no package in
the spec root makes the package invalid, and so does a dependency whose paths
share no actor with this one (§6).

**Rules that make the plan invalid.** Every test in `test_spec.json` is
listed in the `tests` of at least one task (C7). Every criterion is covered
by the `criteria` of at least one **`implement`** task, directly or through
its requirement id (C8). Every id in `criteria`, `tests` and `depends_on`
resolves, and `depends_on` points at lower ids (C6). Exactly one task has
kind `integration`, it is last, and its `tests` include **every** `smoke`
test (C9).

**Check ownership before you take the verdict.** Go through the test list and
write each test id against the task whose `tests` holds it; then through the
requirements and each criterion id against the `implement` task whose
`criteria` holds it. An id with no owner is the commonest way a plan ships
with a hole. Criteria belong to implement tasks; the integration task owns
the smoke tests.

Keep a task to at most 10 tests and 12 steps, and the plan to at most 12
tasks. **Group by requirement, not by layer**: a task delivers one or two
requirements end to end — data, logic, interface and tests — so each one
leaves the system working.

**The integration task** is last and exists to catch the wiring gaps
component tests cannot see. Its steps cover, in the project's own vocabulary:
trace every execution path through the real code and confirm each step calls
the next with no stub left; for every criterion with a `contract`, confirm a
caller in production code consumes the producer's result, not only a test;
search the files every task's `touches` lists for the project's stub marker
and the common ones; and for any path whose entry point belongs to another
package, confirm that entry point is called from production code. An
execution path that is not live in production code fails this task, and no
erratum satisfies it.
