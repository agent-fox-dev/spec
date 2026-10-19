# Examples

Three small programs that use the [`afspec`](../afspec/README.md) library on
a spec package. Each one is a `main` package you can run with `go run` from
the repository root, and each has a test, so `make test` keeps them working.

| Example | What it shows |
| --- | --- |
| [`create/`](create/main.go) | How to create a spec: start from `CreateSpec`, add requirements with the EARS criterion builders, an execution path, tests and tasks with the `Add…` and `Next…ID` functions, then `Validate`, `Save` and `Transition` to `active` |
| [`validate/`](validate/main.go) | How to read and validate a spec: `LoadSpec`, `Validate` (the schemas, then the cross-file rules C1 to C11), the errors and warnings, and the derived coverage and traceability |
| [`tasks/`](tasks/main.go) | How to work through a spec's plan: pick the next task that is ready, move it to `in_progress` with `TransitionTask`, render the package scoped to it with `RenderIndividualScoped`, and after the work record it as `done` in `tasks.json` with `Save` |

## Running them

The three fit together: create a package, check it, then work through its
two tasks.

```bash
dir=$(go run ./examples/create)        # writes 01_greet_command under a new temporary directory
go run ./examples/validate "$dir"      # valid; coverage 4 of 4; the traceability matrix
go run ./examples/tasks "$dir"         # task 1 → in_progress, prints its Markdown, task 1 → done
go run ./examples/tasks "$dir"         # task 2, the integration task, the same way
go run ./examples/tasks "$dir"         # "every task is done or dropped"
```

`create -root DIR` writes the package under `DIR` instead, e.g. `.specs`.
`validate` and `tasks` take any package directory, so they also work on the
packages under `.specs/` or on the fixtures in `afspec/testdata/` (copy a
fixture first before running `tasks` on it: it writes `tasks.json`).

`tasks -task N DIR` works on task `N` instead of the next one that is ready.
The scoped Markdown goes to stdout and the state changes to stderr. Where the
example only prints the Markdown, a real agent would hand it to a model, make
the task's tests pass, and release the task back to `pending` if the work
fails, which the example also shows.
