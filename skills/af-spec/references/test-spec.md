# test_spec.json

What `af-spec` §7 writes: the example, the fields and the four kinds, the
coverage rules the format enforces, and what one test per behaviour means.

How each criterion is verified: **one flat list** of tests, written against
the complete requirements artifact — its real ids, never an invented one.

```json
{
  "$schema": "https://agent-fox.dev/schemas/test_spec.v2.json",
  "spec_id": "07",
  "spec_name": "recipe_catalog",
  "schema_version": 2,
  "tests": [
    { "id": "TS-07-1", "kind": "unit", "verifies": ["07-REQ-1.1"],
      "title": "A valid recipe body is persisted and its identifier returned",
      "given": ["an empty catalog"],
      "when": "POST /recipes is called with a name and one ingredient",
      "then": ["the status is 201", "the body carries a non-empty id", "the catalog holds one row"],
      "pseudocode": "in http/handlers_test.go: r = post('/recipes', body); assert r.status == 201; assert count(catalog) == 1" },
    { "id": "TS-07-4", "kind": "smoke", "verifies": ["07-PATH-1"],
      "title": "A cook saves a recipe and reads it back through the running service",
      "given": ["the service is running against a real database"],
      "when": "a recipe is posted and then fetched by its returned identifier",
      "then": ["the fetch returns the recipe that was posted"],
      "pseudocode": "extends the end-to-end test in http/server_test.go: id = post('/recipes', rec).json.id; assert get('/recipes/' + id).json == rec",
      "real_components": ["HTTP handler", "recipe catalog", "database"] }
  ]
}
```

The example shows two of the four tests this package needs; `TS-07-2` and
`TS-07-3`, which verify the other two criteria, are left out for length. In
the file the series has no gaps.

**Fields.** `id` is `TS-{spec_id}-{N}`, N sequential from 1 — **one number
series for all four kinds**, with no `E`, `P` or `SMOKE` variant. `kind` is
`unit` (one component, collaborators may be stubbed), `integration` (two or
more real components, only external I/O stubbed), `property` (the generator
in `when`, the invariant in `then`) or `smoke` (one full execution path with
real components). `verifies` is non-empty: criterion ids for `unit`,
`integration` and `property`, a path id for `smoke`, which may also list
criterion ids. `title` says what is verified as a caller sees it. `given` may
be empty; `when` is the action or input; `then` is non-empty and observable.
`pseudocode` is optional, language-agnostic, and may name concrete functions
and files. `real_components` is required and non-empty for `smoke` and
**must be absent for every other kind** (rule C11).

**Coverage, which the format enforces.** Every criterion is in the `verifies`
of at least one test (C4). Every path is in the `verifies` of at least one
`smoke` test, and every `smoke` test verifies at least one path (C5). Every
id in `verifies` resolves in this package (C3). There is no `coverage`
object: coverage is computed, never authored.

**One test per behaviour.** A behaviour is one thing a caller can observe: a
request answered with a status and a body, a store that refuses a duplicate,
a command that exits with a code and writes a file.

- A test lists every criterion its behaviour satisfies, up to four; more is
  usually two behaviours, and the validator warns past four.
- **Variants are a table.** Two tests whose `then` differ only in the input
  are one test over a table: name the cases in `when` and their outcomes in
  `then`.
- A test whose `then` only restates another's is a duplicate: drop it and add
  its criteria to the other's `verifies`.
- For every `unwanted` criterion, assert the caller-observable outcome its
  `contract` names, not that "an error occurred".
- A `property` test is only for an invariant a table of representative
  inputs cannot establish — a round trip, idempotence, an algebraic law. A
  "for any …" criterion is otherwise a `unit` test over such a table, its
  boundaries among them.
- A `smoke` test proves the wiring of its path: it runs the path once with
  its `real_components` and asserts the end state, without asserting again
  what the unit tests verify. One per path, and its `verifies` lists the
  path alone.
- **No artifact tests.** Nothing checks that a struct has a field, a constant
  has a value, a document mentions something, an import graph holds or a
  helper exists.
- **Look at the tests the repository already has** for the code each test
  exercises. Where one already verifies the behaviour, keep the entry — its
  criteria still need a test — and name that test's file and name in
  `pseudocode`, so `af-impl` extends it instead of writing the behaviour
  again under a new name.

The ids stay in this file. `af-impl` writes each test into the test file of
the code it exercises, named for the behaviour it checks
(`TestCreate_RejectsDuplicateSlug`), and records the map from test id to test
in the commit. Write `title` and `pseudocode` so that is possible: say the
behaviour, name the file, never the id.

Take the verdict again. Only the `tasks.json` `minItems` error should remain
beside anything the tests broke.
