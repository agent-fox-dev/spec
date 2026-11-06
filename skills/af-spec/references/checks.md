# The checks

What `af-spec` §6 writes into every criterion and every path: the check and
its fields and kinds, the smoke check, what one check per behaviour means, and
how `af-impl` turns them into the project's tests.

How each criterion is verified is written **on the criterion**, and how each
path is proved **on the path**. There is no test artifact and no test id:
format v3 took `test_spec.json` out because its tests were the criteria said
twice. A check is the test of its criterion in the three fields a test adds
that the criterion does not already say — the criterion's `condition` is the
given, its `action` the when, its `contract` the then — so write none of
those again.

```json
"check": { "kind": "unit", "setup": ["an empty catalog"],
  "pseudocode": "in http/handlers_test.go: r = post('/recipes', body); assert r.status == 201; assert count(catalog) == 1" }
```

```json
"check": { "same_as": "REQ-1.2" }
```

**A check is its own or shared, never both.** Its own: `kind`, required, one
of `unit` (one component, collaborators may be stubbed), `integration` (two or
more real components, only external I/O stubbed) or `property` (an invariant
over an input domain the pseudocode names); `setup`, optional, the
preconditions the `condition` does not state; `pseudocode`, optional but
expected, language-agnostic assertions that name the function, handler or
command the check exercises and, when the repository has a test file for it,
that file and the existing test it extends. Shared: `same_as` alone, naming a
criterion of this package whose check is its own — never one that is itself
shared, so a reader follows one link (rule C3) — or a path of this package
whose smoke check is the only check the criterion needs. The schema refuses a
criterion without a check and a check that mixes the two forms.

**The smoke check** is on every path and proves its wiring: it runs the path
once with real components and asserts the end state, without asserting again
what the checks verify.

```json
"smoke": { "real_components": ["HTTP handler", "recipe catalog", "database"],
  "setup": ["the service is running against a real database"],
  "end_state": ["the fetch returns the recipe that was posted"],
  "pseudocode": "extends the end-to-end test in http/server_test.go: id = post('/recipes', rec).json.id; assert get('/recipes/' + id).json == rec" }
```

`real_components` is required and non-empty: what must not be mocked.
`setup` is optional. `end_state` is required and non-empty: the side effects
at the end of the path. `pseudocode` is optional, and names the existing
end-to-end test it extends when there is one. The integration task the tools
derive runs every smoke check (§7), and a `lite` package has no path and so
no smoke check.

**One check per behaviour.** A behaviour is one thing a caller can observe: a
request answered with a status and a body, a store that refuses a duplicate,
a command that exits with a code and writes a file.

- **Variants are a table.** Criteria whose checks differ only in the input
  share one check over a table of inputs: the first carries it and names the
  cases in `pseudocode`, the others say `same_as` it.
- A check whose assertions only restate another's is a duplicate: make the
  criterion `same_as` the other.
- For every `unwanted` criterion, the check asserts the caller-observable
  outcome its `contract` names, not that "an error occurred".
- A `property` check is only for an invariant a table of representative
  inputs cannot establish — a round trip, idempotence, an algebraic law. A
  "for any …" criterion is otherwise a `unit` check over such a table, its
  boundaries among them.
- A criterion a path's smoke check establishes on its own, and nothing else
  needs to, is `same_as` that path.
- **No artifact checks.** Nothing checks that a struct has a field, a
  constant has a value, a document mentions something, an import graph holds
  or a helper exists.
- **Look at the tests the repository already has** (§2) for the code each
  check exercises. Where one already verifies the behaviour, the criterion
  keeps its check — it still needs one — and `pseudocode` names that test's
  file and name, so `af-impl` extends it instead of writing the behaviour
  again under a new name.

The ids stay in this file. `af-impl` writes each check into the test file of
the code it exercises, named for the behaviour it checks
(`TestCreate_RejectsDuplicateSlug`), never for the criterion or the package,
and records which test covers which criterion in the commit. Write
`pseudocode` so that is possible: say the behaviour, name the file, never the
id.
