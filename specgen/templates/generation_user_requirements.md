Generate the requirements artifact: one flat list of criteria per requirement, each criterion with its check, plus the end-to-end execution paths, each with its smoke check.

## Scope first

Fit the PRD in **at most 10 requirements**, each with at most 8 criteria. The PRD phase has already sized the work, so do not answer in prose — this phase ends only when you call the tool. If the PRD still seems larger than that, merge closely related behaviour into one requirement rather than exceeding the limits; the validator warns on an oversized spec.

## Criterion fields

Each criterion is one EARS sentence decomposed into fields. Choose the pattern first, then fill in only the fields that pattern allows.

| `pattern`       | `condition`      | Rendered sentence                                  |
|-----------------|------------------|----------------------------------------------------|
| `ubiquitous`    | must be absent   | THE {system} SHALL {action}                         |
| `event_driven`  | the trigger      | WHEN {condition}, THE {system} SHALL {action}       |
| `state_driven`  | the state        | WHILE {condition}, THE {system} SHALL {action}      |
| `unwanted`      | the error case   | IF {condition}, THEN THE {system} SHALL {action}    |
| `optional`      | the feature      | WHERE {condition}, THE {system} SHALL {action}      |

A second condition goes into `condition`, joined with "and": there is no `guard` and no `complex_event`.

Every criterion also has:

- `id` — `REQ-{N}.{C}`, C sequential from 1 within the requirement.
- `system` — the component that acts. Optional; a requirement's `system` applies to every criterion of it that names none, so write it once on the requirement when they agree, and defaults to "system" when rendered.
- `action` — what the system must do. Testable and unambiguous.
- `contract` — what the caller observes, and what the check asserts: return value, exit code, status code, emitted event. **Required for every `unwanted` criterion** and expected whenever the result is consumed by something else.
- `check` — how the criterion is verified (below). **Required.**

A requirement may carry a one-sentence `rationale` explaining why it exists. There is no `user_story` object.

## The check

A check is the test of its criterion, in the three fields a test adds that the criterion does not already say. The criterion's `condition` is the given, its `action` the when, its `contract` the then: do not repeat them.

- `kind` — `unit` (one component, collaborators may be stubbed), `integration` (two or more real components, only external I/O stubbed) or `property` (an invariant over an input domain; the pseudocode names the domain).
- `setup` — optional. The preconditions the condition does not state.
- `pseudocode` — optional but expected. Language-agnostic assertions. Name the function, handler or command the check exercises and, when the repository already has a test file for it, that file and the existing test to extend (`in catalog/store_test.go beside TestStore_Insert: …`), so the coder extends it rather than starting a file of its own. For a check over a table of inputs, name the cases.

Or, instead of those, `{"same_as": "REQ-N.C"}` when another criterion's own check verifies this one too (variants of one behaviour share one table-driven check: the first criterion carries it and names the cases, the others say `same_as`), or `{"same_as": "PATH-N"}` when the path's smoke check is the only check the criterion needs. A `same_as` names a criterion whose check is its own, never one that is itself shared.

Rules for the checks:

- **One check per behaviour.** A behaviour is one thing a caller can observe: a request answered with a status and a body, a store that refuses a duplicate, a command that exits with a code and writes a file. Criteria whose checks differ only in the input share one check over a table.
- For every `unwanted` criterion, the check asserts the caller-observable outcome its `contract` names — the status code, the exit code, the returned error — not merely that "an error occurred".
- A `property` check only for a real invariant a table of representative inputs cannot establish: a round trip, idempotence over arbitrary input, an algebraic law. A "for any …" criterion is otherwise a `unit` check over such a table, its boundaries among them.
- **No artefact checks.** Every check checks what the code does. None checks that a struct has a field, a constant has a value, a file or a document mentions something, an import graph holds or a helper exists.
- **Look at the existing tests first.** Before writing a check, look at the tests the repository already has for the code it exercises: the test files in the repository map, read where it matters. When an existing test already verifies the behaviour, keep the check, since the criterion still needs one, and name that test's file and name in `pseudocode`, so the coder maps the criterion to that test instead of writing the behaviour again.

The coder writes each check as the project's own test, in the test file of the code it exercises, named for the behaviour and never for the criterion id, and records which test covers which criterion outside the code. Write `pseudocode` so that this is possible: say the behaviour and name the file, never the id.

## What used to be separate sections

- **Edge cases** are ordinary criteria, usually `unwanted`. There is no `edge_cases` array.
- **Correctness properties** are `ubiquitous` criteria whose `action` starts with "for any …".
- **Error handling** is an `unwanted` criterion with a `contract`. There is no `error_handling` array.

## Edge-case checklist

For every requirement, consider — and where it applies, write a criterion for:

- empty or null input;
- boundary values;
- the operation failing;
- authorization failing;
- a concurrent operation.

For anything that spawns processes, loops, retries or calls a service, also consider: timeout, resource cleanup on failure, an iteration cap, and the rule that library code returns errors instead of terminating the process.

## Language

Prefer measurable constraints over qualitative language. Avoid the words *appropriate*, *properly*, *correctly*, *reasonable*, *as needed* and *etc*: they are flagged and they make a criterion untestable.

## Execution paths

Each path is an end-to-end scenario with at least two `{actor, action}` steps and a `smoke` check. A path starts at a user action, CLI command, API call or scheduled trigger, ends at a concrete side effect, and uses logical actors. When several specs form a dependency chain, the last spec in the chain owns a path that crosses every spec boundary.

**The smoke check** proves the wiring of its path: it runs the path once with real components and asserts the end state. It does not assert again the criteria the checks verify. Its fields: `real_components` (required, non-empty: what must not be mocked), `setup` (optional), `end_state` (required, non-empty: the side effects at the end of the path) and `pseudocode` (optional; when a test in the repository already runs the path end to end, name it, so the coder extends that test instead of writing another).

**Paths across specs.** For every spec this one depends on (the specs the PRD's `## Dependencies` table names, which the tasks artifact lists in `dependencies`), at least one step of this spec's paths must be played by an actor that also plays a step in one of that spec's paths: the same name, with case ignored. A spec whose paths share an actor with none of its dependencies is rejected, and the tasks step cannot repair that, so decide it here. Where this spec drives or calls a component the other spec owns, name that component as the other spec does instead of coining a name for it; the actors of the existing specs are listed above when there are any.

## Handoffs

`handoffs` is optional. Write one wherever the PRD says that another package will call, register into or fill something this package offers: `{"id": "H-1", "what": "<the extension point or entry point>", "to": "<the receiving package's spec id>", "criteria": ["REQ-2.1"]}`. The receiver must depend on this package, directly or transitively; a handoff to a package that lands earlier is refused. `criteria` are this package's criteria that depend on the handoff being wired.

## Example

The fragment below is structurally correct for a recipe-manager system. Use your own domain — this is for shape only.

```json
{
  "requirements": [
    {
      "id": "REQ-1",
      "title": "Recipe creation",
      "rationale": "A cook who cannot save a recipe has no reason to open the app twice.",
      "system": "recipe catalog",
      "criteria": [
        {
          "id": "REQ-1.1",
          "pattern": "event_driven",
          "condition": "a client submits POST /recipes with a body carrying a name and at least one ingredient",
          "action": "persist the recipe and return its assigned identifier",
          "contract": "HTTP 201 with body {id: string, name: string}; the catalog holds one more row",
          "check": {
            "kind": "unit",
            "setup": ["an empty catalog"],
            "pseudocode": "in http/handlers_test.go beside TestRecipes_Create: r = post('/recipes', body); assert r.status == 201; assert len(r.json.id) > 0; assert count(catalog) == 1"
          }
        },
        {
          "id": "REQ-1.2",
          "pattern": "unwanted",
          "condition": "the submitted body has no name field",
          "action": "reject the request and persist nothing",
          "contract": "HTTP 400 with body {error: string}; the catalog row count is unchanged",
          "check": {
            "kind": "unit",
            "setup": ["an empty catalog"],
            "pseudocode": "in http/handlers_test.go: table of bodies (no name, empty name, no ingredients); for each: r = post('/recipes', body); assert r.status == 400; assert count(catalog) == 0"
          }
        },
        {
          "id": "REQ-1.3",
          "pattern": "unwanted",
          "condition": "the submitted body has an empty ingredient list",
          "action": "reject the request and persist nothing",
          "contract": "HTTP 400 with body {error: string}; the catalog row count is unchanged",
          "check": { "same_as": "REQ-1.2" }
        },
        {
          "id": "REQ-1.4",
          "pattern": "ubiquitous",
          "action": "for any persisted recipe, return the same identifier for every subsequent read of it",
          "check": {
            "kind": "unit",
            "pseudocode": "in catalog/store_test.go: for rec in table (one ingredient, twenty ingredients, a name with non-ASCII characters): id = store.Insert(rec); assert store.Get(id).id == id; assert store.Get(id).id == id"
          }
        }
      ]
    }
  ],
  "execution_paths": [
    {
      "id": "PATH-1",
      "title": "A cook saves a recipe and reads it back",
      "steps": [
        { "actor": "cook", "action": "submits POST /recipes with a recipe body" },
        { "actor": "recipe catalog", "action": "validates the body and writes a row" },
        { "actor": "recipe catalog", "action": "returns 201 with the assigned identifier" },
        { "actor": "cook", "action": "reads GET /recipes/{id} and receives the saved recipe" }
      ],
      "smoke": {
        "real_components": ["HTTP handler", "recipe catalog", "database"],
        "setup": ["the service is running against a real database"],
        "end_state": ["the fetch returns the recipe that was posted"],
        "pseudocode": "extends the end-to-end test in http/server_test.go: id = post('/recipes', rec).json.id; assert get('/recipes/' + id).json == rec"
      }
    }
  ]
}
```
