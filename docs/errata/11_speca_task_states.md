# Erratum: spec 11, speca: change the state of a package's tasks

Where the implementation of `.specs/11_speca_task_states` differs from the
spec, and why. The decisions are numbered as in the implementation's survey;
each is added by the task that made it.

## D-14: task 1 regenerates speca's schema golden

**Spec:** task 1 touches the files of `envelope` and `internal/cli` only, and
task 8 regenerates `cmd/speca/testdata/schema.golden.json`.

**Code:** `speca --schema` is generated from `envelope.AdminResult`, so adding
`changes` and `dry_run` to it (`envelope/admin_result.go`, `AdminResult` and
`Change`) changes the document, and `TestSchemaGolden_TS10_20` is red from that
commit on. A task does not land on a red gate, so the golden is regenerated in
task 1 (`UPDATE_GOLDEN=1`; the diff is the two properties and the `Change`
schema, 34 added lines). Task 8 adds only what the flags and the help change.

**Test of the delivered behaviour:** `TestSchemaGolden_TS10_20`
(`cmd/speca/schema_test.go`).
