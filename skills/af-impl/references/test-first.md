# Writing the tests first

What `af-impl` §5 step 4 does with the checks of the criteria a task owns,
and what the integration task writes and checks that no component test can.

## The checks a task owns

Each criterion the task owns carries its check in `requirements.json`:
`kind`, `setup` and `pseudocode`, with the criterion's `condition` as the
given, its `action` as the when and its `contract` as the then — or
`same_as`, naming the criterion whose own check covers it too, or the path
whose smoke check does. A check is a behaviour to cover, not a function to
write. Write it in the project's own framework, in the test file of the
production area it exercises (the file named after the handler, store,
command or function; create one only when none exists), with a name that
says the behaviour (`TestCreate_RejectsDuplicateSlug`). Test at the level
where the behaviour lives: a handler through its router, a store
function directly, a pure function directly. When several criteria share
one check (the error cases, a status list, a caller matrix), write one
table-driven test that covers them all. When an existing test already
exercises the behaviour — the check's `pseudocode` names it where the
spec's author found one — extend it rather than add another. Use the
shared test helpers the survey found instead of copying one. Do not put
spec, requirement, criterion or path ids in function names, file names or
comments, and do not create a test file named after the spec, the task or
a check kind (`smoke_spec05_test.go`, `property_test.go`). Write down,
for the commit, which test covers each criterion the task owns
(`REQ-3.2 → internal/workspace/create_test.go:TestCreate_RejectsDuplicateSlug`),
a criterion with `same_as` mapped to the test of the check it shares; one
that is `same_as` a path is covered by that path's smoke test, which the
integration task writes, so name the path.

Run the new and extended tests and watch them fail **before** any
implementation file changes; keep the command and the failure, because
you report them. If the tests cannot compile until the task adds a
signature, add the minimal stub first (empty body, zero value, "not
implemented"), then see them fail on behaviour, not on a compile error.
An `unwanted` criterion's test asserts the outcome its `contract` names,
not merely "an error occurred". A `unit` check stubs collaborators at
most; an `integration` check runs two or more real components and stubs
only external I/O. A `property` check is a real invariant over an input
domain (a round trip, idempotence); where a table of representative
inputs proves the same, write the table. A test that passes before the
implementation proves nothing about it: when a red first run is
impossible (a pure refactor, a docs-only change, a check whose behaviour
the code and an existing test already deliver), say so as a test-first
deviation, with the existing test that covers it, rather than burying it
in notes.

## The integration task

**The integration task** is last: task N+1, derived by the tools from the
package and in no file, which `specr <package> --task N+1` renders with
every path's smoke check and a `## Contracts` section. It exists to catch
the wiring gaps component tests cannot see. Write each path's smoke check
as a test that runs the path once with the `real_components` it lists,
none of them mocked, and asserts its `end_state`, without asserting again
what the tasks' tests assert; it goes in the area's existing end-to-end
test file where there is one (the `pseudocode` names it), not in a file of
the spec's own. Then trace every execution path through the real code and
confirm each step calls the next; for every criterion under `## Contracts`,
confirm production code consumes the result, not only a test; search every
`touches` path for stub markers (`panic("not implemented")`, `TODO`,
`NotImplementedError`, a bare `return nil` on a non-trivial path); confirm
entry points that belong to another spec are called from production code.
A path that is not live in production code fails this task, and a recorded
decision or a deferral does not satisfy it.

## The traceability line in the commit

Once the code no longer carries the spec's ids, the commit body is where the
map from a criterion to the test that covers it lives. Write one line per
criterion the task owns, and in the integration task one per path:

```
REQ-3.2 → internal/workspace/create_test.go:TestCreate_RejectsDuplicateSlug
PATH-1 → cmd/search/main_test.go:TestSearch_IndexesAndFinds
```

Name a table test's case after the colon where it helps. Write the intent
of the check's `pseudocode` into the test's name and file, never the id
itself.
