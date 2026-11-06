# Writing the tests first

What `af-impl` §5 step 4 does with the tests a task owns, and what the
integration task checks that no component test can.

## The tests a task owns

Each test the task owns in `test_spec.json` — `given`,
`when`, `then`, `pseudocode` — is a behaviour to cover, not a function to
write. Write it in the project's own framework, in the test file of the
production area it exercises (the file named after the handler, store,
command or function; create one only when none exists), with a name that
says the behaviour (`TestCreate_RejectsDuplicateSlug`). Test at the level
where the behaviour lives: a handler through its router, a store
function directly, a pure function directly. When several entries are
variants of one behaviour (the error cases, a status list, a caller
matrix), write one table-driven test that covers them all. When an
existing test already exercises the behaviour, extend it rather than add
another. Use the shared test helpers the survey found instead of copying
one. Do not put spec, requirement or test ids in function names, file
names or comments, and do not create a test file named after the spec,
the task or a test kind (`smoke_spec05_test.go`, `property_test.go`).
Write down, for the commit, which test covers each id the task owns
(`TS-16-3 → internal/workspace/create_test.go:TestCreate_RejectsDuplicateSlug`).

Run the new and extended tests and watch them fail **before** any
implementation file changes; keep the command and the failure, because
you report them. If the tests cannot compile until the task adds a
signature, add the minimal stub first (empty body, zero value, "not
implemented"), then see them fail on behaviour, not on a compile error.
An `unwanted` criterion's test asserts the outcome its `contract` names,
not merely "an error occurred". A `smoke` test runs its path once with
the `real_components` it lists, none of them mocked, and asserts the end
state of the path; it does not assert again what the task's other tests
assert. A `property` test is a real invariant over an input domain (a
round trip, idempotence); where a table of representative inputs proves
the same, write the table. A test that passes before the implementation
proves nothing about it: when a red first run is impossible (a pure
refactor, a docs-only change, an entry whose behaviour the code and an
existing test already deliver), say so as a test-first deviation, with
the existing test that covers it, rather than burying it in notes.

## The integration task

**The integration task** is last, and it exists to catch the wiring gaps
component tests cannot see. Trace every execution path through the real
code and confirm each step calls the next; for every criterion with a
`contract`, confirm production code consumes the result, not only a test;
search every `touches` path for stub markers (`panic("not implemented")`,
`TODO`, `NotImplementedError`, a bare `return nil` on a non-trivial path);
confirm entry points that belong to another spec are called from production
code. A path that is not live in production code fails this task, and a
recorded decision or a deferral does not satisfy it. Its smoke tests go in
the area's existing end-to-end test file where there is one, not in a file
of the spec's own.

## The traceability line in the commit

Once the code no longer carries the spec's ids, the commit body is where the
map from a test id to the test that covers it lives. Write one line per id the
task owns:

```
TS-16-3 → internal/workspace/create_test.go:TestCreate_RejectsDuplicateSlug
```

Name a table test's case after the colon where it helps. Write `title` and
`pseudocode`'s intent into the test's name and file, never the id itself.
