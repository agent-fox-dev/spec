Generate the test specification: **one flat list** of tests. The `kind` field distinguishes them and the `verifies` field links each to what it proves. There are no separate arrays for edge-case, property or smoke tests, and no `coverage` object — coverage is computed from `verifies`.

The complete requirements artifact is above. Use its real IDs. Do not invent one.

## Test fields

- `id` — `TS-{spec_id}-{N}`, N sequential from 1. **One number series for all kinds.**
- `kind` — one of:
  - `unit` — exercises one component in isolation; collaborators may be stubbed.
  - `integration` — exercises two or more real components; only external I/O may be stubbed.
  - `property` — property-based; `when` names the input domain, `then` states the invariant.
  - `smoke` — runs a full execution path once with real components and checks where it ends.
- `verifies` — non-empty. Criterion IDs (`…-REQ-N.C`) for `unit`, `integration` and `property`; a path ID (`…-PATH-N`) for `smoke`, which may also list criterion IDs.
- `title` — one sentence saying what is verified: the behaviour, as a caller sees it.
- `given` — preconditions. May be empty, but write them when they matter.
- `when` — the action or input. For `property`, the generator: "for any non-empty list of …". For a test over a table of inputs, say so and name the cases.
- `then` — non-empty. Observable outcomes. For `smoke`, the side effects at the end of the path.
- `pseudocode` — optional, language-agnostic assertions. Recommended for `unit`, `integration` and `property`; it may name concrete functions and files. Name the function, handler or command the test exercises and, when the repository already has a test file for it, that file (`in catalog/store_test.go: …`), so the coder extends it rather than starting a file of its own.
- `real_components` — required and non-empty for `smoke`, and **must be absent for every other kind**. Lists what must not be mocked.

The ids stay in this file. The coder writes each test into the test file of the code it exercises and names it for the behaviour it checks (`TestCreate_RejectsDuplicateSlug`), never for its id, and records which test covers which id outside the code. Write `title` and `pseudocode` so that this is possible: say the behaviour, not the id.

## Coverage you must achieve

- Every criterion in the requirements artifact appears in the `verifies` of at least one test. A criterion with no test makes the spec invalid.
- Every execution path appears in the `verifies` of at least one `smoke` test.
- For every `unwanted` criterion, assert the caller-observable outcome its `contract` names — the status code, the exit code, the returned error — not merely that "an error occurred".

## One test per behaviour

A behaviour is one thing the code does that a caller can observe: a request answered with a status and a body, a store that refuses a duplicate, a command that exits with a code and writes a file.

- **One test per behaviour.** A test lists every criterion its behaviour satisfies, up to four; a behaviour that satisfies more is usually two behaviours.
- **Variants are a table.** Two tests whose `then` differ only in the input are one test over a table of inputs: the error cases of one operation, the members of a status list, a matrix of callers. Name the cases in `when` and their outcomes in `then`.
- **No duplicates.** A test whose `then` only restates another test's `then` is a duplicate: drop it and add its criteria to the other test's `verifies`.
- **Property tests only for a real invariant.** Write a `property` test only for an invariant over an input domain that a table of representative inputs cannot establish: a round trip, idempotence over arbitrary input, an algebraic law. A criterion whose action begins "for any …" is otherwise verified by a `unit` test over a table of representative inputs, its boundaries among them.
- **Smoke tests prove the wiring.** A `smoke` test proves that the components of a path are wired together: it runs the path once with the real components it lists and asserts the end state of the path. It does not assert again the criteria the path's `unit` and `integration` tests verify, so its `verifies` lists the path alone. One smoke test per path. When a test in the repository already runs the path end to end, name it in `pseudocode`, so the coder extends that test instead of writing another.
- **No artefact tests.** Every test checks what the code does. None checks that a struct has a field, a constant has a value, a file or a document mentions something, an import graph holds or a helper exists.
- **Look at the existing tests first.** Before writing a test, look at the tests the repository already has for the code it exercises: the test files in the repository map, read where it matters. When an existing test already verifies the behaviour, keep the entry, since its criteria still need a test, and name that test's file and name in `pseudocode`, so the coder maps it to that test instead of writing the behaviour again under a new name.

## Example

```json
{
  "tests": [
    {
      "id": "TS-07-1",
      "kind": "unit",
      "verifies": ["07-REQ-1.1"],
      "title": "A valid recipe body is persisted and its identifier returned",
      "given": ["an empty catalog"],
      "when": "POST /recipes is called with a name and one ingredient",
      "then": ["the status is 201", "the body carries a non-empty id", "the catalog holds one row"],
      "pseudocode": "in http/handlers_test.go: r = post('/recipes', body); assert r.status == 201; assert len(r.json.id) > 0; assert count(catalog) == 1"
    },
    {
      "id": "TS-07-2",
      "kind": "unit",
      "verifies": ["07-REQ-1.2"],
      "title": "A body with no name is rejected and nothing is persisted",
      "given": ["an empty catalog"],
      "when": "POST /recipes is called with a body that has no name field",
      "then": ["the status is 400", "the body carries a non-empty error string", "the catalog is still empty"],
      "pseudocode": "in http/handlers_test.go: r = post('/recipes', {}); assert r.status == 400; assert count(catalog) == 0"
    },
    {
      "id": "TS-07-3",
      "kind": "unit",
      "verifies": ["07-REQ-1.3"],
      "title": "A persisted recipe keeps its identifier across reads",
      "given": ["an empty catalog"],
      "when": "each recipe of a table (one ingredient, twenty ingredients, a name with non-ASCII characters) is inserted once and read twice",
      "then": ["both reads of each recipe return the identifier its insert returned"],
      "pseudocode": "in catalog/store_test.go: for rec in table: id = store.Insert(rec); assert store.Get(id).id == id; assert store.Get(id).id == id"
    },
    {
      "id": "TS-07-4",
      "kind": "smoke",
      "verifies": ["07-PATH-1"],
      "title": "A cook saves a recipe and reads it back through the running service",
      "given": ["the service is running against a real database"],
      "when": "a recipe is posted and then fetched by its returned identifier",
      "then": ["the fetch returns the recipe that was posted"],
      "pseudocode": "extends the end-to-end test in http/server_test.go: id = post('/recipes', rec).json.id; assert get('/recipes/' + id).json == rec",
      "real_components": ["HTTP handler", "recipe catalog", "database"]
    }
  ]
}
```
