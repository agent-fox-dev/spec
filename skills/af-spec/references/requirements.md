# requirements.json

What `af-spec` §6 writes: the example, the ids, the EARS patterns and their
fields, contracts, the execution paths and `external_apis`. The check every
criterion carries and the smoke check every path carries are in
[checks.md](checks.md).

What the system must do: one flat list of criteria per requirement, plus the
end-to-end execution paths. Everything comes from the PRD; nothing comes from
your view of how it should be built.

```json
{
  "$schema": "https://agent-fox.dev/schemas/requirements.v3.json",
  "spec_id": "07",
  "spec_name": "recipe_catalog",
  "schema_version": 3,
  "requirements": [
    {
      "id": "REQ-1",
      "title": "Recipe creation",
      "rationale": "A cook who cannot save a recipe has no reason to open the app twice.",
      "system": "recipe catalog",
      "criteria": [
        { "id": "REQ-1.1", "pattern": "event_driven",
          "condition": "a client submits POST /recipes with a body carrying a name and at least one ingredient",
          "action": "persist the recipe and return its assigned identifier",
          "contract": "HTTP 201 with body {id: string, name: string}",
          "check": { "kind": "unit", "setup": ["an empty catalog"],
            "pseudocode": "in http/handlers_test.go: r = post('/recipes', body); assert r.status == 201; assert r.json.id != ''; assert count(catalog) == 1" } },
        { "id": "REQ-1.2", "pattern": "unwanted",
          "condition": "the submitted body has no name field",
          "action": "reject the request and persist nothing",
          "contract": "HTTP 400 with body {error: string}; the catalog row count is unchanged",
          "check": { "kind": "unit",
            "pseudocode": "in http/handlers_test.go beside the test above, a table over the bodies {no name, empty name}: r = post('/recipes', body); assert r.status == 400; assert r.json.error != ''; assert count(catalog) == 0" } },
        { "id": "REQ-1.3", "pattern": "ubiquitous",
          "action": "for any persisted recipe, return the same identifier for every subsequent read of it",
          "check": { "same_as": "PATH-1" } }
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
        "end_state": ["the fetch returns the recipe that was posted, under the identifier the post returned"],
        "pseudocode": "extends the end-to-end test in http/server_test.go: id = post('/recipes', rec).json.id; assert get('/recipes/' + id).json == rec"
      }
    }
  ]
}
```

The example is the shape `spec`'s own prompt carries; the domain is yours.

**Ids.** `REQ-{N}` for a requirement, `REQ-{N}.{C}` for a criterion with C
restarting at 1 inside each requirement, `PATH-{N}` for a path. N and C are
sequential from 1. No id carries the package's number: nothing references a
criterion across packages. Every id is unique in its family (rule C2).

**The criterion is one EARS sentence in fields.** Choose the pattern first,
then fill only the fields it allows — the schema enforces the combination,
and it reports a wrong one as `'not' failed` on that criterion, which means
`condition` is present where the pattern forbids it or absent where it
requires it. A second condition is written into `condition`, joined with
"and": there is no `guard` and no `complex_event`.

| `pattern` | `condition` | Rendered sentence |
|---|---|---|
| `ubiquitous` | absent | THE {system} SHALL {action} |
| `event_driven` | the trigger | WHEN {condition}, THE {system} SHALL {action} |
| `state_driven` | the state | WHILE {condition}, THE {system} SHALL {action} |
| `unwanted` | the error case | IF {condition}, THEN THE {system} SHALL {action} |
| `optional` | the feature | WHERE {condition}, THE {system} SHALL {action} |

`action` is required and is what the system must do, testably. `system` is
optional and names the component that acts: on the requirement, for every
criterion of it that names none, or on a criterion, over the requirement's;
absent on both, the sentence says "system". `contract` is what the caller
observes — the return value, the exit code, the status code, the emitted
event — **required for every `unwanted` criterion** (rule C6) and expected
wherever something else consumes the result; it is also what the check
asserts. "It reports an error" is not a contract; "exit code 2 and nothing
written to stdout" is. `check` is required on every criterion
([checks.md](checks.md)).

**There are no other sections.** An edge case is a criterion, usually
`unwanted`. A correctness property is a `ubiquitous` criterion whose action
begins "for any …". Error handling is an `unwanted` criterion with a
contract. There is no `edge_cases`, no `correctness_properties`, no
`error_handling` and no `user_story`: a requirement carries an optional
one-sentence `rationale` instead. There is no `introduction`, which would
restate `## Intent`, and no `glossary`: project vocabulary a newcomer would not
know is a `## Glossary` section of the PRD (§5).

**For every requirement, consider** — and write a criterion where it
applies: empty or null input; boundary values, with numbers; the operation
failing; authorization failing; a concurrent operation. For anything that
spawns processes, loops, retries or calls a service, also: a timeout,
resource cleanup on failure, an iteration cap, and that library code returns
an error instead of terminating the process.

**Language.** Prefer a measurable constraint to an adjective. `appropriate`,
`properly`, `correctly`, `reasonable`, `relevant`, `adequate`, `suitable`, `as
needed`, `if necessary` and `etc` each make a criterion untestable, so say what
the word stands for; the validator matches no words, so nobody else will.

**Execution paths** are the end-to-end scenarios, each with at least two
`{actor, action}` steps and a `smoke` check. A `full` package has at least one;
a `lite` package has none (rule C7, §4). A path starts at a user action, a CLI
command, an API call or a scheduled trigger, ends at a concrete side effect,
and uses logical actors. Each one's smoke check is run by the integration
task the tools derive (§7), so write paths you can watch happen.

**Paths across packages.** For every package this one depends on that has
paths, at least one step of this package's paths must be played by an actor
that also plays a step in one of that package's paths — the same name,
ignoring case. A dependency with no shared actor is an **error** across
specs, and no later artifact can repair it: decide it here, with the actors
you read in §2, and name a component the other package owns as that package
names it.

**`handoffs`** is optional. Write one wherever the PRD says that another
package will call, register into or fill something this package offers:
`{id: "H-N", what, to, criteria}`, `what` the extension point or entry point,
`to` the spec id of the receiving package, which must depend on this one
directly or transitively (a package that lands earlier leaves nothing to
wire it, and `speca` refuses it), and `criteria` this package's criteria
that depend on the handoff being wired.

**`external_apis`** is optional and holds the signatures the work relies on:
`{package, version, verified, symbols: [{name, import_path, signature}]}`.
Set `verified` to `true` only for a symbol whose source you read in §2;
otherwise `false`, with the signature the PRD implies. Never write
"unverified" into `version` or a note — the flag is the record, and `af-impl`
reads it to know what to confirm before use.

Write the file, then take the verdict: `speca <NN> --validate`. With
`tasks.json` still empty you will see one expected error — `at '/tasks':
minItems: got 0, want 1`. Every other error is yours to fix now; a criterion
without a `check`, a path without `smoke` and a check that mixes its two forms
are schema errors, so they show here. Note that the cross-file rules C1–C8 do
**not** run while any file fails its schema, so this gate checks the
requirements' shape, not yet whether every `same_as` resolves (C3) or every
criterion is owned (C5).
