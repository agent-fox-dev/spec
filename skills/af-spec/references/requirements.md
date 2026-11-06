# requirements.json

What `af-spec` §6 writes: the example, the ids, the EARS patterns and their
fields, contracts, the glossary, the execution paths and `external_apis`.

What the system must do: one flat list of criteria per requirement, plus the
end-to-end execution paths. Everything comes from the PRD; nothing comes from
your view of how it should be built.

```json
{
  "$schema": "https://agent-fox.dev/schemas/requirements.v2.json",
  "spec_id": "07",
  "spec_name": "recipe_catalog",
  "schema_version": 2,
  "introduction": "The recipe catalog stores user-created recipes and their ingredients.",
  "glossary": { "recipe": "A named collection of ingredients and preparation steps in the catalog." },
  "requirements": [
    {
      "id": "07-REQ-1",
      "title": "Recipe creation",
      "rationale": "A cook who cannot save a recipe has no reason to open the app twice.",
      "criteria": [
        { "id": "07-REQ-1.1", "pattern": "event_driven",
          "condition": "a client submits POST /recipes with a body carrying a name and at least one ingredient",
          "system": "recipe catalog",
          "action": "persist the recipe and return its assigned identifier",
          "contract": "HTTP 201 with body {id: string, name: string}" },
        { "id": "07-REQ-1.2", "pattern": "unwanted",
          "condition": "the submitted body has no name field",
          "system": "recipe catalog",
          "action": "reject the request and persist nothing",
          "contract": "HTTP 400 with body {error: string}; the catalog row count is unchanged" },
        { "id": "07-REQ-1.3", "pattern": "ubiquitous",
          "system": "recipe catalog",
          "action": "for any persisted recipe, return the same identifier for every subsequent read of it" }
      ]
    }
  ],
  "execution_paths": [
    {
      "id": "07-PATH-1",
      "title": "A cook saves a recipe and reads it back",
      "steps": [
        { "actor": "cook", "action": "submits POST /recipes with a recipe body" },
        { "actor": "recipe catalog", "action": "validates the body and writes a row" },
        { "actor": "recipe catalog", "action": "returns 201 with the assigned identifier" },
        { "actor": "cook", "action": "reads GET /recipes/{id} and receives the saved recipe" }
      ]
    }
  ]
}
```

The example is the shape `spec`'s own prompt carries; the domain is yours.

**Ids.** `{spec_id}-REQ-{N}` for a requirement, `{spec_id}-REQ-{N}.{C}` for a
criterion with C restarting at 1 inside each requirement,
`{spec_id}-PATH-{N}` for a path. N and C are sequential from 1. Every id
carries this package's `spec_id` and is unique in its family (rule C2).

**The criterion is one EARS sentence in fields.** Choose the pattern first,
then fill only the fields it allows — the schema enforces the combination,
and it reports a wrong one as `'not' failed` on that criterion, which means
`condition` or `guard` is present where the pattern forbids it or absent
where it requires it.

| `pattern` | `condition` | `guard` | Rendered sentence |
|---|---|---|---|
| `ubiquitous` | absent | absent | THE {system} SHALL {action} |
| `event_driven` | the trigger | absent | WHEN {condition}, THE {system} SHALL {action} |
| `complex_event` | the trigger | required | WHEN {condition} AND {guard}, THE {system} SHALL {action} |
| `state_driven` | the state | absent | WHILE {condition}, THE {system} SHALL {action} |
| `unwanted` | the error case | absent | IF {condition}, THEN THE {system} SHALL {action} |
| `optional` | the feature | absent | WHERE {condition}, THE {system} SHALL {action} |

`action` is required and is what the system must do, testably. `system` is
optional and names the component that acts. `contract` is what the caller
observes — the return value, the exit code, the status code, the emitted
event — **required for every `unwanted` criterion** (rule C10) and expected
wherever something else consumes the result. "It reports an error" is not a
contract; "exit code 2 and nothing written to stdout" is.

**There are no other sections.** An edge case is a criterion, usually
`unwanted` or `complex_event`. A correctness property is a `ubiquitous`
criterion whose action begins "for any …". Error handling is an `unwanted`
criterion with a contract. There is no `edge_cases`, no
`correctness_properties`, no `error_handling` and no `user_story`: a
requirement carries an optional one-sentence `rationale` instead.

**For every requirement, consider** — and write a criterion where it
applies: empty or null input; boundary values, with numbers; the operation
failing; authorization failing; a concurrent operation. For anything that
spawns processes, loops, retries or calls a service, also: a timeout,
resource cleanup on failure, an iteration cap, and that library code returns
an error instead of terminating the process.

**Language.** Prefer a measurable constraint to an adjective. The validator
warns on `appropriate`, `properly`, `correctly`, `reasonable`, `relevant`,
`adequate`, `suitable`, `as needed`, `if necessary` and `etc` — each of them
makes a criterion untestable, so say what the word stands for.

**The glossary is an object**, term → definition, for project-specific
vocabulary a newcomer would not know. Not every identifier: it is
documentation, and the validator only warns about a backticked term used in
three or more criteria with no entry. A term an existing package defines
keeps that definition.

**Execution paths** are the end-to-end scenarios, at least one, each with at
least two `{actor, action}` steps. A path starts at a user action, a CLI
command, an API call or a scheduled trigger, ends at a concrete side effect,
and uses logical actors. Each one becomes a smoke test in §7 and work in the
integration task in §8, so write paths you can watch happen.

**Paths across packages.** For every package this one depends on, at least
one step of this package's paths must be played by an actor that also plays a
step in one of that package's paths — the same name, ignoring case. A
dependency with no shared actor is an **error** across specs, and no later
artifact can repair it: decide it here, with the actors you read in §2, and
name a component the other package owns as that package names it.

**`external_apis`** is optional and holds the signatures the work relies on:
`{package, version, verified, symbols: [{name, import_path, signature}]}`.
Set `verified` to `true` only for a symbol whose source you read in §2;
otherwise `false`, with the signature the PRD implies. Never write
"unverified" into `version` or a note — the flag is the record, and `af-impl`
reads it to know what to confirm before use.

Write the file, then take the verdict: `speca <NN> --validate`. With
`test_spec.json` and `tasks.json` still empty you will see two expected
errors — `at '/tests': minItems: got 0, want 1` and `at '/tasks': minItems:
got 0, want 2`. Every other error is yours to fix now. Note that the
cross-file rules C1–C11 do **not** run while any file fails its schema, so
this gate checks the requirements' shape, not yet their coverage.
